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

	// T378 归属判定桩：表内无键 = 无团队归属（fail-closed）
	doctorTeam  map[string]string // admin_id → team_id
	patientTeam map[string]string // patient_id → team_id
	alertTeam   map[string]string // alert_id → team_id
	scopeCalls  int               // 触库判定次数（拒绝路径零写断言用）
	writes      int               // CreateFile / MarkUploaded 落库次数（零写断言用）
	scopeErr    error             // DoctorTeamByAdmin 故障注入
	probeErr    error             // 归属探测（FileOwnerInTeam / OwnerInTeam）故障注入
	teamQuery   repo.QueryFilter  // 最近一次 QueryFiles/CountFiles 收到的过滤条件
}

func newMemStore() *memStore {
	return &memStore{
		files:       map[string]*model.FileMetadata{},
		doctorTeam:  map[string]string{},
		patientTeam: map[string]string{},
		alertTeam:   map[string]string{},
	}
}

func (m *memStore) CreateFile(_ context.Context, fm *model.FileMetadata) error {
	m.writes++
	if _, exists := m.files[fm.FileID]; exists {
		return nil
	}
	cp := *fm
	m.files[fm.FileID] = &cp
	return nil
}

func (m *memStore) MarkUploaded(_ context.Context, fileID, publicURL string, size int64) error {
	m.writes++
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
	m.teamQuery = f
	var out []model.FileMetadata
	for _, fm := range m.files {
		if !m.matchFilter(fm, f) {
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
	m.teamQuery = f
	var n int64
	for _, fm := range m.files {
		if !m.matchFilter(fm, f) {
			continue
		}
		n++
	}
	return n, nil
}

func (m *memStore) matchFilter(fm *model.FileMetadata, f repo.QueryFilter) bool {
	if f.OwnerType != "" && fm.OwnerType != f.OwnerType {
		return false
	}
	if f.OwnerID != "" && fm.OwnerID != f.OwnerID {
		return false
	}
	return !f.TeamScoped || m.ownerInTeam(fm.OwnerType, fm.OwnerID, f.TeamID)
}

// ─── T378 归属判定桩 ───

// ownerInTeam 是 repo.teamScopedCond 的内存镜像：非患者材料（ReviewTemplate 等）恒放行，
// 患者维度材料（patient / alert）须 owner 患者落在调用者团队内。
func (m *memStore) ownerInTeam(ownerType, ownerID, teamID string) bool {
	switch ownerType {
	case "patient", "alert":
		if teamID == "" {
			return false
		}
		got := m.patientTeam[ownerID]
		if ownerType == "alert" {
			got = m.alertTeam[ownerID]
		}
		return got == teamID && got != ""
	default:
		return true
	}
}

func (m *memStore) DoctorTeamByAdmin(_ context.Context, adminID string) (string, bool, error) {
	m.scopeCalls++
	if m.scopeErr != nil {
		return "", false, m.scopeErr
	}
	teamID := m.doctorTeam[adminID]
	return teamID, teamID != "", nil
}

func (m *memStore) FileOwnerInTeam(_ context.Context, fileID, teamID string) (bool, error) {
	m.scopeCalls++
	if m.probeErr != nil {
		return false, m.probeErr
	}
	fm, ok := m.files[fileID]
	if !ok {
		return false, nil // 查无此文件与跨团队合一
	}
	return m.ownerInTeam(fm.OwnerType, fm.OwnerID, teamID), nil
}

func (m *memStore) OwnerInTeam(_ context.Context, ownerType, ownerID, teamID string) (bool, error) {
	m.scopeCalls++
	if m.probeErr != nil {
		return false, m.probeErr
	}
	return m.ownerInTeam(ownerType, ownerID, teamID), nil
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

// ─────────────────────────────────────────────────────────────
// T261 身份单一来源：owner 归属校验
// ─────────────────────────────────────────────────────────────

// seedFile 向 memStore 注入一条指定 owner 的文件（用于归属校验测试）
func seedFile(store *memStore, fileID, ownerType, ownerID string, status model.FileStatus) {
	store.files[fileID] = &model.FileMetadata{
		FileID:    fileID,
		Bucket:    "test-bucket",
		ObjectKey: ownerType + "/" + ownerID + "/" + fileID,
		FileType:  model.FileTypeCommPhoto,
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Status:    status,
	}
}

// TestT261_Presign_PatientOwnerForced patient presign 时请求体 owner 字段被忽略，
// 落库 owner_type=patient / owner_id=X-User-Id。
func TestT261_Presign_PatientOwnerForced(t *testing.T) {
	store := newMemStore()
	srv := setupTestServer(store)
	defer srv.Close()

	// patient 提交伪造的 owner（指向他人）
	body := `{"file_type":"comm_photo","owner_type":"patient","owner_id":"P-OTHER","content_type":"image/jpeg"}`
	code, resp := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "P-SELF", "patient", body)
	require.Equal(t, http.StatusOK, code)
	fileID := resp["data"].(map[string]interface{})["file_id"].(string)

	// 落库 owner 应为调用者本人，非请求体伪造值
	fm, err := store.GetFileByFileID(context.Background(), fileID)
	require.NoError(t, err)
	assert.Equal(t, "patient", fm.OwnerType)
	assert.Equal(t, "P-SELF", fm.OwnerID, "owner_id 应被强制为 X-User-Id，忽略请求体")
}

// TestT261_Presign_StaffKeepsBodyOwner staff（technician）presign 保留请求体 owner
// （跨 owner 场景：技师上传安装照片 owner=install_record）。
func TestT261_Presign_StaffKeepsBodyOwner(t *testing.T) {
	store := newMemStore()
	srv := setupTestServer(store)
	defer srv.Close()

	body := `{"file_type":"signature","owner_type":"install_record","owner_id":"IR-999","content_type":"image/jpeg"}`
	code, resp := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "TECH-1", "technician", body)
	require.Equal(t, http.StatusOK, code)
	fileID := resp["data"].(map[string]interface{})["file_id"].(string)

	fm, err := store.GetFileByFileID(context.Background(), fileID)
	require.NoError(t, err)
	assert.Equal(t, "install_record", fm.OwnerType)
	assert.Equal(t, "IR-999", fm.OwnerID, "staff 应保留请求体 owner")
}

// TestT261_GetFileByID_OwnershipEnforced 非 staff 仅可访问本人文件，跨 owner → 403；staff 放行。
//
// T378 之后 staff 放行多了一层团队前提：DOC-1 与 P-A/P-B 同团队，本用例才仍在验
// 「角色跨 owner」这一维；跨团队那条由 TestT378_FileDetail_CrossTeamDenied 覆盖。
func TestT261_GetFileByID_OwnershipEnforced(t *testing.T) {
	store := newMemStore()
	store.doctorTeam["DOC-1"] = "TEAM-T261"
	store.patientTeam["P-A"] = "TEAM-T261"
	store.patientTeam["P-B"] = "TEAM-T261"
	seedFile(store, "F-OWN", "patient", "P-A", model.FileStatusUploaded)
	srv := setupTestServer(store)
	defer srv.Close()

	// 本人 → 200
	code, _ := doGET(t, srv.URL+"/api/v1/files/F-OWN", "P-A", "patient")
	assert.Equal(t, http.StatusOK, code, "本人应可访问")

	// 他人（patient）→ 403
	code, body := doGET(t, srv.URL+"/api/v1/files/F-OWN", "P-B", "patient")
	assert.Equal(t, http.StatusForbidden, code, "跨 owner 应 403")
	assert.Equal(t, float64(ErrorCodeForbidden), body["code"])

	// staff（doctor）→ 200
	code, _ = doGET(t, srv.URL+"/api/v1/files/F-OWN", "DOC-1", "ROLE_DOCTOR")
	assert.Equal(t, http.StatusOK, code, "staff 应放行")
}

// TestT261_Download_OwnershipEnforced 下载端点同样执行归属校验。
// T378 后医护放行需与 owner 患者同团队（跨团队那条见 TestT378_Download_CrossTeamDenied）。
func TestT261_Download_OwnershipEnforced(t *testing.T) {
	store := newMemStore()
	store.doctorTeam["DOC-1"] = "TEAM-T261"
	store.patientTeam["P-A"] = "TEAM-T261"
	seedFile(store, "F-DL", "patient", "P-A", model.FileStatusUploaded)
	srv := setupTestServer(store)
	defer srv.Close()

	// 本人 → 200（mock COS 返回预签名 URL）
	code, _ := doGET(t, srv.URL+"/api/v1/files/F-DL/download", "P-A", "patient")
	assert.Equal(t, http.StatusOK, code, "本人应可下载")

	// 他人 → 403
	code, body := doGET(t, srv.URL+"/api/v1/files/F-DL/download", "P-B", "patient")
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, float64(ErrorCodeForbidden), body["code"])

	// staff → 200
	code, _ = doGET(t, srv.URL+"/api/v1/files/F-DL/download", "DOC-1", "ROLE_DOCTOR")
	assert.Equal(t, http.StatusOK, code, "staff 应放行下载")
}

