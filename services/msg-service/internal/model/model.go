// Package model msg-service 领域模型与 API DTO 定义
//
// 对齐：docs/ §3.5（错误码分段：5xxxx 消息域）
//
//	docs/ 消息域（sendAlertNotification 等 9 接口）
//	packages/shared-types（NotifyRule / SubscriptionQuota / WearReminderSettings / NotificationRecord）
//	database-design.md（alert_notify_rules / patient_preferences / 000003 通知域表）
package model

import (
	"fmt"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────
// 枚举（对齐 DB CHECK 约束 + shared-types）
// ─────────────────────────────────────────────────────────────

// AlertType 告警类型（alerts.type / alert_notify_rules.type CHECK 约束一致）
type AlertType string

// 告警类型枚举
const (
	AlertTypePressureHigh AlertType = "pressure_high"
	// AlertTypePressureFluctuation T257 2.6（方案A）：alert-service 引擎已停止产生该类型，
	// 常量保留 = DB CHECK 仍允许（000001 起有存量行），admin 仍可查看/配置其通知规则。
	AlertTypePressureFluctuation AlertType = "pressure_fluctuation"
	AlertTypeWearInterrupt       AlertType = "wear_interrupt"
	AlertTypeSensorDrift         AlertType = "sensor_drift"
	AlertTypeWearDurationShort   AlertType = "wear_duration_short"
)

// KnownAlertTypes 全部合法告警类型（admin 规则更新校验用）
var KnownAlertTypes = []AlertType{
	AlertTypePressureHigh,
	AlertTypePressureFluctuation,
	AlertTypeWearInterrupt,
	AlertTypeSensorDrift,
	AlertTypeWearDurationShort,
}

// AlertTypeList 合法类型清单的 "/" 拼接串（报错文案用）。
// 由 KnownAlertTypes 生成，避免文案与枚举两处以不同速度漂移（T257 加类型时曾漏改硬编码文案）。
func AlertTypeList() string {
	parts := make([]string, 0, len(KnownAlertTypes))
	for _, t := range KnownAlertTypes {
		parts = append(parts, string(t))
	}
	return strings.Join(parts, "/")
}

// ValidAlertType 校验告警类型是否在 CHECK 约束枚举内
func ValidAlertType(t AlertType) bool {
	for _, k := range KnownAlertTypes {
		if t == k {
			return true
		}
	}
	return false
}

// Channel 通知渠道（shared-types NotifyChannel）
const (
	ChannelWechat = "wechat"
	ChannelSMS    = "sms"
)

// ValidChannel 校验渠道枚举
func ValidChannel(c string) bool { return c == ChannelWechat || c == ChannelSMS }

// Target 通知目标角色（shared-types NotifyTarget）
const (
	TargetPatient = "patient"
	TargetDoctor  = "doctor"
	TargetTech    = "tech"
	TargetOps     = "ops"
)

// ValidTarget 校验通知目标枚举
func ValidTarget(t string) bool {
	switch t {
	case TargetPatient, TargetDoctor, TargetTech, TargetOps:
		return true
	}
	return false
}

// 通知记录状态（notification_records.status CHECK / shared-types NotificationRecord.status）
const (
	StatusPending  = "pending"  // 已受理待发送
	StatusSent     = "sent"     // 发送成功（sent_at 落库）
	StatusFailed   = "failed"   // 发送失败（进重试队列）
	StatusDegraded = "degraded" // 额度耗尽降级短信
)

// 通知记录类别（notification_records.kind）
const (
	KindAlert        = "alert"         // 告警通知
	KindWearReminder = "wear_reminder" // 佩戴提醒（定时任务）
)

// ─────────────────────────────────────────────────────────────
// 错误码（架构 §3.5：5xxxx 消息域；9xxxx 系统级）
// ─────────────────────────────────────────────────────────────

const (
	CodeOK               = 0
	CodeInvalidParam     = 50400 // 参数非法（未知告警类型 / 非法渠道 / Idempotency-Key 缺失）
	CodeForbidden        = 50403 // 越权访问（水平越权：患者传他人 patientId / 非 admin 调权益写端点）
	CodeNotFound         = 50404 // 消息域资源不存在（规则 / 通知记录）
	CodePatientNotFound  = 10404 // 患者档案不存在（T353：档案 owner 是 user-service，与 data-service CodePatientNotFound / user-service CodeNotFound 同码，不落在本域 5xxxx 分段）
	CodeQuotaExhausted   = 54002 // 订阅额度耗尽（T017 review 定稿：落在消息域分段）
	CodeInternal         = 90001 // 系统内部错误
	CodeInternalDisabled = 50001 // 内部接口鉴权失败（X-Internal-Service 缺失）
)

// AppError 业务错误：携带统一响应 code 与建议 HTTP 状态
type AppError struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *AppError) Error() string { return fmt.Sprintf("code=%d: %s", e.Code, e.Message) }

