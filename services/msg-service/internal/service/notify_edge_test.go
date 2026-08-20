// Package service — NotifyService 高危分支补强测试（deliver / DrainRetries / 扫描错误分支）
//
// 针对路由/额度/重试高危路径的内部分支：deliver 通道缺失与落库错误、
// DrainRetries 记录缺失与短信重试、ScanReminders 依赖错误、循环默认间隔。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/msg-service/internal/repo"
)

// errStore2 在 errStore 基础上补充 deliver / drain 分支所需的失败方法
type errStore2 struct {
	errStore
}

func (e *errStore2) UpdateNotificationStatus(ctx context.Context, recordID int64, status string, sentAt *time.Time) error {
	if e.failMethod == "UpdateNotificationStatus" {
		return e.err
	}
	return e.Store.UpdateNotificationStatus(ctx, recordID, status, sentAt)
}

func (e *errStore2) ConsumeQuota(ctx context.Context, patientID string) (*model.SubscriptionQuota, error) {
	if e.failMethod == "ConsumeQuota" {
		return nil, e.err
	}
	return e.Store.ConsumeQuota(ctx, patientID)
}

func (e *errStore2) GetNotificationRecord(ctx context.Context, recordID int64) (*model.NotificationRecord, error) {
	if e.failMethod == "GetNotificationRecord" {
		return nil, e.err
	}
	return e.Store.GetNotificationRecord(ctx, recordID)
}

func (e *errStore2) ReminderSentToday(ctx context.Context, patientID string, dayStart time.Time) (bool, error) {
	if e.failMethod == "ReminderSentToday" {
		return false, e.err
	}
	return e.Store.ReminderSentToday(ctx, patientID, dayStart)
}

func (e *errStore2) TodayWearMinutes(ctx context.Context, patientID string, bizDate string) (int, error) {
	if e.failMethod == "TodayWearMinutes" {
		return 0, e.err
	}
	return e.Store.TodayWearMinutes(ctx, patientID, bizDate)
}

func (e *errStore2) FindRules(ctx context.Context) ([]model.NotifyRule, error) {
	if e.failMethod == "FindRules" {
		return nil, e.err
	}
	return e.Store.FindRules(ctx)
}

func (e *errStore2) EnqueueRetry(ctx context.Context, recordID int64, nextRetryAt time.Time) error {
	if e.failMethod == "EnqueueRetry" {
		return e.err
	}
	return e.Store.EnqueueRetry(ctx, recordID, nextRetryAt)
}

func newErrFixture2(t *testing.T, failMethod string) *implFixture {
	t.Helper()
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.svc.store = &errStore2{errStore{Store: f.store, failMethod: failMethod, err: errInjected}}
	return f
}

// ─────────────────────────────────────────────────────────────
// deliver 内部分支
// ─────────────────────────────────────────────────────────────

func TestImplDeliver_NilChannel_FailedAndQueued(t *testing.T) {
	f := newImplFixture(t)
	rec := model.NotificationRecord{
		RecordID: 0, PatientID: "P20260001", Kind: model.KindAlert,
		Channel: model.ChannelWechat, Status: model.StatusPending, Content: "x",
	}
	require.NoError(t, f.store.CreateNotificationRecord(context.Background(), &rec))

	f.svc.wechat = nil // 受理后通道失效：deliver 走失败 + 重试队列分支
	f.svc.deliver(rec, model.StatusSent)
	got := f.waitForRecord(t, rec.RecordID, model.StatusFailed)
	assert.Equal(t, model.StatusFailed, got.Status)

	items, err := f.store.ListDueRetries(context.Background(), f.now.Add(RetryDelay(1)), 10)
	require.NoError(t, err)
	assert.Len(t, items, 1, "通道缺失同样进重试队列，不丢通知")
}

