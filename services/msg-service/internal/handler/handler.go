// Package handler msg-service HTTP 接入层（Gin）
//
// 路由（对齐 docs/ 消息域 9 接口）：
//
//	POST /internal/msg/send                                 告警通知受理（服务间内部，不经 gateway）
//	GET  /api/v1/patients/:patientId/subscription-quota     查询订阅额度
//	POST /api/v1/patients/:patientId/subscription-quota/grant 授予额度（Idempotency-Key 幂等）
//	GET  /api/v1/patients/:patientId/wear-reminder          读取佩戴提醒设置
//	PUT  /api/v1/patients/:patientId/wear-reminder          更新佩戴提醒设置
//	GET  /api/v1/patients/:patientId/notifications          患者通知记录（分页）
//	GET  /api/v1/admin/notify-rules                         查询通知规则
//	PUT  /api/v1/admin/notify-rules/:type                   更新通知规则
//	GET  /api/v1/admin/notification-logs                    后台通知记录（过滤）
//	GET  /healthz                                           存活探针
//
// 统一响应体（架构 §3.5）：{ "code": 0, "message": "success", "data": {...} }
// 鉴权归 gateway（JWT / RBAC）；内部接口以 X-Internal-Service 头 + Compose 内网隔离（架构 §5.2 内部信任链）。
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/msg-service/internal/repo"
	"github.com/bracesync/bracesync/services/msg-service/internal/service"
)

// 内部信任链头（架构 §5.2）：服务间直连白名单标识；gateway 会剥离外部请求的该头
const (
	headerInternalService = "X-Internal-Service"
	headerUserID          = "X-User-Id" // gateway 鉴权通过后注入的操作人身份头
)

// Handler HTTP 处理器
type Handler struct {
	svc *service.NotifyService
}

// New 创建 Handler
func New(svc *service.NotifyService) *Handler { return &Handler{svc: svc} }

// Router 组装路由（可测试）
func (h *Handler) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 服务间内部接口（不经 gateway，Compose 内网隔离 + X-Internal-Service 头）
	internal := r.Group("/internal")
	internal.Use(h.requireInternalHeader())
	{
		internal.POST("/msg/send", h.sendAlert)
	}

	v1 := r.Group("/api/v1")
	{
		v1.GET("/patients/:patientId/subscription-quota", h.getQuota)
		v1.POST("/patients/:patientId/subscription-quota/grant", h.grantQuota)
		v1.GET("/patients/:patientId/wear-reminder", h.getWearReminder)
		v1.PUT("/patients/:patientId/wear-reminder", h.updateWearReminder)
		v1.GET("/patients/:patientId/notifications", h.getPatientNotifications)

		v1.GET("/admin/notify-rules", h.getNotifyRules)
		v1.PUT("/admin/notify-rules/:type", h.updateNotifyRule)
		v1.GET("/admin/notification-logs", h.getNotificationLogs)
	}
	return r
}

// jsonResp 统一响应体（命名避开 Ella 预置测试 handler_test.go 中的 apiResponse 定义）
type jsonResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, jsonResp{Code: model.CodeOK, Message: "success", Data: data})
}

func fail(c *gin.Context, appErr *model.AppError) {
	c.JSON(appErr.HTTPStatus, jsonResp{Code: appErr.Code, Message: appErr.Message, Data: nil})
}

// requireInternalHeader 内部接口鉴权（架构 §5.2）：X-Internal-Service 头必须非空，
// 配合 Compose 内网隔离构成内部信任链；gateway 对外部流量剥离该头。
func (h *Handler) requireInternalHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader(headerInternalService) == "" {
			fail(c, model.ErrInternalAuth("missing %s header", headerInternalService))
			c.Abort()
			return
		}
		c.Next()
	}
}

// ─────────────────────────────────────────────────────────────
// 请求 DTO（camelCase，对齐契约 SendAlertNotificationRequest 等）
// ─────────────────────────────────────────────────────────────

type sendAlertRequest struct {
	AlertID        string   `json:"alertId"`
	Type           string   `json:"type"`
	PatientID      string   `json:"patientId"`
	DeviceID       string   `json:"deviceId"`
	Detail         string   `json:"detail"`
	SensorPoint    string   `json:"sensorPoint"`
	ThresholdValue *float64 `json:"thresholdValue"`
	ActualValue    *float64 `json:"actualValue"`
	Timestamp      string   `json:"timestamp"`
}

type wearReminderRequest struct {
	ReminderEnabled bool    `json:"reminderEnabled"`
	ReminderTime    *string `json:"reminderTime"`
}

type updateRuleRequest struct {
	Channels      []string `json:"channels"`
	NotifyTargets []string `json:"notifyTargets"`
}

// ─────────────────────────────────────────────────────────────
// 处理器
// ─────────────────────────────────────────────────────────────

// sendAlert 告警通知受理（契约 sendAlertNotification；alert-service 调用，同步受理 + 异步推送）
func (h *Handler) sendAlert(c *gin.Context) {
	var req sendAlertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if req.PatientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}
	if req.Type == "" {
		fail(c, model.ErrInvalidParam("type is required"))
		return
	}
	result, err := h.svc.SendAlert(c.Request.Context(), model.AlertNotifyRequest{
		AlertID:        req.AlertID,
		Type:           model.AlertType(req.Type),
		PatientID:      req.PatientID,
		DeviceID:       req.DeviceID,
		Detail:         req.Detail,
		SensorPoint:    req.SensorPoint,
		ThresholdValue: req.ThresholdValue,
		ActualValue:    req.ActualValue,
		Timestamp:      req.Timestamp,
	})
	if err != nil {
		failAppErr(c, err)
		return
	}
	ok(c, result)
}

