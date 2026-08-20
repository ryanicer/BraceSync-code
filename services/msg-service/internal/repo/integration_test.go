//go:build integration
// +build integration

// Package repo 集成测试：真实 PG15（testcontainers）验证 SQL 与约束
//
// 对齐：docs/ §1（集成层）· T017 验收标准 1-4：
//
//	规则 upsert / 额度 grant 幂等（quota_grants UNIQUE）/ 消费下限 0 /
//	通知记录落库与过滤分页 / 重试队列状态机 / 佩戴提醒扫描候选 /
//	daily_wear_stats 达标只读
//
// 运行：make test-integration（需 Docker）
package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/testhelper"
)

var itPool *pgxpool.Pool

const (
	itPatient  = "P-MSG-IT-001"
	itPatient2 = "P-MSG-IT-002"
)

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		return runIT(m, cfg.DBURL)
	})
}

// runIT 建连 + 迁移 + 种子 + 执行用例（msg-service 仅依赖 PG，不使用 Redis）
func runIT(m *testing.M, dbURL string) int {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "it: pgxpool: %v\n", err)
		return 1
	}
	itPool = pool
	defer pool.Close()

	applyMigrations(ctx)
	seedITData(ctx)
	return m.Run()
}

// migrationsDir 定位 scripts/db/migrations（相对本文件 4 级上）
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "db", "migrations")
}

// applyMigrations 顺序执行全部 *.up.sql（单一事实源，含 000003 通知域表）
func applyMigrations(ctx context.Context) {
	entries, err := os.ReadDir(migrationsDir())
	if err != nil {
		panic("it: read migrations dir: " + err.Error())
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, f := range files {
		sql, err := os.ReadFile(filepath.Join(migrationsDir(), f))
		if err != nil {
			panic("it: read migration " + f + ": " + err.Error())
		}
		if _, err := itPool.Exec(ctx, string(sql)); err != nil {
			panic("it: apply migration " + f + ": " + err.Error())
		}
	}
}

// seedITData 集成测试专用种子（不走 seed.sql，避免业务样本串扰）
func seedITData(ctx context.Context) {
	for _, p := range []struct{ id, phoneHash string }{
		{itPatient, "aaaa0000000000000000000000000000000000000000000000000000000001"},
		{itPatient2, "aaaa0000000000000000000000000000000000000000000000000000000002"},
	} {
		if _, err := itPool.Exec(ctx,
			`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
			 VALUES ($1, '集成测试患者', '\x00'::bytea, $2, 'active')
			 ON CONFLICT (patient_id) DO NOTHING`, p.id, p.phoneHash); err != nil {
			panic("it: seed patient: " + err.Error())
		}
	}
}

func newITStore() *PGStore { return NewPGStore(itPool) }

// ─────────────────────────────────────────────────────────────
// 通知规则
// ─────────────────────────────────────────────────────────────

func TestITRules_UpsertAndFind(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	rule := &model.NotifyRule{
		Type:          model.AlertTypePressureHigh,
		Channels:      []string{model.ChannelWechat, model.ChannelSMS},
		NotifyTargets: []string{model.TargetPatient, model.TargetDoctor},
		UpdatedBy:     "it-admin",
	}
	require.NoError(t, store.UpsertRule(ctx, rule))

	got, err := store.FindRuleByType(ctx, model.AlertTypePressureHigh)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []string{"wechat", "sms"}, got.Channels)
	assert.Equal(t, []string{"patient", "doctor"}, got.NotifyTargets)
	assert.Equal(t, "it-admin", got.UpdatedBy)

	// upsert 覆盖更新
	rule.Channels = []string{model.ChannelWechat}
	rule.NotifyTargets = []string{model.TargetDoctor}
	require.NoError(t, store.UpsertRule(ctx, rule))
	got, err = store.FindRuleByType(ctx, model.AlertTypePressureHigh)
	require.NoError(t, err)
	assert.Equal(t, []string{"wechat"}, got.Channels)

	rules, err := store.FindRules(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, rules)

	// 未配置规则 → nil（service 层据此"未知 type 不发送"）
	missing, err := store.FindRuleByType(ctx, model.AlertTypeWearInterrupt)
	require.NoError(t, err)
	assert.Nil(t, missing)
}

// ─────────────────────────────────────────────────────────────
// 订阅额度：默认 3 / grant 幂等 / 消费下限
// ─────────────────────────────────────────────────────────────

func TestITQuota_DefaultThree_WithoutPrefsRow(t *testing.T) {
	store := newITStore()

	quota, err := store.GetQuota(context.Background(), itPatient2)
	require.NoError(t, err)
	assert.Equal(t, model.DefaultQuota, quota.Remaining, "无偏好行 → 默认额度 3")
	assert.False(t, quota.IsLow)
}

