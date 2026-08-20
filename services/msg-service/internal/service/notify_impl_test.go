// Package service — NotifyService 实现侧单元测试（Winner 实现，T017 转绿阶段）
//
// 与 Ella 预置契约测试（service_test.go，31 用例之一部）互补：本文件直接验证
// internal/service/notify.go + scheduler.go 的真实实现（内存 FakeStore + mock 发送器），
// 覆盖高危模块（路由 / 额度 / 重试）≥90% 门禁。
//
// 覆盖语义（对齐 T017 验收标准 1-4 + review 定稿）：
//   - 路由：已知 type 命中规则；未知 type / 未配置规则不发送
//   - 额度：grant 幂等（Idempotency-Key）；发送成功内部扣减；耗尽降级短信（accepted=true+degraded=true）；
//     SMS 不可用 → accepted=false
//   - 佩戴提醒：开关关闭不推送；额度耗尽静默跳过（不降级）；当日去重；达标跳过
//   - 重试：失败落 failed + 入队；重试成功置 sent+sentAt；退避重排；达上限放弃
package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/msg-service/internal/repo"
	"github.com/bracesync/bracesync/services/msg-service/internal/testutil"
)

// cst 业务时区（Asia/Shanghai 等价固定时区，测试免 tzdata 依赖）
var cst = time.FixedZone("CST", 8*3600)

// implFixture 实现侧测试夹具：FakeStore + mock 发送器 + 固定时钟（2026-08-10 20:05 CST）
type implFixture struct {
	svc   *NotifyService
	store *testutil.FakeStore
	wx    *MockWechatSender
	sms   *MockSMSSender
	now   time.Time
}

func newImplFixture(t *testing.T) *implFixture {
	t.Helper()
	store := testutil.NewFakeStore()
	wx := NewMockWechatSender(zerolog.Nop())
	sms := NewMockSMSSender(zerolog.Nop())
	svc := NewNotifyService(store, wx, sms, zerolog.Nop())
	now := time.Date(2026, 8, 10, 20, 5, 0, 0, cst)
	svc.SetNow(func() time.Time { return now })
	return &implFixture{svc: svc, store: store, wx: wx, sms: sms, now: now}
}

// seedDefaultRules 预置默认路由规则（对齐 seed.sql alert_notify_rules）
func (f *implFixture) seedDefaultRules() {
	f.store.SeedRule(model.NotifyRule{
		Type: model.AlertTypePressureHigh, Channels: []string{model.ChannelWechat, model.ChannelSMS},
		NotifyTargets: []string{model.TargetPatient, model.TargetDoctor},
	})
	f.store.SeedRule(model.NotifyRule{
		Type: model.AlertTypeSensorDrift, Channels: []string{model.ChannelWechat},
		NotifyTargets: []string{model.TargetTech, model.TargetOps},
	})
}

func (f *implFixture) alertReq(alertType model.AlertType) model.AlertNotifyRequest {
	return model.AlertNotifyRequest{
		AlertID:   "A-20260810-001",
		Type:      alertType,
		PatientID: "P20260001",
		DeviceID:  "PRS-ML05-RC-20260701001",
		Detail:    "压力偏高：P03 压力 47.2N",
		Timestamp: "2026-08-10T18:00:00+08:00",
	}
}

// waitForRecord 等待异步 deliver 将记录置为目标状态（goroutine 推送，最终一致）
func (f *implFixture) waitForRecord(t *testing.T, recordID int64, status string) *model.NotificationRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, err := f.store.GetNotificationRecord(context.Background(), recordID)
		require.NoError(t, err)
		if rec.Status == status {
			return rec
		}
		time.Sleep(5 * time.Millisecond)
	}
	rec, _ := f.store.GetNotificationRecord(context.Background(), recordID)
	t.Fatalf("record %d did not reach status %q (current: %s)", recordID, status, rec.Status)
	return nil
}

// ─────────────────────────────────────────────────────────────
// 路由（验收 1）
// ─────────────────────────────────────────────────────────────

func TestImplRouteNotify_KnownTypeReturnsRule(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()

	rule, err := f.svc.RouteNotify(context.Background(), model.AlertTypePressureHigh)
	require.NoError(t, err)
	require.NotNil(t, rule, "pressure_high 已配置规则")
	assert.Contains(t, rule.Channels, model.ChannelWechat)
	assert.Contains(t, rule.NotifyTargets, model.TargetPatient)
}

func TestImplRouteNotify_UnknownTypeNoSend(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()

	rule, err := f.svc.RouteNotify(context.Background(), model.AlertType("unknown_type"))
	assert.NoError(t, err, "未知 type 静默忽略，不报错")
	assert.Nil(t, rule)
}