func newAppError(code, httpStatus int, format string, args ...any) *AppError {
	return &AppError{Code: code, HTTPStatus: httpStatus, Message: fmt.Sprintf(format, args...)}
}

// ErrInvalidParam 参数非法（400）
func ErrInvalidParam(format string, args ...any) *AppError {
	return newAppError(CodeInvalidParam, 400, format, args...)
}

// ErrForbidden 越权访问（403）：患者域端点的水平越权、权益写端点的角色不符
func ErrForbidden(format string, args ...any) *AppError {
	return newAppError(CodeForbidden, 403, format, args...)
}

// ErrNotFound 消息域资源不存在（404）
func ErrNotFound(format string, args ...any) *AppError {
	return newAppError(CodeNotFound, 404, format, args...)
}

// ErrPatientNotFound T353：查无要与「有此人但无偏好行/无记录」在 HTTP 面上可区分，状态位显式 404。
func ErrPatientNotFound(patientID string) *AppError {
	return newAppError(CodePatientNotFound, 404, "patient %q not found", patientID)
}

// ErrQuotaExhausted 订阅额度耗尽（200 受理降级场景不抛此错；仅对外查询/显式拒绝时用）
func ErrQuotaExhausted(format string, args ...any) *AppError {
	return newAppError(CodeQuotaExhausted, 429, format, args...)
}

// ErrInternal 系统内部错误（500）
func ErrInternal(format string, args ...any) *AppError {
	return newAppError(CodeInternal, 500, format, args...)
}

// ErrInternalAuth 内部接口鉴权失败（401）
func ErrInternalAuth(format string, args ...any) *AppError {
	return newAppError(CodeInternalDisabled, 401, format, args...)
}

// ─────────────────────────────────────────────────────────────
// 领域实体（对齐 database-design / 000003 migration）
// ─────────────────────────────────────────────────────────────

// NotifyRule 告警通知规则（alert_notify_rules 表行，PRD §7D.6）
type NotifyRule struct {
	Type          AlertType
	Channels      []string // wechat / sms
	NotifyTargets []string // patient / doctor / tech / ops
	UpdatedBy     string
	UpdatedAt     time.Time
}

// SubscriptionQuota 订阅授权额度快照（patient_preferences.subscription_quota）
type SubscriptionQuota struct {
	PatientID string
	Remaining int        // 剩余可用次数（subscription_quota 列，DEFAULT 3）
	Total     int        // 累计授予额度（3 默认 + grant 台账增量合计）
	IsLow     bool       // Remaining ≤ 1 → 引导重新授权（架构 §2.5）
	UpdatedAt *time.Time // 最近一次额度变更（patient_preferences.updated_at）
}

// QuotaLowThreshold 低额度阈值：剩余 ≤1 时前端引导重新授权（架构 §2.5）
const QuotaLowThreshold = 1

// DefaultQuota 新患者默认额度（patient_preferences.subscription_quota DEFAULT 3）
const DefaultQuota = 3

// WearReminderSettings 佩戴提醒设置（patient_preferences.reminder_*）
type WearReminderSettings struct {
	ReminderEnabled bool
	ReminderTime    *string // HH:mm（Asia/Shanghai 业务时区）
}

// NotificationRecord 通知发送记录（notification_records 表行）
type NotificationRecord struct {
	RecordID    int64
	PatientID   string
	PatientName *string    // T278-③：LEFT JOIN patients 带出（表里只有 patient_id）；NULL = 患者行已不在
	AlertID     *string    // 关联告警 ID（提醒类为 NULL）
	AlertType   *AlertType // 提醒类为 NULL
	Kind        string     // alert / wear_reminder
	Channel     string     // wechat / sms
	Status      string     // pending / sent / failed / degraded
	Content     string
	RetryCount  int
	SentAt      *time.Time
	CreatedAt   time.Time
}

// RetryQueueItem 本地重试队列项（notification_retry_queue 表行，对齐 T010 降级队列模式）
type RetryQueueItem struct {
	QueueID     int64
	RecordID    int64
	RetryCount  int
	NextRetryAt time.Time
	Status      string // pending / done / failed
}

