// Package service — COS 预签名签发与文件元数据登记（T022，架构 §3.3/§8 ADR-11）
//
// 业务链路：presign 签发（登记 pending）→ 客户端直传 COS → upload-complete 落库闭环。
// 文件字节流不经本服务；COS 外部依赖经 storage.StorageClient 接口打桩，CI 离线可跑。
package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
	"github.com/bracesync/bracesync/services/file-service/internal/repo"
	"github.com/bracesync/bracesync/services/file-service/internal/storage"
)

// 业务错误（handler 层据此映射 400/403/404 与错误码段 6xxxx）
var (
	ErrInvalidRequest = errors.New("invalid request parameters")
	ErrForbidden      = errors.New("role not allowed for this file type")
	ErrFileNotFound   = errors.New("file not found")
)

// presignExpires 预签名 URL 有效期：短时效 10 分钟（任务要求 5–15 分钟区间），
// 防长期有效 URL 泄露；单次 PUT 语义由 COS 预签名本身保证。
const presignExpires = 10 * time.Minute

// Presigner 预签名签发 + 元数据登记服务
type Presigner struct {
	cosClient     storage.StorageClient
	store         repo.Store
	defaultBucket string
	defaultRegion string
	now           func() time.Time // 测试可注入时刻
}

// NewPresigner 构造 Presigner（cosClient 可为 mock，store 用于登记/闭环落库）
func NewPresigner(cosClient storage.StorageClient, store repo.Store, defaultBucket, defaultRegion string) *Presigner {
	return &Presigner{
		cosClient:     cosClient,
		store:         store,
		defaultBucket: defaultBucket,
		defaultRegion: defaultRegion,
		now:           time.Now,
	}
}

// UploadRequest 签发请求参数
type UploadRequest struct {
	FileType    model.FileType
	OwnerType   string
	OwnerID     string
	ContentType string
	FileName    string // T130 增补单：原始文件名（含扩展名），用于扩展名白名单校验
	FileHeader  []byte // T130 增补单：文件头前 N 字节（魔数指纹），用于文件内容校验
}

// UploadResponse 签发响应（含预签名 URL 与登记的元数据）
type UploadResponse struct {
	FileID       string
	ObjectKey    string
	SignatureURL string
	ExpiresAt    time.Time
	Metadata     *model.FileMetadata
}

// validFileType 委托 model.ValidFileType（handler 参数校验与签发共用同一口径）
func validFileType(ft model.FileType) bool { return model.ValidFileType(ft) }

// roleFileTypeMatrix 角色 × 文件类型权限矩阵（任务需求 4，执行人设计）：
//   - admin（ROLE_ADMIN/ROLE_DOCTOR/ROLE_CS）：全类型（沟通图/日志图/运维留档）
//   - technician：电子签名图 + 安装照片（安装流程产物）
//   - patient：沟通图片 + 矫形日志图片（患者端产物）
//   - review_report（T130）：仅 admin / ROLE_ADMIN / ROLE_DOCTOR 可上传
//
// 未知角色（含空）一律拒绝（fail-closed）。
var roleFileTypeMatrix = map[string]map[model.FileType]bool{
	"admin": {
		model.FileTypeSignature:    true,
		model.FileTypeInstallPhoto: true,
		model.FileTypeCommPhoto:    true,
		model.FileTypeLogPhoto:     true,
		model.FileTypeReviewReport: true,
	},
	"ROLE_ADMIN": {
		model.FileTypeSignature:    true,
		model.FileTypeInstallPhoto: true,
		model.FileTypeCommPhoto:    true,
		model.FileTypeLogPhoto:     true,
		model.FileTypeReviewReport: true,
	},
	"ROLE_DOCTOR": {
		model.FileTypeCommPhoto:    true,
		model.FileTypeLogPhoto:     true,
		model.FileTypeReviewReport: true,
	},
	"ROLE_CS": {
		model.FileTypeCommPhoto: true,
	},
	"technician": {
		model.FileTypeSignature:    true,
		model.FileTypeInstallPhoto: true,
	},
	"patient": {
		model.FileTypeCommPhoto: true,
		model.FileTypeLogPhoto:  true,
	},
}

// ─────────────────────────────────────────────────────────────
// T130 增补单（Boss 2026-09-10 12:42 裁定）：复查报告三道校验
//   1. 扩展名白名单：pdf/jpg/png/doc/docx/xlsx/pptx/zip
//   2. MIME 白名单：扩展名对应 MIME，或 application/octet-stream（Office 文件浏览器常误报 octet-stream，魔数强制兜底）
//   3. 魔数（文件头指纹）：扩展名与文件头必须匹配，防改名伪装
// 明确拒绝：xls/ppt（宏病毒风险）、wps/et/dps（WPS 自有格式）、dot/exe/bat/js 等
// 已知残留：doc/xls/ppt 同属 OLE2（魔数相同），xls/ppt 改名为 .doc 无法用魔数拦截，
//
//	待上线前评估是否加深 CFB stream 解析。
//
// ─────────────────────────────────────────────────────────────

