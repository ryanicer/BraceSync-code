//go:build integration
// +build integration

// Package repo T595：Dashboard「今日」两格当日明细聚合 KPILiveToday 真库回归（testcontainers PG15）。
//
// 必须真库证明的点（SQL 侧 stub 不掉）：
//  1. 与 rollup aggregateDateSQL 同式：佩戴帧 COUNT + MAX(ts)-MIN(ts) 跨度，
//     分钟在 Go 侧走 WearMinutesFromSpan —— 2 枚佩戴帧跨度 3h ⇒ 360min（不是帧数×配置间隔）。
//  2. 乱序入库（晚帧先到、早帧补传）不影响跨度：SQL 用 MIN/MAX，与插入顺序无关。
//  3. 分母 = 当日有帧患者全集（含 0 佩戴分钟者）；无帧患者不出 GROUP 行，不进分母；
//     全范围无行 ⇒ ActiveWear=0、AvgWearMinutes=nil（T636：无数据不涂 0）。
//  4. 阈值进 SQL（抬高阈值后同一患者不再算佩戴）；团队 scope 与 kpiSQL 同一 CTE 收口。
//
// 用独立团队 + ScopeTeam 隔离断言，与同包 T350/T411 等 2026-08 种子互不串数。
package repo

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

const (
	t595Team = "TEAM-T595-IT"
	t595PatA = "P-T595-ITA"
	t595PatB = "P-T595-ITB"
	t595PatC = "P-T595-ITC"
	t595DevA = "D-T595-ITA"
	t595DevB = "D-T595-ITB"
)

// seedT595LiveToday 三名同团队患者：
// A：2 枚佩戴帧（01:00/04:00，跨度 3h；故意先插 04:00 再插 01:00 验乱序）+ 1 枚窗口外帧；
// B：当日 1 帧但未佩戴（0.01 < 默认阈值 0.05）⇒ 出行但 0 分钟；
// C：建档当日无任何帧 ⇒ 不出 GROUP 行（证明不进均值分母）。
func seedT595LiveToday(ctx context.Context, t *testing.T) {
	t.Helper()
	// 全部参数化（曾用 fmt.Sprintf 混位置动词把患者 ID 插错，CI 真库 FK 23503 抓出）
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO teams (team_id, name) VALUES ($1, 'T595 IT 科')
		 ON CONFLICT (team_id) DO NOTHING`, []any{t595Team}},
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status, team_id)
		 VALUES ($1, 'T595 A', '\x00'::bytea, 't595a' || repeat('0', 59), 'active', $4),
		        ($2, 'T595 B', '\x00'::bytea, 't595b' || repeat('0', 59), 'active', $4),
		        ($3, 'T595 C', '\x00'::bytea, 't595c' || repeat('0', 59), 'active', $4)
		 ON CONFLICT (patient_id) DO NOTHING`, []any{t595PatA, t595PatB, t595PatC, t595Team}},
		{`INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		 VALUES ($1, '\x00'::bytea, $2, 'online'),
		        ($3, '\x00'::bytea, $4, 'online')
		 ON CONFLICT (device_id) DO NOTHING`, []any{t595DevA, t595PatA, t595DevB, t595PatB}},
	}
	for _, s := range stmts {
		_, err := dashPool.Exec(ctx, s.sql, s.args...)
		require.NoError(t, err, "t595 seed")
	}
	// A：乱序入库 + 窗口外帧（2026-08-16 00:30+08 = UTC 16:30，落在右端 16:00 之外）
	seedT352Frames(ctx, t, dashPool, t595PatA, t595DevA, []t352Frame{
		{"2026-08-15 04:00:00+08", 3.0, 1.0},
		{"2026-08-15 01:00:00+08", 3.0, 1.0},
		{"2026-08-16 00:30:00+08", 9.0, 0.0},
	})
	// B：唯一一帧未佩戴
	seedT352Frames(ctx, t, dashPool, t595PatB, t595DevB, []t352Frame{
		{"2026-08-15 10:00:00+08", 0.01, 0.0},
	})
	t.Cleanup(func() {
		for _, s := range []string{
			fmt.Sprintf(`DELETE FROM pressure_records WHERE device_id IN ('%s','%s')`, t595DevA, t595DevB),
			fmt.Sprintf(`DELETE FROM devices WHERE device_id IN ('%s','%s')`, t595DevA, t595DevB),
			fmt.Sprintf(`DELETE FROM patients WHERE patient_id IN ('%s','%s','%s')`, t595PatA, t595PatB, t595PatC),
			fmt.Sprintf(`DELETE FROM teams WHERE team_id = '%s'`, t595Team),
		} {
			if _, err := dashPool.Exec(context.Background(), s); err != nil {
				t.Errorf("t595 cleanup: %v", err)
			}
		}
	})
}

