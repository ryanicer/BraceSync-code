// Package handler — T028 公开端点实现侧测试（不与测试专家 handler_test.go 路径重叠）
//
// 覆盖：GET /api/v1/alerts 参数校验/分页/筛选组合/DTO 映射；
// POST /api/v1/alerts/{alertId}/process 幂等/404/400。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	listHits int
	procHits int
}

func (s *fakePublicStore) ListAlerts(_ context.Context, f repo.AlertQueryFilter) ([]repo.AlertRow, int64, error) {
	s.filter = f
	s.listHits++
	return s.rows, s.total, s.listErr
}

func (s *fakePublicStore) ProcessAlert(_ context.Context, alertID int64) (bool, error) {
	s.procID = alertID
	s.procHits++
	return s.exists, s.procErr
}

// newPublicHandler 组装挂 fake store 的 Handler（evaluate 依赖用 nil 安全的最小装配）
func newPublicHandler(store PublicAlertStore) *Handler {
	h := New(nil, nil, nil)
	h.SetPublicStore(store)
	return h
}

func doGet(h *Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
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
