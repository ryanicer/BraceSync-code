// Package service — COS 预签名签发与文件元数据登记（T022，架构 §3.3/§8 ADR-11）
//
// 业务链路：presign 签发（登记 pending）→ 客户端直传 COS → upload-complete 落库闭环。
// 文件字节流不经本服务；COS 外部依赖经 storage.StorageClient 接口打桩，CI 离线可跑。
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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
//
// 未知角色（含空）一律拒绝（fail-closed）。
var roleFileTypeMatrix = map[string]map[model.FileType]bool{
	"admin": {
		model.FileTypeSignature:    true,
		model.FileTypeInstallPhoto: true,
		model.FileTypeCommPhoto:    true,
		model.FileTypeLogPhoto:     true,
	},
	"ROLE_ADMIN": {
		model.FileTypeSignature:    true,
		model.FileTypeInstallPhoto: true,
		model.FileTypeCommPhoto:    true,
		model.FileTypeLogPhoto:     true,
	},
	"ROLE_DOCTOR": {
		model.FileTypeCommPhoto: true,
		model.FileTypeLogPhoto:  true,
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

	// object key：{owner_type}/{owner_id}/{unixnano}_{rand16}.{ext}（唯一化，防覆盖）
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("generate random object key: %w", err)
	}
	now := p.now()
	objectKey := fmt.Sprintf("%s/%s/%d_%s.%s",
		req.OwnerType, req.OwnerID, now.UnixNano(),
		hex.EncodeToString(randomBytes), fileExtension(req.ContentType),
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
	default:
		return "bin"
	}
}
