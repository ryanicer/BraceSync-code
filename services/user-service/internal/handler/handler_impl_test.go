// Package handler 实现侧测试（T030）：fake store 驱动的全端点 HTTP 层用例
//
// 覆盖：参数校验（分页/枚举/范围）、join 字段、404/400/409/500 分支、幂等、
// 登录 bcrypt+JWT、settings 脱敏回写合并、权限矩阵读写。
// 命名遵循实现侧测试约定（*_impl_test.go，不与测试专家文件重叠）。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
)

func init() { gin.SetMode(gin.TestMode) }

const testPhoneKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakeStore repo.Store 的内存实现（各用例按需装配字段）
type fakeStore struct {
	admin            *repo.AdminRow
	adminErr         error
	adminUpdatedHash string // T040: 用于验证更新逻辑
	scope            string
	scopeErr         error
	doctorID         string
	doctorFound      bool
	doctorErr        error
	patients         []repo.PatientRow
	patientTotal     int64
	patientsErr      error
	patient          *repo.PatientRow
	patientErr       error
	teams            []repo.TeamRow
	teamsErr         error
	teamExists       bool
	teamErr          error
	doctors          []repo.DoctorRow
	doctorsErr       error
	techs            []repo.TechnicianRow
	techTotal        int64
	techsErr         error
	teamTechs        []repo.TechnicianRow
	teamTechsErr     error
	tech             *repo.TechnicianRow
	techErr          error
	createdTech      *repo.TechnicianRow
	createErr        error
	updatedTech      *repo.TechnicianRow
	updateErr        error
	toggleExists     bool
	toggleErr        error
	phoneTaken       bool
	takenErr         error
	feedbacks        []repo.FeedbackRow
	feedbacksErr     error
	processOK        bool
	processErr       error
	plans            []repo.OrthosisPlanRow
	plansErr         error
	latest           string
	hasLatest        bool
	latestErr        error
	createdPlan      *repo.OrthosisPlanRow
	createPlanEr     error
	feelings         []repo.FeelingLogRow
	feelingsErr      error
	replyOK          bool
	replyErr         error
	roles            []repo.RoleRow
	rolesErr         error
	role             *repo.RoleRow
	roleErr          error
	updRoleOK        bool
	updRoleErr       error
	configs          map[string]string
	configsErr       error
	upsertErr        error

	lastUpsert    []repo.ConfigKV
	lastUpsertBy  string
	lastFilter    repo.PatientFilter
	lastProcessR  *string
	lastTechInput repo.TechInput
	lastPermJSON  string
	lastReply     string
	lastToggle    string

	techLogin       *repo.TechLoginRow
	techLoginErr    error
	patientLogin    *repo.PatientLoginRow
	patientLoginErr error

	// T057 患者写操作 stub 字段（实现方转绿时由用例装配返回值）
	createdPatient    *repo.PatientRow
	createPatientErr  error
	assignedPatient   *repo.PatientRow
	assignPatientErr  error
	batchBindResult   *repo.BatchBindResult
	batchBindErr      error
	lastCreateInput   repo.PatientInput
	lastAssignPatient string
	lastAssignTeam    string
	lastBatchIDs      []string
	lastBatchTeam     string

	// T059 团队/成员写操作 stub 字段（实现方转绿时由用例装配返回值）
	createdTeam        *repo.TeamDetailRow
	createTeamErr      error
	updatedTeam        *repo.TeamDetailRow
	updateTeamErr      error
	deleteTeamErr      error
	addedMember        *repo.TeamMemberRow
	addMemberErr       error
	updatedMember      *repo.TeamMemberRow
	updateMemberErr    error
	removeMemberErr    error
	lastCreateTeamIn   repo.TeamInput
	lastUpdateTeamID   string
	lastUpdateTeamIn   repo.TeamInput
	lastDeleteTeamID   string
	lastAddTeamID      string
	lastAddMemberIn    repo.MemberInput
	lastUpdateMTeamID  string
	lastUpdateMID      string
	lastUpdateMemberIn repo.MemberInput
	lastRemoveTeamID   string
	lastRemoveMID      string
	lastRemoveMType    string
}

func (f *fakeStore) GetAdminByUsername(_ context.Context, _ string) (*repo.AdminRow, error) {
	return f.admin, f.adminErr
}

