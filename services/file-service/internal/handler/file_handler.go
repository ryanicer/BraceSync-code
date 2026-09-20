// Package handler — file-service HTTP 层（T022）
//
// 身份注入对齐 gateway（架构 §5.2）：网关 JWT 校验通过后以 HTTP 头注入
// X-User-Id / X-Role（gateway/cmd/server/middleware.go jwtAuth），本服务
// 只读头不读 gin context；缺失即 401（内网信任链，fail-closed）。
// 响应统一 {code, message, data}（架构 §3.5，文件域错误码段 6xxxx）。
package handler

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
	"github.com/bracesync/bracesync/services/file-service/internal/repo"
	"github.com/bracesync/bracesync/services/file-service/internal/service"
)

// 网关身份注入头（gateway jwtAuth 验签后写入，外部同名头已被网关剥离）
const (
	headerUserID = "X-User-Id"
	headerRole   = "X-Role"
)

// FileHandler 文件端点处理器
type FileHandler struct {
	presigner *service.Presigner
	store     repo.Store
}

// NewFileHandler 构造 FileHandler
func NewFileHandler(presigner *service.Presigner, store repo.Store) *FileHandler {
	return &FileHandler{presigner: presigner, store: store}
}

// Router 构建 file-service 路由（含 healthz）
func (h *FileHandler) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler())) // T234 Prometheus 采集端点

	api := r.Group("/api/v1/files")
	{
		api.POST("/presign", h.handlePresignURL)
		api.POST("/upload-complete", h.handleUploadComplete)
		api.GET("/query", h.queryFiles)
		api.GET("/:fileID/download", h.handleDownloadURL) // T130 下载预签名 URL
		api.GET("/:fileID", h.getFileByID)
	}
	return r
}

// identity 从网关注入头提取调用者身份；缺失视为未鉴权（401）
func identity(c *gin.Context) (userID, role string, ok bool) {
	userID = c.GetHeader(headerUserID)
	role = c.GetHeader(headerRole)
	return userID, role, userID != ""
}

// staffRoles 内部 staff 角色集合（与 gateway staffRoles 对齐；不含 patient）
var staffRoles = map[string]bool{
	"admin":       true,
	"ROLE_ADMIN":  true,
	"ROLE_DOCTOR": true,
	"ROLE_CS":     true,
	"technician":  true,
}

// isStaffRole 判断角色是否属于内部 staff
func isStaffRole(role string) bool { return staffRoles[role] }

// canAccessFile 文件归属校验（T261 身份单一来源）：
//   - staff 放行（跨 owner 访问，如医生看患者复查报告、admin 管模板）
//   - 非 staff（patient）仅可访问 owner_type=="patient" 且 owner_id==本人 的文件
func canAccessFile(role, userID string, fm *model.FileMetadata) bool {
	if isStaffRole(role) {
		return true
	}
	return fm.OwnerType == "patient" && fm.OwnerID == userID
}

// presignRequest 签发请求体
type presignRequest struct {
	FileType    string `json:"file_type" binding:"required"`
	OwnerType   string `json:"owner_type" binding:"required"`
	OwnerID     string `json:"owner_id" binding:"required"`
	ContentType string `json:"content_type"`
	FileName    string `json:"file_name"`   // T130 增补单：原始文件名（含扩展名）
	FileHeader  string `json:"file_header"` // T130 增补单：文件头魔数指纹（base64，前 8 字节）
}