func TestITKPILiveToday(t *testing.T) {
	ctx := context.Background()
	seedT595LiveToday(ctx, t)
	r := NewDashboardRepo(dashPool)

	// 窗口 = CST 2026-08-15 全天（复用 T352 的 UTC 半开区间常量）
	got, err := r.KPILiveToday(ctx, t352From, t352To, float32(model.WearingThresholdN), model.ScopeTeam(t595Team))
	require.NoError(t, err)
	require.NotNil(t, got)

	// A：2 枚佩戴帧乱序入库，跨度仍为 01:00→04:00=10800s
	// ⇒ WearMinutesFromSpan(2,10800)=round(10800*2/1/60)=360min（不是 2*30 配置间隔）
	assert.Equal(t, int64(1), got.ActiveWear, "只有 A 当日有佩戴分钟")
	require.NotNil(t, got.AvgWearMinutes, "A、B 当日有帧 ⇒ 均值非 nil（无帧群体才是 nil）")
	assert.InDelta(t, 180.0, *got.AvgWearMinutes, 1e-9,
		"分母=有帧患者 A+B 共 2 名（B 为 0 分钟照入分母），(360+0)/2=180；无帧的 C 不进分母")

	// 阈值进 SQL：抬到 9.5 后 A 的 3.0 帧也不再算佩戴 ⇒ 全员 0 分钟（行仍在，均值=0 非 nil）
	high, err := r.KPILiveToday(ctx, t352From, t352To, 9.5, model.ScopeTeam(t595Team))
	require.NoError(t, err)
	assert.Zero(t, high.ActiveWear)
	require.NotNil(t, high.AvgWearMinutes)
	assert.InDelta(t, 0.0, *high.AvgWearMinutes, 1e-9)

	// 窗口外那天（CST 2026-08-16 全天）：A 有 1 帧（00:30）但佩戴帧 <2 ⇒ 0 分钟；
	// B/C 无帧 ⇒ 只 A 出行，ActiveWear=0、均值 0（非 nil，因为有帧患者存在）
	next, err := r.KPILiveToday(ctx, t352To, t352To.AddDate(0, 0, 1), float32(model.WearingThresholdN), model.ScopeTeam(t595Team))
	require.NoError(t, err)
	assert.Zero(t, next.ActiveWear)
	require.NotNil(t, next.AvgWearMinutes)
	assert.InDelta(t, 0.0, *next.AvgWearMinutes, 1e-9, "单帧不构成时长，且该帧属窗口右端外的次日窗")

	// 与种子完全不沾边的一天：无任何出行 ⇒ nil 均值（T636 口径，不涂 0）
	farFrom := t352From.AddDate(0, 0, 30)
	empty, err := r.KPILiveToday(ctx, farFrom, farFrom.AddDate(0, 0, 1), float32(model.WearingThresholdN), model.ScopeTeam(t595Team))
	require.NoError(t, err)
	assert.Zero(t, empty.ActiveWear)
	assert.Nil(t, empty.AvgWearMinutes, "范围内无任何帧 ⇒ NULL，不得被 COALESCE 涂成 0")

	// T350 同一 scope CTE：空团队（医生无 team_id）fail-closed，不退化全院
	none, err := r.KPILiveToday(ctx, t352From, t352To, float32(model.WearingThresholdN), model.ScopeTeam(""))
	require.NoError(t, err)
	assert.Zero(t, none.ActiveWear)
	assert.Nil(t, none.AvgWearMinutes)
}