// UpdateAdminPasswordHash T040: 渐进式重哈希落库模拟
func (f *fakeStore) UpdateAdminPasswordHash(_ context.Context, adminID string, newHash string) error {
	if f.admin != nil && f.admin.AdminID == adminID {
		f.adminUpdatedHash = newHash
		return nil
	}
	return errors.New("admin not found")
}
func (f *fakeStore) RoleScope(_ context.Context, _ string) (string, error) {
	return f.scope, f.scopeErr
}
func (f *fakeStore) DoctorIDByAdmin(_ context.Context, _ string) (string, bool, error) {
	return f.doctorID, f.doctorFound, f.doctorErr
}
func (f *fakeStore) ListPatients(_ context.Context, flt repo.PatientFilter) ([]repo.PatientRow, int64, error) {
	f.lastFilter = flt
	return f.patients, f.patientTotal, f.patientsErr
}
func (f *fakeStore) GetPatient(_ context.Context, _ string) (*repo.PatientRow, error) {
	return f.patient, f.patientErr
}
func (f *fakeStore) ListTeams(_ context.Context) ([]repo.TeamRow, error) { return f.teams, f.teamsErr }
func (f *fakeStore) TeamExists(_ context.Context, _ string) (bool, error) {
	return f.teamExists, f.teamErr
}
func (f *fakeStore) ListDoctors(_ context.Context) ([]repo.DoctorRow, error) {
	return f.doctors, f.doctorsErr
}
func (f *fakeStore) ListDoctorsByTeam(_ context.Context, _ string) ([]repo.DoctorRow, error) {
	return f.doctors, f.doctorsErr
}
func (f *fakeStore) ListTechnicians(_ context.Context, _, _ int) ([]repo.TechnicianRow, int64, error) {
	return f.techs, f.techTotal, f.techsErr
}
func (f *fakeStore) ListTechniciansByTeam(_ context.Context, _ string) ([]repo.TechnicianRow, error) {
	return f.teamTechs, f.teamTechsErr
}
func (f *fakeStore) GetTechnician(_ context.Context, _ string) (*repo.TechnicianRow, error) {
	return f.tech, f.techErr
}
func (f *fakeStore) CreateTechnician(_ context.Context, in repo.TechInput) (*repo.TechnicianRow, error) {
	f.lastTechInput = in
	return f.createdTech, f.createErr
}
func (f *fakeStore) UpdateTechnician(_ context.Context, _ string, in repo.TechInput) (*repo.TechnicianRow, error) {
	f.lastTechInput = in
	return f.updatedTech, f.updateErr
}
func (f *fakeStore) ToggleTechnician(_ context.Context, _, status string) (bool, error) {
	f.lastToggle = status
	return f.toggleExists, f.toggleErr
}
func (f *fakeStore) TechPhoneHashTaken(_ context.Context, _, _ string) (bool, error) {
	return f.phoneTaken, f.takenErr
}
func (f *fakeStore) ListFeedbacks(_ context.Context, _ string) ([]repo.FeedbackRow, error) {
	return f.feedbacks, f.feedbacksErr
}
func (f *fakeStore) ProcessFeedback(_ context.Context, _ int64, _ string, reply *string) (bool, error) {
	f.lastProcessR = reply
	return f.processOK, f.processErr
}
func (f *fakeStore) ListPlans(_ context.Context, _ string) ([]repo.OrthosisPlanRow, error) {
	return f.plans, f.plansErr
}
func (f *fakeStore) LatestPlanVersion(_ context.Context, _ string) (string, bool, error) {
	return f.latest, f.hasLatest, f.latestErr
}
func (f *fakeStore) CreatePlan(_ context.Context, _, _, _, version string) (*repo.OrthosisPlanRow, error) {
	if f.createdPlan != nil {
		f.createdPlan.Version = version
	}
	return f.createdPlan, f.createPlanEr
}
func (f *fakeStore) ListFeelingLogs(_ context.Context, _ string) ([]repo.FeelingLogRow, error) {
	return f.feelings, f.feelingsErr
}
func (f *fakeStore) ReplyFeelingLog(_ context.Context, _ int64, reply string) (bool, error) {
	f.lastReply = reply
	return f.replyOK, f.replyErr
}
func (f *fakeStore) ListRoles(_ context.Context) ([]repo.RoleRow, error) { return f.roles, f.rolesErr }
func (f *fakeStore) GetRole(_ context.Context, _ string) (*repo.RoleRow, error) {
	return f.role, f.roleErr
}
func (f *fakeStore) UpdateRolePermissions(_ context.Context, _, permJSON string) (bool, error) {
	f.lastPermJSON = permJSON
	return f.updRoleOK, f.updRoleErr
}
func (f *fakeStore) GetConfigs(_ context.Context, _ []string) (map[string]string, error) {
	return f.configs, f.configsErr
}
func (f *fakeStore) UpsertConfigs(_ context.Context, kvs []repo.ConfigKV, updatedBy string) error {
	f.lastUpsert = kvs
	f.lastUpsertBy = updatedBy
	return f.upsertErr
}
func (f *fakeStore) GetTechByPhoneHash(_ context.Context, _ string) (*repo.TechLoginRow, error) {
	return f.techLogin, f.techLoginErr
}
func (f *fakeStore) GetPatientByPhoneHash(_ context.Context, _ string) (*repo.PatientLoginRow, error) {
	return f.patientLogin, f.patientLoginErr
}

// T057 患者写操作 stub 实现（仅满足扩展后的 Store 接口编译；
// stub handler 当前不调用 store，这些方法待实现方转绿时被触达）
func (f *fakeStore) CreatePatient(_ context.Context, in repo.PatientInput) (*repo.PatientRow, error) {
	f.lastCreateInput = in
	return f.createdPatient, f.createPatientErr
}
func (f *fakeStore) AssignPatientTeam(_ context.Context, patientID, teamID string) (*repo.PatientRow, error) {
	f.lastAssignPatient = patientID
	f.lastAssignTeam = teamID
	return f.assignedPatient, f.assignPatientErr
}
func (f *fakeStore) BatchBindPatients(_ context.Context, patientIDs []string, teamID string) (*repo.BatchBindResult, error) {
	f.lastBatchIDs = patientIDs
	f.lastBatchTeam = teamID
	return f.batchBindResult, f.batchBindErr
}

// T059 团队/成员写操作 stub 实现（仅满足扩展后的 Store 接口编译；
// stub handler 当前不调用 store，这些方法待实现方转绿时被触达）
func (f *fakeStore) CreateTeam(_ context.Context, in repo.TeamInput) (*repo.TeamDetailRow, error) {
	f.lastCreateTeamIn = in
	return f.createdTeam, f.createTeamErr
}
func (f *fakeStore) UpdateTeam(_ context.Context, teamID string, in repo.TeamInput) (*repo.TeamDetailRow, error) {
	f.lastUpdateTeamID = teamID
	f.lastUpdateTeamIn = in
	return f.updatedTeam, f.updateTeamErr
}
func (f *fakeStore) DeleteTeam(_ context.Context, teamID string) error {
	f.lastDeleteTeamID = teamID
	return f.deleteTeamErr
}
func (f *fakeStore) AddTeamMember(_ context.Context, teamID string, in repo.MemberInput) (*repo.TeamMemberRow, error) {
	f.lastAddTeamID = teamID
	f.lastAddMemberIn = in
	return f.addedMember, f.addMemberErr
}
func (f *fakeStore) UpdateTeamMember(_ context.Context, teamID, memberID string, in repo.MemberInput) (*repo.TeamMemberRow, error) {
	f.lastUpdateMTeamID = teamID
	f.lastUpdateMID = memberID
	f.lastUpdateMemberIn = in
	return f.updatedMember, f.updateMemberErr
}
func (f *fakeStore) RemoveTeamMember(_ context.Context, teamID, memberID, memberType string) error {
	f.lastRemoveTeamID = teamID
	f.lastRemoveMID = memberID
	f.lastRemoveMType = memberType
	return f.removeMemberErr
}

// testEnv 装配 Handler + 请求工具
type testEnv struct {
	t      *testing.T
	store  *fakeStore
	signer *token.Signer
	h      *Handler
}

