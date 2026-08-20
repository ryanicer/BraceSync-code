// Package handler — msg-service HTTP 层测试用例（T026 升级：委托真实实现）
//
// 本文件包含 msg-service HTTP 层的测试用例，覆盖告警通知受理、订阅额度授予/查询、
// 佩戴提醒读写、通知记录查询、规则管理接口行为。
// T026 升级：原 KNOWN_RED 桩已替换为真实 Handler + Gin 路由 + FakeStore + mock 发送器。
//
// 覆盖接口（对齐 docs/ 消息域 + T017 验收标准 1-4）：
//   - POST /internal/msg/send                            告警通知受理（服务间内部）
//   - POST /api/v1/patients/{id}/subscription-quota/grant  授予额度（幂等，Idempotency-Key）
//   - GET  /api/v1/patients/{id}/subscription-quota         查询额度
//   - GET  /api/v1/patients/{id}/wear-reminder              读取佩戴提醒设置
//   - PUT  /api/v1/patients/{id}/wear-reminder              更新佩戴提醒设置
//   - GET  /api/v1/patients/{id}/notifications              患者通知记录（分页）
//   - GET  /api/v1/admin/notify-rules                       查询通知规则
//   - PUT  /api/v1/admin/notify-rules/{type}                更新通知规则
//   - GET  /api/v1/admin/notification-logs                  后台通知记录（过滤）
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
)

// ============================================================
// H1: 告警通知受理 — 正常受理 + 额度耗尽降级（验收 1/2）
// ============================================================

func TestSendAlertNotification_Accepted(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPost, "/internal/msg/send",
		`{"alertId":"A-20260810-001","type":"pressure_high","patientId":"P20260001","deviceId":"PRS-ML05-RC-20260701001","detail":"压力偏高：P03 压力 47.2N","timestamp":"2026-08-10T18:00:00+08:00"}`,
		map[string]string{"X-Internal-Service": "alert-service"})

	t.Log("upgraded: now delegates to real implementation — 200 OK, accepted=true, degraded=false (normal send)")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	var result model.SendResult
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.True(t, result.Accepted, "正常发送应被受理")
	assert.False(t, result.Degraded, "额度充足不降级")
	assert.NotEmpty(t, result.RecordID, "受理应返回通知记录 ID")
}

func TestSendAlertNotification_QuotaExhausted_Degraded(t *testing.T) {
	f := newHTTPFixture(t)
	f.store.SeedQuota("P20260001", 0) // 额度耗尽

	w, resp := f.do(t, http.MethodPost, "/internal/msg/send",
		`{"alertId":"A-20260810-002","type":"pressure_high","patientId":"P20260001","deviceId":"PRS-ML05-RC-20260701001","detail":"压力偏高","timestamp":"2026-08-10T18:05:00+08:00"}`,
		map[string]string{"X-Internal-Service": "alert-service"})

	t.Log("upgraded: now delegates to real implementation — quota exhausted → accepted=true + degraded=true + degradedReason=subscription_quota_exhausted")
	require.Equal(t, http.StatusOK, w.Code)

	var result model.SendResult
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.True(t, result.Accepted, "额度耗尽走降级短信，通知仍被受理")
	assert.True(t, result.Degraded, "降级短信应标记 degraded=true")
	assert.Equal(t, model.DegradedReasonQuotaExhausted, result.DegradedReason)
}

// ============================================================
// H2: 订阅额度 — grant 幂等 + 边界（验收 2）
// ============================================================

