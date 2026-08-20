// Package handler 实现侧 HTTP 层测试（T022 返工）
//
// 覆盖矩阵（对齐验收标准：400/401/403 明确 + 覆盖率 ≥90%）：
//   - 身份：无 X-User-Id → 401（对齐 gateway 头注入机制，非 gin context）
//   - 授权：角色 × 文件类型矩阵越权 → 403
//   - 参数：非法 JSON / 非法 file_type → 400
//   - 闭环：presign → upload-complete → GET 元数据 uploaded
//   - 查询：query 分页 total 为全量计数
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
	"github.com/bracesync/bracesync/services/file-service/internal/repo"
	"github.com/bracesync/bracesync/services/file-service/internal/service"
	"github.com/bracesync/bracesync/services/file-service/internal/storage"
)

// ─────────────────────────────────────────────────────────────
// 测试基建：内存 store 打桩 + httptest 路由
// ─────────────────────────────────────────────────────────────

type memStore struct {
	files map[string]*model.FileMetadata
}

func newMemStore() *memStore { return &memStore{files: map[string]*model.FileMetadata{}} }

func (m *memStore) CreateFile(_ context.Context, fm *model.FileMetadata) error {
	if _, exists := m.files[fm.FileID]; exists {
		return nil
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

func (m *memStore) QueryFiles(_ context.Context, f repo.QueryFilter) ([]model.FileMetadata, error) {
	var out []model.FileMetadata
	for _, fm := range m.files {
		if f.OwnerType != "" && fm.OwnerType != f.OwnerType {
			continue
		}
		out = append(out, *fm)
		if f.PageSize > 0 && len(out) >= f.PageSize {
			break
		}
	}
	return out, nil
}

func (m *memStore) CountFiles(_ context.Context, f repo.QueryFilter) (int64, error) {
	var n int64
	for _, fm := range m.files {
		if f.OwnerType != "" && fm.OwnerType != f.OwnerType {
			continue
		}
		n++
	}
	return n, nil
}

// setupTestServer 构建带身份头的 httptest 服务器（模拟 gateway 注入 X-User-Id/X-Role）
func setupTestServer(store repo.Store) *httptest.Server {
	svc := service.NewPresigner(storage.NewMockCOSClient(), store, "test-bucket", "ap-guangzhou")
	h := NewFileHandler(svc, store)
	return httptest.NewServer(h.Router())
}

// doJSON 发起带身份的 JSON 请求，返回状态码与解析后的响应体
func doJSON(t *testing.T, method, url, userID, role, body string) (int, map[string]interface{}) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-User-Id", userID)
	}
	if role != "" {
		req.Header.Set("X-Role", role)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var parsed map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	return resp.StatusCode, parsed
}

func doGET(t *testing.T, url, userID, role string) (int, map[string]interface{}) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	if userID != "" {
		req.Header.Set("X-User-Id", userID)
	}
	if role != "" {
		req.Header.Set("X-Role", role)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var parsed map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	return resp.StatusCode, parsed
}

const presignBody = `{
	"file_type": "signature",
	"owner_type": "install_record",
	"owner_id": "123",
	"content_type": "image/jpeg"
}`

// ─────────────────────────────────────────────────────────────
// healthz
// ─────────────────────────────────────────────────────────────

func TestHealthz(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ─────────────────────────────────────────────────────────────
// presign：401 / 400 / 403 / 200
// ─────────────────────────────────────────────────────────────

func TestPresign_NoIdentity_401(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "", "", presignBody)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, float64(ErrorCodeUnauthorized), body["code"])
}

func TestPresign_InvalidJSON_400(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "T0001", "technician", `{bad json`)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, float64(ErrorCodeInvalidRequest), body["code"])
}

func TestPresign_MissingRequiredField_400(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "T0001", "technician",
		`{"file_type": "signature", "owner_id": "123"}`) // 缺 owner_type
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestPresign_RoleForbidden_403(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	// 患者无权签发 signature（签名图归技师）
	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "P20260001", "patient", presignBody)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, float64(ErrorCodeForbidden), body["code"])
}

func TestPresign_UnknownRole_403(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "X9999", "hacker", presignBody)
	assert.Equal(t, http.StatusForbidden, code)
}

func TestPresign_InvalidFileType_400(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "A0001", "admin",
		`{"file_type": "exe_binary", "owner_type": "patient", "owner_id": "P1"}`)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, float64(ErrorCodeInvalidRequest), body["code"])
}

func TestPresign_Success_200(t *testing.T) {
	store := newMemStore()
	srv := setupTestServer(store)
	defer srv.Close()

	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "T0001", "technician", presignBody)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, float64(0), body["code"])

	data := body["data"].(map[string]interface{})
	fileID := data["file_id"].(string)
	assert.NotEmpty(t, fileID)
	assert.Contains(t, data["signature_url"], "mock-cos.example.com")
	assert.Contains(t, data["object_key"], "install_record/123/")
	expiresIn := data["expires_in_seconds"].(float64)
	assert.Greater(t, expiresIn, float64(0))
	assert.LessOrEqual(t, expiresIn, float64(600), "短时效 ≤ 10 分钟")

	// 签发即登记 pending
	fm, err := store.GetFileByFileID(context.Background(), fileID)
	require.NoError(t, err)
	assert.Equal(t, model.FileStatusPending, fm.Status)
}

