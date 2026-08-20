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

// hasSuffix 避免额外依赖的轻量断言辅助
func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
