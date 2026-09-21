// Package handler — T028 公开端点实现侧测试（不与测试专家 handler_test.go 路径重叠）
//
// 覆盖：GET /api/v1/alerts 参数校验/分页/筛选组合/DTO 映射；
// POST /api/v1/alerts/{alertId}/process 幂等/404/400；
// POST /api/v1/alerts/{alertId}/processing（T257 2.7）幂等/409/404/400。
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

// ─────────────────────────────────────────────────────────────
// fake PublicAlertStore
// ─────────────────────────────────────────────────────────────

type fakePublicStore struct {
	filter   repo.AlertQueryFilter
	rows     []repo.AlertRow
	total    int64
	listErr  error
	exists   bool
	procErr  error
	procID   int64
	procOpID string
	procNote string // T278-①：handler 解析后的备注（空串 = 未提供）
	listHits int
	procHits int

	// T257 2.7 /processing
	state     repo.ProcessState
	startErr  error
	startID   int64
	startHits int
}

func (s *fakePublicStore) ListAlerts(_ context.Context, f repo.AlertQueryFilter) ([]repo.AlertRow, int64, error) {
	s.filter = f
	s.listHits++
	return s.rows, s.total, s.listErr
}

func (s *fakePublicStore) ProcessAlert(_ context.Context, alertID int64, operatorID, note string) (bool, error) {
	s.procID = alertID
	s.procOpID = operatorID
	s.procNote = note
	s.procHits++
	return s.exists, s.procErr
}

func (s *fakePublicStore) StartProcessing(_ context.Context, alertID int64) (repo.ProcessState, error) {
	s.startID = alertID
	s.startHits++
	if s.startErr != nil {
		return repo.ProcessState{}, s.startErr
	}
	st := s.state
	st.Exists = s.exists
	return st, nil
}

// newPublicHandler 组装挂 fake store 的 Handler（evaluate 依赖用 nil 安全的最小装配）
func newPublicHandler(store PublicAlertStore) *Handler {
	h := New(nil, nil, nil)
	h.SetPublicStore(store)
	return h
}

func doGet(h *Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	// 默认 staff 身份（ROLE_ADMIN），使 ?patientId= 等筛选参数生效；
	// 患者身份绑定另由 T264 专项测试覆盖。
	req.Header.Set(headerRole, roleAdmin)
	req.Header.Set(headerUserID, "ADMIN-001")
	h.Router().ServeHTTP(rec, req)
	return rec
}

func doProcess(h *Handler, alertID string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID+"/process", nil))
	return rec
}

type pageEnvelope struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    alertPageData `json:"data"`
}

// ─────────────────────────────────────────────────────────────
// GET /api/v1/alerts
// ─────────────────────────────────────────────────────────────

func TestListAlerts_DefaultsAndEnvelope(t *testing.T) {
	store := &fakePublicStore{total: 0}
	rec := doGet(newPublicHandler(store), "/api/v1/alerts")

	require.Equal(t, http.StatusOK, rec.Code)
	var env pageEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, 0, env.Code)
	assert.Equal(t, "success", env.Message)
	assert.Equal(t, 1, env.Data.Page, "缺省 page=1")
	assert.Equal(t, 20, env.Data.PageSize, "缺省 pageSize=20")
	assert.NotNil(t, env.Data.List, "空结果 list 为 [] 而非 null")
	assert.EqualValues(t, 0, env.Data.Total)
	assert.Equal(t, repo.AlertQueryFilter{Page: 1, PageSize: 20}, store.filter)
}

func TestListAlerts_FilterCombination(t *testing.T) {
	store := &fakePublicStore{}
	rec := doGet(newPublicHandler(store),
		"/api/v1/alerts?patientId=P001&type=wear_interrupt&status=pending&page=2&pageSize=5")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, repo.AlertQueryFilter{
		PatientID: "P001", Type: "wear_interrupt", Status: "pending", Page: 2, PageSize: 5,
	}, store.filter)
}

func TestListAlerts_AllTypesAccepted(t *testing.T) {
	for _, typ := range []string{"pressure_high", "pressure_fluctuation", "wear_interrupt", "sensor_drift"} {
		store := &fakePublicStore{}
		rec := doGet(newPublicHandler(store), "/api/v1/alerts?type="+typ)
		assert.Equal(t, http.StatusOK, rec.Code, "type=%s 应合法", typ)
		assert.Equal(t, typ, store.filter.Type)
	}
	for _, status := range []string{"pending", "processed"} {
		store := &fakePublicStore{}
		rec := doGet(newPublicHandler(store), "/api/v1/alerts?status="+status)
		assert.Equal(t, http.StatusOK, rec.Code, "status=%s 应合法", status)
	}
}