// reviewReportAllowedExtensions 扩展名白名单
var reviewReportAllowedExtensions = map[string]bool{
	"pdf":  true,
	"jpg":  true,
	"jpeg": true,
	"png":  true,
	"doc":  true,
	"docx": true,
	"xlsx": true,
	"pptx": true,
	"zip":  true,
}

// reviewReportExpectedMIMEs 扩展名 → 允许的 MIME 列表
// 所有类型均允许 application/octet-stream（Office 文件浏览器常误报），魔数强制兜底
var reviewReportExpectedMIMEs = map[string][]string{
	"pdf":  {"application/pdf", "application/octet-stream"},
	"jpg":  {"image/jpeg", "application/octet-stream"},
	"jpeg": {"image/jpeg", "application/octet-stream"},
	"png":  {"image/png", "application/octet-stream"},
	"doc":  {"application/msword", "application/octet-stream"},
	"docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/octet-stream"},
	"xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/octet-stream"},
	"pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", "application/octet-stream"},
	"zip":  {"application/zip", "application/x-zip-compressed", "application/octet-stream"},
}

// reviewReportMagicNumbers 扩展名 → 期望的文件头魔数（字节序列）
//   - pdf:  %PDF
//   - png:  \x89PNG\r\n\x1a\n
//   - jpg:  \xFF\xD8\xFF
//   - doc:  \xD0\xCF\x11\xE0（OLE2 复合文档头，doc/xls/ppt 共用）
//   - docx/xlsx/pptx/zip: PK\x03\x04（OOXML 本质是 zip 容器）
var reviewReportMagicNumbers = map[string][][]byte{
	"pdf":  {[]byte("%PDF")},
	"jpg":  {{0xFF, 0xD8, 0xFF}},
	"jpeg": {{0xFF, 0xD8, 0xFF}},
	"png":  {{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}},
	"doc":  {{0xD0, 0xCF, 0x11, 0xE0}},
	"docx": {[]byte("PK\x03\x04")},
	"xlsx": {[]byte("PK\x03\x04")},
	"pptx": {[]byte("PK\x03\x04")},
	"zip":  {[]byte("PK\x03\x04")},
}

// extractExtension 从文件名提取小写扩展名（去点），jpeg 归一化为 jpg
func extractExtension(fileName string) string {
	idx := strings.LastIndex(fileName, ".")
	if idx < 0 || idx == len(fileName)-1 {
		return ""
	}
	ext := strings.ToLower(fileName[idx+1:])
	if ext == "jpeg" {
		ext = "jpg"
	}
	return ext
}

// ValidateReviewReportFile 复查报告三道校验（扩展名 + MIME + 魔数）
// 任一不通过返回 error，调用方映射为 400。
func ValidateReviewReportFile(fileName, contentType string, fileHeader []byte) error {
	ext := extractExtension(fileName)
	if ext == "" {
		return fmt.Errorf("file name missing extension")
	}
	// 第一道：扩展名白名单
	if !reviewReportAllowedExtensions[ext] {
		return fmt.Errorf("unsupported file extension: %s (allow: pdf/jpg/png/doc/docx/xlsx/pptx/zip)", ext)
	}
	// 第二道：MIME 白名单（含 octet-stream 放宽）
	allowedMIMEs := reviewReportExpectedMIMEs[ext]
	mimeOK := false
	for _, m := range allowedMIMEs {
		if contentType == m {
			mimeOK = true
			break
		}
	}
	if !mimeOK {
		return fmt.Errorf("unsupported content_type %q for extension %s", contentType, ext)
	}
	// 第三道：魔数指纹（强制，防改名伪装）
	expectedMagics := reviewReportMagicNumbers[ext]
	magicOK := false
	for _, magic := range expectedMagics {
		if len(fileHeader) >= len(magic) && bytes.Equal(fileHeader[:len(magic)], magic) {
			magicOK = true
			break
		}
	}
	if !magicOK {
		return fmt.Errorf("file header magic number does not match extension %s", ext)
	}
	return nil
}

// Authorize 校验角色对文件类型的签发权限（网关已完成 JWT 鉴权，此处做端点级授权）
func Authorize(role string, fileType model.FileType) error {
	allowed, ok := roleFileTypeMatrix[role]
	if !ok {
		return ErrForbidden
	}
	if !allowed[fileType] {
		return ErrForbidden
	}
	return nil
}