func newEnv(t *testing.T, withSigner, withPhone bool) *testEnv {
	t.Helper()
	var signer *token.Signer
	if withSigner {
		s, err := token.NewSigner("test-secret", time.Hour)
		require.NoError(t, err)
		signer = s
	}
	var cipher *phone.Cipher
	if withPhone {
		c, err := phone.NewCipher(testPhoneKey)
		require.NoError(t, err)
		cipher = c
	}
	store := &fakeStore{}
	return &testEnv{t: t, store: store, signer: signer, h: New(store, signer, cipher)}
}

// do 发起请求并解析统一响应体
func (e *testEnv) do(method, path string, body any, headers map[string]string) (*httptest.ResponseRecorder, struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}) {
	e.t.Helper()
	var reader *strings.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(e.t, err)
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.h.Router().ServeHTTP(w, req)
	var resp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if w.Body.Len() > 0 {
		require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &resp))
	}
	return w, resp
}

// ─────────────────────────────────────────────────────────────
// 基础
// ─────────────────────────────────────────────────────────────

func TestHealthz(t *testing.T) {
	e := newEnv(t, true, true)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	e.h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ─────────────────────────────────────────────────────────────
// 登录（T030 #9）
// ─────────────────────────────────────────────────────────────

func adminHash(t *testing.T, pwd string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

func TestLoginSuccess(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.admin = &repo.AdminRow{AdminID: "A0001", Username: "ops_admin", Name: "运营小张",
		PasswordHash: adminHash(t, "admin123"), RoleID: "ROLE_ADMIN", Status: "enabled"}
	e.store.scope = "all"

	w, resp := e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "ops_admin", "password": "admin123",
	}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, resp.Code)

	var dto model.LoginResultDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "A0001", dto.AdminID)
	assert.Equal(t, "ROLE_ADMIN", dto.RoleID)
	assert.Equal(t, "all", dto.Scope)

	claims, err := e.signer.Verify(dto.Token)
	require.NoError(t, err)
	assert.Equal(t, "A0001", claims.Subject)
	assert.Equal(t, "ROLE_ADMIN", claims.RoleID)
}

func TestLoginValidationAndErrors(t *testing.T) {
	e := newEnv(t, true, true)

	// body 非法 JSON
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 缺字段
	w, resp := e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "x"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)

	// 用户不存在 → 401（统一文案防枚举）
	w, resp = e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "ghost", "password": "p"}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, model.CodeUnauthorized, resp.Code)

	// 密码错误 → 401
	e.store.admin = &repo.AdminRow{AdminID: "A1", Username: "u", Name: "n",
		PasswordHash: adminHash(t, "right"), RoleID: "ROLE_CS", Status: "enabled"}
	w, _ = e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "u", "password": "wrong"}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 账号禁用 → 401
	e.store.admin.Status = "disabled"
	w, _ = e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "u", "password": "right"}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// store 错误 → 500
	e.store.adminErr = errors.New("db down")
	w, _ = e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "u", "password": "p"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// scope 查询错误 → 500
	e.store.adminErr = nil
	e.store.scopeErr = errors.New("boom")
	e.store.admin.Status = "enabled"
	w, _ = e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "u", "password": "right"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestLoginWithoutSigner(t *testing.T) {
	e := newEnv(t, false, true) // 未配置 JWT_SECRET
	w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "u", "password": "p"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)
}

// ─────────────────────────────────────────────────────────────
// 技师登录（T037）
// ─────────────────────────────────────────────────────────────

func TestTechLoginSuccess(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.techLogin = &repo.TechLoginRow{
		TechID: "T0001", Name: "技师老陈",
		PasswordHash: adminHash(t, "Password1!"),
		TeamID:       "TEAM01", Status: "enabled", AuthStatus: "authorized",
	}

	w, resp := e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "Password1!",
	}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, resp.Code)

	var dto model.TechLoginResultDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "T0001", dto.TechID)
	assert.Equal(t, "技师老陈", dto.Name)
	assert.Equal(t, "TEAM01", dto.TeamID)
	assert.Equal(t, "technician", dto.Role)

	claims, err := e.signer.Verify(dto.Token)
	require.NoError(t, err)
	assert.Equal(t, "T0001", claims.Subject)
	assert.Equal(t, "technician", claims.RoleID)
	assert.Equal(t, "TEAM01", claims.TeamID)
}

func TestTechLoginErrors(t *testing.T) {
	e := newEnv(t, true, true)

	// 非法 JSON
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tech/login", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 手机号格式错误 → 400
	w, _ = e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "123", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 缺密码 → 400
	w, _ = e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "",
	}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 技师不存在 → 401
	w, resp := e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, model.CodeUnauthorized, resp.Code)

	// 密码错误 → 401
	e.store.techLogin = &repo.TechLoginRow{
		TechID: "T1", Name: "n", PasswordHash: adminHash(t, "right"),
		Status: "enabled", AuthStatus: "authorized",
	}
	w, _ = e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "wrong",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 禁用 → 401
	e.store.techLogin.Status = "disabled"
	w, _ = e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "right",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 授权未通过 → 401
	e.store.techLogin.Status = "enabled"
	e.store.techLogin.AuthStatus = "unauthorized"
	w, _ = e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "right",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// DB 错误 → 500
	e.store.techLoginErr = errors.New("db down")
	e.store.techLogin = nil
	w, _ = e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTechLoginWithoutSigner(t *testing.T) {
	e := newEnv(t, false, true)
	w, resp := e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)
}

// ─────────────────────────────────────────────────────────────
// 患者登录（T037）
// ─────────────────────────────────────────────────────────────

func TestPatientLoginSuccess(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.patientLogin = &repo.PatientLoginRow{
		PatientID: "P20260001", Name: "患者小明",
		PasswordHash: adminHash(t, "Password1!"),
		Status:       "active",
	}

	w, resp := e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "13800000002", "password": "Password1!",
	}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, resp.Code)

	var dto model.PatientLoginResultDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "P20260001", dto.PatientID)
	assert.Equal(t, "患者小明", dto.Name)
	assert.Equal(t, "patient", dto.Role)

	claims, err := e.signer.Verify(dto.Token)
	require.NoError(t, err)
	assert.Equal(t, "P20260001", claims.Subject)
	assert.Equal(t, "patient", claims.RoleID)
	assert.Equal(t, "", claims.TeamID)
}

