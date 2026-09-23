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

	// scopes T350：各端点最近一次实收的数据范围（handler → service 透传断言用）
	scopes map[string]model.TeamScope
}

func (m *mockQuerier) see(op string, scope model.TeamScope) {
	if m.scopes == nil {
		m.scopes = map[string]model.TeamScope{}
	}
	m.scopes[op] = scope
}

func (m *mockQuerier) GetKPI(ctx context.Context, period string, scope model.TeamScope) (*service.DashboardKPIDTO, *model.AppError) {
	m.see("GetKPI", scope)
	return m.kpi, m.err
}
func (m *mockQuerier) GetWearTrend(ctx context.Context, days int, scope model.TeamScope) ([]service.WearTrendPoint, *model.AppError) {
	m.see("GetWearTrend", scope)
	return m.wearTrend, m.err
}
func (m *mockQuerier) GetAlertTrend(ctx context.Context, days int, scope model.TeamScope) ([]service.AlertTrendPoint, *model.AppError) {
	m.see("GetAlertTrend", scope)
	return m.alertTrend, m.err
}
func (m *mockQuerier) GetTeamRanking(ctx context.Context, scope model.TeamScope) ([]service.TeamRankingDTO, *model.AppError) {
	m.see("GetTeamRanking", scope)
	return m.teamRank, m.err
}
func (m *mockQuerier) GetDoctorRanking(ctx context.Context, scope model.TeamScope) ([]service.DoctorRankingDTO, *model.AppError) {
	m.see("GetDoctorRanking", scope)
	return m.docRank, m.err
}
func (m *mockQuerier) GetWearDistribution(ctx context.Context, scope model.TeamScope) ([]service.WearDistributionBucket, *model.AppError) {
	m.see("GetWearDistribution", scope)
	return m.dist, m.err
}

// newRouterWithMockQuerier 创建带 Mock Querier 的 Router（直接使用 Handler 自建的 Gin Engine）
func newRouterWithMockQuerier(q DashboardQuerier) *gin.Engine {
	return newRouterWithScopeDeps(q, nil)
}

// newRouterWithScopeDeps T350：Dashboard 范围推导要读 patients/doctors，故一并注入 PatientLookup。
func newRouterWithScopeDeps(q DashboardQuerier, lookup PatientLookup) *gin.Engine {
	svc := &service.RecordService{}
	h := New(svc)
	if q != nil {
		h.SetDashboardQuerier(q)
	}
	if lookup != nil {
		h.SetPatientLookup(lookup)
	}
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
