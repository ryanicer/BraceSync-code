// Package service — msg-service 业务层测试用例（T026 升级：委托真实实现）
//
// 本文件覆盖 msg-service 核心业务规则：告警通知路由、订阅额度消费/降级、
// 佩戴提醒推送、发送记录与重试队列语义。
// T026 升级：原 KNOWN_RED 桩已替换为真实 NotifyService + FakeStore + mock 发送器。
//
// 覆盖规则（对齐 T017 验收标准 1-4 + 接口 review 定稿语义）：
//   - 告警通知路由：alert_notify_rules 按 type 路由到 channels/targets，未知 type 不发送
//   - 订阅额度：grant 幂等（同 Idempotency-Key 不重复增额）；额度耗尽 → 降级短信
//     （accepted=true + degraded=true）；佩戴提醒不降级短信（成本控制，架构 §2.5）
//   - 佩戴提醒：到点且未达标患者推送；开关关闭不推送（对齐架构 §7 定时任务语义）
//   - 发送记录落库 + 失败重试（不丢通知）；额度扣减发生在发送时（内部行为，不暴露）
package service

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/msg-service/internal/testutil"
)

// serviceFixture 业务层测试夹具（真实 NotifyService + FakeStore + mock 发送器）
type serviceFixture struct {
	svc   *NotifyService
	store *testutil.FakeStore
	wx    *MockWechatSender
	sms   *MockSMSSender
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	store := testutil.NewFakeStore()
	wx := NewMockWechatSender(zerolog.Nop())
	sms := NewMockSMSSender(zerolog.Nop())
	svc := NewNotifyService(store, wx, sms, zerolog.Nop())
	// 固定时钟（2026-08-10 20:05 CST）：避免依赖真实 time.Now()，
	// 与 notify_impl_test.go 的 implFixture 对齐（同一 cst 变量）
	svc.SetNow(func() time.Time { return time.Date(2026, 8, 10, 20, 5, 0, 0, cst) })
	return &serviceFixture{svc: svc, store: store, wx: wx, sms: sms}
}

// ============================================================
// S1: 告警通知路由 — 按 type 路由到 channels/targets（验收 1）
// ============================================================

func TestRouteNotify_KnownType_RoutesToConfiguredChannelsAndTargets(t *testing.T) {
	f := newServiceFixture(t)
	f.store.SeedRule(model.NotifyRule{
		Type:          model.AlertTypePressureHigh,
		Channels:      []string{model.ChannelWechat, model.ChannelSMS},
		NotifyTargets: []string{model.TargetPatient, model.TargetDoctor},
	})

	rule, err := f.svc.RouteNotify(context.Background(), model.AlertTypePressureHigh)

	t.Log("upgraded: now delegates to real implementation — pressure_high → channels=[wechat,sms], targets=[patient,doctor]")
	require.NoError(t, err)
	require.NotNil(t, rule, "已知告警类型应返回路由规则")
	assert.Contains(t, rule.Channels, model.ChannelWechat, "pressure_high 应配置微信渠道")
	assert.Contains(t, rule.NotifyTargets, model.TargetPatient, "告警必须通知患者本人")
	assert.Contains(t, rule.NotifyTargets, model.TargetDoctor, "告警必须通知医生")
}

func TestRouteNotify_UnknownType_NoSend(t *testing.T) {
	f := newServiceFixture(t)

	rule, err := f.svc.RouteNotify(context.Background(), model.AlertType("unknown_type"))

	t.Log("upgraded: now delegates to real implementation — unknown type → nil rule, no error (silently ignore)")
	assert.NoError(t, err)
	assert.Nil(t, rule, "未知告警类型不应返回规则")
}

// ============================================================
// S2: 订阅额度 — grant 幂等（验收 2）
// ============================================================

func TestGrantQuota_Idempotent_SameKeyNoDoubleIncrement(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()

	// 同一次授权（同一 Idempotency-Key）重复回报 → 只增额一次
	first, err1 := f.svc.GrantQuota(ctx, "P20260001", "idem-key-001")
	require.NoError(t, err1)
	second, err2 := f.svc.GrantQuota(ctx, "P20260001", "idem-key-001")
	require.NoError(t, err2)

	t.Log("upgraded: now delegates to real implementation — same Idempotency-Key → second call does NOT re-increment (idempotent)")
	assert.Equal(t, first.Remaining, second.Remaining, "同 Idempotency-Key 重复 grant 不得重复增额")
}

