// Package handler T264：alert-service listAlerts 患者身份绑定
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestT264_ListAlerts_PatientForcesOwnPatientId 患者调 /alerts 时，
// 服务端强制 patientId = X-User-Id，忽略调用方传入的 ?patientId=
func TestT264_ListAlerts_PatientForcesOwnPatientId(t *testing.T) {
	store := &fakePublicStore{}
	h := newPublicHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?patientId=OTHER&page=1", nil)
	req.Header.Set(headerRole, "patient")
	req.Header.Set(headerUserID, "P-SELF")

	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "P-SELF", store.filter.PatientID, "患者 patientId 应被强制覆盖为 X-User-Id")
}

// TestT264_ListAlerts_PatientMissingUserID_403 患者缺失 X-User-Id → 403（fail-closed）
func TestT264_ListAlerts_PatientMissingUserID_403(t *testing.T) {
	store := &fakePublicStore{}
	h := newPublicHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req.Header.Set(headerRole, "patient") // 无 X-User-Id

	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 0, store.listHits, "store 不应被触达")
}

// TestT264_ListAlerts_StaffKeepsPatientIdFilter staff 保留 ?patientId= 过滤
func TestT264_ListAlerts_StaffKeepsPatientIdFilter(t *testing.T) {
	store := &fakePublicStore{}
	h := newPublicHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?patientId=P-STAFF-QUERY", nil)
	req.Header.Set(headerRole, roleAdmin)
	req.Header.Set(headerUserID, "ADMIN-001")

	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "P-STAFF-QUERY", store.filter.PatientID, "staff 应保留 patientId 过滤参数")
}