func TestPatientLoginErrors(t *testing.T) {
	e := newEnv(t, true, true)

	// 非法 JSON
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patient/login", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 手机号格式错误 → 400
	w, _ = e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "abc", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 患者不存在 → 401
	w, resp := e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "13800000002", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, model.CodeUnauthorized, resp.Code)

	// 密码错误 → 401
	e.store.patientLogin = &repo.PatientLoginRow{
		PatientID: "P1", Name: "n", PasswordHash: adminHash(t, "right"),
		Status: "active",
	}
	w, _ = e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "13800000002", "password": "wrong",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 未激活 → 401
	e.store.patientLogin.Status = "pending"
	w, _ = e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "13800000002", "password": "right",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// DB 错误 → 500
	e.store.patientLoginErr = errors.New("db down")
	e.store.patientLogin = nil
	w, _ = e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "13800000002", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestPatientLoginWithoutSigner(t *testing.T) {
	e := newEnv(t, false, true)
	w, resp := e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "13800000002", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)
}

// TestLoginAntiEnumeration 防枚举：所有 401 文案一致，不泄漏失败原因
func TestLoginAntiEnumeration(t *testing.T) {
	e := newEnv(t, true, true)
	const expectMsg = "invalid phone or password"

	// 技师不存在
	w, resp := e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, expectMsg, resp.Message)

	// 技师密码错误
	e.store.techLogin = &repo.TechLoginRow{
		TechID: "T1", Name: "n", PasswordHash: adminHash(t, "right"),
		Status: "enabled", AuthStatus: "authorized",
	}
	w, resp = e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "wrong",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, expectMsg, resp.Message)

	// 技师禁用
	e.store.techLogin.Status = "disabled"
	w, resp = e.do(http.MethodPost, "/api/v1/tech/login", map[string]string{
		"phone": "13800000001", "password": "right",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, expectMsg, resp.Message)

	// 患者不存在
	e.store.patientLogin = nil
	w, resp = e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "13800000002", "password": "p",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, expectMsg, resp.Message)

	// 患者密码错误
	e.store.patientLogin = &repo.PatientLoginRow{
		PatientID: "P1", Name: "n", PasswordHash: adminHash(t, "right"),
		Status: "active",
	}
	w, resp = e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "13800000002", "password": "wrong",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, expectMsg, resp.Message)

	// 患者未激活
	e.store.patientLogin.Status = "pending"
	w, resp = e.do(http.MethodPost, "/api/v1/patient/login", map[string]string{
		"phone": "13800000002", "password": "right",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, expectMsg, resp.Message)
}

// ─────────────────────────────────────────────────────────────
// 患者（T030 #1/#2）
// ─────────────────────────────────────────────────────────────

func samplePatient() repo.PatientRow {
	teamName, doctorName := "脊柱矫形一组", "李医师"
	teamID, doctorID := "TEAM01", "D0001"
	return repo.PatientRow{
		PatientID: "P20260001", Name: "患者小明", Gender: strPtr("male"), Age: intPtr(14),
		Diagnosis: strPtr("胸椎右侧凸"), CobbAngle: floatPtr(28.0),
		DeviceID: strPtr("PRS-001"), TeamID: &teamID, DoctorID: &doctorID,
		Status: "active", CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		TeamName:  &teamName, DoctorName: &doctorName,
	}
}

func strPtr(s string) *string     { return &s }
func intPtr(i int) *int           { return &i }
func floatPtr(f float64) *float64 { return &f }

func TestListPatients(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.patients = []repo.PatientRow{samplePatient()}
	e.store.patientTotal = 1

	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients?keyword=xiao&teamId=TEAM01&page=2&pageSize=10", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "xiao", e.store.lastFilter.Keyword)
	assert.Equal(t, "TEAM01", e.store.lastFilter.TeamID)
	assert.Equal(t, 2, e.store.lastFilter.Page)
	assert.Equal(t, 10, e.store.lastFilter.PageSize)

	var page model.PageData
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	assert.Equal(t, int64(1), page.Total)
	var list []model.AdminPatientDTO
	raw, _ := json.Marshal(page.List)
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "脊柱矫形一组", *list[0].TeamName)
	assert.Equal(t, "李医师", *list[0].DoctorName)
	assert.Equal(t, "2026-07-01T00:00:00Z", list[0].CreatedAt)
}