// TestT261_Query_PatientOwnerForced patient 查询时 owner_id 查询参数被忽略，
// 强制过滤为本人，无法枚举他人文件。
func TestT261_Query_PatientOwnerForced(t *testing.T) {
	store := newMemStore()
	store.doctorTeam["DOC-1"] = "TEAM-T261"
	store.patientTeam["P-A"] = "TEAM-T261"
	store.patientTeam["P-B"] = "TEAM-T261"
	seedFile(store, "F-A1", "patient", "P-A", model.FileStatusUploaded)
	seedFile(store, "F-A2", "patient", "P-A", model.FileStatusUploaded)
	seedFile(store, "F-B1", "patient", "P-B", model.FileStatusUploaded)
	srv := setupTestServer(store)
	defer srv.Close()

	// P-A 尝试以 owner_id=P-B 查询 → 仍只返回 P-A 自己的 2 条
	code, body := doGET(t, srv.URL+"/api/v1/files/query?owner_type=patient&owner_id=P-B", "P-A", "patient")
	require.Equal(t, http.StatusOK, code)
	data := body["data"].(map[string]interface{})
	assert.Equal(t, float64(2), data["total"], "patient 查询应被强制限定为本人 owner")

	// staff 查询 P-B 的文件 → 可见 1 条（同团队前提下跨 owner 仍放行）
	code, body = doGET(t, srv.URL+"/api/v1/files/query?owner_type=patient&owner_id=P-B", "DOC-1", "ROLE_DOCTOR")
	require.Equal(t, http.StatusOK, code)
	data = body["data"].(map[string]interface{})
	assert.Equal(t, float64(1), data["total"], "staff 可跨 owner 查询")
}

