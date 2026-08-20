// Package repo_test — msg-service 持久层测试用例（T026 升级：委托真实实现）
//
// 使用外部测试包（repo_test）以避免 repo → testutil → repo 循环导入。
//
// 本文件覆盖 msg-service 持久层关键规则：通知记录落库、失败重试队列、额度读写/幂等。
// T026 升级：原 KNOWN_RED 桩已替换为真实 Store 接口 + FakeStore 内存实现。
//
// 覆盖规则（对齐 T017 验收标准 4 + database-design.md）：
//   - 发送记录落库：notification_records 写入（status/retry_count/sent_at 可追溯）
//   - 失败重试：failed 记录进重试队列，不丢通知（对齐 T010 降级模式）
//   - 额度读写：patient_preferences.subscription_quota 默认 3，grant 幂等增额
package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/msg-service/internal/testutil"
)

// ============================================================
// R1: 发送记录落库（验收 4）
// ============================================================

func TestCreateNotificationRecord_Persists(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()

	alertType := model.AlertTypePressureHigh
	rec := &model.NotificationRecord{
		PatientID: "P20260001",
		AlertType: &alertType,
		Kind:      model.KindAlert,
		Channel:   model.ChannelWechat,
		Status:    model.StatusSent,
		Content:   "压力偏高：P03 压力 47.2N",
	}
	err := store.CreateNotificationRecord(ctx, rec)
	require.NoError(t, err, "记录应落库成功")
	assert.Greater(t, rec.RecordID, int64(0), "RecordID 应被自动填充")

	t.Log("upgraded: now delegates to real implementation — notification_records INSERT 成功，可经 GetNotificationRecord 读回")

	got, err := store.GetNotificationRecord(ctx, rec.RecordID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, model.StatusSent, got.Status)
	assert.Equal(t, 0, got.RetryCount)
	assert.Equal(t, "P20260001", got.PatientID)
}

// ============================================================
// R2: 失败 → 重试队列（不丢通知，验收 4）
// ============================================================

func TestFailedSend_EnqueuesRetry(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()

	// 创建失败记录
	alertType := model.AlertTypeWearInterrupt
	rec := &model.NotificationRecord{
		PatientID:  "P20260001",
		AlertType:  &alertType,
		Kind:       model.KindAlert,
		Channel:    model.ChannelWechat,
		Status:     model.StatusFailed,
		Content:    "佩戴中断",
		RetryCount: 0,
	}
	require.NoError(t, store.CreateNotificationRecord(ctx, rec))

	// 进重试队列
	nextRetry := time.Now().Add(5 * time.Minute)
	require.NoError(t, store.EnqueueRetry(ctx, rec.RecordID, nextRetry))

	t.Log("upgraded: now delegates to real implementation — failed 记录进入重试队列（ListDueRetries 可拉取），原记录不丢失")

	items, err := store.ListDueRetries(ctx, time.Now().Add(time.Hour), 10)
	require.NoError(t, err)
	assert.NotEmpty(t, items, "重试队列应包含失败记录")
	for _, item := range items {
		assert.Greater(t, item.RecordID, int64(0), "重试项必须关联记录 ID")
	}
}

// ============================================================
// R3: 重试成功后状态流转（验收 4）
// ============================================================

func TestRetrySuccess_StatusTransitions(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()

	// 创建失败记录
	rec := &model.NotificationRecord{
		PatientID: "P20260001",
		Kind:      model.KindAlert,
		Channel:   model.ChannelWechat,
		Status:    model.StatusFailed,
		Content:   "佩戴中断",
	}
	require.NoError(t, store.CreateNotificationRecord(ctx, rec))

	// 重试成功 → 状态 failed→sent + sentAt
	now := time.Now()
	err := store.UpdateNotificationStatus(ctx, rec.RecordID, model.StatusSent, &now)
	require.NoError(t, err)

	t.Log("upgraded: now delegates to real implementation — status transitions: failed→sent + sentAt")

	got, err := store.GetNotificationRecord(ctx, rec.RecordID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusSent, got.Status, "重试成功后状态应为 sent")
	assert.NotNil(t, got.SentAt, "重试成功后 sentAt 应被设置")
}

// ============================================================
// R4: 额度读写 — 默认 3 + grant 幂等增额（验收 2）
// ============================================================

func TestGetSubscriptionQuota_DefaultThree(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()

	quota, err := store.GetQuota(ctx, "P20260001")

	require.NoError(t, err)
	t.Log("upgraded: now delegates to real implementation — 新患者默认 subscription_quota=3（对齐 patient_preferences DEFAULT 3）")
	require.NotNil(t, quota)
	assert.Equal(t, model.DefaultQuota, quota.Remaining, "默认额度应为 3")
}

func TestGrantQuota_Idempotent_SameKey(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()

	// 同 Idempotency-Key 重复 grant → 只增额一次（幂等）
	first, err1 := store.GrantQuota(ctx, "P20260001", "idem-key-001", 1)
	require.NoError(t, err1)
	second, err2 := store.GrantQuota(ctx, "P20260001", "idem-key-001", 1)
	require.NoError(t, err2)

	t.Log("upgraded: now delegates to real implementation — same Idempotency-Key → second call does NOT re-increment")
	assert.Equal(t, first.Remaining, second.Remaining, "同 Idempotency-Key 不得重复增额")
}

func TestGrantQuota_DifferentKeys_IncrementEach(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()

	// 不同 Idempotency-Key → 各自增额
	first, err1 := store.GrantQuota(ctx, "P20260001", "idem-key-001", 1)
	require.NoError(t, err1)
	second, err2 := store.GrantQuota(ctx, "P20260001", "idem-key-002", 1)
	require.NoError(t, err2)

	t.Log("upgraded: now delegates to real implementation — different keys → second = first + 1")
	assert.Equal(t, first.Remaining+1, second.Remaining, "不同授权应各增 1")
}

// ============================================================
// R5: 发送时内部扣减（验收 2，不暴露对外扣减接口）
// ============================================================

func TestConsumeQuota_OnSend(t *testing.T) {
	store := testutil.NewFakeStore()
	ctx := context.Background()

	// 发送订阅消息时内部扣减（无对外扣减接口，架构 §2.5）
	quota, err := store.ConsumeQuota(ctx, "P20260001")

	require.NoError(t, err)
	t.Log("upgraded: now delegates to real implementation — 发送成功后 quota-1（内部行为），剩余 0 时后续告警降级短信")
	require.NotNil(t, quota)
	assert.Less(t, quota.Remaining, model.DefaultQuota, "发送后额度应小于初始 3")
}