func TestGrantQuota_DifferentKeys_Increment(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()

	first, err1 := f.svc.GrantQuota(ctx, "P20260001", "idem-key-001")
	require.NoError(t, err1)
	second, err2 := f.svc.GrantQuota(ctx, "P20260001", "idem-key-002")
	require.NoError(t, err2)

	t.Log("upgraded: now delegates to real implementation — different keys → second call remaining > first (each authorization increments)")
	assert.Greater(t, second.Remaining, first.Remaining, "不同授权应各自增额")
}

// ============================================================
// S3: 额度耗尽 → 降级短信（验收 2，review 修改项 #3）
// ============================================================

func TestSendAlert_QuotaExhausted_DegradesToSMS(t *testing.T) {
	f := newServiceFixture(t)
	f.store.SeedRule(model.NotifyRule{
		Type:          model.AlertTypePressureHigh,
		Channels:      []string{model.ChannelWechat, model.ChannelSMS},
		NotifyTargets: []string{model.TargetPatient},
	})
	f.store.SeedQuota("P20260001", 0) // 额度耗尽

	result, err := f.svc.SendAlert(context.Background(), model.AlertNotifyRequest{
		PatientID: "P20260001",
		Type:      model.AlertTypePressureHigh,
		Detail:    "压力偏高：P03 压力 47.2N",
	})

	t.Log("upgraded: now delegates to real implementation — quota exhausted → accepted=true + degraded=true + degradedReason=subscription_quota_exhausted")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Accepted, "额度耗尽走降级短信，通知仍被受理")
	assert.True(t, result.Degraded, "降级短信应标记 degraded=true")
	assert.Equal(t, model.DegradedReasonQuotaExhausted, result.DegradedReason)
}

func TestSendAlert_ServiceDown_AcceptedFalse(t *testing.T) {
	// SMS 通道不可用 + 额度耗尽 → accepted=false（区别于额度耗尽的降级）
	store := testutil.NewFakeStore()
	wx := NewMockWechatSender(zerolog.Nop())
	svc := NewNotifyService(store, wx, nil, zerolog.Nop()) // nil sms
	store.SeedRule(model.NotifyRule{
		Type:          model.AlertTypePressureHigh,
		Channels:      []string{model.ChannelWechat, model.ChannelSMS},
		NotifyTargets: []string{model.TargetPatient},
	})
	store.SeedQuota("P20260001", 0) // 额度耗尽 → 需降级 SMS，但 SMS 不可用

	result, err := svc.SendAlert(context.Background(), model.AlertNotifyRequest{
		PatientID: "P20260001",
		Type:      model.AlertTypePressureHigh,
		Detail:    "内容",
	})

	t.Log("upgraded: now delegates to real implementation — service down / SMS unavailable → accepted=false (NOT degraded)")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Accepted, "服务异常/SMS 不可用 → accepted=false")
	assert.False(t, result.Degraded, "服务异常不属于降级短信语义")
}

// ============================================================
// S4: 佩戴提醒 — 不降级短信 + 开关关闭不推送（验收 2/3）
// ============================================================

func TestSendWearReminder_NoDegradeToSMS(t *testing.T) {
	f := newServiceFixture(t)
	// 启用佩戴提醒 + 额度充足
	_, _ = f.store.UpdateWearReminder(context.Background(), "P20260001", true, nil)

	result, err := f.svc.SendWearReminder(context.Background(), "P20260001")

	t.Log("upgraded: now delegates to real implementation — wear reminder NEVER degrades to SMS (cost control, architecture §2.5)")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Degraded, "佩戴提醒不降级短信")
	assert.Empty(t, result.DegradedReason, "佩戴提醒无降级原因")
}

func TestSendWearReminder_ReminderDisabled_NoSend(t *testing.T) {
	f := newServiceFixture(t)
	f.store.SeedQuota("P20260001", 3)
	_, _ = f.store.UpdateWearReminder(context.Background(), "P20260001", false, nil) // 关闭开关

	result, err := f.svc.SendWearReminder(context.Background(), "P20260001")

	t.Log("upgraded: now delegates to real implementation — reminderEnabled=false → 不推送（Accepted=false），不产生发送记录")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Accepted, "提醒开关关闭时不应推送")
}

// ============================================================
// S5: 佩戴提醒 — 到点且未达标推送（验收 3）
// ============================================================

