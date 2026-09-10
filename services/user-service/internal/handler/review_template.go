// Package handler — T135 复查报告模板管理端点（合同运营后台子系统「复查报告模板管理」）
//
// 路由（网关 RBAC 限 admin + doctor）：
//
//	POST /api/v1/admin/review-templates                  上传/创建模板（版本 v1；重名 409）
//	POST /api/v1/admin/review-templates/:groupId/replace 版本替换（新版本 active，旧版 retired）
//	GET  /api/v1/admin/review-templates                  模板列表（每模板组当前 active 版本 + 下载 URL）
//	GET  /api/v1/admin/review-templates/:groupId/download 模板下载预签名 URL
//
// 设计要点（对齐 PRD §7A.11.y + R1-c 读法 A）：
//   - 文件本体走 file-service review_report 预签名通道（owner_type=ReviewTemplate，
//     R4-a 白名单 9 类 + R4-b 20MB，三道校验均在 file-service presign/upload-complete 强制）。
//   - 本文件端点只登记「模板文件引用元数据」，【不】据模板自动生成/渲染报告。
//   - uploaded_by 一律取登录凭证 X-User-Id（operatorID），不接受前端传，防伪造上传人。
//   - 版本替换语义：同模板组 version 递增，旧版标记 retired（非物理删除）；
//     已上传的填写报告(review_records)引用各自 file_id，与模板文件互不影响。
package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// templateGroupIDPrefix 模板组在 URL 中的参数名
const templateGroupIDParam = "groupId"

// buildReviewTemplateDTO 装配模板 DTO；fileSvc 不可用时下载 URL/文件元数据为空（降级）。
func (h *Handler) buildReviewTemplateDTO(ctx context.Context, row *repo.ReviewTemplateRow) model.ReviewTemplateDTO {
	dto := model.ReviewTemplateDTO{
		TemplateID: row.TemplateID,
		GroupID:    row.TemplateGroupID,
		Name:       row.Name,
		Version:    row.Version,
		FileID:     row.FileID,
		Status:     row.Status,
		UploadedBy: row.UploadedBy,
		UploadedAt: row.CreatedAt.In(time.Local).Format("2006-01-02"),
		UpdatedAt:  row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if h.fileSvc != nil && row.FileID != "" {
		h.fillTemplateFileInfo(ctx, &dto, row.FileID)
	}
	return dto
}

// fillTemplateFileInfo 从 file-service 拉取模板文件元数据 + 下载预签名 URL（失败静默降级）
func (h *Handler) fillTemplateFileInfo(ctx context.Context, dto *model.ReviewTemplateDTO, fileID string) {
	fm, err := h.fileSvc.GetFileByID(ctx, fileID)
	if err != nil || fm == nil {
		return
	}
	dto.ContentType = &fm.ContentType
	dto.FileSize = &fm.Size
	dto.FileName = &fm.ObjectKey
	if dlURL, err := h.fileSvc.GetDownloadURL(ctx, fileID); err == nil && dlURL != "" {
		dto.DownloadURL = &dlURL
	}
}

// templateAllowed 端点级兜底：仅 admin 或 doctor 可管理/下载模板（网关 RBAC 已拦截）
func (h *Handler) templateAllowed(c *gin.Context) bool {
	role := c.GetHeader(headerRole)
	return role == roleAdmin || role == "ROLE_DOCTOR"
}

// createReviewTemplate POST /api/v1/admin/review-templates —— 上传/创建模板（版本 v1）
func (h *Handler) createReviewTemplate(c *gin.Context) {
	if !h.templateAllowed(c) {
		fail(c, model.ErrForbidden("only admin or doctor can manage review templates"))
		return
	}
	var req model.CreateReviewTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		fail(c, model.ErrInvalidParam("name is required"))
		return
	}
	if len([]rune(name)) > 128 {
		fail(c, model.ErrInvalidParam("name exceeds 128 chars"))
		return
	}
	if strings.TrimSpace(req.FileID) == "" {
		fail(c, model.ErrInvalidParam("fileId is required"))
		return
	}

	// uploaded_by 从登录凭证取，不接受前端传（防伪造上传人）
	row, err := h.store.CreateReviewTemplateVersion(
		c.Request.Context(), "", name, strings.TrimSpace(req.FileID), operatorID(c, "ops"))
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrTemplateNameExists):
			fail(c, model.ErrConflict("template name already exists"))
		default:
			fail(c, model.ErrInternal("create review template failed: %v", err))
		}
		return
	}
	ok(c, h.buildReviewTemplateDTO(c.Request.Context(), row))
}

