// Package handler — T033：admin Dashboard 6 聚合查询端点（api-contracts.ts getDashboard* 落地）
//
// 路由（gateway JWT 鉴权组挂载，T032）：
//
//	GET /api/v1/admin/dashboard/kpi?period=today|week|month
//	GET /api/v1/admin/dashboard/wear-trend?days=
//	GET /api/v1/admin/dashboard/alert-trend?days=
//	GET /api/v1/admin/dashboard/team-ranking
//	GET /api/v1/admin/dashboard/doctor-ranking
//	GET /api/v1/admin/dashboard/wear-distribution
//
// 数据源 daily_wear_stats + Redis kpi:dashboard:{period}（架构 §4.7 TTL 60s 查询回填）；
// 参数校验/枚举白名单/400 对齐 T028/T030 端点风格；DashboardQuerier 未注入时返回 500。
package handler

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/service"
)

// DashboardQuerier Dashboard 查询契约（service.DashboardService 实现）
type DashboardQuerier interface {
	GetKPI(ctx context.Context, period string) (*service.DashboardKPIDTO, *model.AppError)
	GetWearTrend(ctx context.Context, days int) ([]service.WearTrendPoint, *model.AppError)
	GetAlertTrend(ctx context.Context, days int) ([]service.AlertTrendPoint, *model.AppError)
	GetTeamRanking(ctx context.Context) ([]service.TeamRankingDTO, *model.AppError)
	GetDoctorRanking(ctx context.Context) ([]service.DoctorRankingDTO, *model.AppError)
	GetWearDistribution(ctx context.Context) ([]service.WearDistributionBucket, *model.AppError)
}

// SetDashboardQuerier 注入 Dashboard 数据源（生产由 main 注入；未注入时端点返回 500）
func (h *Handler) SetDashboardQuerier(q DashboardQuerier) { h.dashboard = q }

// registerDashboardRoutes 挂载 admin Dashboard 6 查询端点
func (h *Handler) registerDashboardRoutes(v1 *gin.RouterGroup) {
	dash := v1.Group("/admin/dashboard")
	dash.GET("/kpi", h.getDashboardKPI)
	dash.GET("/wear-trend", h.getWearTrend)
	dash.GET("/alert-trend", h.getAlertTrend)
	dash.GET("/team-ranking", h.getTeamRanking)
	dash.GET("/doctor-ranking", h.getDoctorRanking)
	dash.GET("/wear-distribution", h.getWearDistribution)
}

// getDashboardKPI KPI 六指标（period 缺省 today，枚举白名单校验在 service 层）
func (h *Handler) getDashboardKPI(c *gin.Context) {
	if h.dashboard == nil {
		fail(c, model.ErrInternal("dashboard querier not configured"))
		return
	}
	period := c.DefaultQuery("period", "today")
	dto, appErr := h.dashboard.GetKPI(c.Request.Context(), period)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, dto)
}

// daysParam 解析 days 查询参数（缺省返回 0 由 service 兜底默认值）；非法整数 → 400
func daysParam(c *gin.Context) (int, *model.AppError) {
	v := c.Query("days")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, model.ErrQueryParam("invalid days %q", v)
	}
	return n, nil
}

// getWearTrend 近 days 日平均佩戴小时趋势
func (h *Handler) getWearTrend(c *gin.Context) {
	if h.dashboard == nil {
		fail(c, model.ErrInternal("dashboard querier not configured"))
		return
	}
	days, appErr := daysParam(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	list, appErr := h.dashboard.GetWearTrend(c.Request.Context(), days)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, list)
}

// getAlertTrend 近 days 日告警数趋势
func (h *Handler) getAlertTrend(c *gin.Context) {
	if h.dashboard == nil {
		fail(c, model.ErrInternal("dashboard querier not configured"))
		return
	}
	days, appErr := daysParam(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	list, appErr := h.dashboard.GetAlertTrend(c.Request.Context(), days)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, list)
}

// getTeamRanking 团队排行
func (h *Handler) getTeamRanking(c *gin.Context) {
	if h.dashboard == nil {
		fail(c, model.ErrInternal("dashboard querier not configured"))
		return
	}
	list, appErr := h.dashboard.GetTeamRanking(c.Request.Context())
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, list)
}

// getDoctorRanking 医生排行
func (h *Handler) getDoctorRanking(c *gin.Context) {
	if h.dashboard == nil {
		fail(c, model.ErrInternal("dashboard querier not configured"))
		return
	}
	list, appErr := h.dashboard.GetDoctorRanking(c.Request.Context())
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, list)
}

// getWearDistribution 佩戴时长分布
func (h *Handler) getWearDistribution(c *gin.Context) {
	if h.dashboard == nil {
		fail(c, model.ErrInternal("dashboard querier not configured"))
		return
	}
	list, appErr := h.dashboard.GetWearDistribution(c.Request.Context())
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, list)
}