// TestWearReminderScheduler_DueAndNotCompliant_Pushes 定时任务语义：到点 + 今日未达标 → 推送（架构 §7 每 15min 扫描）
// 经 ScanReminders 验证完整调度链路：reminder_enabled=true + reminder_time 已到 + 今日佩戴未达标 → 推送。
func TestWearReminderScheduler_DueAndNotCompliant_Pushes(t *testing.T) {
	f := newServiceFixture(t)
	f.store.SeedQuota("P20260001", 3)
	reminderTime := "20:00"
	_, _ = f.store.UpdateWearReminder(context.Background(), "P20260001", true, &reminderTime)
	f.store.SeedWearMinutes("P20260001", "2026-08-10", 60) // 远低于 22h 目标，未达标

	t.Log("upgraded: now delegates to real implementation — due (now>=reminder_time) + not compliant today → push wear reminder")

	// 场景：reminder_time=20:00，当前 20:05，今日佩戴时长未达标 → 应推送
	pushed, err := f.svc.ScanReminders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, pushed, "到点且未达标应推送 1 条提醒")
}

// ============================================================
// S6: 发送记录落库 + 失败重试（验收 4）
// ============================================================

func TestSendAlert_RecordsPersisted(t *testing.T) {
	f := newServiceFixture(t)
	f.store.SeedRule(model.NotifyRule{
		Type:          model.AlertTypePressureHigh,
		Channels:      []string{model.ChannelWechat},
		NotifyTargets: []string{model.TargetPatient},
	})

	result, err := f.svc.SendAlert(context.Background(), model.AlertNotifyRequest{
		PatientID: "P20260001",
		Type:      model.AlertTypePressureHigh,
		Detail:    "压力偏高",
	})

	t.Log("upgraded: now delegates to real implementation — every send (accepted) creates a notification_record (status=sent or pending)")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Accepted)
	assert.NotEmpty(t, result.RecordID, "受理成功应返回通知记录 ID（落库可追溯）")
}

func TestSendAlert_Failure_EnqueuesRetry(t *testing.T) {
	f := newServiceFixture(t)
	f.wx.Err = assert.AnError // 模拟微信推送失败
	f.store.SeedRule(model.NotifyRule{
		Type:          model.AlertTypeWearInterrupt,
		Channels:      []string{model.ChannelWechat},
		NotifyTargets: []string{model.TargetPatient},
	})

	result, err := f.svc.SendAlert(context.Background(), model.AlertNotifyRequest{
		PatientID: "P20260001",
		Type:      model.AlertTypeWearInterrupt,
		Detail:    "佩戴中断",
	})

	t.Log("upgraded: now delegates to real implementation — on send failure → record persisted as failed + enqueued for retry")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.RecordID, "失败也应产生通知记录（重试可追溯）")

	// 等待异步推送完成
	time.Sleep(200 * time.Millisecond)

	// 验证记录状态为 failed
	store := f.store
	items, listErr := store.ListDueRetries(context.Background(), time.Now().Add(time.Hour), 10)
	require.NoError(t, listErr)
	assert.NotEmpty(t, items, "失败的发送应进入重试队列")
}

// ============================================================
// S7: 额度扣减发生在发送时（内部行为，不暴露对外接口）（验收 2）
// ============================================================

func TestQuotaConsumedInternally_OnSend(t *testing.T) {
	f := newServiceFixture(t)
	f.store.SeedRule(model.NotifyRule{
		Type:          model.AlertTypePressureHigh,
		Channels:      []string{model.ChannelWechat},
		NotifyTargets: []string{model.TargetPatient},
	})
	ctx := context.Background()

	// 发送告警通知（微信渠道消耗额度）
	_, err := f.svc.SendAlert(ctx, model.AlertNotifyRequest{
		PatientID: "P20260001",
		Type:      model.AlertTypePressureHigh,
		Detail:    "压力偏高",
	})
	require.NoError(t, err)

	// 等待异步发送完成（额度扣减在 goroutine 中执行）
	time.Sleep(200 * time.Millisecond)

	t.Log("upgraded: now delegates to real implementation — grant 由患者授权回报 +1；扣减发生在实际发送订阅消息时（内部自动扣减，无对外扣减接口）")

	quota, err := f.store.GetQuota(ctx, "P20260001")
	require.NoError(t, err)
	assert.Less(t, quota.Remaining, model.DefaultQuota, "发送成功后额度应被内部扣减")
}
