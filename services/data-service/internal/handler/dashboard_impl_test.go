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
	"github.com/stretchr/testify/require"

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
	// periods T489：各端点最近一次实收的 period（查询参数透传断言用）
	periods map[string]string
}

// see 记录一次实收 scope（结构体以字面量构造，map 懒初始化）
func (m *mockQuerier) see(op string, scope model.TeamScope) {
	if m.scopes == nil {
		m.scopes = map[string]model.TeamScope{}
	}
	m.scopes[op] = scope
}

// seePeriod 记录一次实收 period 与 scope（T489）
func (m *mockQuerier) seePeriod(op string, period string, scope model.TeamScope) {
	if m.periods == nil {
		m.periods = map[string]string{}
	}
	m.periods[op] = period
	m.see(op, scope)
}

func (m *mockQuerier) GetKPI(ctx context.Context, period string, scope model.TeamScope) (*service.DashboardKPIDTO, *model.AppError) {
	m.seePeriod("GetKPI", period, scope)
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
func (m *mockQuerier) GetTeamRanking(ctx context.Context, period string, scope model.TeamScope) ([]service.TeamRankingDTO, *model.AppError) {
	m.seePeriod("GetTeamRanking", period, scope)
	return m.teamRank, m.err
}
func (m *mockQuerier) GetDoctorRanking(ctx context.Context, period string, scope model.TeamScope) ([]service.DoctorRankingDTO, *model.AppError) {
	m.seePeriod("GetDoctorRanking", period, scope)
	return m.docRank, m.err
}
func (m *mockQuerier) GetWearDistribution(ctx context.Context, period string, scope model.TeamScope) ([]service.WearDistributionBucket, *model.AppError) {
	m.seePeriod("GetWearDistribution", period, scope)
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

// TestHandlerDashboardPeriodParam T489：period 查询参数必须原样透到 service。
//
// 断言三件事：
//  1. 不带 period ⇒ service 收到 "today"（旧调用方零改动的向后兼容面）；
//  2. 带 week/month ⇒ 原值下发，handler 不做二次映射（否则两处口径会漂）；
//  3. 带非法值 ⇒ 同样原样下发，由 service 的枚举白名单回 400（此处只测透传，
//     400 那一条在 service 层测；handler 提前拦会让 KPI 与排行两套校验并存）。
func TestHandlerDashboardPeriodParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 四个吃 period 的端点（两个趋势端点吃 days，不在此列）
	type probe struct {
		path string
		op   string
	}
	periodEndpoints := []probe{
		{"/api/v1/admin/dashboard/kpi", "GetKPI"},
		{"/api/v1/admin/dashboard/team-ranking", "GetTeamRanking"},
		{"/api/v1/admin/dashboard/doctor-ranking", "GetDoctorRanking"},
		{"/api/v1/admin/dashboard/wear-distribution", "GetWearDistribution"},
	}

	cases := []struct {
		suffix string
		want   string
	}{
		{"", "today"},
		{"?period=today", "today"},
		{"?period=week", "week"},
		{"?period=month", "month"},
		{"?period=year", "year"}, // 非法值：透传后由 service 判 400
	}

	for _, tc := range cases {
		t.Run("want="+tc.want+tc.suffix, func(t *testing.T) {
			for _, ep := range periodEndpoints {
				q := &mockQuerier{
					kpi:      &service.DashboardKPIDTO{},
					teamRank: []service.TeamRankingDTO{{Rank: 1}},
					docRank:  []service.DoctorRankingDTO{{Rank: 1}},
					dist:     []service.WearDistributionBucket{{Range: "< 4小时"}},
					err:      nil,
				}
				r := newRouterWithMockQuerier(q)
				req := httptest.NewRequest("GET", ep.path+tc.suffix, http.NoBody)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				require.Equal(t, 200, w.Code, ep.op)
				got, ok := q.periods[ep.op]
				require.True(t, ok, "service.%s 未被调用，period 无处可验", ep.op)
				assert.Equal(t, tc.want, got, "service.%s 实收 period", ep.op)
			}
		})
	}
}