// replaceReviewTemplate POST /api/v1/admin/review-templates/:groupId/replace —— 版本替换
// 确认后生效：新版本 active、旧版 retired（非删除）、已填报告不受影响（各自引用 file_id）。
func (h *Handler) replaceReviewTemplate(c *gin.Context) {
	if !h.templateAllowed(c) {
		fail(c, model.ErrForbidden("only admin or doctor can manage review templates"))
		return
	}
	groupID := c.Param(templateGroupIDParam)
	if groupID == "" {
		fail(c, model.ErrInvalidParam("groupId is required"))
		return
	}
	var req model.ReplaceReviewTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if strings.TrimSpace(req.FileID) == "" {
		fail(c, model.ErrInvalidParam("fileId is required"))
		return
	}

	// 取旧 active 版本以沿用其组名
	existing, err := h.store.GetReviewTemplateGroup(c.Request.Context(), groupID)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrTemplateNotFound):
			fail(c, model.ErrNotFound("review template not found: %s", groupID))
		default:
			fail(c, model.ErrInternal("get review template failed: %v", err))
		}
		return
	}

	row, err := h.store.CreateReviewTemplateVersion(
		c.Request.Context(), existing.TemplateGroupID, existing.Name,
		strings.TrimSpace(req.FileID), operatorID(c, "ops"))
	if err != nil {
		fail(c, model.ErrInternal("replace review template failed: %v", err))
		return
	}
	ok(c, h.buildReviewTemplateDTO(c.Request.Context(), row))
}

// listReviewTemplates GET /api/v1/admin/review-templates —— 模板列表（每模板组当前 active 版本）
func (h *Handler) listReviewTemplates(c *gin.Context) {
	if !h.templateAllowed(c) {
		fail(c, model.ErrForbidden("only admin or doctor can manage review templates"))
		return
	}
	rows, err := h.store.ListActiveReviewTemplates(c.Request.Context())
	if err != nil {
		fail(c, model.ErrInternal("list review templates failed: %v", err))
		return
	}
	list := make([]model.ReviewTemplateDTO, 0, len(rows))
	for i := range rows {
		list = append(list, h.buildReviewTemplateDTO(c.Request.Context(), &rows[i]))
	}
	ok(c, list)
}

// downloadReviewTemplate GET /api/v1/admin/review-templates/:groupId/download —— 模板下载
// 返回组当前 active 版本文件下载预签名 URL（fileSvc 不可用时降级为无 URL）。
func (h *Handler) downloadReviewTemplate(c *gin.Context) {
	if !h.templateAllowed(c) {
		fail(c, model.ErrForbidden("only admin or doctor can manage review templates"))
		return
	}
	groupID := c.Param(templateGroupIDParam)
	if groupID == "" {
		fail(c, model.ErrInvalidParam("groupId is required"))
		return
	}
	row, err := h.store.GetReviewTemplateGroup(c.Request.Context(), groupID)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrTemplateNotFound):
			fail(c, model.ErrNotFound("review template not found: %s", groupID))
		default:
			fail(c, model.ErrInternal("get review template failed: %v", err))
		}
		return
	}
	dto := h.buildReviewTemplateDTO(c.Request.Context(), row)
	if dto.DownloadURL == nil {
		fail(c, model.ErrInternal("download url unavailable (file-service not configured)"))
		return
	}
	ok(c, gin.H{"downloadUrl": *dto.DownloadURL})
}