func TestListAlerts_InvalidParams(t *testing.T) {
	cases := []struct {
		name   string
		target string
	}{
		{"非法 type", "/api/v1/alerts?type=bogus"},
		{"非法 status", "/api/v1/alerts?status=done"},
		{"page 非数字", "/api/v1/alerts?page=x"},
		{"page 越界", "/api/v1/alerts?page=0"},
		{"pageSize 非数字", "/api/v1/alerts?pageSize=x"},
		{"pageSize 越上界", "/api/v1/alerts?pageSize=101"},
		{"pageSize 为 0", "/api/v1/alerts?pageSize=0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakePublicStore{}
			rec := doGet(newPublicHandler(store), tc.target)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			var env envelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			assert.Equal(t, codeInvalidParam, env.Code)
			assert.Zero(t, store.listHits, "参数校验失败不应触达存储")
		})
	}
}

func TestListAlerts_DTOMapping(t *testing.T) {
	ts := time.Date(2026, 8, 11, 6, 30, 0, 0, time.UTC)
	resolvedAt := ts.Add(time.Hour)
	processedAt := ts.Add(2 * time.Hour)
	by := "tech01"
	note := "已电话确认"
	store := &fakePublicStore{
		total: 1,
		rows: []repo.AlertRow{{
			AlertID: 42, PatientID: "P001", PatientName: "林小雨", DeviceID: "DEV01", Type: "pressure_high",
			Detail: "P03 超阈值", SensorPoint: "P03", ThresholdValue: 45, ActualValue: 52.5,
			Ts: ts, ReadStatus: "unread", ProcessStatus: "processed", ResolvedStatus: "active",
			ResolvedAt: &resolvedAt, ProcessedBy: &by, ProcessedAt: &processedAt, ProcessNote: &note,
		}},
	}
	rec := doGet(newPublicHandler(store), "/api/v1/alerts")
	require.Equal(t, http.StatusOK, rec.Code)

	var env pageEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Len(t, env.Data.List, 1)
	item := env.Data.List[0]
	// 字段名对齐 shared-types Alert
	assert.Equal(t, "42", item.AlertID, "alertId 字符串化")
	assert.Equal(t, "P001", item.PatientID)
	assert.Equal(t, "林小雨", item.PatientName)
	assert.Equal(t, "DEV01", item.DeviceID)
	assert.Equal(t, "pressure_high", item.Type)
	assert.Equal(t, "P03", item.SensorPoint)
	assert.InDelta(t, 45.0, item.ThresholdValue, 0.001)
	assert.InDelta(t, 52.5, item.ActualValue, 0.001)
	assert.Equal(t, "2026-08-11T06:30:00Z", item.Timestamp)
	assert.Equal(t, "unread", item.ReadStatus)
	assert.Equal(t, "processed", item.ProcessStatus)
	assert.Equal(t, "active", item.ResolvedStatus)
	require.NotNil(t, item.ResolvedAt)
	assert.Equal(t, "2026-08-11T07:30:00Z", *item.ResolvedAt)
	require.NotNil(t, item.ProcessedBy)
	assert.Equal(t, "tech01", *item.ProcessedBy)
	require.NotNil(t, item.ProcessedAt)
	assert.Equal(t, "2026-08-11T08:30:00Z", *item.ProcessedAt)
	require.NotNil(t, item.ProcessNote)
	assert.Equal(t, "已电话确认", *item.ProcessNote)
}