// getQuota 查询订阅额度（契约 getSubscriptionQuota；患者仅可查本人，越权边界归 gateway JWT）
func (h *Handler) getQuota(c *gin.Context) {
	quota, err := h.svc.GetQuota(c.Request.Context(), c.Param("patientId"))
	if err != nil {
		failAppErr(c, err)
		return
	}
	ok(c, quota.ToDTO())
}

// grantQuota 授予额度（契约 grantSubscriptionQuota；Idempotency-Key 头幂等，缺失 → 400）
func (h *Handler) grantQuota(c *gin.Context) {
	idemKey := c.GetHeader("Idempotency-Key")
	if idemKey == "" {
		fail(c, model.ErrInvalidParam("Idempotency-Key header is required"))
		return
	}
	quota, err := h.svc.GrantQuota(c.Request.Context(), c.Param("patientId"), idemKey)
	if err != nil {
		failAppErr(c, err)
		return
	}
	// 契约返回 { remaining, isLow }（授予回报精简字段）
	ok(c, gin.H{"remaining": quota.Remaining, "isLow": quota.IsLow})
}

// getWearReminder 读取佩戴提醒设置（契约 getWearReminder）
func (h *Handler) getWearReminder(c *gin.Context) {
	settings, err := h.svc.GetWearReminder(c.Request.Context(), c.Param("patientId"))
	if err != nil {
		failAppErr(c, err)
		return
	}
	ok(c, settings.ToDTO())
}

// updateWearReminder 更新佩戴提醒设置（契约 updateWearReminder；直写 patient_preferences 一期偏离）
func (h *Handler) updateWearReminder(c *gin.Context) {
	var req wearReminderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	settings, err := h.svc.UpdateWearReminder(c.Request.Context(), c.Param("patientId"), req.ReminderEnabled, req.ReminderTime)
	if err != nil {
		failAppErr(c, err)
		return
	}
	ok(c, settings.ToDTO())
}

// getPatientNotifications 患者通知记录（契约 getPatientNotifications；时间倒序分页）
func (h *Handler) getPatientNotifications(c *gin.Context) {
	page, pageSize := pageParams(c)
	records, total, err := h.svc.GetNotificationLogs(c.Request.Context(), repo.RecordFilter{
		PatientID: c.Param("patientId"),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		failAppErr(c, err)
		return
	}
	ok(c, paginated(records, total, page, pageSize))
}

// getNotifyRules 查询通知规则（契约 getNotifyRules；ROLE_DOCTOR 只读全量，RBAC 归 gateway）
func (h *Handler) getNotifyRules(c *gin.Context) {
	rules, err := h.svc.GetNotifyRules(c.Request.Context())
	if err != nil {
		failAppErr(c, err)
		return
	}
	list := make([]model.NotifyRuleDTO, 0, len(rules))
	for i := range rules {
		list = append(list, rules[i].ToDTO())
	}
	ok(c, list)
}

// updateNotifyRule 更新通知规则（契约 updateNotifyRule；未知 type → 400；直写 alert_notify_rules 一期偏离）
func (h *Handler) updateNotifyRule(c *gin.Context) {
	var req updateRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	rule, err := h.svc.UpdateNotifyRule(c.Request.Context(), model.AlertType(c.Param("type")),
		req.Channels, req.NotifyTargets, c.GetHeader(headerUserID))
	if err != nil {
		failAppErr(c, err)
		return
	}
	ok(c, rule.ToDTO())
}

// getNotificationLogs 后台通知记录（契约 getNotificationLogs；过滤 + 分页）
func (h *Handler) getNotificationLogs(c *gin.Context) {
	page, pageSize := pageParams(c)
	records, total, err := h.svc.GetNotificationLogs(c.Request.Context(), repo.RecordFilter{
		PatientID: c.Query("patientId"),
		AlertType: c.Query("alertType"),
		Channel:   c.Query("channel"),
		Status:    c.Query("status"),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		failAppErr(c, err)
		return
	}
	ok(c, paginated(records, total, page, pageSize))
}

// ─────────────────────────────────────────────────────────────
// 辅助
// ─────────────────────────────────────────────────────────────

// failAppErr error → 统一错误响应（AppError 透传，其余收敛为 90001）
func failAppErr(c *gin.Context, err error) {
	if appErr, okErr := err.(*model.AppError); okErr {
		fail(c, appErr)
		return
	}
	fail(c, model.ErrInternal("%v", err))
}

// pageParams 分页参数（架构 §3.5：page 1 起，pageSize 默认 20 上限 100）
func pageParams(c *gin.Context) (int, int) {
	page := queryInt(c, "page", 1)
	if page < 1 {
		page = 1
	}
	pageSize := queryInt(c, "pageSize", 20)
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func queryInt(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// paginated 分页响应体（对齐 shared-types PaginatedResponse）
func paginated(records []model.NotificationRecord, total, page, pageSize int) gin.H {
	list := make([]model.NotificationRecordDTO, 0, len(records))
	for i := range records {
		list = append(list, records[i].ToDTO())
	}
	return gin.H{"list": list, "total": total, "page": page, "pageSize": pageSize}
}