// ─────────────────────────────────────────────────────────────
// upload-complete：闭环 + 幂等 + 404
// ─────────────────────────────────────────────────────────────

func presignOne(t *testing.T, srvURL, userID, role string) string {
	t.Helper()
	code, body := doJSON(t, http.MethodPost, srvURL+"/api/v1/files/presign", userID, role, presignBody)
	require.Equal(t, http.StatusOK, code)
	return body["data"].(map[string]interface{})["file_id"].(string)
}

func TestUploadComplete_ClosedLoop(t *testing.T) {
	store := newMemStore()
	srv := setupTestServer(store)
	defer srv.Close()

	fileID := presignOne(t, srv.URL, "T0001", "technician")

	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "T0001", "technician",
		`{"file_id": "`+fileID+`", "size": 2048, "public_url": "https://cdn.example.com/x.jpg"}`)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, float64(0), body["code"])

	fm, err := store.GetFileByFileID(context.Background(), fileID)
	require.NoError(t, err)
	assert.Equal(t, model.FileStatusUploaded, fm.Status)
	assert.NotNil(t, fm.UploadedAt)
	assert.Equal(t, int64(2048), fm.Size)
}

func TestUploadComplete_Idempotent(t *testing.T) {
	store := newMemStore()
	srv := setupTestServer(store)
	defer srv.Close()

	fileID := presignOne(t, srv.URL, "T0001", "technician")
	payload := `{"file_id": "` + fileID + `", "size": 100}`

	code1, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "T0001", "technician", payload)
	require.Equal(t, http.StatusOK, code1)
	code2, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "T0001", "technician", payload)
	assert.Equal(t, http.StatusOK, code2, "重复回调幂等 200")
}

func TestUploadComplete_FileNotFound_404(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "T0001", "technician",
		`{"file_id": "file_ghost", "size": 1}`)
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, float64(ErrorCodeFileNotFound), body["code"])
}

func TestUploadComplete_InvalidBody_400(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "T0001", "technician", `{}`)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestUploadComplete_NoIdentity_401(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "", "",
		`{"file_id": "file_x", "size": 1}`)
	assert.Equal(t, http.StatusUnauthorized, code)
}

// ─────────────────────────────────────────────────────────────
// GET /:fileID 与 /query
// ─────────────────────────────────────────────────────────────

func TestGetFileByID_Success(t *testing.T) {
	store := newMemStore()
	srv := setupTestServer(store)
	defer srv.Close()

	fileID := presignOne(t, srv.URL, "T0001", "technician")

	code, body := doGET(t, srv.URL+"/api/v1/files/"+fileID, "T0001", "technician")
	require.Equal(t, http.StatusOK, code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, fileID, data["file_id"])
	assert.Equal(t, "pending", data["status"])
}

func TestGetFileByID_NotFound_404(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/file_ghost", "T0001", "technician")
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, float64(ErrorCodeFileNotFound), body["code"])
}

func TestGetFileByID_NoIdentity_401(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, _ := doGET(t, srv.URL+"/api/v1/files/file_x", "", "")
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestQuery_TotalIsFullCount(t *testing.T) {
	store := newMemStore()
	srv := setupTestServer(store)
	defer srv.Close()

	// 造 3 条同 owner 数据
	for i := 0; i < 3; i++ {
		presignOne(t, srv.URL, "T0001", "technician")
	}

	// pageSize=1 时 total 仍应为 3（全量计数，非当前页条数）
	code, body := doGET(t, srv.URL+"/api/v1/files/query?owner_type=install_record&pageSize=1", "T0001", "technician")
	require.Equal(t, http.StatusOK, code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, float64(3), data["total"], "total 必须是过滤后总数")
	assert.Equal(t, float64(1), data["pageSize"])
}

func TestQuery_EmptyResult(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/query?owner_type=nothing", "T0001", "technician")
	require.Equal(t, http.StatusOK, code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, float64(0), data["total"])
}

func TestQuery_InvalidPagingFallsBack(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/query?page=-1&pageSize=abc", "T0001", "technician")
	require.Equal(t, http.StatusOK, code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, float64(1), data["page"], "非法 page 回默认 1")
	assert.Equal(t, float64(20), data["pageSize"], "非法 pageSize 回默认 20")
}

func TestQuery_NoIdentity_401(t *testing.T) {
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, _ := doGET(t, srv.URL+"/api/v1/files/query", "", "")
	assert.Equal(t, http.StatusUnauthorized, code)
}

// ─────────────────────────────────────────────────────────────
// intParam 边界
// ─────────────────────────────────────────────────────────────

func TestIntParam_DirectUnit(t *testing.T) {
	// 通过 query 端点间接验证 intParam 各分支已被 TestQuery_* 覆盖；
	// 此处补 page=0 分支（<=0 回默认）
	srv := setupTestServer(newMemStore())
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/query?page=0", "T0001", "technician")
	require.Equal(t, http.StatusOK, code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, float64(1), data["page"])
}