// TestT261_UploadComplete_OwnershipEnforced 非 staff 不得为他人文件确认上传。
func TestT261_UploadComplete_OwnershipEnforced(t *testing.T) {
	store := newMemStore()
	store.doctorTeam["DOC-1"] = "TEAM-T261"
	store.patientTeam["P-A"] = "TEAM-T261"
	seedFile(store, "F-UP", "patient", "P-A", model.FileStatusPending)
	srv := setupTestServer(store)
	defer srv.Close()

	// P-B 尝试为 P-A 的文件确认上传 → 403
	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "P-B", "patient",
		`{"file_id":"F-UP","size":1024}`)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, float64(ErrorCodeForbidden), body["code"])

	// P-A 本人确认 → 200
	code, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "P-A", "patient",
		`{"file_id":"F-UP","size":1024}`)
	assert.Equal(t, http.StatusOK, code)

	// staff 为同团队他人文件确认 → 200（跨 owner 放行；跨团队那条见 TestT378_UploadComplete_CrossTeamDenied）
	seedFile(store, "F-STAFF", "patient", "P-A", model.FileStatusPending)
	code, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "DOC-1", "ROLE_DOCTOR",
		`{"file_id":"F-STAFF","size":1024}`)
	assert.Equal(t, http.StatusOK, code, "staff 应放行跨 owner 上传确认")
}
