// Package handler T033：Dashboard HTTP 端点实现侧测试（路由/参数校验/nil 检查）
package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/service"
)

// mockQuerier 轻量 fake querier
type mockQuerier struct {
	kpi        *service.DashboardKPIDTO
	wearTrend  []service.WearTrendPoint
	alertTrend []service.AlertTrendPoint
	teamRank   []service.TeamRankingDTO
	docRank    []service.DoctorRankingDTO
	dist       []service.WearDistributionBucket
	err        *model.AppError
}

func (m *mockQuerier) GetKPI(ctx context.Context, period string) (*service.DashboardKPIDTO, *model.AppError) {
	return m.kpi, m.err
}
func (m *mockQuerier) GetWearTrend(ctx context.Context, days int) ([]service.WearTrendPoint, *model.AppError) {
	return m.wearTrend, m.err
}
func (m *mockQuerier) GetAlertTrend(ctx context.Context, days int) ([]service.AlertTrendPoint, *model.AppError) {
	return m.alertTrend, m.err
}
func (m *mockQuerier) GetTeamRanking(ctx context.Context) ([]service.TeamRankingDTO, *model.AppError) {
	return m.teamRank, m.err
}
func (m *mockQuerier) GetDoctorRanking(ctx context.Context) ([]service.DoctorRankingDTO, *model.AppError) {
	return m.docRank, m.err
}
func (m *mockQuerier) GetWearDistribution(ctx context.Context) ([]service.WearDistributionBucket, *model.AppError) {
	return m.dist, m.err
}

// newRouterWithMockQuerier 创建带 Mock Querier 的 Router（直接使用 Handler 自建的 Gin Engine）
func newRouterWithMockQuerier(q DashboardQuerier) *gin.Engine {
	svc := &service.RecordService{}
	h := New(svc)
	h.SetDashboardQuerier(q)
	return h.Router() // return the engine created inside Handler#Router()
}

func TestHandlerDashboardNilQuerier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newRouterWithMockQuerier(nil)

	tests := []struct {
		name   string
		path   string
		method string
	}{
		{"kpi", "/api/v1/admin/dashboard/kpi", "GET"},
		{"wear-trend", "/api/v1/admin/dashboard/wear-trend?days=abc", "GET"},
		{"alert-trend", "/api/v1/admin/dashboard/alert-trend?days=-5", "GET"},
		{"team-ranking", "/api/v1/admin/dashboard/team-ranking", "GET"},
		{"doctor-ranking", "/api/v1/admin/dashboard/doctor-ranking", "GET"},
		{"distribution", "/api/v1/admin/dashboard/wear-distribution", "GET"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, 500, w.Code) // nil querier → internal error 500
		})
	}
}

func TestHandlerDashboardHappyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	q := &mockQuerier{
		kpi:        &service.DashboardKPIDTO{TotalPatients: 100, TodayActiveWear: 80, TodayAlerts: 50},
		wearTrend:  []service.WearTrendPoint{{Date: "08-05", AvgHours: 7.8}},
		alertTrend: []service.AlertTrendPoint{{Date: "08-05", Count: 52}},
		teamRank:   []service.TeamRankingDTO{{Rank: 1, TeamName: "TEAM-A"}},
		docRank:    []service.DoctorRankingDTO{{Rank: 1, DoctorName: "DR-X"}},
		dist:       []service.WearDistributionBucket{{Range: "6-8 小时", Count: 312}},
	}
	r := newRouterWithMockQuerier(q)

	endpoints := []string{
		"/api/v1/admin/dashboard/kpi?period=week",
		"/api/v1/admin/dashboard/wear-trend?days=5",
		"/api/v1/admin/dashboard/alert-trend?days=7",
		"/api/v1/admin/dashboard/team-ranking",
		"/api/v1/admin/dashboard/doctor-ranking",
		"/api/v1/admin/dashboard/wear-distribution",
	}

	for _, ep := range endpoints {
		idx := strings.LastIndex(ep, "/")
		name := "unknown"
		if idx != -1 {
			name = ep[idx+1:]
		}
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", ep, http.NoBody)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, 200, w.Code)
		})
	}
}