// handlePresignURL 签发 COS PUT 预签名 URL（短时效 10 分钟）
// POST /api/v1/files/presign
func (h *FileHandler) handlePresignURL(c *gin.Context) {
	userID, role, ok := identity(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "user identity missing")
		return
	}

	var req presignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errorJSON(c, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid request body")
		return
	}

	fileType := model.FileType(req.FileType)
	// 参数合法性先于授权（非法类型对任何角色都是 400，避免角色探测）
	if !model.ValidFileType(fileType) {
		errorJSON(c, http.StatusBadRequest, ErrorCodeInvalidRequest, "unsupported file_type")
		return
	}
	// T130 增补单：复查报告三道校验（扩展名白名单 + MIME + 魔数指纹）
	var fileHeader []byte
	if fileType == model.FileTypeReviewReport {
		if req.FileHeader != "" {
			decoded, err := base64.StdEncoding.DecodeString(req.FileHeader)
			if err != nil {
				errorJSON(c, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid file_header encoding (expect base64)")
				return
			}
			fileHeader = decoded
		}
		if err := service.ValidateReviewReportFile(req.FileName, req.ContentType, fileHeader); err != nil {
			errorJSON(c, http.StatusBadRequest, ErrorCodeInvalidRequest, err.Error())
			return
		}
	}
	// 端点级授权：角色 × 文件类型矩阵（service.Authorize，任务需求 4）
	if err := service.Authorize(role, fileType); err != nil {
		errorJSON(c, http.StatusForbidden, ErrorCodeForbidden, "role not allowed for this file type")
		return
	}

	// T261 身份单一来源：owner 由服务端决定——
	//   非 staff（patient）强制 owner_type=patient / owner_id=本人，忽略请求体 owner 字段；
	//   staff 保留请求体 owner（医生上传患者复查报告指定 owner_id=患者ID 等跨 owner 场景）。
	if !isStaffRole(role) {
		req.OwnerType = "patient"
		req.OwnerID = userID
	}

	resp, err := h.presigner.GenerateUploadURL(c.Request.Context(), service.UploadRequest{
		FileType:    fileType,
		OwnerType:   req.OwnerType,
		OwnerID:     req.OwnerID,
		ContentType: req.ContentType,
		FileName:    req.FileName,
		FileHeader:  fileHeader,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidRequest):
			errorJSON(c, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid presign request")
		default:
			log.Error().Err(err).Str("user_id", userID).Msg("generate presigned URL failed")
			errorJSON(c, http.StatusInternalServerError, ErrorCodePresignFailed, "failed to generate presigned url")
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"file_id":            resp.FileID,
			"object_key":         resp.ObjectKey,
			"signature_url":      resp.SignatureURL,
			"expires_at":         resp.ExpiresAt.UTC().Format(time.RFC3339),
			"expires_in_seconds": int(time.Until(resp.ExpiresAt).Seconds()),
		},
	})
}

// uploadCompleteRequest 上传完成回调请求体
type uploadCompleteRequest struct {
	FileID    string `json:"file_id" binding:"required"`
	Size      int64  `json:"size"`
	PublicURL string `json:"public_url"`
}

// handleUploadComplete 客户端直传 COS 成功后的登记闭环（需求 2）：
// pending → uploaded + uploaded_at + size + url，幂等不重复。
// POST /api/v1/files/upload-complete
func (h *FileHandler) handleUploadComplete(c *gin.Context) {
	userID, role, ok := identity(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "user identity missing")
		return
	}

	var req uploadCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errorJSON(c, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid request body")
		return
	}

	// T261 归属校验：先取文件元数据，非 staff 须本人所有方可确认上传
	fm, err := h.store.GetFileByFileID(c.Request.Context(), req.FileID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			errorJSON(c, http.StatusNotFound, ErrorCodeFileNotFound, "file not found")
			return
		}
		log.Error().Err(err).Str("file_id", req.FileID).Msg("get file for ownership check failed")
		errorJSON(c, http.StatusInternalServerError, ErrorCodeInternal, "error retrieving file")
		return
	}
	if !canAccessFile(role, userID, fm) {
		log.Warn().Str("user_id", userID).Str("role", role).Str("file_id", req.FileID).
			Str("owner_type", fm.OwnerType).Str("owner_id", fm.OwnerID).
			Msg("upload-complete ownership denied")
		errorJSON(c, http.StatusForbidden, ErrorCodeForbidden, "not allowed to complete upload for this file")
		return
	}

	err = h.presigner.OnUploadComplete(c.Request.Context(), req.FileID, req.PublicURL, req.Size)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFileNotFound):
			errorJSON(c, http.StatusNotFound, ErrorCodeFileNotFound, "file not found")
		case errors.Is(err, service.ErrFileTooLarge):
			errorJSON(c, http.StatusBadRequest, ErrorCodeInvalidRequest, err.Error())
		case errors.Is(err, service.ErrInvalidRequest):
			errorJSON(c, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid upload-complete request")
		default:
			log.Error().Err(err).Str("file_id", req.FileID).Msg("upload-complete registration failed")
			errorJSON(c, http.StatusInternalServerError, ErrorCodeUploadFailed, "failed to register upload")
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"file_id": req.FileID}})
}

