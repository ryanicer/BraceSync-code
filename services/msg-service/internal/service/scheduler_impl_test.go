// Package service — NotifyService 实现侧补充测试（错误分支 / 通道缺失 / 常驻循环）
//
// notify_impl_test.go 的补强：覆盖 store 错误传播分支、nil 通道防护、
// RunReminderScheduler / RunRetryWorker 循环启动与 ctx 取消退出。
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

// errStore 注入错误的 Store 包装（仅在指定方法返回注入错误）
type errStore struct {
	repo.Store
	failMethod string
	err        error
}

var errInjected = errors.New("injected store failure")

func (e *errStore) FindRuleByType(ctx context.Context, t model.AlertType) (*model.NotifyRule, error) {
	if e.failMethod == "FindRuleByType" {
		return nil, e.err
	}
	return e.Store.FindRuleByType(ctx, t)
}

func (e *errStore) GetQuota(ctx context.Context, patientID string) (*model.SubscriptionQuota, error) {
	if e.failMethod == "GetQuota" {
		return nil, e.err
	}
	return e.Store.GetQuota(ctx, patientID)
}

func (e *errStore) CreateNotificationRecord(ctx context.Context, rec *model.NotificationRecord) error {
	if e.failMethod == "CreateNotificationRecord" {
		return e.err
	}
	return e.Store.CreateNotificationRecord(ctx, rec)
}

func (e *errStore) GetWearReminder(ctx context.Context, patientID string) (*model.WearReminderSettings, error) {
	if e.failMethod == "GetWearReminder" {
		return nil, e.err
	}
	return e.Store.GetWearReminder(ctx, patientID)
}

func (e *errStore) UpdateWearReminder(ctx context.Context, patientID string, enabled bool, reminderTime *string) (*model.WearReminderSettings, error) {
	if e.failMethod == "UpdateWearReminder" {
		return nil, e.err
	}
	return e.Store.UpdateWearReminder(ctx, patientID, enabled, reminderTime)
}

func (e *errStore) ListReminderCandidates(ctx context.Context, nowHM string) ([]repo.ReminderCandidate, error) {
	if e.failMethod == "ListReminderCandidates" {
		return nil, e.err
	}
	return e.Store.ListReminderCandidates(ctx, nowHM)
}

func (e *errStore) ListDueRetries(ctx context.Context, now time.Time, limit int) ([]model.RetryQueueItem, error) {
	if e.failMethod == "ListDueRetries" {
		return nil, e.err
	}
	return e.Store.ListDueRetries(ctx, now, limit)
}

func (e *errStore) GrantQuota(ctx context.Context, patientID, idempotencyKey string, increment int) (*model.SubscriptionQuota, error) {
	if e.failMethod == "GrantQuota" {
		return nil, e.err
	}
	return e.Store.GrantQuota(ctx, patientID, idempotencyKey, increment)
}

// newErrFixture 构造注入错误的 service
func newErrFixture(t *testing.T, failMethod string) *implFixture {
	t.Helper()
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.svc.store = &errStore{Store: f.store, failMethod: failMethod, err: errInjected}
	return f
}

// ─────────────────────────────────────────────────────────────
// store 错误传播分支（均收敛为 AppError 90001）
// ─────────────────────────────────────────────────────────────

func TestImplErrors_StoreFailuresPropagate(t *testing.T) {
	t.Run("RouteNotify", func(t *testing.T) {
		f := newErrFixture(t, "FindRuleByType")
		_, err := f.svc.RouteNotify(context.Background(), model.AlertTypePressureHigh)
		require.Error(t, err)
		assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
	})

	t.Run("SendAlert_GetQuota", func(t *testing.T) {
		f := newErrFixture(t, "GetQuota")
		_, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
		require.Error(t, err)
		assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
	})

	t.Run("SendAlert_CreateRecord", func(t *testing.T) {
		f := newErrFixture(t, "CreateNotificationRecord")
		_, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
		require.Error(t, err)
		assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
	})

	t.Run("GrantQuota", func(t *testing.T) {
		f := newErrFixture(t, "GrantQuota")
		_, err := f.svc.GrantQuota(context.Background(), "P20260001", "k1")
		require.Error(t, err)
		assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
	})

	t.Run("GetWearReminder", func(t *testing.T) {
		f := newErrFixture(t, "GetWearReminder")
		_, err := f.svc.GetWearReminder(context.Background(), "P20260001")
		require.Error(t, err)
		assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
	})

	t.Run("UpdateWearReminder", func(t *testing.T) {
		f := newErrFixture(t, "UpdateWearReminder")
		rt := "20:00"
		_, err := f.svc.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
		require.Error(t, err)
		assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
	})

	t.Run("SendWearReminder_GetSettings", func(t *testing.T) {
		f := newErrFixture(t, "GetWearReminder")
		_, err := f.svc.SendWearReminder(context.Background(), "P20260001")
		require.Error(t, err)
		assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
	})

	t.Run("ScanReminders_ListCandidates", func(t *testing.T) {
		f := newErrFixture(t, "ListReminderCandidates")
		_, err := f.svc.ScanReminders(context.Background())
		require.Error(t, err)
		assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
	})

	t.Run("DrainRetries_ListDue", func(t *testing.T) {
		f := newErrFixture(t, "ListDueRetries")
		_, err := f.svc.DrainRetries(context.Background())
		require.Error(t, err)
		assert.Equal(t, model.CodeInternal, err.(*model.AppError).Code)
	})
}

