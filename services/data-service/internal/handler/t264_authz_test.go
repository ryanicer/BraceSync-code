// Package handler T264：data-service 水平鉴权（getHistory / getRealtime / getHealthReports）
//
// 覆盖：患者本人 → 200；患者他人 → 403；staff → 200；缺失身份头 → 403（fail-closed）。
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doDataReq 带身份头发起 data-service 请求
func doDataReq(t *testing.T, h http.Handler, method, path string, role, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if role != "" {
		req.Header.Set(headerRole, role)
	}
	if userID != "" {
		req.Header.Set(headerUserID, userID)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestT264_GetHistory_Authz 压力历史水平鉴权
func TestT264_GetHistory_Authz(t *testing.T) {
	srv := newTestServer(nil)
	path := "/api/v1/patients/P1/records?date=2026-08-08"

	// staff 放行
	w := doDataReq(t, srv.router, http.MethodGet, path, roleAdmin, "ADMIN-001")
	assert.Equal(t, http.StatusOK, w.Code, "admin 应放行")

	// 患者本人 → 200
	w = doDataReq(t, srv.router, http.MethodGet, path, "ROLE_PATIENT", "P1")
	assert.Equal(t, http.StatusOK, w.Code, "患者本人应放行")

	// 患者他人 → 403
	w = doDataReq(t, srv.router, http.MethodGet, path, "ROLE_PATIENT", "P2")
	assert.Equal(t, http.StatusForbidden, w.Code, "患者他人应 403")

	// 缺失身份头 → 403（fail-closed）
	w = doDataReq(t, srv.router, http.MethodGet, path, "", "")
	assert.Equal(t, http.StatusForbidden, w.Code, "缺失身份头应 403")
}

// TestT264_GetRealtime_Authz 实时快照水平鉴权
func TestT264_GetRealtime_Authz(t *testing.T) {
	srv := newTestServer(nil)
	path := "/api/v1/patients/P1/realtime"

	w := doDataReq(t, srv.router, http.MethodGet, path, roleAdmin, "ADMIN-001")
	assert.Equal(t, http.StatusOK, w.Code, "admin 应放行")

	w = doDataReq(t, srv.router, http.MethodGet, path, "ROLE_PATIENT", "P1")
	assert.Equal(t, http.StatusOK, w.Code, "患者本人应放行")

	w = doDataReq(t, srv.router, http.MethodGet, path, "ROLE_PATIENT", "P2")
	assert.Equal(t, http.StatusForbidden, w.Code, "患者他人应 403")
}

// TestT264_GetHealthReports_Authz 健康报告水平鉴权
func TestT264_GetHealthReports_Authz(t *testing.T) {
	h := New(nil)
	h.SetReportLister(&fakeReportLister{})
	router := h.Router()
	path := "/api/v1/patients/P1/health-reports"

	w := doDataReq(t, router, http.MethodGet, path, roleAdmin, "ADMIN-001")
	assert.Equal(t, http.StatusOK, w.Code, "admin 应放行")

	w = doDataReq(t, router, http.MethodGet, path, "ROLE_PATIENT", "P1")
	assert.Equal(t, http.StatusOK, w.Code, "患者本人应放行")

	w = doDataReq(t, router, http.MethodGet, path, "ROLE_PATIENT", "P2")
	require.Equal(t, http.StatusForbidden, w.Code, "患者他人应 403")
}
