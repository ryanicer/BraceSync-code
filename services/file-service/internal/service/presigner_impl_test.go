// Package service 实现侧测试（T022 返工，对齐新实现：Store 注入 + 上传闭环 + 角色授权）
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
	"github.com/bracesync/bracesync/services/file-service/internal/repo"
	"github.com/bracesync/bracesync/services/file-service/internal/storage"
)

// memStore 内存打桩仓储（接口打桩，不依赖 PG，CI 离线可跑）
type memStore struct {
	files map[string]*model.FileMetadata
}

func newMemStore() *memStore { return &memStore{files: map[string]*model.FileMetadata{}} }

func (m *memStore) CreateFile(_ context.Context, fm *model.FileMetadata) error {
	if _, exists := m.files[fm.FileID]; exists {
		return nil // 幂等：重复登记不覆盖
	}
	cp := *fm
	m.files[fm.FileID] = &cp
	return nil
}

func (m *memStore) MarkUploaded(_ context.Context, fileID, publicURL string, size int64) error {
	fm, ok := m.files[fileID]
	if !ok {
		return repo.ErrNotFound
	}
	fm.Status = model.FileStatusUploaded
	if fm.UploadedAt == nil {
		now := time.Now()
		fm.UploadedAt = &now
	}
	fm.Size = size
	if publicURL != "" {
		fm.URL = publicURL
	}
	return nil
}