func TestListPatientsInvalidPaging(t *testing.T) {
	e := newEnv(t, true, true)
	for _, qs := range []string{"page=0", "page=abc", "pageSize=0", "pageSize=101", "pageSize=-1"} {
		w, resp := e.do(http.MethodGet, "/api/v1/admin/patients?"+qs, nil, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code, qs)
		assert.Equal(t, model.CodeInvalidParam, resp.Code, qs)
	}
	e.store.patientsErr = errors.New("db")
	w, _ := e.do(http.MethodGet, "/api/v1/admin/patients", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetPatient(t *testing.T) {
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p
	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients/P20260001", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var dto model.AdminPatientDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "P20260001", dto.PatientID)

	e.store.patient = nil
	w, resp = e.do(http.MethodGet, "/api/v1/admin/patients/P-XX", nil, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	e.store.patientErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/admin/patients/P-XX", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ─────────────────────────────────────────────────────────────
// 团队 / 医生 / 成员（T030 #10）
// ─────────────────────────────────────────────────────────────

func TestListTeams(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teams = []repo.TeamRow{{TeamID: "TEAM01", Name: "一组", MemberCount: 2, PatientCount: 3}}
	w, resp := e.do(http.MethodGet, "/api/v1/teams", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var list []model.TeamDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, 3, list[0].PatientCount)

	e.store.teamsErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/teams", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetTeamMembers(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.doctors = []repo.DoctorRow{{DoctorID: "D1", Name: "医生甲", Status: "enabled"}}
	e.store.teamTechs = []repo.TechnicianRow{{TechID: "T1", Name: "技师甲", Status: "enabled", AuthStatus: "authorized"}}

	w, resp := e.do(http.MethodGet, "/api/v1/teams/TEAM01/members", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var members model.TeamMembersDTO
	require.NoError(t, json.Unmarshal(resp.Data, &members))
	require.Len(t, members.Doctors, 1)
	require.Len(t, members.Technicians, 1)

	// 团队不存在 → 404
	e.store.teamExists = false
	w, resp = e.do(http.MethodGet, "/api/v1/teams/NOPE/members", nil, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	// 各查询错误分支 → 500
	e.store.teamExists = true
	e.store.teamErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/teams/X/members", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.teamErr = nil
	e.store.doctorsErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/teams/X/members", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.doctorsErr = nil
	e.store.teamTechsErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/teams/X/members", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListDoctors(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.doctors = []repo.DoctorRow{{DoctorID: "D1", Name: "医生甲", Title: strPtr("主任医师"), Status: "enabled", PatientCount: 5}}
	w, resp := e.do(http.MethodGet, "/api/v1/doctors", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var list []model.DoctorDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "主任医师", list[0].Title)
	assert.Equal(t, 5, list[0].PatientCount)

	e.store.doctorsErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/doctors", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ─────────────────────────────────────────────────────────────
// 技师（T030 #4）
// ─────────────────────────────────────────────────────────────

func TestListTechnicians(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.techs = []repo.TechnicianRow{{TechID: "T1", Name: "技师甲", Status: "enabled", AuthStatus: "authorized"}}
	e.store.techTotal = 1
	w, resp := e.do(http.MethodGet, "/api/v1/technicians?page=1&pageSize=5", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var page model.PageData
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	assert.Equal(t, int64(1), page.Total)

	w, _ = e.do(http.MethodGet, "/api/v1/technicians?page=0", nil, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	e.store.techsErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/technicians", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateTechnician(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.createdTech = &repo.TechnicianRow{TechID: "TECH-NEW", Name: "新技师", TeamID: strPtr("TEAM01"), Status: "enabled", AuthStatus: "authorized"}

	w, resp := e.do(http.MethodPost, "/api/v1/admin/technicians",
		map[string]any{"name": "新技师", "phone": "13800001111", "teamId": "TEAM01"}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, e.store.lastTechInput.TechID)
	assert.NotEmpty(t, e.store.lastTechInput.PhoneHash)
	require.NotEmpty(t, e.store.lastTechInput.PhoneEnc)
	var dto model.TechnicianDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "TECH-NEW", dto.TechID)

	// 参数校验分支
	w, _ = e.do(http.MethodPost, "/api/v1/admin/technicians", map[string]any{"phone": "13800001111"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = e.do(http.MethodPost, "/api/v1/admin/technicians", map[string]any{"name": "x", "phone": "123"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 团队不存在 → 400
	e.store.teamExists = false
	w, resp = e.do(http.MethodPost, "/api/v1/admin/technicians",
		map[string]any{"name": "x", "phone": "13800001111", "teamId": "NOPE"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	e.store.teamExists = true
	e.store.teamErr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/admin/technicians",
		map[string]any{"name": "x", "phone": "13800001111", "teamId": "T"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.teamErr = nil

	// 手机号重复 → 409
	e.store.phoneTaken = true
	w, resp = e.do(http.MethodPost, "/api/v1/admin/technicians",
		map[string]any{"name": "x", "phone": "13800001111"}, nil)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, model.CodeConflict, resp.Code)
	e.store.phoneTaken = false
	e.store.takenErr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/admin/technicians",
		map[string]any{"name": "x", "phone": "13800001111"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.takenErr = nil

	// store 创建错误 → 500
	e.store.createErr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/admin/technicians",
		map[string]any{"name": "x", "phone": "13800001111"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.createErr = nil

	// body 非法 → 400
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/technicians", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateTechnicianWithoutPhoneKey(t *testing.T) {
	e := newEnv(t, true, false) // PHONE_ENC_KEY 未配置
	e.store.teamExists = true
	w, resp := e.do(http.MethodPost, "/api/v1/admin/technicians",
		map[string]any{"name": "x", "phone": "13800001111"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)
}

func TestUpdateTechnician(t *testing.T) {
	e := newEnv(t, true, true)
	cipher, _ := phone.NewCipher(testPhoneKey)
	enc, _ := cipher.Encrypt("13900002222")
	existing := &repo.TechnicianRow{TechID: "T1", Name: "旧名", PhoneEnc: enc,
		PhoneHash: phone.Hash("13900002222"), TeamID: strPtr("TEAM01"), Status: "enabled", AuthStatus: "authorized"}
	e.store.tech = existing
	e.store.teamExists = true
	e.store.updatedTech = existing

	// 仅改名：phone 保持原密文与哈希
	w, resp := e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"name": "新名"}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "新名", e.store.lastTechInput.Name)
	assert.Equal(t, phone.Hash("13900002222"), e.store.lastTechInput.PhoneHash)
	var dto model.TechnicianDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "139****2222", dto.PhoneMasked)

	// 换号：新 hash 生效
	w, _ = e.do(http.MethodPut, "/api/v1/admin/technicians/T1",
		map[string]any{"name": "新名", "phone": "13700003333"}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, phone.Hash("13700003333"), e.store.lastTechInput.PhoneHash)

	// 换号非法 → 400
	w, _ = e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"phone": "abc"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 换号查重 → 409
	e.store.phoneTaken = true
	w, _ = e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"phone": "13700003333"}, nil)
	assert.Equal(t, http.StatusConflict, w.Code)
	e.store.phoneTaken = false
	e.store.takenErr = errors.New("db")
	w, _ = e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"phone": "13700003333"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.takenErr = nil

	// 不存在 → 404
	e.store.tech = nil
	w, resp = e.do(http.MethodPut, "/api/v1/admin/technicians/NOPE", map[string]any{"name": "x"}, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	// store 错误 → 500
	e.store.techErr = errors.New("db")
	w, _ = e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"name": "x"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.techErr = nil

	// 更新返回 nil（并发删除）→ 404
	e.store.tech = existing
	e.store.updatedTech = nil
	w, _ = e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"name": "x"}, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	e.store.updatedTech = existing
	e.store.updateErr = errors.New("db")
	w, _ = e.do(http.MethodPut, "/api/v1/admin/technicians/T1", map[string]any{"name": "x"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// 团队校验 → 400 / 500
	e.store.teamExists = false
	w, _ = e.do(http.MethodPut, "/api/v1/admin/technicians/T1",
		map[string]any{"name": "x", "teamId": "NOPE"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// body 非法 → 400
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/technicians/T1", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestToggleTechnician(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.toggleExists = true

	w, _ := e.do(http.MethodPost, "/api/v1/technicians/T1/toggle", map[string]string{"action": "disable"}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "disabled", e.store.lastToggle)

	w, _ = e.do(http.MethodPost, "/api/v1/technicians/T1/toggle", map[string]string{"action": "enable"}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "enabled", e.store.lastToggle)

	w, resp := e.do(http.MethodPost, "/api/v1/technicians/T1/toggle", map[string]string{"action": "reboot"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)

	e.store.toggleExists = false
	w, resp = e.do(http.MethodPost, "/api/v1/technicians/NOPE/toggle", map[string]string{"action": "enable"}, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	e.store.toggleErr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/technicians/T1/toggle", map[string]string{"action": "enable"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ─────────────────────────────────────────────────────────────
// 反馈（T030 #5）
// ─────────────────────────────────────────────────────────────

func TestListFeedbacks(t *testing.T) {
	e := newEnv(t, true, true)
	reply := "已回复"
	replyTime := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	e.store.feedbacks = []repo.FeedbackRow{{
		FeedbackID: 1, PatientID: "P1", Type: strPtr("佩戴咨询"), Content: "疼",
		SubmitTime:   time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
		ReplyContent: &reply, ReplyTime: &replyTime, Status: "replied",
	}}
	w, resp := e.do(http.MethodGet, "/api/v1/feedbacks?keyword=疼", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var list []model.FeedbackDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "1", list[0].FeedbackID)
	assert.Equal(t, "已回复", *list[0].ReplyContent)
	assert.Equal(t, "2026-08-01T00:00:00Z", *list[0].ReplyTime)

	e.store.feedbacksErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/feedbacks", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestProcessFeedback(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.processOK = true

	w, _ := e.do(http.MethodPost, "/api/v1/feedbacks/12/process",
		map[string]string{"replyContent": "正常现象"}, map[string]string{"X-User-Id": "A0003"})
	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, e.store.lastProcessR)
	assert.Equal(t, "正常现象", *e.store.lastProcessR)

	// 无 body 亦可（仅标记处理）
	w, _ = e.do(http.MethodPost, "/api/v1/feedbacks/12/process", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, e.store.lastProcessR)

	// 超长回复 → 400
	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks/12/process",
		map[string]string{"replyContent": strings.Repeat("长", 501)}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)

	// 非法 ID → 400
	w, _ = e.do(http.MethodPost, "/api/v1/feedbacks/abc/process", map[string]string{}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = e.do(http.MethodPost, "/api/v1/feedbacks/0/process", map[string]string{}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 不存在 → 404
	e.store.processOK = false
	w, resp = e.do(http.MethodPost, "/api/v1/feedbacks/99/process", map[string]string{}, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	e.store.processErr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/feedbacks/12/process", map[string]string{}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ─────────────────────────────────────────────────────────────
// 矫形方案 / 感受日志（T030 #6）
// ─────────────────────────────────────────────────────────────

func TestListPlansAndSave(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.plans = []repo.OrthosisPlanRow{{
		PlanID: 9, PatientID: "P1", DoctorID: "D1", Content: "方案A", Version: "v1.2",
		CreatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}}
	w, resp := e.do(http.MethodGet, "/api/v1/patients/P1/orthosis-plans", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var list []model.OrthosisPlanDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "9", list[0].PlanID)

	e.store.plansErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/patients/P1/orthosis-plans", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.plansErr = nil

	// 保存：医生身份 + 版本递增
	p := samplePatient()
	e.store.patient = &p
	e.store.doctorFound = true
	e.store.doctorID = "D0001"
	e.store.latest = "v1.2"
	e.store.hasLatest = true
	e.store.createdPlan = &repo.OrthosisPlanRow{PlanID: 10, PatientID: "P20260001", DoctorID: "D0001", Content: "新方案"}

	w, resp = e.do(http.MethodPost, "/api/v1/patients/P20260001/orthosis-plans",
		map[string]string{"content": "新方案"}, map[string]string{"X-User-Id": "A0002"})
	assert.Equal(t, http.StatusOK, w.Code)
	var saved model.OrthosisPlanDTO
	require.NoError(t, json.Unmarshal(resp.Data, &saved))
	assert.Equal(t, "v1.3", saved.Version)

	// 无历史 → v1.0
	e.store.hasLatest = false
	e.store.createdPlan = &repo.OrthosisPlanRow{PlanID: 11, PatientID: "P20260001", DoctorID: "D0001", Content: "首版"}
	w, resp = e.do(http.MethodPost, "/api/v1/patients/P20260001/orthosis-plans",
		map[string]string{"content": "首版"}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(resp.Data, &saved))
	assert.Equal(t, "v1.0", saved.Version)

	// 空内容 → 400
	w, _ = e.do(http.MethodPost, "/api/v1/patients/P20260001/orthosis-plans", map[string]string{"content": "  "}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	// 超长 → 400
	w, _ = e.do(http.MethodPost, "/api/v1/patients/P20260001/orthosis-plans",
		map[string]string{"content": strings.Repeat("方", 2001)}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	// 患者不存在 → 404
	e.store.patient = nil
	w, resp = e.do(http.MethodPost, "/api/v1/patients/NOPE/orthosis-plans", map[string]string{"content": "x"}, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)
	// 非医生身份 → 403
	e.store.patient = &p
	e.store.doctorFound = false
	w, resp = e.do(http.MethodPost, "/api/v1/patients/P20260001/orthosis-plans", map[string]string{"content": "x"}, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	// 错误分支 → 500
	e.store.patientErr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/patients/P1/orthosis-plans", map[string]string{"content": "x"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.patientErr = nil
	e.store.doctorFound = true
	e.store.doctorErr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/patients/P1/orthosis-plans", map[string]string{"content": "x"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.doctorErr = nil
	e.store.latestErr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/patients/P1/orthosis-plans", map[string]string{"content": "x"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.latestErr = nil
	e.store.createPlanEr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/patients/P1/orthosis-plans", map[string]string{"content": "x"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFeelingLogsAndReply(t *testing.T) {
	e := newEnv(t, true, true)
	score := 4.0
	reply := "建议观察"
	replyTime := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	e.store.feelings = []repo.FeelingLogRow{{
		LogID: 5, PatientID: "P1", LogDate: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		ComfortScore: &score, DiscomfortAreas: nil, Notes: nil,
		ReplyContent: &reply, ReplyTime: &replyTime,
	}}
	w, resp := e.do(http.MethodGet, "/api/v1/patients/P1/feeling-logs", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var list []model.FeelingLogDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "5", list[0].LogID)
	assert.Equal(t, "2026-08-01", list[0].LogDate)
	assert.NotNil(t, list[0].DiscomfortAreas)
	assert.Empty(t, list[0].DiscomfortAreas)

	e.store.feelingsErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/patients/P1/feeling-logs", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.feelingsErr = nil

	// 回复（T030 #6 写入契约）
	e.store.replyOK = true
	w, _ = e.do(http.MethodPost, "/api/v1/feeling-logs/5/reply", map[string]string{"replyContent": "建议观察"}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "建议观察", e.store.lastReply)

	w, _ = e.do(http.MethodPost, "/api/v1/feeling-logs/abc/reply", map[string]string{"replyContent": "x"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = e.do(http.MethodPost, "/api/v1/feeling-logs/5/reply", map[string]string{"replyContent": ""}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = e.do(http.MethodPost, "/api/v1/feeling-logs/5/reply",
		map[string]string{"replyContent": strings.Repeat("a", 201)}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	e.store.replyOK = false
	w, resp = e.do(http.MethodPost, "/api/v1/feeling-logs/99/reply", map[string]string{"replyContent": "x"}, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	e.store.replyErr = errors.New("db")
	w, _ = e.do(http.MethodPost, "/api/v1/feeling-logs/5/reply", map[string]string{"replyContent": "x"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ─────────────────────────────────────────────────────────────
// RBAC 角色 / 权限矩阵（T030 #7）
// ─────────────────────────────────────────────────────────────

func TestListRoles(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.roles = []repo.RoleRow{
		{RoleID: "ROLE_ADMIN", Name: "运营管理员", Description: strPtr("全量"), PermissionsJSON: `{"scope":"all"}`,
			Status: "enabled", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), MemberCount: 3},
		{RoleID: "ROLE_CUSTOM", Name: "自定义", PermissionsJSON: `{}`, Status: "enabled",
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	w, resp := e.do(http.MethodGet, "/api/v1/admin/roles", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var list []model.AdminRoleDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 2)
	assert.True(t, list[0].Preset)
	assert.False(t, list[1].Preset)
	assert.Equal(t, 3, list[0].MemberCount)

	e.store.rolesErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/admin/roles", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetPermissions(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.role = &repo.RoleRow{RoleID: "ROLE_ADMIN", PermissionsJSON: `{"scope":"all","modules":["dashboard"]}`}

	w, resp := e.do(http.MethodGet, "/api/v1/admin/roles/ROLE_ADMIN/permissions", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var perms model.RolePermissionsDTO
	require.NoError(t, json.Unmarshal(resp.Data, &perms))
	assert.Equal(t, "all", perms.Scope)
	assert.Equal(t, []string{"dashboard"}, perms.Modules)

	e.store.role = nil
	w, resp = e.do(http.MethodGet, "/api/v1/admin/roles/NOPE/permissions", nil, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	e.store.role = &repo.RoleRow{RoleID: "R", PermissionsJSON: `{bad`}
	w, _ = e.do(http.MethodGet, "/api/v1/admin/roles/R/permissions", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	e.store.roleErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/admin/roles/R/permissions", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUpdatePermissions(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.updRoleOK = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_DOCTOR/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts", "orthosis"}}, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, e.store.lastPermJSON, `"scope":"team"`)
	var perms model.RolePermissionsDTO
	require.NoError(t, json.Unmarshal(resp.Data, &perms))
	assert.Equal(t, "team", perms.Scope)

	w, _ = e.do(http.MethodPut, "/api/v1/admin/roles/R/permissions", map[string]any{"scope": "galaxy", "modules": []string{"a"}}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = e.do(http.MethodPut, "/api/v1/admin/roles/R/permissions", map[string]any{"scope": "team", "modules": []string{}}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	e.store.updRoleOK = false
	w, resp = e.do(http.MethodPut, "/api/v1/admin/roles/NOPE/permissions",
		map[string]any{"scope": "team", "modules": []string{"a"}}, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	e.store.updRoleErr = errors.New("db")
	w, _ = e.do(http.MethodPut, "/api/v1/admin/roles/R/permissions",
		map[string]any{"scope": "team", "modules": []string{"a"}}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ─────────────────────────────────────────────────────────────
// 系统参数（T030 #8）
// ─────────────────────────────────────────────────────────────

func TestGetSettings(t *testing.T) {
	e := newEnv(t, true, true)
	// 全缺失 → 默认值
	w, resp := e.do(http.MethodGet, "/api/v1/admin/settings", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var dto model.SystemSettingsDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, 22.0, dto.DailyWearTargetHours)
	assert.Equal(t, 45.0, dto.PressureHighThresholdN)
	assert.Equal(t, 30.0, dto.PressureFluctuationPct)
	assert.Equal(t, 60.0, dto.WearInterruptMinutes)
	assert.Equal(t, 2.8, dto.SensorDriftN)
	assert.Empty(t, dto.WifiPresets)

	// 有值 + 密码脱敏
	e.store.configs = map[string]string{
		keyWearTarget: "20", keyPressureHigh: "50", keyFluctuationPct: "25",
		keyWearInterrupt: "90", keySensorDrift: "3",
		keyWifiPresets: `[{"ssid":"ClinicWiFi","password":"secret123"}]`,
	}
	w, resp = e.do(http.MethodGet, "/api/v1/admin/settings", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, 20.0, dto.DailyWearTargetHours)
	require.Len(t, dto.WifiPresets, 1)
	assert.Equal(t, "********", dto.WifiPresets[0].Password)

	// 非法 JSON → 空列表；非法数值 → 默认值
	e.store.configs = map[string]string{keyWifiPresets: `{bad`, keyWearTarget: "abc"}
	w, resp = e.do(http.MethodGet, "/api/v1/admin/settings", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Empty(t, dto.WifiPresets)
	assert.Equal(t, 22.0, dto.DailyWearTargetHours)

	e.store.configsErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/admin/settings", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func validSettingsBody() map[string]any {
	return map[string]any{
		"dailyWearTargetHours": 22, "pressureHighThresholdN": 45, "pressureFluctuationPct": 30,
		"wearInterruptMinutes": 60, "sensorDriftN": 2.8,
		"wifiPresets": []map[string]any{{"ssid": "ClinicWiFi", "password": "newpass"}},
	}
}

func TestUpdateSettings(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings", validSettingsBody(), map[string]string{"X-User-Id": "A0001"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "A0001", e.store.lastUpsertBy)
	require.Len(t, e.store.lastUpsert, 6)
	// 响应中密码脱敏
	var dto model.SystemSettingsDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "********", dto.WifiPresets[0].Password)
	// 落库保留真实密码
	var wifiKV repo.ConfigKV
	for _, kv := range e.store.lastUpsert {
		if kv.Key == keyWifiPresets {
			wifiKV = kv
		}
	}
	assert.Contains(t, wifiKV.Value, "newpass")

	// 脱敏占位回写时保留既有密码
	e.store.configs = map[string]string{
		keyCollectInterval: "30",
		keyWifiPresets:     `[{"ssid":"ClinicWiFi","password":"oldpass"}]`,
	}
	body := validSettingsBody()
	body["wifiPresets"] = []map[string]any{{"ssid": "ClinicWiFi", "password": "********"}}
	w, _ = e.do(http.MethodPut, "/api/v1/admin/settings", body, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	for _, kv := range e.store.lastUpsert {
		if kv.Key == keyWifiPresets {
			assert.Contains(t, kv.Value, "oldpass")
		}
	}

	// 范围校验分支
	cases := []map[string]any{
		mergeBody(validSettingsBody(), "dailyWearTargetHours", 0),
		mergeBody(validSettingsBody(), "dailyWearTargetHours", 25),
		mergeBody(validSettingsBody(), "pressureHighThresholdN", 0),
		mergeBody(validSettingsBody(), "pressureHighThresholdN", 201),
		mergeBody(validSettingsBody(), "pressureFluctuationPct", 0),
		mergeBody(validSettingsBody(), "pressureFluctuationPct", 101),
		mergeBody(validSettingsBody(), "wearInterruptMinutes", 9),
		mergeBody(validSettingsBody(), "wearInterruptMinutes", 721),
		mergeBody(validSettingsBody(), "sensorDriftN", 0.05),
		mergeBody(validSettingsBody(), "sensorDriftN", 21),
	}
	for i, c := range cases {
		w, resp = e.do(http.MethodPut, "/api/v1/admin/settings", c, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code, "case %d", i)
		assert.Equal(t, model.CodeInvalidParam, resp.Code, "case %d", i)
	}

	// 中断阈值 < 2×采集间隔 → 400
	w, resp = e.do(http.MethodPut, "/api/v1/admin/settings",
		mergeBody(validSettingsBody(), "wearInterruptMinutes", 50), nil) // 50 < 2×30
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 空 ssid → 400
	bad := validSettingsBody()
	bad["wifiPresets"] = []map[string]any{{"ssid": ""}}
	w, _ = e.do(http.MethodPut, "/api/v1/admin/settings", bad, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 预设超 32 条 → 400
	tooMany := make([]map[string]any, 33)
	for i := range tooMany {
		tooMany[i] = map[string]any{"ssid": "s"}
	}
	bad = validSettingsBody()
	bad["wifiPresets"] = tooMany
	w, _ = e.do(http.MethodPut, "/api/v1/admin/settings", bad, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// store 错误分支 → 500
	e.store.configsErr = errors.New("db")
	w, _ = e.do(http.MethodPut, "/api/v1/admin/settings", validSettingsBody(), nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	e.store.configsErr = nil
	e.store.upsertErr = errors.New("db")
	w, _ = e.do(http.MethodPut, "/api/v1/admin/settings", validSettingsBody(), nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// body 非法 → 400
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func mergeBody(base map[string]any, key string, val any) map[string]any {
	out := make(map[string]any, len(base))
	for k, v := range base {
		out[k] = v
	}
	out[key] = val
	return out
}

// ─────────────────────────────────────────────────────────────
// 纯函数
// ─────────────────────────────────────────────────────────────

func TestNextPlanVersion(t *testing.T) {
	assert.Equal(t, "v1.0", nextPlanVersion("", false))
	assert.Equal(t, "v1.3", nextPlanVersion("v1.2", true))
	assert.Equal(t, "v2.1", nextPlanVersion("v2.0", true))
	assert.Equal(t, "v1.0", nextPlanVersion("weird", true))
	assert.Equal(t, "v1.0", nextPlanVersion("v1.x", true))
	assert.Equal(t, "v1.0", nextPlanVersion("v0.5", true)) // 主版本 <1 视为非法
}

func TestValidPhone(t *testing.T) {
	assert.True(t, validPhone("13800001111"))
	assert.False(t, validPhone("2380000111"))
	assert.False(t, validPhone("1380000111a"))
	assert.False(t, validPhone("138000011111"))
	assert.False(t, validPhone(""))
}

func TestNumOrAndParseWifiPresets(t *testing.T) {
	assert.Equal(t, 1.5, numOr("1.5", 0))
	assert.Equal(t, 9.0, numOr("", 9))
	assert.Equal(t, 9.0, numOr("bad", 9))
	assert.Empty(t, parseWifiPresets(""))
	assert.Empty(t, parseWifiPresets("{bad"))
	list := parseWifiPresets(`[{"ssid":"a","password":"b"}]`)
	require.Len(t, list, 1)
	assert.Equal(t, "a", list[0].Ssid)
}

func TestMaskAndMergeWifiPasswords(t *testing.T) {
	masked := maskWifiPasswords([]model.WifiPresetDTO{{Ssid: "a", Password: "p"}, {Ssid: "b"}})
	assert.Equal(t, "********", masked[0].Password)
	assert.Equal(t, "", masked[1].Password)

	stored := []model.WifiPresetDTO{{Ssid: "a", Password: "old"}}
	merged := mergeWifiPasswords(
		[]model.WifiPresetDTO{{Ssid: "a", Password: "********"}, {Ssid: "c", Password: "new"}}, stored)
	assert.Equal(t, "old", merged[0].Password)
	assert.Equal(t, "new", merged[1].Password)
}
