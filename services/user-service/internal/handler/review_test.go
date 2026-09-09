// Package handler — T130 复查记录端点测试
//
// 覆盖：
//   - POST /api/v1/admin/review-records：doctor/admin 可创建；其他角色 403
//   - GET  /api/v1/patients/:patientId/review-records：水平鉴权（患者仅自查，admin 任意）
package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// TestCreateReviewRecord_DoctorAdminOnly 创建复查记录：仅 doctor/admin
func TestCreateReviewRecord_DoctorAdminOnly(t *testing.T) {
	e := newEnv(t, false, false)

	body := map[string]any{
		"patientId":  "P001",
		"reviewDate": "2026-09-09",
		"reviewType": "follow-up",
		"findings":   "矫正效果良好",
	}

	// ROLE_DOCTOR → 200
	w, resp := e.do(http.MethodPost, "/api/v1/admin/review-records", body,
		map[string]string{"X-Role": "ROLE_DOCTOR", "X-User-Id": "DOC001"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	// ROLE_ADMIN → 200
	w, resp = e.do(http.MethodPost, "/api/v1/admin/review-records", body,
		map[string]string{"X-Role": "ROLE_ADMIN", "X-User-Id": "ADM001"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	// ROLE_CS → 403
	w, resp = e.do(http.MethodPost, "/api/v1/admin/review-records", body,
		map[string]string{"X-Role": "ROLE_CS", "X-User-Id": "CS001"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, model.CodeForbidden, resp.Code)

	// patient → 403
	w, resp = e.do(http.MethodPost, "/api/v1/admin/review-records", body,
		map[string]string{"X-Role": "patient", "X-User-Id": "P001"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, model.CodeForbidden, resp.Code)
}

// TestCreateReviewRecord_InvalidDate 非法日期格式 → 400
func TestCreateReviewRecord_InvalidDate(t *testing.T) {
	e := newEnv(t, false, false)

	body := map[string]any{
		"patientId":  "P001",
		"reviewDate": "2026/09/09", // 错误格式
		"reviewType": "follow-up",
	}
	w, resp := e.do(http.MethodPost, "/api/v1/admin/review-records", body,
		map[string]string{"X-Role": "ROLE_DOCTOR", "X-User-Id": "DOC001"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

// TestCreateReviewRecord_Success 成功创建并返回 DTO
func TestCreateReviewRecord_Success(t *testing.T) {
	e := newEnv(t, false, false)

	now := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	fileID := "FILE001"
	e.store.createdReview = &repo.ReviewRecordRow{
		ReviewID:     "RV_20260909100000_abcd",
		PatientID:    "P001",
		ReviewDate:   now,
		ReviewType:   strPtr("follow-up"),
		Findings:     strPtr("矫正效果良好"),
		DoctorID:     strPtr("DOC001"),
		ReportFileID: &fileID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	body := map[string]any{
		"patientId":  "P001",
		"reviewDate": "2026-09-09",
		"reviewType": "follow-up",
		"findings":   "矫正效果良好",
		"doctorId":   "DOC001",
		"reportFileId": "FILE001",
	}
	w, resp := e.do(http.MethodPost, "/api/v1/admin/review-records", body,
		map[string]string{"X-Role": "ROLE_DOCTOR", "X-User-Id": "DOC001"})
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, model.CodeOK, resp.Code)

	var dto model.ReviewRecordDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "RV_20260909100000_abcd", dto.ReviewID)
	assert.Equal(t, "P001", dto.PatientID)
	assert.Equal(t, "2026-09-09", dto.ReviewDate)
	assert.Equal(t, "follow-up", *dto.ReviewType)
	assert.Equal(t, "矫正效果良好", *dto.Findings)
	assert.Equal(t, "DOC001", *dto.DoctorID)
	assert.Equal(t, "FILE001", *dto.ReportFileID)
}

// TestListReviewRecords_HorizontalAuthz 水平鉴权：患者仅可查本人，admin 可查任意
func TestListReviewRecords_HorizontalAuthz(t *testing.T) {
	e := newEnv(t, false, false)

	now := time.Now().UTC()
	e.store.reviewRows = []repo.ReviewRecordRow{{
		ReviewID:   "RV_001",
		PatientID:  "P001",
		ReviewDate: now,
		ReviewType: strPtr("initial"),
		CreatedAt:  now,
		UpdatedAt:  now,
	}}

	// 患者查本人 → 200
	w, resp := e.do(http.MethodGet, "/api/v1/patients/P001/review-records", nil,
		map[string]string{"X-Role": "patient", "X-User-Id": "P001"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	// 患者查他人 → 403
	w, resp = e.do(http.MethodGet, "/api/v1/patients/P002/review-records", nil,
		map[string]string{"X-Role": "patient", "X-User-Id": "P001"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, model.CodeForbidden, resp.Code)

	// admin 查任意患者 → 200
	w, resp = e.do(http.MethodGet, "/api/v1/patients/P002/review-records", nil,
		map[string]string{"X-Role": "ROLE_ADMIN", "X-User-Id": "ADM001"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	// 缺失 X-User-Id → 403（fail-closed）
	w, resp = e.do(http.MethodGet, "/api/v1/patients/P001/review-records", nil,
		map[string]string{"X-Role": "patient"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, model.CodeForbidden, resp.Code)
}

// TestListReviewRecords_ReturnsList 成功返回复查记录列表
func TestListReviewRecords_ReturnsList(t *testing.T) {
	e := newEnv(t, false, false)

	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	fileID := "FILE001"
	e.store.reviewRows = []repo.ReviewRecordRow{{
		ReviewID:     "RV_001",
		PatientID:    "P001",
		ReviewDate:   now,
		ReviewType:   strPtr("follow-up"),
		Findings:     strPtr("良好"),
		ReportFileID: &fileID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}}

	w, resp := e.do(http.MethodGet, "/api/v1/patients/P001/review-records", nil,
		map[string]string{"X-Role": "ROLE_ADMIN", "X-User-Id": "ADM001"})
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, model.CodeOK, resp.Code)

	var list []model.ReviewRecordDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "RV_001", list[0].ReviewID)
	assert.Equal(t, "2026-09-09", list[0].ReviewDate)
	assert.Equal(t, "follow-up", *list[0].ReviewType)
	assert.Equal(t, "FILE001", *list[0].ReportFileID)
}