func (m *memStore) GetFileByFileID(_ context.Context, fileID string) (*model.FileMetadata, error) {
	fm, ok := m.files[fileID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *fm
	return &cp, nil
}

func (m *memStore) QueryFiles(_ context.Context, _ repo.QueryFilter) ([]model.FileMetadata, error) {
	return nil, nil
}

func (m *memStore) CountFiles(_ context.Context, _ repo.QueryFilter) (int64, error) {
	return int64(len(m.files)), nil
}

func newTestPresigner(store repo.Store) *Presigner {
	return NewPresigner(storage.NewMockCOSClient(), store, "test-bucket", "ap-guangzhou")
}

func TestGenerateUploadURL_Success(t *testing.T) {
	store := newMemStore()
	p := newTestPresigner(store)

	resp, err := p.GenerateUploadURL(context.Background(), UploadRequest{
		FileType:    model.FileTypeSignature,
		OwnerType:   "install_record",
		OwnerID:     "123",
		ContentType: "image/jpeg",
	})
	require.NoError(t, err)

	assert.NotEmpty(t, resp.FileID)
	assert.Contains(t, resp.ObjectKey, "install_record/123/")
	assert.True(t, hasSuffix(resp.ObjectKey, ".jpg"), "object key should end with .jpg, got %s", resp.ObjectKey)
	assert.Contains(t, resp.SignatureURL, "mock-cos.example.com")
	assert.Equal(t, model.FileStatusPending, resp.Metadata.Status)

	// 短时效：10 分钟 ± 秒级误差
	assert.WithinDuration(t, time.Now().Add(presignExpires), resp.ExpiresAt, 5*time.Second)

	// 签发即登记（pending 行已落库，闭环前置）
	fm, err := store.GetFileByFileID(context.Background(), resp.FileID)
	require.NoError(t, err)
	assert.Equal(t, model.FileStatusPending, fm.Status)
}

func TestGenerateUploadURL_InvalidFileType(t *testing.T) {
	p := newTestPresigner(newMemStore())

	_, err := p.GenerateUploadURL(context.Background(), UploadRequest{
		FileType:  model.FileType("invalid_type"),
		OwnerType: "install_record",
		OwnerID:   "123",
	})
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func TestGenerateUploadURL_EmptyFileType(t *testing.T) {
	p := newTestPresigner(newMemStore())

	_, err := p.GenerateUploadURL(context.Background(), UploadRequest{
		OwnerType: "install_record",
		OwnerID:   "123",
	})
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func TestGenerateUploadURL_MissingOwner(t *testing.T) {
	p := newTestPresigner(newMemStore())

	_, err := p.GenerateUploadURL(context.Background(), UploadRequest{
		FileType: model.FileTypeSignature,
		OwnerID:  "123",
	})
	assert.ErrorIs(t, err, ErrInvalidRequest, "missing owner_type")

	_, err = p.GenerateUploadURL(context.Background(), UploadRequest{
		FileType:  model.FileTypeSignature,
		OwnerType: "install_record",
	})
	assert.ErrorIs(t, err, ErrInvalidRequest, "missing owner_id")
}

func TestGenerateUploadURL_AllValidFileTypes(t *testing.T) {
	p := newTestPresigner(newMemStore())

	for _, ft := range []model.FileType{
		model.FileTypeSignature, model.FileTypeInstallPhoto,
		model.FileTypeCommPhoto, model.FileTypeLogPhoto,
	} {
		resp, err := p.GenerateUploadURL(context.Background(), UploadRequest{
			FileType:    ft,
			OwnerType:   "patient",
			OwnerID:     "P20260001",
			ContentType: "image/png",
		})
		require.NoError(t, err, "file_type=%s", ft)
		assert.Equal(t, ft, resp.Metadata.FileType)
		assert.NotEmpty(t, resp.FileID)
	}
}

func TestGenerateUploadURL_NilStore_SkipsRegistration(t *testing.T) {
	// store 未配置时仍可签发（降级路径，生产 main.go 恒注入真实 store）
	p := NewPresigner(storage.NewMockCOSClient(), nil, "test-bucket", "ap-guangzhou")

	resp, err := p.GenerateUploadURL(context.Background(), UploadRequest{
		FileType:  model.FileTypeCommPhoto,
		OwnerType: "patient",
		OwnerID:   "P20260001",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.FileID)
}

// ─────────────────────────────────────────────────────────────
// OnUploadComplete 闭环（需求 2）
// ─────────────────────────────────────────────────────────────

func TestOnUploadComplete_ClosedLoop(t *testing.T) {
	store := newMemStore()
	p := newTestPresigner(store)

	resp, err := p.GenerateUploadURL(context.Background(), UploadRequest{
		FileType:    model.FileTypeInstallPhoto,
		OwnerType:   "install_record",
		OwnerID:     "42",
		ContentType: "image/jpeg",
	})
	require.NoError(t, err)

	// 模拟客户端直传 COS 成功后的回调
	err = p.OnUploadComplete(context.Background(), resp.FileID, "https://cdn.example.com/42.jpg", 2048)
	require.NoError(t, err)

	fm, err := store.GetFileByFileID(context.Background(), resp.FileID)
	require.NoError(t, err)
	assert.Equal(t, model.FileStatusUploaded, fm.Status, "status should flip to uploaded")
	assert.NotNil(t, fm.UploadedAt, "uploaded_at must be set")
	assert.Equal(t, int64(2048), fm.Size)
	assert.Equal(t, "https://cdn.example.com/42.jpg", fm.URL)
}

func TestOnUploadComplete_EmptyURL_FallsBackToObjectAddress(t *testing.T) {
	store := newMemStore()
	p := newTestPresigner(store)

	resp, err := p.GenerateUploadURL(context.Background(), UploadRequest{
		FileType:  model.FileTypeLogPhoto,
		OwnerType: "patient",
		OwnerID:   "P20260001",
	})
	require.NoError(t, err)

	require.NoError(t, p.OnUploadComplete(context.Background(), resp.FileID, "", 100))

	fm, err := store.GetFileByFileID(context.Background(), resp.FileID)
	require.NoError(t, err)
	assert.Contains(t, fm.URL, "test-bucket", "url should fall back to bucket object address")
	assert.Contains(t, fm.URL, resp.ObjectKey)
}

func TestOnUploadComplete_Idempotent(t *testing.T) {
	store := newMemStore()
	p := newTestPresigner(store)

	resp, err := p.GenerateUploadURL(context.Background(), UploadRequest{
		FileType:  model.FileTypeCommPhoto,
		OwnerType: "patient",
		OwnerID:   "P20260001",
	})
	require.NoError(t, err)

	require.NoError(t, p.OnUploadComplete(context.Background(), resp.FileID, "u1", 10))
	fm1, _ := store.GetFileByFileID(context.Background(), resp.FileID)
	firstUploadedAt := *fm1.UploadedAt

	// 重复回调：幂等（终态不回退，uploaded_at 不漂移）
	require.NoError(t, p.OnUploadComplete(context.Background(), resp.FileID, "u2", 99))
	fm2, _ := store.GetFileByFileID(context.Background(), resp.FileID)
	assert.Equal(t, model.FileStatusUploaded, fm2.Status)
	assert.Equal(t, firstUploadedAt.Unix(), fm2.UploadedAt.Unix(), "uploaded_at must not drift on repeat")
}

func TestOnUploadComplete_FileNotFound(t *testing.T) {
	p := newTestPresigner(newMemStore())

	err := p.OnUploadComplete(context.Background(), "file_not_exist", "", 1)
	assert.ErrorIs(t, err, ErrFileNotFound)
}

func TestOnUploadComplete_InvalidParams(t *testing.T) {
	p := newTestPresigner(newMemStore())

	assert.ErrorIs(t, p.OnUploadComplete(context.Background(), "", "u", 1), ErrInvalidRequest)
	assert.ErrorIs(t, p.OnUploadComplete(context.Background(), "file_x", "u", -1), ErrInvalidRequest)
}

func TestOnUploadComplete_NilStore(t *testing.T) {
	p := NewPresigner(storage.NewMockCOSClient(), nil, "b", "r")
	err := p.OnUploadComplete(context.Background(), "file_x", "u", 1)
	assert.Error(t, err)
}

func TestOnUploadComplete_ReviewTemplateSizeLimit(t *testing.T) {
	// T135（R4-b 定稿值，服务端强制）：复查模板文件本体复用 review_report 通道，
	// 以 owner_type=ReviewTemplate 区分，upload-complete 时校验 20MB 上限（前端预校验外最后一道门）。
	// 直接构造 pending 的模板文件元数据，验证 >20MB 拒绝、=20MB 放行。
	newTemplateFile := func(store repo.Store, fileID string) {
		require.NoError(t, store.CreateFile(context.Background(), &model.FileMetadata{
			FileID:    fileID,
			Bucket:    "test-bucket",
			ObjectKey: "review-reports/tpl.pdf",
			FileType:  model.FileTypeReviewReport,
			OwnerType: OwnerTypeReviewTemplate,
			OwnerID:   "ADMIN",
			Status:    model.FileStatusPending,
		}))
	}

	t.Run("超过20MB拒绝", func(t *testing.T) {
		store := newMemStore()
		p := newTestPresigner(store)
		newTemplateFile(store, "TPL-big")
		err := p.OnUploadComplete(context.Background(), "TPL-big", "u", ReviewReportMaxBytes+1)
		assert.ErrorIs(t, err, ErrFileTooLarge)
		// 拒绝后保持 pending，未置 uploaded
		fm, _ := store.GetFileByFileID(context.Background(), "TPL-big")
		assert.Equal(t, model.FileStatusPending, fm.Status)
	})

	t.Run("等于20MB放行", func(t *testing.T) {
		store := newMemStore()
		p := newTestPresigner(store)
		newTemplateFile(store, "TPL-ok")
		require.NoError(t, p.OnUploadComplete(context.Background(), "TPL-ok", "u", ReviewReportMaxBytes))
		fm, _ := store.GetFileByFileID(context.Background(), "TPL-ok")
		assert.Equal(t, model.FileStatusUploaded, fm.Status)
	})

	t.Run("非模板类型不受20MB限制", func(t *testing.T) {
		store := newMemStore()
		p := newTestPresigner(store)
		require.NoError(t, store.CreateFile(context.Background(), &model.FileMetadata{
			FileID: "FILE-patient", Bucket: "test-bucket", ObjectKey: "review-reports/r.pdf",
			FileType: model.FileTypeReviewReport, OwnerType: "patient",
			OwnerID: "P0001", Status: model.FileStatusPending,
		}))
		// 非 ReviewTemplate 的复查报告文件 >20MB 不触发 T135 限制，正常置 uploaded
		require.NoError(t, p.OnUploadComplete(context.Background(), "FILE-patient", "u", ReviewReportMaxBytes+1))
		fm, _ := store.GetFileByFileID(context.Background(), "FILE-patient")
		assert.Equal(t, model.FileStatusUploaded, fm.Status)
	})
}

// ─────────────────────────────────────────────────────────────
// Authorize 角色权限矩阵（需求 4）
// ─────────────────────────────────────────────────────────────

func TestAuthorize_Matrix(t *testing.T) {
	cases := []struct {
		role     string
		fileType model.FileType
		wantErr  bool
	}{
		// admin 全类型
		{"admin", model.FileTypeSignature, false},
		{"admin", model.FileTypeInstallPhoto, false},
		{"admin", model.FileTypeCommPhoto, false},
		{"admin", model.FileTypeLogPhoto, false},
		// RBAC 角色
		{"ROLE_ADMIN", model.FileTypeCommPhoto, false},
		{"ROLE_DOCTOR", model.FileTypeCommPhoto, false},
		{"ROLE_DOCTOR", model.FileTypeSignature, true}, // 医生不签安装签名
		{"ROLE_CS", model.FileTypeCommPhoto, false},
		{"ROLE_CS", model.FileTypeInstallPhoto, true}, // 客服不碰安装照片
		// 技师：签名 + 安装照片
		{"technician", model.FileTypeSignature, false},
		{"technician", model.FileTypeInstallPhoto, false},
		{"technician", model.FileTypeLogPhoto, true},
		// 患者：沟通 + 日志
		{"patient", model.FileTypeCommPhoto, false},
		{"patient", model.FileTypeLogPhoto, false},
		{"patient", model.FileTypeSignature, true},
		// 未知角色 fail-closed
		{"", model.FileTypeCommPhoto, true},
		{"hacker", model.FileTypeCommPhoto, true},
	}

	for _, tc := range cases {
		err := Authorize(tc.role, tc.fileType)
		if tc.wantErr {
			assert.ErrorIs(t, err, ErrForbidden, "role=%s type=%s should be forbidden", tc.role, tc.fileType)
		} else {
			assert.NoError(t, err, "role=%s type=%s should be allowed", tc.role, tc.fileType)
		}
	}
}

// ─────────────────────────────────────────────────────────────
// fileExtension
// ─────────────────────────────────────────────────────────────

func TestFileExtension(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
	}{
		{"image/jpeg", "jpg"},
		{"image/png", "png"},
		{"image/webp", "webp"},
		{"application/pdf", "pdf"},
		{"application/octet-stream", "bin"},
		{"", "bin"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, fileExtension(tt.contentType), "content_type=%s", tt.contentType)
	}
}

// ─────────────────────────────────────────────────────────────
// T130 增补单：ValidateReviewReportFile（扩展名白名单 + MIME + 魔数三道校验）
// ─────────────────────────────────────────────────────────────

// 魔数常量（与 presigner.go reviewReportMagicNumbers 对齐）
var (
	magicPDF  = []byte("%PDF-1.4")
	magicPNG  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	magicJPG  = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	magicDOC  = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	magicZIP  = []byte("PK\x03\x04\x14\x00\x00\x00")
	magicEXE  = []byte("MZ\x90\x00\x03\x00\x00\x00")
	magicTEXT = []byte("Hello Wo")
)

func TestValidateReviewReportFile_WhitelistPass(t *testing.T) {
	// 白名单内扩展名 + 对应魔数 + 对应 MIME → 通过
	cases := []struct {
		fileName    string
		contentType string
		header      []byte
	}{
		{"report.pdf", "application/pdf", magicPDF},
		{"photo.jpg", "image/jpeg", magicJPG},
		{"photo.jpeg", "image/jpeg", magicJPG},
		{"image.png", "image/png", magicPNG},
		{"doc.doc", "application/msword", magicDOC},
		{"doc.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", magicZIP},
		{"sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", magicZIP},
		{"slide.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", magicZIP},
		{"archive.zip", "application/zip", magicZIP},
		// Office MIME 误报 octet-stream 放宽（魔数必须匹配）
		{"doc.docx", "application/octet-stream", magicZIP},
		{"sheet.xlsx", "application/octet-stream", magicZIP},
		{"report.pdf", "application/octet-stream", magicPDF},
	}
	for _, tc := range cases {
		err := ValidateReviewReportFile(tc.fileName, tc.contentType, tc.header)
		assert.NoError(t, err, "fileName=%s contentType=%s should pass", tc.fileName, tc.contentType)
	}
}

func TestValidateReviewReportFile_ExtensionRejected(t *testing.T) {
	// 非白名单扩展名直接拒（xls/ppt 宏病毒、wps/et/dps WPS 自有格式、exe/bat/js 等）
	rejected := []string{
		"mal.xls", "bad.ppt", "doc.wps", "sheet.et", "slide.dps",
		"run.exe", "script.bat", "evil.js", "template.dot",
	}
	for _, name := range rejected {
		err := ValidateReviewReportFile(name, "application/octet-stream", magicZIP)
		assert.Error(t, err, "extension %s should be rejected", name)
		assert.Contains(t, err.Error(), "unsupported file extension")
	}
}

func TestValidateReviewReportFile_MagicMismatchRejected(t *testing.T) {
	// 扩展名在白名单但魔数不匹配 → 拒（防改名伪装）
	cases := []struct {
		fileName    string
		contentType string
		header      []byte
	}{
		{"fake.pdf", "application/pdf", magicEXE},           // exe 改名为 pdf
		{"fake.png", "image/png", magicTEXT},                // 文本改名为 png
		{"fake.docx", "application/octet-stream", magicDOC}, // doc(OLE2) 改名为 docx(PK)
		{"fake.zip", "application/zip", magicPDF},           // pdf 改名为 zip
	}
	for _, tc := range cases {
		err := ValidateReviewReportFile(tc.fileName, tc.contentType, tc.header)
		assert.Error(t, err, "fileName=%s with mismatched magic should be rejected", tc.fileName)
		assert.Contains(t, err.Error(), "magic number does not match")
	}
}

func TestValidateReviewReportFile_MIMEMismatchRejected(t *testing.T) {
	// MIME 既非扩展名对应类型也非 octet-stream → 拒
	err := ValidateReviewReportFile("report.pdf", "text/html", magicPDF)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported content_type")
}

func TestValidateReviewReportFile_MissingExtension(t *testing.T) {
	err := ValidateReviewReportFile("noext", "application/pdf", magicPDF)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing extension")
}

// hasSuffix 避免额外依赖的轻量断言辅助
func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
