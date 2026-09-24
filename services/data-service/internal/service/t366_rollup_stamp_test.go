// Package service T366：聚合任务写行时必须盖章（写侧唯一盖章点）
//
// 印章只在 stampStats 一处产生，读侧 provenance=rollup 的全部可信度都押在这里：
// 盖错时刻 / 盖了另一个阈值 / 漏盖，都会让「可信聚合行」变成假证据。
// 故这里既测「盖了什么」，也测两条写入口（每日聚合 + 补传重算）都盖。
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// t366AggRows 聚合 SQL 返回的行（尚未盖章，模拟 AggregateDate 的真实产物）
func t366AggRows() []model.DailyWearStats {
	return []model.DailyWearStats{
		{PatientID: "P1", FrameCount: 588, WearMinutes: 300, AvgPressure: 1.2},
		{PatientID: "P2", FrameCount: 6, WearMinutes: 45},
	}
}

// requireT366Stamped 断言落库行带着「与本次聚合同源」的印章
func requireT366Stamped(t *testing.T, rows []model.DailyWearStats, wantAt time.Time, wantThreshold float64) {
	t.Helper()
	require.NotEmpty(t, rows)
	for i := range rows {
		s := &rows[i]
		require.True(t, s.HasRollupStamp(), "行 %d(%s) 必须带完整印章", i, s.PatientID)
		assert.True(t, s.AggregatedAt.Equal(wantAt), "印章时刻 = 本次聚合时刻（UTC）：%s", s.AggregatedAt)
		assert.InDelta(t, wantThreshold, *s.WearingThresholdN, 1e-9,
			"印章阈值必须等于传给 AggregateDate 的那一个，否则读侧按它复算就算错")
	}
}

func TestT366DailyRollupStampsRows(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = t366AggRows()
	cfg := &t352ThresholdConfigs{intervalMinutes: 30, wearingN: 0.42}

	svc := NewRollupService(stats, newFakeCache(), cfg)
	// CST 2026-08-11 00:10 跑昨日（08-10）聚合
	now := time.Date(2026, 8, 10, 16, 10, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	svc.RunDailyRollup(context.Background())

	requireT366Stamped(t, stats.upserted, now, 0.42)
	for _, s := range stats.upserted {
		assert.Equal(t, "2026-08-10", s.StatDate.In(model.CSTZone()).Format("2006-01-02"),
			"盖章不得改坏切日（StatDate 仍是业务日）")
	}
}

func TestT366BackfillRecomputeStampsRows(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = []model.DailyWearStats{{PatientID: "P1", FrameCount: 20, WearMinutes: 600}}
	cache := newFakeCache()
	cfg := &t352ThresholdConfigs{intervalMinutes: 30, wearingN: 0.42}

	task := rollupTask{PatientID: "P1", Date: "2026-08-10", QueuedAt: "2026-08-11T00:00:00Z"}
	payload, _ := json.Marshal(task)
	cache.rollup = append(cache.rollup, string(payload))

	svc := NewRollupService(stats, cache, cfg)
	now := time.Date(2026, 8, 11, 16, 10, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	svc.ProcessBackfillQueue(context.Background())

	// 补传重算也是「聚合任务写的行」，同样必须盖章；否则重算一次就把可信行降级成未佐证
	requireT366Stamped(t, stats.upserted, now, 0.42)
	assert.Empty(t, cache.rollup)
}

func TestT366StampFallsBackToDefaultThreshold(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = t366AggRows()

	svc := NewRollupService(stats, newFakeCache(), &fakeConfigs{interval: 30, version: 1})
	now := time.Date(2026, 8, 10, 16, 10, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	svc.RunDailyRollup(context.Background())

	// 配置读不到时盖的是代码兜底值——仍然要盖（有章可查），且章里的阈值就是实际用的那个
	requireT366Stamped(t, stats.upserted, now, model.WearingThresholdN)
}
