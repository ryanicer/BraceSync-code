// Package handler 实现侧测试（T030）：健康报告查询端点（fake ReportLister）
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

func init() { gin.SetMode(gin.TestMode) }

// fakeReportLister ReportLister 内存实现
type fakeReportLister struct {
	reports []model.HealthReport
	err     error
	lastID  string
}

func (f *fakeReportLister) ListReports(_ context.Context, patientID string) ([]model.HealthReport, error) {
	f.lastID = patientID
	return f.reports, f.err
}

func TestGetHealthReports(t *testing.T) {
	lister := &fakeReportLister{
		reports: []model.HealthReport{{
			ReportID: 3, PatientID: "P20260001", ReportType: "weekly",
			PeriodStart:        time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC),
			PeriodEnd:          time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
			WearComplianceRate: 92.5, AvgPressure: 38.2, TrendJudgment: "up",
			Suggestion:   "维持当前方案",
			GenerateTime: time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC),
		}},
	}
	h := New(nil)
	h.SetReportLister(lister)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patients/P20260001/health-reports", nil)
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "P20260001", lister.lastID)

	var resp struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	var list []healthReportDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "3", list[0].ReportID)
	assert.Equal(t, "2026-08-04", list[0].PeriodStart)
	assert.Equal(t, "2026-08-10", list[0].PeriodEnd)
	assert.Equal(t, 92.5, list[0].WearComplianceRate)
	assert.Equal(t, "up", list[0].TrendJudgment)
	assert.Equal(t, "2026-08-11T02:00:00Z", list[0].GenerateTime)
}

func TestGetHealthReportsErrors(t *testing.T) {
	// 未注入 → 500
	h := New(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/patients/P1/health-reports", nil)
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// store 错误 → 500
	lister := &fakeReportLister{err: errors.New("db")}
	h.SetReportLister(lister)
	w = httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// 空列表 → 200 + []
	lister.err = nil
	w = httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"data":[]`)
}
