// Package handler data-service HTTP 接入层（Gin）
//
// 路由：
//
//	POST /api/v1/device/records        单帧实时上报（设备签名鉴权归 gateway）
//	POST /api/v1/device/records/batch  批量补传
//	GET  /api/v1/patients/:patientId/records   压力历史查询
//	GET  /api/v1/patients/:patientId/realtime  实时快照（Redis，零 DB）
//	GET  /api/v1/patients/:patientId/health-reports 健康报告列表（T030）
//	GET  /api/v1/patients/:patientId/daily-wear      患者日佩戴聚合（T076）
//	GET  /api/v1/admin/dashboard/*         admin Dashboard 6 聚合查询端点（T033）
//	GET  /healthz                      存活探针
//	GET  /metrics                      Prometheus 采集端点（架构 §6.1，T010）
//
// 统一响应体（架构 §3.5）：{ "code": 0, "message": "success", "data": {...} }
package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/service"
)

// headerDeviceID gateway 验签后注入的设备身份头（T006 骨架缺口：未上线前允许 body 回退）
const headerDeviceID = "X-Device-Id"

// Gateway 注入的身份头（JWT 中间件在 middleware.go 注入，见 setupRouter）
const (
	headerUserID = "X-User-Id"
	headerRole   = "X-Role"
	roleAdmin    = "ROLE_ADMIN"
)

// Handler HTTP 处理器
type Handler struct {
	svc       *service.RecordService
	reports   ReportLister     // T030 健康报告查询（SetReportLister 注入；nil 时该端点 500）
	dashboard DashboardQuerier // T033 Dashboard 聚合查询（SetDashboardQuerier 注入；nil 时端点 500）
	dailyWear DailyWearQuerier // T076 患者日佩戴聚合（SetDailyWearQuerier 注入；nil 时端点 500）
}

// New 创建 Handler
func New(svc *service.RecordService) *Handler { return &Handler{svc: svc} }

// Router 组装路由（可测试）
func (h *Handler) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	v1 := r.Group("/api/v1")
	{
		v1.POST("/device/records", h.uploadSingle)
		v1.POST("/device/records/batch", h.uploadBatch)
		v1.GET("/patients/:patientId/records", h.getHistory)
		v1.GET("/patients/:patientId/realtime", h.getRealtime)
		v1.GET("/patients/:patientId/health-reports", h.getHealthReports) // T030
		v1.GET("/patients/:patientId/daily-wear", h.getDailyWear)         // T076
		h.registerDashboardRoutes(v1)                                     // T033 admin Dashboard 6 端点
	}
	return r
}

// apiResponse 统一响应体
type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, apiResponse{Code: model.CodeOK, Message: "success", Data: data})
}

func fail(c *gin.Context, appErr *model.AppError) {
	if appErr.RetryAfterSec > 0 {
		c.Header("Retry-After", strconv.Itoa(appErr.RetryAfterSec))
	}
	c.JSON(appErr.HTTPStatus, apiResponse{Code: appErr.Code, Message: appErr.Message, Data: nil})
}

// uploadSingle 单帧实时上报
func (h *Handler) uploadSingle(c *gin.Context) {
	var req model.SingleFrameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	resp, appErr := h.svc.UploadSingle(c.Request.Context(), c.GetHeader(headerDeviceID), &req)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, resp)
}

// uploadBatch 批量补传
func (h *Handler) uploadBatch(c *gin.Context) {
	var req model.BatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	resp, appErr := h.svc.UploadBatch(c.Request.Context(), c.GetHeader(headerDeviceID), &req)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, resp)
}

// getHistory 压力历史查询（period=day|week|month，date=YYYY-MM-DD，分页默认 20 上限 100）
func (h *Handler) getHistory(c *gin.Context) {
	patientID := c.Param("patientId")
	period := c.DefaultQuery("period", "day")
	date := c.DefaultQuery("date", "")
	if date == "" {
		fail(c, model.ErrQueryParam("date is required (YYYY-MM-DD)"))
		return
	}

	page, pageSize := 1, 20
	if v := c.Query("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fail(c, model.ErrQueryParam("invalid page %q", v))
			return
		}
		page = n
	}
	if v := c.Query("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fail(c, model.ErrQueryParam("invalid pageSize %q", v))
			return
		}
		pageSize = n
	}
	if pageSize > 100 {
		pageSize = 100 // 架构 §3.5：pageSize 默认 20，上限 100
	}

	resp, appErr := h.svc.GetHistory(c.Request.Context(), patientID, period, date, page, pageSize)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, resp)
}

// getRealtime 实时快照（读 Redis，零 DB 明细命中）
func (h *Handler) getRealtime(c *gin.Context) {
	resp, appErr := h.svc.GetRealtime(c.Request.Context(), c.Param("patientId"))
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, resp)
}

// ─────────────────────────────────────────────────────────────
// T076：患者日佩戴聚合（daily_wear_stats 范围查询）
// ─────────────────────────────────────────────────────────────

// DailyWearQuerier 患者日佩戴聚合查询契约（service.DailyWearService 实现）
type DailyWearQuerier interface {
	GetDailyWear(ctx context.Context, patientID, start, end string) ([]*model.DailyWearDayDTO, *model.AppError)
}

// SetDailyWearQuerier 注入患者日佩戴聚合数据源（生产由 main 注入 DailyWearService）
func (h *Handler) SetDailyWearQuerier(q DailyWearQuerier) { h.dailyWear = q }

// getDailyWear GET /api/v1/patients/:patientId/daily-wear
//
//	?start=YYYY-MM-DD&end=YYYY-MM-DD（闭区间，Asia/Shanghai 切日；缺省 end=今日 start=end-6d）
//	水平鉴权：ROLE_ADMIN 允许任意；其余角色仅当 X-User-Id == patientId 允许（否则 403）
func (h *Handler) getDailyWear(c *gin.Context) {
	if h.dailyWear == nil {
		fail(c, model.ErrInternal("daily-wear querier not configured"))
		return
	}
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrQueryParam("patientId is required"))
		return
	}

	// 水平越权校验（fail-closed：缺失头视为无权限）
	role := c.GetHeader(headerRole)
	userID := c.GetHeader(headerUserID)
	if role != roleAdmin {
		if userID == "" || userID != patientID {
			fail(c, model.ErrForbidden("may only query your own daily-wear stats"))
			return
		}
	}

	start := c.Query("start")
	end := c.Query("end")
	list, appErr := h.dailyWear.GetDailyWear(c.Request.Context(), patientID, start, end)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	// nil → []，保证前端空态 JSON 是 "data":[]
	if list == nil {
		list = []*model.DailyWearDayDTO{}
	}
	ok(c, list)
}