// AlertNotifyRequest alert→msg 告警推送请求（契约 SendAlertNotificationRequest）
type AlertNotifyRequest struct {
	AlertID        string    `json:"alertId"`
	Type           AlertType `json:"type"`
	PatientID      string    `json:"patientId"`
	DeviceID       string    `json:"deviceId"`
	Detail         string    `json:"detail"`
	SensorPoint    string    `json:"sensorPoint,omitempty"`
	ThresholdValue *float64  `json:"thresholdValue,omitempty"`
	ActualValue    *float64  `json:"actualValue,omitempty"`
	Timestamp      string    `json:"timestamp"` // ISO 8601
}

// SendResult 告警通知受理结果（契约 SendAlertNotificationResponse，review 修改项 #3 定稿）
//
// Accepted=false 仅限服务异常 / SMS 通道不可用；额度耗尽走降级短信（Accepted=true + Degraded=true）。
type SendResult struct {
	Accepted       bool   `json:"accepted"`
	Degraded       bool   `json:"degraded"`
	DegradedReason string `json:"degradedReason,omitempty"`
	RecordID       string `json:"recordId,omitempty"`
}

// 降级原因常量
const DegradedReasonQuotaExhausted = "subscription_quota_exhausted"

// ─────────────────────────────────────────────────────────────
// API DTO（camelCase，对齐 shared-types / api-contracts.ts）
// ─────────────────────────────────────────────────────────────

func fmtTs(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// NotifyRuleDTO 对齐 shared-types NotifyRule
type NotifyRuleDTO struct {
	Type          AlertType `json:"type"`
	Channels      []string  `json:"channels"`
	NotifyTargets []string  `json:"notifyTargets"`
	UpdatedBy     string    `json:"updatedBy,omitempty"`
	UpdatedAt     string    `json:"updatedAt,omitempty"`
}

// ToDTO NotifyRule → DTO
func (r *NotifyRule) ToDTO() NotifyRuleDTO {
	return NotifyRuleDTO{
		Type:          r.Type,
		Channels:      r.Channels,
		NotifyTargets: r.NotifyTargets,
		UpdatedBy:     r.UpdatedBy,
		UpdatedAt:     r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// SubscriptionQuotaDTO 对齐 shared-types SubscriptionQuota
type SubscriptionQuotaDTO struct {
	PatientID string  `json:"patientId"`
	Remaining int     `json:"remaining"`
	Total     int     `json:"total"`
	IsLow     bool    `json:"isLow"`
	UpdatedAt *string `json:"updatedAt"`
}

// ToDTO SubscriptionQuota → DTO
func (q *SubscriptionQuota) ToDTO() SubscriptionQuotaDTO {
	return SubscriptionQuotaDTO{
		PatientID: q.PatientID,
		Remaining: q.Remaining,
		Total:     q.Total,
		IsLow:     q.IsLow,
		UpdatedAt: fmtTs(q.UpdatedAt),
	}
}

// WearReminderDTO 对齐 shared-types WearReminderSettings
type WearReminderDTO struct {
	ReminderEnabled bool    `json:"reminderEnabled"`
	ReminderTime    *string `json:"reminderTime"`
}

// ToDTO WearReminderSettings → DTO
func (w *WearReminderSettings) ToDTO() WearReminderDTO {
	return WearReminderDTO{ReminderEnabled: w.ReminderEnabled, ReminderTime: w.ReminderTime}
}

// NotificationRecordDTO 对齐 shared-types NotificationRecord
// patientName：T278-③ 后端 join 带出，后台列表患者列不再只显示编号；
// NULL = 患者行缺失（前端可回落 patientId，口径同 Alert.patientName）
type NotificationRecordDTO struct {
	RecordID    string     `json:"recordId"`
	PatientID   string     `json:"patientId"`
	PatientName *string    `json:"patientName"`
	AlertID     *string    `json:"alertId,omitempty"`
	AlertType   *AlertType `json:"alertType,omitempty"`
	Channel     string     `json:"channel"`
	Status      string     `json:"status"`
	Content     string     `json:"content"`
	RetryCount  int        `json:"retryCount"`
	SentAt      *string    `json:"sentAt"`
	CreatedAt   string     `json:"createdAt"`
}

// ToDTO NotificationRecord → DTO
func (n *NotificationRecord) ToDTO() NotificationRecordDTO {
	return NotificationRecordDTO{
		RecordID:    fmt.Sprintf("%d", n.RecordID),
		PatientID:   n.PatientID,
		PatientName: n.PatientName,
		AlertID:     n.AlertID,
		AlertType:   n.AlertType,
		Channel:     n.Channel,
		Status:      n.Status,
		Content:     n.Content,
		RetryCount:  n.RetryCount,
		SentAt:      fmtTs(n.SentAt),
		CreatedAt:   n.CreatedAt.UTC().Format(time.RFC3339),
	}
}