// getFileByID 按 ID 查询文件元数据
// GET /api/v1/files/:fileID
func (h *FileHandler) getFileByID(c *gin.Context) {
	userID, role, ok := identity(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "user identity missing")
		return
	}

	fileID := c.Param("fileID")
	fm, err := h.store.GetFileByFileID(c.Request.Context(), fileID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			errorJSON(c, http.StatusNotFound, ErrorCodeFileNotFound, "file not found")
			return
		}
		log.Error().Err(err).Str("file_id", fileID).Msg("get file metadata failed")
		errorJSON(c, http.StatusInternalServerError, ErrorCodeInternal, "error retrieving file")
		return
	}

	// T261 归属校验：非 staff 仅可访问本人文件
	if !canAccessFile(role, userID, fm) {
		log.Warn().Str("user_id", userID).Str("role", role).Str("file_id", fileID).
			Str("owner_type", fm.OwnerType).Str("owner_id", fm.OwnerID).
			Msg("getFileByID ownership denied")
		errorJSON(c, http.StatusForbidden, ErrorCodeForbidden, "not allowed to access this file")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": fm})
}

// handleDownloadURL 签发文件下载预签名 URL（T130 复查报告下载）
// GET /api/v1/files/:fileID/download
func (h *FileHandler) handleDownloadURL(c *gin.Context) {
	userID, role, ok := identity(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "user identity missing")
		return
	}

	fileID := c.Param("fileID")
	// T261 归属校验：先取文件元数据，非 staff 须本人所有方可下载
	fm, err := h.store.GetFileByFileID(c.Request.Context(), fileID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			errorJSON(c, http.StatusNotFound, ErrorCodeFileNotFound, "file not found")
			return
		}
		log.Error().Err(err).Str("file_id", fileID).Msg("get file for download ownership check failed")
		errorJSON(c, http.StatusInternalServerError, ErrorCodeInternal, "error retrieving file")
		return
	}
	if !canAccessFile(role, userID, fm) {
		log.Warn().Str("user_id", userID).Str("role", role).Str("file_id", fileID).
			Str("owner_type", fm.OwnerType).Str("owner_id", fm.OwnerID).
			Msg("download ownership denied")
		errorJSON(c, http.StatusForbidden, ErrorCodeForbidden, "not allowed to download this file")
		return
	}

	resp, err := h.presigner.GenerateDownloadURL(c.Request.Context(), fileID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFileNotFound):
			errorJSON(c, http.StatusNotFound, ErrorCodeFileNotFound, "file not found or not uploaded")
		case errors.Is(err, service.ErrInvalidRequest):
			errorJSON(c, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid file id")
		default:
			log.Error().Err(err).Str("file_id", fileID).Msg("generate download url failed")
			errorJSON(c, http.StatusInternalServerError, ErrorCodePresignFailed, "failed to generate download url")
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"file_id":      resp.FileID,
			"download_url": resp.URL,
			"expires_at":   resp.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})
}

// queryFiles 按 owner/type/status 过滤分页查询（total 为过滤后总数）
// GET /api/v1/files/query?owner_type=&owner_id=&file_type=&status=&page=&pageSize=
func (h *FileHandler) queryFiles(c *gin.Context) {
	userID, role, ok := identity(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "user identity missing")
		return
	}

	filters := repo.QueryFilter{
		OwnerType: c.Query("owner_type"),
		OwnerID:   c.Query("owner_id"),
		FileType:  model.FileType(c.Query("file_type")),
		Status:    model.FileStatus(c.Query("status")),
		Page:      intParam(c, "page", 1),
		PageSize:  intParam(c, "pageSize", 20),
	}

	// T261 身份单一来源：非 staff 查询强制 owner=本人，防止越权枚举他人文件
	if !isStaffRole(role) {
		filters.OwnerType = "patient"
		filters.OwnerID = userID
	}

	ctx := c.Request.Context()
	files, err := h.store.QueryFiles(ctx, filters)
	if err != nil {
		log.Error().Err(err).Msg("query files failed")
		errorJSON(c, http.StatusInternalServerError, ErrorCodeInternal, "error querying files")
		return
	}
	// total 取过滤条件下全量计数（分页计数口径，非当前页条数）
	total, err := h.store.CountFiles(ctx, filters)
	if err != nil {
		log.Error().Err(err).Msg("count files failed")
		errorJSON(c, http.StatusInternalServerError, ErrorCodeInternal, "error counting files")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data": gin.H{
			"list":     files,
			"total":    total,
			"page":     filters.Page,
			"pageSize": filters.PageSize,
		},
	})
}

// intParam 解析整型查询参数（非法/缺失回默认值，上限防滥用）
func intParam(c *gin.Context, key string, def int) int {
	raw := c.Query(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
}