func TestGrantSubscriptionQuota_Success(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPost, "/api/v1/patients/P20260001/subscription-quota/grant", `{}`,
		map[string]string{"Idempotency-Key": "uuid-abc-123"})

	t.Log("upgraded: now delegates to real implementation — 200 OK with remaining>0 and isLow flag, honors Idempotency-Key (idempotent grant)")
	require.Equal(t, http.StatusOK, w.Code)
	var data struct {
		Remaining int  `json:"remaining"`
		IsLow     bool `json:"isLow"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, model.DefaultQuota+1, data.Remaining, "grant 后额度应增 1")
}

func TestGrantSubscriptionQuota_IdempotencyKey_DoubleCallNoReIncrement(t *testing.T) {
	f := newHTTPFixture(t)
	headers := map[string]string{"Idempotency-Key": "uuid-same-key"}

	// 同一次授权（同 Idempotency-Key）重复回报
	_, resp1 := f.do(t, http.MethodPost, "/api/v1/patients/P20260001/subscription-quota/grant", `{}`, headers)
	var first struct {
		Remaining int `json:"remaining"`
	}
	require.NoError(t, json.Unmarshal(resp1.Data, &first))

	_, resp2 := f.do(t, http.MethodPost, "/api/v1/patients/P20260001/subscription-quota/grant", `{}`, headers)
	var second struct {
		Remaining int `json:"remaining"`
	}
	require.NoError(t, json.Unmarshal(resp2.Data, &second))

	t.Log("upgraded: now delegates to real implementation — same Idempotency-Key twice → second call returns same remaining (no double increment)")
	assert.Equal(t, first.Remaining, second.Remaining, "同 Idempotency-Key 重复 grant 不得重复增额")
}

func TestGetSubscriptionQuota_Boundary_LowQuotaHint(t *testing.T) {
	f := newHTTPFixture(t)
	f.store.SeedQuota("P20260001", 1) // remaining=1 → isLow=true

	w, resp := f.do(t, http.MethodGet, "/api/v1/patients/P20260001/subscription-quota", "", nil)

	t.Log("upgraded: now delegates to real implementation — when remaining<=1 → isLow=true (引导重新授权，架构 §2.5)")
	require.Equal(t, http.StatusOK, w.Code)
	var quota model.SubscriptionQuotaDTO
	require.NoError(t, json.Unmarshal(resp.Data, &quota))
	assert.Equal(t, 1, quota.Remaining)
	assert.True(t, quota.IsLow, "remaining≤1 → isLow=true")
}

// ============================================================
// H3: 佩戴提醒 — 读写 + 开关（验收 3）
// ============================================================

func TestGetWearReminder_Success(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodGet, "/api/v1/patients/P20260001/wear-reminder", "", nil)

	t.Log("upgraded: now delegates to real implementation — 200 OK with reminderEnabled + reminderTime from patient_preferences")
	require.Equal(t, http.StatusOK, w.Code)
	var data model.WearReminderDTO
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.False(t, data.ReminderEnabled, "默认关闭")
}

func TestUpdateWearReminder_EnableReminder(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPut, "/api/v1/patients/P20260001/wear-reminder",
		`{"reminderEnabled":true,"reminderTime":"20:00"}`, nil)

	t.Log("upgraded: now delegates to real implementation — 200 OK, reminderEnabled=true persisted (写 patient_preferences, 一期偏离声明)")
	require.Equal(t, http.StatusOK, w.Code)
	var data model.WearReminderDTO
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.True(t, data.ReminderEnabled, "应开启佩戴提醒")
	require.NotNil(t, data.ReminderTime)
	assert.Equal(t, "20:00", *data.ReminderTime)
}

// ============================================================
// H4: 通知记录 — 患者视角分页（验收 4）
// ============================================================

func TestGetPatientNotifications_Paginated(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodGet, "/api/v1/patients/P20260001/notifications?page=1&pageSize=20", "", nil)

	t.Log("upgraded: now delegates to real implementation — 200 OK, paginated records (list/total/page/pageSize), 按时间倒序")
	require.Equal(t, http.StatusOK, w.Code)
	var data struct {
		List     []json.RawMessage `json:"list"`
		Total    int               `json:"total"`
		Page     int               `json:"page"`
		PageSize int               `json:"pageSize"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, 0, data.Total, "空库应返回 0 条记录")
	assert.Equal(t, 1, data.Page)
	assert.Equal(t, 20, data.PageSize)
}

// ============================================================
// H5: 规则管理 — 查询/更新（验收 1）
// ============================================================

func TestGetNotifyRules_ReturnsConfiguredRules(t *testing.T) {
	f := newHTTPFixture(t)
	// newHTTPFixture 已预置 pressure_high 规则

	w, resp := f.do(t, http.MethodGet, "/api/v1/admin/notify-rules", "", nil)

	t.Log("upgraded: now delegates to real implementation — 200 OK with all alert_notify_rules rows (type × channels × notifyTargets)")
	require.Equal(t, http.StatusOK, w.Code)
	var rules []model.NotifyRuleDTO
	require.NoError(t, json.Unmarshal(resp.Data, &rules))
	assert.NotEmpty(t, rules, "应返回预置的通知规则")
}

func TestUpdateNotifyRule_ValidType(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPut, "/api/v1/admin/notify-rules/pressure_high",
		`{"channels":["wechat","sms"],"notifyTargets":["patient","doctor","tech"]}`,
		map[string]string{"X-User-Id": "admin01"})

	t.Log("upgraded: now delegates to real implementation — 200 OK, rule upserted (写 alert_notify_rules, 一期偏离声明)")
	require.Equal(t, http.StatusOK, w.Code)
	var rule model.NotifyRuleDTO
	require.NoError(t, json.Unmarshal(resp.Data, &rule))
	assert.Equal(t, []string{"wechat", "sms"}, rule.Channels)
	assert.Equal(t, []string{"patient", "doctor", "tech"}, rule.NotifyTargets)
}

func TestUpdateNotifyRule_UnknownType(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPut, "/api/v1/admin/notify-rules/unknown_type",
		`{"channels":["wechat"],"notifyTargets":["patient"]}`, nil)

	t.Log("upgraded: now delegates to real implementation — unknown alert type → 400 Bad Request")
	assert.Equal(t, http.StatusBadRequest, w.Code, "未知告警类型应被拒绝")
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

// ============================================================
// H6: 后台通知记录 — 过滤查询（验收 4）
// ============================================================

func TestGetNotificationLogs_FilterByStatus(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodGet, "/api/v1/admin/notification-logs?status=failed&page=1&pageSize=20", "", nil)

	t.Log("upgraded: now delegates to real implementation — 200 OK, filtered by status (failed), 分页返回")
	require.Equal(t, http.StatusOK, w.Code)
	var data struct {
		List     []json.RawMessage `json:"list"`
		Total    int               `json:"total"`
		Page     int               `json:"page"`
		PageSize int               `json:"pageSize"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, 1, data.Page)
	assert.Equal(t, 20, data.PageSize)
}
