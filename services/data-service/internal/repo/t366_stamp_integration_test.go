//go:build integration
// +build integration

// Package repo T366：聚合印章真库回归 + 明细帧按 CST 日计数
//
// 单测能证明判档逻辑，证不了两件事，必须真库跑：
//  1. 可空列的读写闭环（NULL 读回来必须是 nil，不能是零值时间/0 阈值）；
//  2. CountFramesByCSTDay 的切日口径 —— 帧的 UTC 日与业务日差 8 小时，
//     按 UTC 分组会把 23:30 和次日 00:30 算成同一天的 3 帧（真库里实测过的那种错）。
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

const (
	t366Patient = "P-T366-IT1"
	t366Device  = "D-T366-IT1"
)

// TestITT366StampRoundTripAndDemoRow 印章写读闭环 + 手写示例行不许冒充聚合行
func TestITT366StampRoundTripAndDemoRow(t *testing.T) {
	ctx := context.Background()
	pool := dashPool
	require.NotNil(t, pool, "集成测试池未初始化")
	seedT366Patient(ctx, t, pool)

	aggAt := time.Date(2026, 8, 15, 16, 10, 0, 0, time.UTC) // CST 08-16 00:10 跑 08-15 的聚合
	thN := 0.42
	stamped := model.DailyWearStats{
		PatientID: t366Patient, StatDate: t366Day("2026-08-15"),
		WearMinutes: 300, AvgPressure: 1.25, MaxPressure: 3.5, MaxPoint: "P01",
		FrameCount: 2, AbnormalCount: 0,
	}
	stamped.AggregatedAt = &aggAt
	stamped.WearingThresholdN = &thN
	require.NoError(t, NewRollupRepo(pool).Upsert(ctx, []model.DailyWearStats{stamped}))

	// 手写一行（seed 示例行的形态）：数值完全合理，但没有印章
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_wear_stats
		  (patient_id, stat_date, wear_minutes, avg_pressure, max_pressure, max_point, frame_count, abnormal_count)
		VALUES ($1, '2026-08-16', 1200, 22.4, 47.2, 'P03', 40, 1)
		ON CONFLICT (patient_id, stat_date) DO NOTHING`, t366Patient)
	require.NoError(t, err, "seed 形态示例行写入失败（列清单与 000001 建表不符？）")

	rows, err := NewRollupRepo(pool).QueryRange(ctx, t366Patient, t352From, t352To.AddDate(0, 0, 2))
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byDate := map[string]*model.DailyWearStats{}
	for i := range rows {
		byDate[rows[i].StatDate.In(model.CSTZone()).Format("2006-01-02")] = &rows[i]
	}

	got := byDate["2026-08-15"]
	require.NotNil(t, got)
	require.True(t, got.HasRollupStamp(), "聚合任务写的行必须能把印章读回来")
	assert.True(t, got.AggregatedAt.Equal(aggAt), "印章时刻落库再读出应完全一致，实得 %v", got.AggregatedAt)
	require.NotNil(t, got.WearingThresholdN)
	assert.InDelta(t, thN, *got.WearingThresholdN, 1e-6, "REAL 列回读保留生效阈值（复算要用它）")
	assert.Equal(t, 2, got.FrameCount, "盖章不影响原有列")

	demo := byDate["2026-08-16"]
	require.NotNil(t, demo)
	assert.False(t, demo.HasRollupStamp(), "无印章行绝不能被读成有印章")
	assert.Nil(t, demo.AggregatedAt, "NULL 必须读成 nil，不能退化成零值时间")
	assert.Nil(t, demo.WearingThresholdN, "NULL 必须读成 nil，不能退化成 0 阈值")
	assert.Equal(t, 40, demo.FrameCount, "行本身照常返回（本卡不改数据，只让它可辨）")
}

// TestITT366CountFramesByCSTDay 明细帧按业务日计数：切日必须按 Asia/Shanghai，不按 UTC
func TestITT366CountFramesByCSTDay(t *testing.T) {
	ctx := context.Background()
	pool := dashPool
	require.NotNil(t, pool, "集成测试池未初始化")
	seedT366Patient(ctx, t, pool)
	seedT352Frames(ctx, t, pool, t366Patient, t366Device, []t352Frame{
		{"2026-08-15 10:00:00+08", 3.0, 1.0},
		{"2026-08-15 23:30:00+08", 2.0, 1.0}, // UTC 已是 08-15 15:30
		{"2026-08-16 00:30:00+08", 1.0, 1.0}, // UTC 仍是 08-15 16:30 ⇒ 按 UTC 分组会算进前一天
		{"2026-08-17 09:00:00+08", 1.0, 1.0}, // 区间外
	})

	counts, err := NewRecordRepo(pool).CountFramesByCSTDay(ctx, t366Patient, t352From, t352To.AddDate(0, 0, 1))
	require.NoError(t, err)

	assert.Equal(t, 2, counts["2026-08-15"], "CST 08-15 两帧")
	assert.Equal(t, 1, counts["2026-08-16"], "跨零点那帧归 CST 08-16（按 UTC 分组会得到 3/0）")
	assert.NotContains(t, counts, "2026-08-17", "区间外的日期不得出现")
	assert.Len(t, counts, 2, "区间内只应有这两天有帧")

	// 无帧的患者/日期不返回条目（读侧据「缺键」判 0 帧，与「未查」区分开）
	empty, err := NewRecordRepo(pool).CountFramesByCSTDay(ctx, "P-T366-NONE", t352From, t352To)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

// t366Day CST 日历日 → time.Time（与 RollupService 写 StatDate 的口径一致）
func t366Day(dateCST string) time.Time {
	d, _ := time.ParseInLocation("2006-01-02", dateCST, model.CSTZone())
	return d
}

// seedT366Patient 造患者+设备并注册清理（帧由用例自己按点铺）
func seedT366Patient(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	// 同一个占位符既落 varchar 列又进 md5()，PG 会推出两种类型而报 42P08
	// inconsistent types deduced；在 md5 侧写 ::text 并**不能**消除（本地 PG14 实测），
	// 必须是两个占位符（或 ::varchar）。
	_, err := pool.Exec(ctx, `
		INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		VALUES ($1, 'T366 患者', '\x00'::bytea, md5($2) || repeat('0', 32), 'active')
		ON CONFLICT (patient_id) DO NOTHING`, t366Patient, t366Patient)
	require.NoError(t, err, "seed patient")

	_, err = pool.Exec(ctx, `
		INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		VALUES ($1, '\x00'::bytea, $2, 'online')
		ON CONFLICT (device_id) DO NOTHING`, t366Device, t366Patient)
	require.NoError(t, err, "seed device")

	t.Cleanup(func() {
		for _, del := range []struct {
			sql string
			arg string
		}{
			{`DELETE FROM pressure_records WHERE device_id = $1`, t366Device},
			{`DELETE FROM alerts WHERE patient_id = $1`, t366Patient},
			{`DELETE FROM daily_wear_stats WHERE patient_id = $1`, t366Patient},
			{`DELETE FROM devices WHERE device_id = $1`, t366Device},
			{`DELETE FROM patients WHERE patient_id = $1`, t366Patient},
		} {
			if _, err := pool.Exec(context.Background(), del.sql, del.arg); err != nil {
				t.Errorf("cleanup %s: %v", del.sql, err)
			}
		}
	})
}