func TestImplDeliver_StatusUpdateError_StillCompletes(t *testing.T) {
	f := newErrFixture2(t, "UpdateNotificationStatus")
	rec := model.NotificationRecord{
		PatientID: "P20260001", Kind: model.KindAlert,
		Channel: model.ChannelWechat, Status: model.StatusPending, Content: "x",
	}
	require.NoError(t, f.store.CreateNotificationRecord(context.Background(), &rec))

	// 状态更新失败仅记日志，不 panic（降级容错分支）
	assert.NotPanics(t, func() { f.svc.deliver(rec, model.StatusSent) })
	assert.Len(t, f.wx.Sends(), 1, "发送本身已执行")
}

func TestImplDeliver_ConsumeQuotaError_StillSent(t *testing.T) {
	f := newErrFixture2(t, "ConsumeQuota")
	rec := model.NotificationRecord{
		PatientID: "P20260001", Kind: model.KindAlert,
		Channel: model.ChannelWechat, Status: model.StatusPending, Content: "x",
	}
	require.NoError(t, f.store.CreateNotificationRecord(context.Background(), &rec))

	f.svc.deliver(rec, model.StatusSent)
	got := f.waitForRecord(t, rec.RecordID, model.StatusSent)
	assert.NotNil(t, got.SentAt, "扣减失败不影响发送成功落库（仅记日志）")
}

// ─────────────────────────────────────────────────────────────
// DrainRetries 补充分支
// ─────────────────────────────────────────────────────────────

func TestImplDrainRetries_MissingRecord_FinishesQueueItem(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.wx.Err = errors.New("wechat api down")

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	recordID, _ := strconvAtoi(t, res.RecordID)
	f.waitForRecord(t, recordID, model.StatusFailed)

	// 记录查询失败分支：队列项直接置 failed（防死循环）
	f.svc.store = &errStore2{errStore{Store: f.store, failMethod: "GetNotificationRecord", err: errInjected}}
	f.svc.SetNow(func() time.Time { return f.now.Add(RetryDelay(1) + time.Second) })
	n, err := f.svc.DrainRetries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "记录缺失不计入处理数")
}

func TestImplDrainRetries_SMSRecord_RetriedWithoutQuota(t *testing.T) {
	f := newImplFixture(t)
	// 仅短信规则 + 额度耗尽路径：产生 sms 渠道失败记录
	f.store.SeedRule(model.NotifyRule{
		Type: model.AlertTypeWearInterrupt, Channels: []string{model.ChannelSMS},
		NotifyTargets: []string{model.TargetPatient},
	})
	f.sms.Err = errors.New("sms gateway down")

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypeWearInterrupt))
	require.NoError(t, err)
	recordID, _ := strconvAtoi(t, res.RecordID)
	f.waitForRecord(t, recordID, model.StatusFailed)

	f.sms.Err = nil
	f.svc.SetNow(func() time.Time { return f.now.Add(RetryDelay(1) + time.Second) })
	n, err := f.svc.DrainRetries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	rec := f.waitForRecord(t, recordID, model.StatusSent)
	assert.NotNil(t, rec.SentAt, "短信重试成功置 sentAt")

	quota, err := f.svc.GetQuota(context.Background(), "P20260001")
	require.NoError(t, err)
	assert.Equal(t, model.DefaultQuota, quota.Remaining, "短信重试不扣订阅额度")
}

// ─────────────────────────────────────────────────────────────
// SendWearReminder / ScanReminders 错误分支
// ─────────────────────────────────────────────────────────────

func TestImplSendWearReminder_StoreErrorBranches(t *testing.T) {
	t.Run("GetQuotaError", func(t *testing.T) {
		f := newErrFixture(t, "GetQuota")
		rt := "20:00"
		_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
		require.NoError(t, err)
		_, err = f.svc.SendWearReminder(context.Background(), "P20260001")
		require.Error(t, err)
	})

	t.Run("ReminderSentTodayError", func(t *testing.T) {
		f := newErrFixture2(t, "ReminderSentToday")
		rt := "20:00"
		_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
		require.NoError(t, err)
		_, err = f.svc.SendWearReminder(context.Background(), "P20260001")
		require.Error(t, err)
	})

	t.Run("CreateRecordError", func(t *testing.T) {
		f := newErrFixture(t, "CreateNotificationRecord")
		rt := "20:00"
		_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
		require.NoError(t, err)
		_, err = f.svc.SendWearReminder(context.Background(), "P20260001")
		require.Error(t, err)
	})
}