// ─────────────────────────────────────────────────────────────
// nil 通道防护（deliver / DrainRetries 的通道缺失分支）
// ─────────────────────────────────────────────────────────────

func TestImplSendAlert_WecomNil_SMSOnlyRuleSendsSMS(t *testing.T) {
	f := newImplFixture(t)
	f.svc.wechat = nil
	// 仅 sms 渠道的规则（无微信路由 → 不查额度，直接短信）
	f.store.SeedRule(model.NotifyRule{
		Type: model.AlertTypeWearInterrupt, Channels: []string{model.ChannelSMS},
		NotifyTargets: []string{model.TargetPatient, model.TargetDoctor},
	})

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypeWearInterrupt))
	require.NoError(t, err)
	assert.True(t, res.Accepted)
	assert.False(t, res.Degraded, "规则本就路由短信，不算降级")

	recordID, _ := strconvAtoi(t, res.RecordID)
	rec := f.waitForRecord(t, recordID, model.StatusSent)
	assert.Equal(t, model.ChannelSMS, rec.Channel)
	assert.Len(t, f.sms.Sends(), 1)
}

func TestImplSendAlert_SMSOnlyRule_SMSNil_Rejected(t *testing.T) {
	f := newImplFixture(t)
	f.svc.sms = nil
	f.store.SeedRule(model.NotifyRule{
		Type: model.AlertTypeWearInterrupt, Channels: []string{model.ChannelSMS},
		NotifyTargets: []string{model.TargetPatient},
	})

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypeWearInterrupt))
	require.NoError(t, err)
	assert.False(t, res.Accepted, "短信规则但 SMS 通道不可用 → accepted=false")
}

func TestImplDrainRetries_ChannelUnavailable_RetriesThenGivesUp(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.wx.Err = errors.New("wechat api down")

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	recordID, _ := strconvAtoi(t, res.RecordID)
	f.waitForRecord(t, recordID, model.StatusFailed)

	// 通道整体不可用（nil）：重试同样失败，直至放弃
	f.svc.wechat = nil
	cursor := f.now
	for i := 0; i < MaxRetries; i++ {
		cursor = cursor.Add(RetryDelay(i+1) + time.Second)
		f.svc.SetNow(func() time.Time { return cursor })
		n, err := f.svc.DrainRetries(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	}

	items, err := f.store.ListDueRetries(context.Background(), cursor.Add(24*time.Hour), 10)
	require.NoError(t, err)
	assert.Empty(t, items, "通道持续不可用 → 达上限后放弃")
}

// ─────────────────────────────────────────────────────────────
// validHHmm 边界
// ─────────────────────────────────────────────────────────────

func TestImplValidHHmm_Boundaries(t *testing.T) {
	f := newImplFixture(t)
	valid := []string{"00:00", "23:59", "08:30"}
	for _, v := range valid {
		_, err := f.svc.UpdateWearReminder(context.Background(), "P20260001", true, &v)
		assert.NoError(t, err, "合法 HH:mm %q 不应被拒绝", v)
	}
	invalid := []string{"24:00", "12:60", "1200", "ab:cd", "12:5"}
	for _, v := range invalid {
		_, err := f.svc.UpdateWearReminder(context.Background(), "P20260001", true, &v)
		require.Error(t, err, "非法 HH:mm %q 应被拒绝", v)
		assert.Equal(t, model.CodeInvalidParam, err.(*model.AppError).Code)
	}
}

// ─────────────────────────────────────────────────────────────
// 常驻循环：启动即执行一轮 + ctx 取消退出
// ─────────────────────────────────────────────────────────────

func TestImplRunReminderScheduler_StopsOnCancel(t *testing.T) {
	f := newImplFixture(t)
	rt := "20:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.svc.RunReminderScheduler(ctx, 10*time.Millisecond)
		close(done)
	}()

	// 等待至少一轮扫描推送生效（最终一致）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(f.wx.Sends()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	assert.NotEmpty(t, f.wx.Sends(), "循环应按间隔执行扫描并推送")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunReminderScheduler did not stop after context cancel")
	}
}

func TestImplRunRetryWorker_StopsOnCancel(t *testing.T) {
	f := newImplFixture(t)
	f.seedDefaultRules()
	f.wx.Err = errors.New("wechat api down")

	res, err := f.svc.SendAlert(context.Background(), f.alertReq(model.AlertTypePressureHigh))
	require.NoError(t, err)
	recordID, _ := strconvAtoi(t, res.RecordID)
	f.waitForRecord(t, recordID, model.StatusFailed)

	// 通道恢复；worker 以极短间隔运行，时钟固定为退避窗口之后
	f.wx.Err = nil
	f.svc.SetNow(func() time.Time { return f.now.Add(RetryDelay(1) + time.Second) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.svc.RunRetryWorker(ctx, 10*time.Millisecond)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, _ := f.store.GetNotificationRecord(context.Background(), recordID)
		if rec != nil && rec.Status == model.StatusSent {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rec, err := f.store.GetNotificationRecord(context.Background(), recordID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusSent, rec.Status, "worker 应排空重试队列并置 sent")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunRetryWorker did not stop after context cancel")
	}
}
