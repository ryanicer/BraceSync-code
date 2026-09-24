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

// staffRoles 内部 staff 角色集合（与 gateway / file-service 对齐；不含 patient）。
// T264：admin 全量跨患者；doctor/cs/technician 暂按 staff 放行（团队归属过滤待 T184 落地）。
var staffRoles = map[string]bool{
	"admin":       true,
	"ROLE_ADMIN":  true,
	"ROLE_DOCTOR": true,
	"ROLE_CS":     true,
	"technician":  true,
}

// isStaffRole 判断角色是否属于内部 staff
func isStaffRole(role string) bool { return staffRoles[role] }

// assertAdminOrSelf 水平鉴权（T264，照抄 getDailyWear 范式）：
//   - staff 放行（跨患者访问）
//   - 非 staff（patient）仅当 X-User-Id == patientId 放行，否则 403
//
// fail-closed：缺失 X-User-Id 视为无权限。
func assertAdminOrSelf(c *gin.Context, patientID string) bool {
	if isStaffRole(c.GetHeader(headerRole)) {
		return true
	}
	userID := c.GetHeader(headerUserID)
	if userID == "" || userID != patientID {
		fail(c, model.ErrForbidden("may only access your own data"))
		return false
	}
	return true
}

// Handler HTTP 处理器
type Handler struct {
	svc       *service.RecordService
	reports   ReportLister     // T030 健康报告查询（SetReportLister 注入；nil 时该端点 500）
	dashboard DashboardQuerier // T033 Dashboard 聚合查询（SetDashboardQuerier 注入；nil 时端点 500）
	dailyWear DailyWearQuerier // T076 患者日佩戴聚合（SetDailyWearQuerier 注入；nil 时端点 500）
	patients  PatientLookup    // T340 患者档案存在性（SetPatientLookup 注入；nil 时患者域查询端点 500）
}

// New 创建 Handler
func New(svc *service.RecordService) *Handler { return &Handler{svc: svc} }

// PatientLookup 患者档案存在性查询契约（repo.PatientRepo 实现，patients 表只读）
type PatientLookup interface {
	PatientExists(ctx context.Context, patientID string) (bool, error)
	// PatientInAdminTeam T350：医护（X-User-Id = admins.admin_id）与患者是否同团队。
	// false 覆盖「患者不存在 / 无医护档案 / 任一方无团队」三种情形，统一按 403 出。
	PatientInAdminTeam(ctx context.Context, patientID, adminID string) (bool, error)
	// DoctorTeamByAdmin T350：admin_id → 所属团队（Dashboard 聚合范围推导，ok=false = 无团队）
	DoctorTeamByAdmin(ctx context.Context, adminID string) (teamID string, ok bool, err error)
}

// SetPatientLookup 注入患者档案存在性数据源（生产由 main 注入）
func (h *Handler) SetPatientLookup(l PatientLookup) { h.patients = l }

// assertPatientExists T340：「查无此人」必须与「有此人但暂无数据」在 HTTP 面上可区分。
// 放在水平鉴权之后 —— 存在性不该泄露给无权调用方。返回 false 时响应已写出。
func (h *Handler) assertPatientExists(c *gin.Context, patientID string) bool {
	if h.patients == nil {
		fail(c, model.ErrInternal("patient lookup not configured"))
		return false
	}
	exists, err := h.patients.PatientExists(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("patient lookup failed"))
		return false
	}
	if !exists {
		fail(c, model.ErrPatientNotFound(patientID))
		return false
	}
	return true
}

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
	if !assertAdminOrSelf(c, patientID) { // T264：水平鉴权
		return
	}
	if !h.assertTeamScope(c, patientID) { // T350：医护仅限本团队患者
		return
	}
	if !h.assertPatientExists(c, patientID) { // T340
		return
	}
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
	patientID := c.Param("patientId")
	if !assertAdminOrSelf(c, patientID) { // T264：水平鉴权
		return
	}
	if !h.assertTeamScope(c, patientID) { // T350：医护仅限本团队患者（T340 C4 现网证据面）
		return
	}
	if !h.assertPatientExists(c, patientID) { // T340：未绑定设备与查无此人此前在 :477 被折叠成同一个空快照
		return
	}
	resp, appErr := h.svc.GetRealtime(c.Request.Context(), patientID)
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
//	水平鉴权：ROLE_ADMIN 允许任意；ROLE_DOCTOR 允许本团队患者（T350 返工 D-1）；
//	其余角色仅当 X-User-Id == patientId 允许（否则 403）
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
	if role != roleAdmin && role != roleDoctor {
		if userID == "" || userID != patientID {
			fail(c, model.ErrForbidden("may only query your own daily-wear stats"))
			return
		}
	}
	// T350 返工 D-1：医护走团队推导（realtime / records / health-reports 同一条闸门）。
	// 排在存在性探测之前 ⇒ 跨团队与「查无此人」合一 403，患者号存在性不作为探测面。
	if !h.assertTeamScope(c, patientID) {
		return
	}
	if !h.assertPatientExists(c, patientID) { // T340
		return
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