func TestListAlerts_NullableFieldsJSONNull(t *testing.T) {
	store := &fakePublicStore{
		total: 1,
		rows:  []repo.AlertRow{{AlertID: 1, Ts: time.Unix(0, 0).UTC()}},
	}
	rec := doGet(newPublicHandler(store), "/api/v1/alerts")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"resolvedAt":null`)
	assert.Contains(t, rec.Body.String(), `"processedBy":null`)
	assert.Contains(t, rec.Body.String(), `"processedAt":null`)
	assert.Contains(t, rec.Body.String(), `"processNote":null`)
}

func TestListAlerts_StoreError(t *testing.T) {
	store := &fakePublicStore{listErr: errors.New("db down")}
	rec := doGet(newPublicHandler(store), "/api/v1/alerts")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, codeInternalError, env.Code)
}

func TestListAlerts_NilStore(t *testing.T) {
	h := New(nil, nil, nil) // 未注入 public store
	rec := doGet(h, "/api/v1/alerts")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ─────────────────────────────────────────────────────────────
// POST /api/v1/alerts/{alertId}/process
// ─────────────────────────────────────────────────────────────

func TestProcessAlert_Success(t *testing.T) {
	store := &fakePublicStore{exists: true}
	rec := doProcess(newPublicHandler(store), "42")

	require.Equal(t, http.StatusOK, rec.Code)
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, codeSuccess, env.Code)
	assert.EqualValues(t, 42, store.procID)
}

func TestProcessAlert_Idempotent(t *testing.T) {
	store := &fakePublicStore{exists: true} // 已处理记录 repo 同样返回 exists=true
	h := newPublicHandler(store)

	first := doProcess(h, "7")
	second := doProcess(h, "7")
	assert.Equal(t, http.StatusOK, first.Code, "首次处理成功")
	assert.Equal(t, http.StatusOK, second.Code, "重复处理不报错（幂等）")
	assert.Equal(t, 2, store.procHits)
}

func TestProcessAlert_NotFound(t *testing.T) {
	store := &fakePublicStore{exists: false}
	rec := doProcess(newPublicHandler(store), "999")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, codeNotFound, env.Code)
}

func TestProcessAlert_InvalidID(t *testing.T) {
	for _, id := range []string{"abc", "0", "-3", "1.5"} {
		store := &fakePublicStore{exists: true}
		rec := doProcess(newPublicHandler(store), id)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "alertId=%s 应 400", id)
		assert.Zero(t, store.procHits, "非法 ID 不触达存储")
	}
}

func TestProcessAlert_StoreError(t *testing.T) {
	store := &fakePublicStore{procErr: errors.New("db down")}
	rec := doProcess(newPublicHandler(store), "42")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestProcessAlert_NilStore(t *testing.T) {
	h := New(nil, nil, nil)
	rec := doProcess(h, "42")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// processAlert 把 gateway 注入的 X-User-Id 透传给 repo 作 processed_by（T257 2.7）
func TestProcessAlert_PassesOperatorID(t *testing.T) {
	store := &fakePublicStore{exists: true}
	h := newPublicHandler(store)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/42/process", nil)
	req.Header.Set(headerUserID, "DOCTOR-007")
	h.Router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "DOCTOR-007", store.procOpID)
}

// doProcessBody 带请求体调用处理端点（T278-①：{note} 随体提交）
func doProcessBody(h *Handler, alertID, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID+"/process", strings.NewReader(body))
	req.Header.Set(headerUserID, "DOCTOR-007")
	h.Router().ServeHTTP(rec, req)
	return rec
}

// T278-① D4：前端随体提交的 note 必须透传给 repo（此前 handler 直接丢掉 body）
func TestProcessAlert_NotePassedToStore(t *testing.T) {
	store := &fakePublicStore{exists: true}
	rec := doProcessBody(newPublicHandler(store), "205", `{"note":"已电话指导患者调整佩戴位置"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "已电话指导患者调整佩戴位置", store.procNote)
	assert.Equal(t, 1, store.procHits)
}

// 兼容（派发单硬要求）：无 body 时行为不变 —— note 传空串，repo 侧不覆盖 process_note
func TestProcessAlert_NoBodyKeepsBehavior(t *testing.T) {
	store := &fakePublicStore{exists: true}
	rec := doProcess(newPublicHandler(store), "205") // body = nil

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", store.procNote, "无 body ⇒ 空串（不写列）")
	assert.Equal(t, 1, store.procHits)
}

// 空 body 的几种等价写法：都不该报错，也不该产生备注
func TestProcessAlert_BlankBodies(t *testing.T) {
	for name, body := range map[string]string{
		"零字节":       "",
		"空白":        "   ",
		"空对象":       "{}",
		"note 空串":   `{"note":""}`,
		"note 全空格":  `{"note":"   "}`,
		"note null": `{"note":null}`,
	} {
		store := &fakePublicStore{exists: true}
		rec := doProcessBody(newPublicHandler(store), "205", body)
		assert.Equal(t, http.StatusOK, rec.Code, "%s 应放行", name)
		assert.Equal(t, "", store.procNote, "%s 应归一为空串", name)
	}
}

// 非法 JSON 早退：400 且不触达存储（不能让状态被悄悄改掉）
func TestProcessAlert_InvalidJSON(t *testing.T) {
	for _, body := range []string{`{`, `not json`, `{"note":`} {
		store := &fakePublicStore{exists: true}
		rec := doProcessBody(newPublicHandler(store), "205", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "body=%q 应 400", body)
		assert.Zero(t, store.procHits, "body=%q 不应触达存储", body)
	}
}