func TestITQuota_GrantIdempotent_SameKeyNoDoubleIncrement(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	first, err := store.GrantQuota(ctx, itPatient, "it-idem-001", 1)
	require.NoError(t, err)
	assert.Equal(t, model.DefaultQuota+1, first.Remaining, "首次授予 +1（建行 = 默认 3 + 1）")

	second, err := store.GrantQuota(ctx, itPatient, "it-idem-001", 1)
	require.NoError(t, err)
	assert.Equal(t, first.Remaining, second.Remaining, "同 Idempotency-Key 不重复增额（quota_grants UNIQUE）")

	third, err := store.GrantQuota(ctx, itPatient, "it-idem-002", 1)
	require.NoError(t, err)
	assert.Equal(t, first.Remaining+1, third.Remaining, "不同 key 各增 1")
	assert.Equal(t, model.DefaultQuota+2, third.Total, "total = 默认 3 + 台账增量合计")
	assert.False(t, third.IsLow)
}

func TestITQuota_ConsumeFloorZero(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	// itPatient2 无偏好行：首次消费按默认 3 扣减建行
	q, err := store.ConsumeQuota(ctx, itPatient2)
	require.NoError(t, err)
	assert.Equal(t, model.DefaultQuota-1, q.Remaining)

	// 扣到 0 后不再下降（下限 0）
	for i := 0; i < model.DefaultQuota+2; i++ {
		q, err = store.ConsumeQuota(ctx, itPatient2)
		require.NoError(t, err)
	}
	assert.Equal(t, 0, q.Remaining, "额度下限 0")
	assert.True(t, q.IsLow)
}

// ─────────────────────────────────────────────────────────────
// 佩戴提醒：直写 patient_preferences + 扫描候选 + 当日去重
// ─────────────────────────────────────────────────────────────

func TestITWearReminder_RoundtripAndCandidates(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	rt := "20:00"
	settings, err := store.UpdateWearReminder(ctx, itPatient, true, &rt)
	require.NoError(t, err)
	assert.True(t, settings.ReminderEnabled)

	got, err := store.GetWearReminder(ctx, itPatient)
	require.NoError(t, err)
	assert.True(t, got.ReminderEnabled)
	require.NotNil(t, got.ReminderTime)
	assert.Equal(t, "20:00", *got.ReminderTime, "TIME 列经 TO_CHAR 回读 HH:mm")

	// 到点候选：20:05 扫描命中 20:00；19:59 扫描不命中
	due, err := store.ListReminderCandidates(ctx, "20:05")
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, itPatient, due[0].PatientID)

	notDue, err := store.ListReminderCandidates(ctx, "19:59")
	require.NoError(t, err)
	assert.Empty(t, notDue)

	// 关闭开关后不再出现在候选
	_, err = store.UpdateWearReminder(ctx, itPatient, false, &rt)
	require.NoError(t, err)
	due, err = store.ListReminderCandidates(ctx, "20:05")
	require.NoError(t, err)
	assert.Empty(t, due)
}

func TestITWearReminder_SentTodayDedup(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	dayStart := time.Now().Add(-time.Hour)
	sent, err := store.ReminderSentToday(ctx, itPatient2, dayStart)
	require.NoError(t, err)
	assert.False(t, sent)

	rec := &model.NotificationRecord{
		PatientID: itPatient2, Kind: model.KindWearReminder,
		Channel: model.ChannelWechat, Status: model.StatusSent, Content: "提醒",
	}
	require.NoError(t, store.CreateNotificationRecord(ctx, rec))

	sent, err = store.ReminderSentToday(ctx, itPatient2, dayStart)
	require.NoError(t, err)
	assert.True(t, sent, "当日已有 wear_reminder 记录 → 去重")

	// failed 记录不参与去重（重试链路仍可再推）
	require.NoError(t, store.UpdateNotificationStatus(ctx, rec.RecordID, model.StatusFailed, nil))
	sent, err = store.ReminderSentToday(ctx, itPatient2, dayStart)
	require.NoError(t, err)
	assert.False(t, sent)
}

// ─────────────────────────────────────────────────────────────
// 通知记录：落库 / 状态流转 / 过滤分页
// ─────────────────────────────────────────────────────────────

