// Package handler T076：患者日佩戴聚合端点实现侧测试（路由/参数/水平鉴权/nil-querier）
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

func init() { gin.SetMode(gin.TestMode) }

// fakeDailyWearQuerier DailyWearQuerier 内存实现（用于端点测试）
type fakeDailyWearQuerier struct {
	list      []*model.DailyWearDayDTO
	err       *model.AppError
	lastPID   string
	lastStart string
	lastEnd   string
}

func (f *fakeDailyWearQuerier) GetDailyWear(_ context.Context, pid, start, end string) ([]*model.DailyWearDayDTO, *model.AppError) {
	f.lastPID = pid
	f.lastStart = start
	f.lastEnd = end
	if f.err != nil {
		return nil, f.err
	}
	if start == "not-a-date" {
		return nil, model.ErrQueryParam("invalid start date %q (expect YYYY-MM-DD)", start)
	}
	return f.list, nil
}

func TestGetDailyWear_HappyPath(t *testing.T) {
	q := &fakeDailyWearQuerier{
		list: []*model.DailyWearDayDTO{
			{Date: "2026-09-01", WearMinutes: 1200, FrameCount: 40, MaxPoint: "P05"},
			{Date: "2026-09-02", WearMinutes: 360, FrameCount: 12, MaxPoint: ""},
		},
	}
	h := New(nil)
	h.SetDailyWearQuerier(q)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/patients/P1/daily-wear?start=2026-09-01&end=2026-09-02", nil)
	// 患者自查：X-User-Id = P1，X-Role = ROLE_PATIENT
	req.Header.Set(headerUserID, "P1")
	req.Header.Set(headerRole, "ROLE_PATIENT")

	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "P1", q.lastPID)
	assert.Equal(t, "2026-09-01", q.lastStart)
	assert.Equal(t, "2026-09-02", q.lastEnd)

	var resp struct {
		Code int                      `json:"code"`
		Data []*model.DailyWearDayDTO `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, model.CodeOK, resp.Code)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "2026-09-01", resp.Data[0].Date)
	assert.Equal(t, 1200, resp.Data[0].WearMinutes)
}

func TestGetDailyWear_AdminBypass(t *testing.T) {
	// ROLE_ADMIN 可查任意患者（X-User-Id ≠ patientId）
	q := &fakeDailyWearQuerier{list: []*model.DailyWearDayDTO{}}
	h := New(nil)
	h.SetDailyWearQuerier(q)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/patients/P-OTHER/daily-wear?start=2026-09-01&end=2026-09-02", nil)
	req.Header.Set(headerUserID, "ADMIN-001")
	req.Header.Set(headerRole, roleAdmin)

	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "P-OTHER", q.lastPID, "admin 应允许查询任意 patientId")
}

func TestGetDailyWear_NilQuerier_500(t *testing.T) {
	h := New(nil) // 未 SetDailyWearQuerier
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/patients/P1/daily-wear", nil)
	req.Header.Set(headerUserID, "P1")
	req.Header.Set(headerRole, "ROLE_PATIENT")

	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetDailyWear_ForbiddenOtherPatient(t *testing.T) {
	q := &fakeDailyWearQuerier{list: []*model.DailyWearDayDTO{}}
	h := New(nil)
	h.SetDailyWearQuerier(q)

	// ROLE_PATIENT 请求非自身 patientId
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/patients/P-B/daily-wear", nil)
	req.Header.Set(headerUserID, "P-A")
	req.Header.Set(headerRole, "ROLE_PATIENT")

	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, "非 admin 查询他人数据 → 403")

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Contains(t, resp.Message, "your own daily-wear")
	assert.Equal(t, "", q.lastPID, "403 之前不得调用 querier")
}

func TestGetDailyWear_ForbiddenMissingHeaders(t *testing.T) {
	// 缺失 X-User-Id / X-Role → fail-closed
	q := &fakeDailyWearQuerier{list: []*model.DailyWearDayDTO{}}
	h := New(nil)
	h.SetDailyWearQuerier(q)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patients/P1/daily-wear", nil)
	// 不设置身份头（模拟 gateway 故障未注入）
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, "缺失身份头 → fail-closed 403")
}

func TestGetDailyWear_BadParam_400(t *testing.T) {
	q := &fakeDailyWearQuerier{list: []*model.DailyWearDayDTO{}}
	h := New(nil)
	h.SetDailyWearQuerier(q)

	// 错误日期格式
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/patients/P1/daily-wear?start=not-a-date", nil)
	req.Header.Set(headerUserID, "P1")
	req.Header.Set(headerRole, "ROLE_PATIENT")

	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, "非法 start 日期 → 400")

	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, model.CodeQueryParam, resp.Code)
}

func TestGetDailyWear_EmptyData_ReturnsArray(t *testing.T) {
	// 空数据 → data: [] 而非 null（前端空态渲染依赖非 null）
	q := &fakeDailyWearQuerier{list: nil}
	h := New(nil)
	h.SetDailyWearQuerier(q)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/patients/P1/daily-wear?start=2026-09-01&end=2026-09-02", nil)
	req.Header.Set(headerUserID, "P1")
	req.Header.Set(headerRole, "ROLE_PATIENT")

	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"data":[]`)
}