func TestImplScanReminders_ErrorBranchesSkipped(t *testing.T) {
	t.Run("TodayWearMinutesError", func(t *testing.T) {
		f := newErrFixture2(t, "TodayWearMinutes")
		rt := "20:00"
		_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
		require.NoError(t, err)
		pushed, err := f.svc.ScanReminders(context.Background())
		require.NoError(t, err, "达标查询失败仅跳过该患者，不致整轮失败")
		assert.Equal(t, 0, pushed)
	})

	t.Run("SendWearReminderError", func(t *testing.T) {
		f := newErrFixture2(t, "GetQuota") // SendWearReminder 内部 GetQuota 失败
		rt := "20:00"
		_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
		require.NoError(t, err)
		pushed, err := f.svc.ScanReminders(context.Background())
		require.NoError(t, err, "单患者推送失败仅记日志，扫描继续")
		assert.Equal(t, 0, pushed)
	})
}

// ─────────────────────────────────────────────────────────────
// 校验 / 循环默认值补充分支
// ─────────────────────────────────────────────────────────────

func TestImplUpdateNotifyRule_EmptyChannelsOrTargets(t *testing.T) {
	f := newImplFixture(t)
	ctx := context.Background()

	_, err := f.svc.UpdateNotifyRule(ctx, model.AlertTypePressureHigh, nil, []string{"patient"}, "admin")
	require.Error(t, err, "空 channels 应被拒绝")
	_, err = f.svc.UpdateNotifyRule(ctx, model.AlertTypePressureHigh, []string{"wechat"}, nil, "admin")
	require.Error(t, err, "空 notify_targets 应被拒绝")
}

func TestImplGetNotificationLogs_FilterBranches(t *testing.T) {
	f := newImplFixture(t)
	ctx := context.Background()

	_, _, err := f.svc.GetNotificationLogs(ctx, repo.RecordFilter{AlertType: "bogus"})
	require.Error(t, err, "非法 alertType 过滤应被拒绝")
	_, _, err = f.svc.GetNotificationLogs(ctx, repo.RecordFilter{Channel: "email"})
	require.Error(t, err, "非法 channel 过滤应被拒绝")

	records, total, err := f.svc.GetNotificationLogs(ctx, repo.RecordFilter{Status: model.StatusSent})
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, records)
}

func TestImplGetNotifyRules_EmptyWhenNoRules(t *testing.T) {
	// 未预置规则 → 返回空列表（非错误）
	empty := newImplFixture(t)
	rules, err := empty.svc.GetNotifyRules(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rules)
}

func TestImplGetNotifyRulesAndQuota_StoreError(t *testing.T) {
	f := newErrFixture2(t, "FindRules")
	_, err := f.svc.GetNotifyRules(context.Background())
	require.Error(t, err)
	assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)

	f2 := newErrFixture(t, "GetQuota")
	_, err = f2.svc.GetQuota(context.Background(), "P20260001")
	require.Error(t, err)
	assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
}