func TestITRecords_LifecycleAndFilter(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	alertType := model.AlertTypePressureHigh
	alertID := "A-IT-001"
	rec := &model.NotificationRecord{
		PatientID: itPatient, AlertID: &alertID, AlertType: &alertType,
		Kind: model.KindAlert, Channel: model.ChannelWechat,
		Status: model.StatusPending, Content: "压力偏高",
	}
	require.NoError(t, store.CreateNotificationRecord(ctx, rec))
	assert.NotZero(t, rec.RecordID, "IDENTITY 主键回填")
	assert.False(t, rec.CreatedAt.IsZero(), "created_at 回填")

	got, err := store.GetNotificationRecord(ctx, rec.RecordID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusPending, got.Status)
	require.NotNil(t, got.AlertID)
	assert.Equal(t, "A-IT-001", *got.AlertID)

	now := time.Now()
	require.NoError(t, store.UpdateNotificationStatus(ctx, rec.RecordID, model.StatusSent, &now))
	got, err = store.GetNotificationRecord(ctx, rec.RecordID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusSent, got.Status)
	require.NotNil(t, got.SentAt, "成功置 sentAt")

	// 过滤：status=sent 命中，status=failed 不命中
	records, total, err := store.ListNotificationRecords(ctx, RecordFilter{PatientID: itPatient, Status: model.StatusSent, Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.NotEmpty(t, records)

	records, total, err = store.ListNotificationRecords(ctx, RecordFilter{PatientID: itPatient, Status: model.StatusFailed, Page: 1, PageSize: 20})
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, records)

	// 缺失记录 → ErrNotFound
	_, err = store.GetNotificationRecord(ctx, 99999999)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestITRecords_PaginationOrder(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	ids := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		rec := &model.NotificationRecord{
			PatientID: itPatient, Kind: model.KindAlert, Channel: model.ChannelSMS,
			Status: model.StatusPending, Content: fmt.Sprintf("分页用例 %d", i),
		}
		require.NoError(t, store.CreateNotificationRecord(ctx, rec))
		ids = append(ids, rec.RecordID)
	}

	page1, total, err := store.ListNotificationRecords(ctx, RecordFilter{PatientID: itPatient, Channel: model.ChannelSMS, Page: 1, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, page1, 2)
	assert.Equal(t, ids[2], page1[0].RecordID, "created_at DESC 时间倒序（最新在前）")

	page2, _, err := store.ListNotificationRecords(ctx, RecordFilter{PatientID: itPatient, Channel: model.ChannelSMS, Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Len(t, page2, 1)
}

// ─────────────────────────────────────────────────────────────
// 重试队列：入队 / 到期拉取 / 重排 / 终态
// ─────────────────────────────────────────────────────────────

func TestITRetryQueue_Lifecycle(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	rec := &model.NotificationRecord{
		PatientID: itPatient, Kind: model.KindAlert, Channel: model.ChannelWechat,
		Status: model.StatusFailed, Content: "重试用例",
	}
	require.NoError(t, store.CreateNotificationRecord(ctx, rec))
	require.NoError(t, store.UpdateNotificationStatus(ctx, rec.RecordID, model.StatusFailed, nil))

	future := time.Now().Add(5 * time.Minute)
	require.NoError(t, store.EnqueueRetry(ctx, rec.RecordID, future))

	// 未到期拉不到
	items, err := store.ListDueRetries(ctx, time.Now(), 10)
	require.NoError(t, err)
	assert.Empty(t, items)

	// 到期可拉取
	items, err = store.ListDueRetries(ctx, future.Add(time.Second), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, rec.RecordID, items[0].RecordID)
	assert.Equal(t, "pending", items[0].Status)
	queueID := items[0].QueueID

	// 重试失败 → 计数递增 + 退避重排（保持 pending）
	require.NoError(t, store.RescheduleRetry(ctx, queueID, 1, future.Add(10*time.Minute)))
	items, err = store.ListDueRetries(ctx, future.Add(11*time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 1, items[0].RetryCount)

	// 重试成功 → done 终态，不再出现在 pending 拉取
	require.NoError(t, store.FinishRetry(ctx, queueID, "done"))
	items, err = store.ListDueRetries(ctx, future.Add(24*time.Hour), 10)
	require.NoError(t, err)
	assert.Empty(t, items)
}

// ─────────────────────────────────────────────────────────────
// 佩戴达标只读（daily_wear_stats rollup）
// ─────────────────────────────────────────────────────────────

func TestITTodayWearMinutes(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	// 无 rollup 行 → 0（视为未达标）
	minutes, err := store.TodayWearMinutes(ctx, itPatient, "2026-08-10")
	require.NoError(t, err)
	assert.Equal(t, 0, minutes)

	_, err = itPool.Exec(ctx,
		`INSERT INTO daily_wear_stats (patient_id, stat_date, wear_minutes, frame_count)
		 VALUES ($1, '2026-08-10', 1320, 44) ON CONFLICT (patient_id, stat_date) DO UPDATE SET wear_minutes = 1320`,
		itPatient)
	require.NoError(t, err)

	minutes, err = store.TodayWearMinutes(ctx, itPatient, "2026-08-10")
	require.NoError(t, err)
	assert.Equal(t, 1320, minutes, "22h 达标分钟数可读（rollup 层，禁扫明细）")
}