// GenerateUploadURL 签发短时效 PUT 预签名 URL 并登记 pending 元数据。
// 顺序：参数校验 → 生成 object key → COS 预签名 → 落库 pending（签发即登记，
// 保证 upload-complete 时 file_id 必然存在，闭环不断链）。
func (p *Presigner) GenerateUploadURL(ctx context.Context, req UploadRequest) (*UploadResponse, error) {
	if !validFileType(req.FileType) {
		return nil, ErrInvalidRequest
	}
	if req.OwnerType == "" || req.OwnerID == "" {
		return nil, ErrInvalidRequest
	}
	// T130 增补单：复查报告三道校验（扩展名白名单 + MIME + 魔数指纹）
	if req.FileType == model.FileTypeReviewReport {
		if err := ValidateReviewReportFile(req.FileName, req.ContentType, req.FileHeader); err != nil {
			return nil, ErrInvalidRequest
		}
	}

	// object key：{owner_type}/{owner_id}/{unixnano}_{rand16}.{ext}（唯一化，防覆盖）
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("generate random object key: %w", err)
	}
	now := p.now()
	// object key 扩展名：优先取文件名扩展名（review_report 场景），否则按 content-type 推导
	ext := extractExtension(req.FileName)
	if ext == "" {
		ext = fileExtension(req.ContentType)
	}
	objectKey := fmt.Sprintf("%s/%s/%d_%s.%s",
		req.OwnerType, req.OwnerID, now.UnixNano(),
		hex.EncodeToString(randomBytes), ext,
	)

	signedURL, err := p.cosClient.GeneratePresignedURL(ctx, p.defaultBucket, objectKey, "PUT", presignExpires)
	if err != nil {
		return nil, fmt.Errorf("presign url: %w", err)
	}

	fileID := "file_" + hex.EncodeToString(randomBytes)
	meta := &model.FileMetadata{
		FileID:      fileID,
		Bucket:      p.defaultBucket,
		ObjectKey:   objectKey,
		URL:         "", // 上传完成回调时回填
		FileType:    req.FileType,
		OwnerType:   req.OwnerType,
		OwnerID:     req.OwnerID,
		Size:        0,
		ContentType: req.ContentType,
		Status:      model.FileStatusPending,
	}
	if p.store != nil {
		if err := p.store.CreateFile(ctx, meta); err != nil {
			return nil, fmt.Errorf("register file metadata: %w", err)
		}
	}

	return &UploadResponse{
		FileID:       fileID,
		ObjectKey:    objectKey,
		SignatureURL: signedURL,
		ExpiresAt:    now.Add(presignExpires),
		Metadata:     meta,
	}, nil
}

// OnUploadComplete 上传完成闭环（任务需求 2）：
// 校验 file_id 存在且处于 pending → 置 uploaded + uploaded_at + size + url。
// 幂等：重复回调不重复更新 uploaded_at；file_id 不存在返回 ErrFileNotFound。
func (p *Presigner) OnUploadComplete(ctx context.Context, fileID, publicURL string, fileSize int64) error {
	if fileID == "" || fileSize < 0 {
		return ErrInvalidRequest
	}
	if p.store == nil {
		return errors.New("metadata store not configured")
	}

	fm, err := p.store.GetFileByFileID(ctx, fileID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrFileNotFound
		}
		return err
	}
	// 终态防回退：已 uploaded/failed 的行不允许再次变更（幂等返回成功）
	if fm.Status != model.FileStatusPending {
		return nil
	}

	// url 缺省回填 COS 对象地址（未提供公网 URL 时保障元数据可定位）
	url := publicURL
	if url == "" {
		url = fmt.Sprintf("https://%s/%s", fm.Bucket, fm.ObjectKey)
	}
	return p.store.MarkUploaded(ctx, fileID, url, fileSize)
}

// fileExtension content-type → 扩展名（图片类为主，未知类型落 bin）
// review_report 场景优先用文件名扩展名（extractExtension），此函数仅作兜底
func fileExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "application/pdf":
		return "pdf"
	case "application/msword":
		return "doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "xlsx"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return "pptx"
	case "application/zip", "application/x-zip-compressed":
		return "zip"
	default:
		return "bin"
	}
}

// downloadExpires 下载预签名 URL 有效期：5 分钟（短时效，防长期有效 URL 泄露）
const downloadExpires = 5 * time.Minute

// DownloadResponse 下载预签名响应
type DownloadResponse struct {
	FileID    string
	URL       string
	ExpiresAt time.Time
}

// GenerateDownloadURL 为已上传文件签发短时效 GET 预签名 URL（T130 复查报告下载）。
// file_id 不存在 → ErrFileNotFound；仅 uploaded 状态文件可下载。
func (p *Presigner) GenerateDownloadURL(ctx context.Context, fileID string) (*DownloadResponse, error) {
	if fileID == "" {
		return nil, ErrInvalidRequest
	}
	if p.store == nil {
		return nil, errors.New("metadata store not configured")
	}

	fm, err := p.store.GetFileByFileID(ctx, fileID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	// 仅已上传完成的文件可下载
	if fm.Status != model.FileStatusUploaded {
		return nil, ErrFileNotFound
	}

	now := p.now()
	signedURL, err := p.cosClient.GeneratePresignedURL(ctx, fm.Bucket, fm.ObjectKey, "GET", downloadExpires)
	if err != nil {
		return nil, fmt.Errorf("presign download url: %w", err)
	}
	return &DownloadResponse{
		FileID:    fileID,
		URL:       signedURL,
		ExpiresAt: now.Add(downloadExpires),
	}, nil
}