func TestImplRouteNotify_ValidTypeWithoutRuleNoSend(t *testing.T) {
	f := newImplFixture(t) // 未预置任何规则

	rule, err := f.svc.RouteNotify(context.Background(), model.AlertTypeWearInterrupt)
	assert.NoError(t, err)
	assert.Nil(t, rule, "合法类型但未配置规则 → 不发送")
}

func TestImplSendAlert_UnknownTypeRejectedNoRecord(t *testing.T) {
	f := newImplFixture(t)

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertType("bogus")))
	require.NoError(t, err)
	assert.False(t, res.Accepted, "未知 type 不发送")
	assert.False(t, res.Degraded)
	assert.Empty(t, res.RecordID, "不产生通知记录")
}

// ─────────────────────────────────────────────────────────────
// 额度与降级（验收 2，review 修改项 #3）
// ─────────────────────────────────────────────────────────────

func TestImplSendAlert_NormalWechat_ConsumesQuotaOnSuccess(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	require.True(t, res.Accepted)
	assert.False(t, res.Degraded, "额度充足走微信，不降级")
	assert.NotEmpty(t, res.RecordID)

	recordID, _ := strconvAtoi(t, res.RecordID)
	rec := f.waitForRecord(t, recordID, model.StatusSent)
	assert.NotNil(t, rec.SentAt, "成功发送置 sentAt")
	assert.Equal(t, model.ChannelWechat, rec.Channel)

	quota, err := f.svc.GetQuota(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.Equal(t, model.DefaultQuota-1, quota.Remaining, "发送成功内部扣减 1 额度")
	assert.Len(t, f.wx.Sends(), 1)
}

func TestImplSendAlert_QuotaExhausted_DegradesToSMS(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.store.SeedQuota("P20260001", 0) // 额度耗尽

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	assert.True(t, res.Accepted, "额度耗尽走降级短信，通知仍被受理")
	assert.True(t, res.Degraded)
	assert.Equal(t, model.DegradedReasonQuotaExhausted, res.DegradedReason)

	recordID, _ := strconvAtoi(t, res.RecordID)
	rec := f.waitForRecord(t, recordID, model.StatusDegraded)
	assert.Equal(t, model.ChannelSMS, rec.Channel, "降级走短信渠道")
	assert.Len(t, f.sms.Sends(), 1)
	assert.Empty(t, f.wx.Sends(), "额度耗尽不发微信")
}

func TestImplSendAlert_QuotaExhausted_SMSUnavailable_Rejected(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.store.SeedQuota("P20260001", 0)
	f.svc.sms = nil // SMS 通道不可用

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	assert.False(t, res.Accepted, "额度耗尽且 SMS 不可用 → accepted=false（非降级）")
	assert.False(t, res.Degraded)
}

func TestImplGrantQuota_IdempotentByKey(t *testing.T) {
	f := newImplFixture(t)
	ctx := context.Background()

	first, err := f.svc.GrantQuota(ctx, "P20260001", "idem-key-001")
	require.NoError(t, err)
	assert.Equal(t, model.DefaultQuota+1, first.Remaining, "首次授权 +1")

	second, err := f.svc.GrantQuota(ctx, "P20260001", "idem-key-001")
	require.NoError(t, err)
	assert.Equal(t, first.Remaining, second.Remaining, "同 Idempotency-Key 不重复增额")

	third, err := f.svc.GrantQuota(ctx, "P20260001", "idem-key-002")
	require.NoError(t, err)
	assert.Equal(t, first.Remaining+1, third.Remaining, "不同授权各增 1")
	assert.Equal(t, model.DefaultQuota+2, third.Total, "total = 默认 3 + 台账增量合计")
}

func TestImplGetQuota_LowBoundary(t *testing.T) {
	f := newImplFixture(t)
	f.store.SeedQuota("P20260001", 1)

	quota, err := f.svc.GetQuota(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.True(t, quota.IsLow, "remaining≤1 → isLow（引导重新授权，架构 §2.5）")
}

// ─────────────────────────────────────────────────────────────
// 失败重试（验收 4）
// ─────────────────────────────────────────────────────────────

func TestImplSendAlert_SendFailure_FailedRecordAndRetryQueued(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.wx.Err = errors.New("wechat api down")

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	require.True(t, res.Accepted, "推送失败仍受理（记录落库 + 重试队列，不丢通知）")

	recordID, _ := strconvAtoi(t, res.RecordID)
	rec := f.waitForRecord(t, recordID, model.StatusFailed)
	assert.Equal(t, model.StatusFailed, rec.Status)

	// 重试项在退避窗口后到期可拉取
	items, err := f.store.ListDueRetries(context.Background(), f.now.Add(RetryDelay(1)), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, recordID, items[0].RecordID)
}

func TestImplDrainRetries_SuccessSetsSentAndConsumesQuota(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.wx.Err = errors.New("wechat api down")

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	recordID, _ := strconvAtoi(t, res.RecordID)
	f.waitForRecord(t, recordID, model.StatusFailed)

	// 通道恢复 + 时钟越过退避窗口 → 排空重试
	f.wx.Err = nil
	f.svc.SetNow(func() time.Time { return f.now.Add(RetryDelay(1) + time.Second) })
	n, err := f.svc.DrainRetries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	rec := f.waitForRecord(t, recordID, model.StatusSent)
	assert.NotNil(t, rec.SentAt, "重试成功置 sentAt")

	quota, err := f.svc.GetQuota(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.Equal(t, model.DefaultQuota-1, quota.Remaining, "重试成功时补扣额度（首次失败未扣）")

	// 队列已排空（done 终态，不再重复处理）
	items, err := f.store.ListDueRetries(context.Background(), f.now.Add(24*time.Hour), 10)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestImplDrainRetries_ExhaustedGivesUp(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.wx.Err = errors.New("wechat api down")

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	recordID, _ := strconvAtoi(t, res.RecordID)
	f.waitForRecord(t, recordID, model.StatusFailed)

	// 连续失败直至 MaxRetries：每轮时钟越过下一个退避窗口
	cursor := f.now
	for i := 0; i < MaxRetries; i++ {
		cursor = cursor.Add(RetryDelay(i+1) + time.Second)
		f.svc.SetNow(func() time.Time { return cursor })
		_, err := f.svc.DrainRetries(context.Background())
		require.NoError(t, err)
	}

	rec, err := f.store.GetNotificationRecord(context.Background(), recordID)
	require.NoError(t, err)
	assert.Equal(t, MaxRetries, rec.RetryCount, "重试次数累计到上限")
	assert.Equal(t, model.StatusFailed, rec.Status, "记录保留可追溯")

	items, err := f.store.ListDueRetries(context.Background(), cursor.Add(24*time.Hour), 10)
	require.NoError(t, err)
	assert.Empty(t, items, "达上限后队列项置 failed，不再重排")
}

func TestImplRetryDelay_ExponentialBackoff(t *testing.T) {
	assert.Equal(t, 5*time.Minute, RetryDelay(1))
	assert.Equal(t, 10*time.Minute, RetryDelay(2))
	assert.Equal(t, 20*time.Minute, RetryDelay(3))
	assert.Equal(t, RetryMaxDelay, RetryDelay(10), "退避上限 2h")
}

// ─────────────────────────────────────────────────────────────
// 佩戴提醒（验收 2/3）
// ─────────────────────────────────────────────────────────────

func TestImplSendWearReminder_DisabledNoPush(t *testing.T) {
	f := newImplFixture(t)
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", false, nil)
	require.NoError(t, err)

	res, err := f.svc.SendWearReminder(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.False(t, res.Accepted, "开关关闭不推送")
	assert.Empty(t, f.wx.Sends(), "不产生发送")
}

func TestImplSendWearReminder_SuccessConsumesQuota_NoDegrade(t *testing.T) {
	f := newImplFixture(t)
	rt := "20:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
	require.NoError(t, err)

	res, err := f.svc.SendWearReminder(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.True(t, res.Accepted)
	assert.False(t, res.Degraded, "佩戴提醒永不降级短信")
	assert.Empty(t, res.DegradedReason)
	assert.NotEmpty(t, res.RecordID)

	quota, err := f.svc.GetQuota(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.Equal(t, model.DefaultQuota-1, quota.Remaining, "提醒发送成功同样内部扣减")
}

func TestImplSendWearReminder_QuotaExhausted_SilentSkip(t *testing.T) {
	f := newImplFixture(t)
	rt := "20:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
	require.NoError(t, err)
	f.store.SeedQuota("P20260001", 0)

	res, err := f.svc.SendWearReminder(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.False(t, res.Accepted, "额度耗尽静默跳过")
	assert.False(t, res.Degraded, "佩戴提醒不降级短信（成本控制）")
	assert.Empty(t, f.sms.Sends(), "不发短信")
	assert.Empty(t, f.wx.Sends())
}

func TestImplSendWearReminder_DuplicateSameDay_Skipped(t *testing.T) {
	f := newImplFixture(t)
	rt := "20:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
	require.NoError(t, err)

	first, err := f.svc.SendWearReminder(context.Background(), "P20260001")
	require.NoError(t, err)
	require.True(t, first.Accepted)

	second, err := f.svc.SendWearReminder(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.False(t, second.Accepted, "当日已推送 → 去重（防 15min 扫描窗口重复）")
	assert.Len(t, f.wx.Sends(), 1)
}

func TestImplSendWearReminder_SendFailure_EnqueuesRetry(t *testing.T) {
	f := newImplFixture(t)
	rt := "20:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
	require.NoError(t, err)
	f.wx.Err = errors.New("wechat api down")

	res, err := f.svc.SendWearReminder(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.True(t, res.Accepted, "失败仍受理：记录落库 + 重试队列")

	recordID, _ := strconvAtoi(t, res.RecordID)
	rec := f.waitForRecord(t, recordID, model.StatusFailed)
	assert.Equal(t, model.KindWearReminder, rec.Kind)
}

// ─────────────────────────────────────────────────────────────
// 定时扫描（验收 3，架构 §7）
// ─────────────────────────────────────────────────────────────

func TestImplScanReminders_DueNotCompliant_Pushes(t *testing.T) {
	f := newImplFixture(t) // now = 20:05
	rt := "20:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
	require.NoError(t, err)
	// 今日佩戴 10h（< 22h 目标）→ 未达标

	pushed, err := f.svc.ScanReminders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, pushed, "到点 + 未达标 → 推送")
	assert.Len(t, f.wx.Sends(), 1)
}

func TestImplScanReminders_Compliant_Skipped(t *testing.T) {
	f := newImplFixture(t)
	rt := "20:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
	require.NoError(t, err)
	f.store.SeedWearMinutes("P20260001", "2026-08-10", 23*60) // 今日已达标

	pushed, err := f.svc.ScanReminders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, pushed, "今日已达标 → 跳过")
	assert.Empty(t, f.wx.Sends())
}

func TestImplScanReminders_NotDueOrDisabled_Skipped(t *testing.T) {
	f := newImplFixture(t) // now = 20:05
	late := "21:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &late)
	require.NoError(t, err)
	off := "19:00"
	_, err = f.store.UpdateWearReminder(context.Background(), "P20260002", false, &off)
	require.NoError(t, err)

	pushed, err := f.svc.ScanReminders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, pushed, "未到点 / 开关关闭均不推送")
}

func TestImplScanReminders_SecondScan_Deduped(t *testing.T) {
	f := newImplFixture(t)
	rt := "20:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
	require.NoError(t, err)

	first, err := f.svc.ScanReminders(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, first)

	// 15min 后二次扫描：当日已推送 → 去重
	f.svc.SetNow(func() time.Time { return f.now.Add(15 * time.Minute) })
	second, err := f.svc.ScanReminders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, second)
}

// ─────────────────────────────────────────────────────────────
// 规则管理 / 记录查询（验收 1/4）
// ─────────────────────────────────────────────────────────────

func TestImplUpdateNotifyRule_Validation(t *testing.T) {
	f := newImplFixture(t)
	ctx := context.Background()

	_, err := f.svc.UpdateNotifyRule(ctx, model.AlertType("unknown_type"), []string{"wechat"}, []string{"patient"}, "admin")
	require.Error(t, err, "未知告警类型应被拒绝")
	assert.Equal(t, model.CodeInvalidParam, err.(*model.AppError).Code)

	_, err = f.svc.UpdateNotifyRule(ctx, model.AlertTypePressureHigh, []string{"email"}, []string{"patient"}, "admin")
	require.Error(t, err, "非法渠道应被拒绝")

	_, err = f.svc.UpdateNotifyRule(ctx, model.AlertTypePressureHigh, []string{"wechat"}, []string{"boss"}, "admin")
	require.Error(t, err, "非法通知目标应被拒绝")

	rule, err := f.svc.UpdateNotifyRule(ctx, model.AlertTypePressureHigh,
		[]string{"wechat", "sms"}, []string{"patient", "doctor", "tech"}, "admin")
	require.NoError(t, err)
	assert.Equal(t, model.AlertTypePressureHigh, rule.Type)

	rules, err := f.svc.GetNotifyRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, []string{"wechat", "sms"}, rules[0].Channels)
}

func TestImplGetNotificationLogs_FilterValidation(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()
	_, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)

	_, _, err = f.svc.GetNotificationLogs(context.Background(), repo.RecordFilter{Status: "bogus"})
	require.Error(t, err, "非法 status 过滤应被拒绝")

	records, total, err := f.svc.GetNotificationLogs(context.Background(), repo.RecordFilter{PatientID: "P20260001"})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, records, 1)
	assert.Equal(t, model.AlertTypePressureHigh, *records[0].AlertType)
}

func TestImplUpdateWearReminder_InvalidTime(t *testing.T) {
	f := newImplFixture(t)
	bad := "25:00"
	_, err := f.svc.UpdateWearReminder(context.Background(), "P20260001", true, &bad)
	require.Error(t, err, "非法 HH:mm 应被拒绝")
	assert.Equal(t, model.CodeInvalidParam, err.(*model.AppError).Code)
}

// ─────────────────────────────────────────────────────────────
// 测试辅助
// ─────────────────────────────────────────────────────────────

func strconvAtoi(t *testing.T, s string) (int64, error) {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	require.NoError(t, err)
	return n, nil
}