// process_note 是 VARCHAR(512)：超长必须 400，而不是让 PG 抛 22001 变 500
func TestProcessAlert_NoteLengthBoundary(t *testing.T) {
	store := &fakePublicStore{exists: true}
	rec := doProcessBody(newPublicHandler(store), "205", `{"note":"`+strings.Repeat("字", 512)+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, "512 字符（含中文）恰好合法")
	assert.Equal(t, 512, len([]rune(store.procNote)))

	over := &fakePublicStore{exists: true}
	rec = doProcessBody(newPublicHandler(over), "205", `{"note":"`+strings.Repeat("字", 513)+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "513 字符应 400")
	assert.Zero(t, over.procHits, "超长不应触达存储")
}

// ─────────────────────────────────────────────────────────────
// POST /api/v1/alerts/{alertId}/processing（T257 2.7）
// ─────────────────────────────────────────────────────────────

func doStartProcessing(h *Handler, alertID string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID+"/processing", nil)
	req.Header.Set(headerUserID, "DOCTOR-001")
	h.Router().ServeHTTP(rec, req)
	return rec
}

func TestStartProcessing_PendingToProcessing(t *testing.T) {
	at := time.Date(2026, 9, 20, 3, 4, 5, 0, time.UTC)
	store := &fakePublicStore{exists: true, state: repo.ProcessState{Status: "processing", InProgressAt: &at}}
	rec := doStartProcessing(newPublicHandler(store), "42")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.EqualValues(t, 42, store.startID)
	assert.Contains(t, rec.Body.String(), `"processStatus":"processing"`)
	assert.Contains(t, rec.Body.String(), `"inProgressAt":"2026-09-20T03:04:05Z"`)
}

// 已 processing 再点一次：200 + 状态不变（repo 不刷新 in_progress_at，见 repo 层测试）
func TestStartProcessing_Idempotent(t *testing.T) {
	at := time.Date(2026, 9, 20, 3, 4, 5, 0, time.UTC)
	store := &fakePublicStore{exists: true, state: repo.ProcessState{Status: "processing", InProgressAt: &at}}
	h := newPublicHandler(store)

	first := doStartProcessing(h, "7")
	second := doStartProcessing(h, "7")
	assert.Equal(t, http.StatusOK, first.Code)
	assert.Equal(t, http.StatusOK, second.Code)
	assert.Equal(t, 2, store.startHits)
}

func TestStartProcessing_AlreadyProcessedIsConflict(t *testing.T) {
	store := &fakePublicStore{exists: true, state: repo.ProcessState{Status: "processed"}}
	rec := doStartProcessing(newPublicHandler(store), "42")

	assert.Equal(t, http.StatusConflict, rec.Code)
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, codeConflict, env.Code)
}

func TestStartProcessing_NotFound(t *testing.T) {
	store := &fakePublicStore{exists: false}
	rec := doStartProcessing(newPublicHandler(store), "999")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestStartProcessing_InvalidID(t *testing.T) {
	for _, id := range []string{"abc", "0", "-3"} {
		store := &fakePublicStore{exists: true}
		rec := doStartProcessing(newPublicHandler(store), id)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "alertId=%s 应 400", id)
		assert.Zero(t, store.startHits, "非法 ID 不触达存储")
	}
}

func TestStartProcessing_StoreError(t *testing.T) {
	store := &fakePublicStore{startErr: errors.New("db down")}
	rec := doStartProcessing(newPublicHandler(store), "42")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestStartProcessing_NilStore(t *testing.T) {
	h := New(nil, nil, nil)
	rec := doStartProcessing(h, "42")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// inProgressAt 为 null（历史行从未进入处理中）时响应仍为 null 而非缺字段
func TestStartProcessing_NullInProgressAt(t *testing.T) {
	store := &fakePublicStore{exists: true, state: repo.ProcessState{Status: "processing"}}
	rec := doStartProcessing(newPublicHandler(store), "42")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"inProgressAt":""`)
}

// 方法边界：GET 到 process 路由 / POST 到 list 路由均不匹配（405/404）
func TestPublicRoutes_MethodMismatch(t *testing.T) {
	h := newPublicHandler(&fakePublicStore{})

	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/alerts", nil))
	assert.Contains(t, []int{http.StatusMethodNotAllowed, http.StatusNotFound}, rec.Code)

	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/alerts/1/process", nil))
	assert.Contains(t, []int{http.StatusMethodNotAllowed, http.StatusNotFound}, rec.Code)
}