func TestImplSchedulers_DefaultIntervalAndErrorLoop(t *testing.T) {
	// (a) interval≤0 走默认值分支：立即取消，15min 首 tick 不会触发
	f := newImplFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.svc.RunReminderScheduler(ctx, 0)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunReminderScheduler did not stop")
	}

	// (b) 短间隔 + 扫描失败 → 错误日志分支（循环继续，不退出）
	fErr := newErrFixture(t, "ListReminderCandidates")
	ctxA, cancelA := context.WithCancel(context.Background())
	doneA := make(chan struct{})
	go func() {
		fErr.svc.RunReminderScheduler(ctxA, 10*time.Millisecond)
		close(doneA)
	}()
	time.Sleep(30 * time.Millisecond)
	cancelA()
	select {
	case <-doneA:
	case <-time.After(2 * time.Second):
		t.Fatal("RunReminderScheduler did not stop")
	}

	// retry worker 同型：默认间隔分支 + 排空错误分支
	f2 := newImplFixture(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	go func() {
		f2.svc.RunRetryWorker(ctx2, 0)
		close(done2)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel2()
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("RunRetryWorker did not stop")
	}

	f2Err := newErrFixture(t, "ListDueRetries")
	ctxB, cancelB := context.WithCancel(context.Background())
	doneB := make(chan struct{})
	go func() {
		f2Err.svc.RunRetryWorker(ctxB, 10*time.Millisecond)
		close(doneB)
	}()
	time.Sleep(30 * time.Millisecond)
	cancelB()
	select {
	case <-doneB:
	case <-time.After(2 * time.Second):
		t.Fatal("RunRetryWorker did not stop")
	}
}

func TestImplSetWearTargetMinutes(t *testing.T) {
	f := newImplFixture(t)
	f.svc.SetWearTargetMinutes(0) // 非法值忽略
	assert.Equal(t, 22*60, f.svc.wearTargetMinutes)
	f.svc.SetWearTargetMinutes(10 * 60)
	assert.Equal(t, 600, f.svc.wearTargetMinutes)
}

// ─────────────────────────────────────────────────────────────
// 发送/提醒成功链路的尾部错误分支（仅记日志，不影响受理）
// ─────────────────────────────────────────────────────────────

func TestImplDeliver_FailurePath_StoreErrors(t *testing.T) {
	// 推送失败 + 状态置 failed 失败 + 入队失败：均仅记日志，不 panic
	f := newErrFixture2(t, "UpdateNotificationStatus")
	f.svc.store = &errStore2{errStore{Store: &errStore2{errStore{Store: f.store, failMethod: "EnqueueRetry", err: errInjected}}, failMethod: "UpdateNotificationStatus", err: errInjected}}
	f.wx.Err = errors.New("wechat api down")

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	require.True(t, res.Accepted)
	recordID, _ := strconvAtoi(t, res.RecordID)
	require.NotZero(t, recordID)

	// 等待异步 deliver 完成：发送尝试已执行（失败 mock 不留痕），尾部错误仅日志
	time.Sleep(200 * time.Millisecond)
	assert.NotPanics(t, func() {}, "尾部 store 错误不得导致 panic")
}

func TestImplSendWearReminder_SuccessTailErrors(t *testing.T) {
	t.Run("StatusUpdateError", func(t *testing.T) {
		f := newErrFixture2(t, "UpdateNotificationStatus")
		rt := "20:00"
		_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
		require.NoError(t, err)
		res, err := f.svc.SendWearReminder(context.Background(), "P20260001")
		require.NoError(t, err, "状态更新失败仅记日志，推送仍受理")
		assert.True(t, res.Accepted)
	})

	t.Run("ConsumeQuotaError", func(t *testing.T) {
		f := newErrFixture2(t, "ConsumeQuota")
		rt := "20:00"
		_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
		require.NoError(t, err)
		res, err := f.svc.SendWearReminder(context.Background(), "P20260001")
		require.NoError(t, err, "扣减失败仅记日志")
		assert.True(t, res.Accepted)
	})

	t.Run("WechatNil", func(t *testing.T) {
		f := newImplFixture(t)
		rt := "20:00"
		_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
		require.NoError(t, err)
		f.svc.wechat = nil
		res, err := f.svc.SendWearReminder(context.Background(), "P20260001")
		require.NoError(t, err)
		assert.False(t, res.Accepted, "微信通道不可用 → 提醒不受理（不降级短信）")
	})
}
