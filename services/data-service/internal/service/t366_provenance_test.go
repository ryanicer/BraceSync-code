// Package service T366：daily-wear 行的来源可辨（provenance）与聚合入参可复算
//
// 这里锁住三件事：
//  1. 三值判档本身（rollup / corroborated / unsupported）——只有正向证据才升档；
//  2. 未知用 JSON null 表达（aggregatedAt / wearingThresholdN / detailFrameCount），
//     不许退化成空串或 0（T361 同族教训：空串把「不知道」说成「已知为空」）；
//  3. 反证：一行数值完全合理、但无印章且帧数与明细差 1 的行，必须落 unsupported，
//     半枚印章（只有一列有值）不得判成 rollup。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// t366Day CST 日历日 → StatDate（与 newDailyWearStatsTestRow 同口径）
func t366Day(t *testing.T, dateCST string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", dateCST, model.CSTZone())
	require.NoError(t, err)
	return d
}

// t366Stamp 给行盖聚合印章（模拟 RollupService.stampStats 的产物）
func t366Stamp(s *model.DailyWearStats, at time.Time, thresholdN float64) {
	s.AggregatedAt = &at
	s.WearingThresholdN = &thresholdN
}

func TestT366DeriveWearProvenance(t *testing.T) {
	ptr := func(v int) *int { return &v }

	cases := []struct {
		name     string
		stamped  bool
		declared int
		detail   *int
		want     string
	}{
		{"有印章即 rollup（明细不符也不降档：明细可能已按保留策略清掉）", true, 40, ptr(3), model.ProvenanceRollup},
		{"有印章 + 明细一致", true, 40, ptr(40), model.ProvenanceRollup},
		{"无印章 + 帧数与明细一致 → 佐证成立", false, 6, ptr(6), model.ProvenanceCorroborated},
		{"无印章 + 帧数与明细差 1 → 未佐证（反假绿主判据）", false, 6, ptr(5), model.ProvenanceUnsupported},
		{"无印章 + seed 示例行（声明 40 帧 / 明细 0 帧）", false, 40, ptr(0), model.ProvenanceUnsupported},
		{"无印章 + 明细未取到（nil）→ 不得当作已佐证", false, 6, nil, model.ProvenanceUnsupported},
		{"无印章 + 双方都是 0 → 声明与明细自洽，按佐证处理（不代表可信来源）", false, 0, ptr(0), model.ProvenanceCorroborated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, deriveWearProvenance(tc.stamped, tc.declared, tc.detail))
		})
	}
}

func TestT366HasRollupStampRequiresBothColumns(t *testing.T) {
	full := model.DailyWearStats{}
	t366Stamp(&full, time.Now().UTC(), model.WearingThresholdN)
	assert.True(t, full.HasRollupStamp())

	// 反证：半枚印章（只盖了时刻、没盖阈值）不得判成 rollup —— 否则读侧给出的
	// wearingThresholdN 是 null，而 provenance 却写着「可信聚合行」，自相矛盾。
	halfTime := model.DailyWearStats{AggregatedAt: ptrTime(time.Now().UTC())}
	assert.False(t, halfTime.HasRollupStamp(), "只有 aggregated_at 不算印章")

	halfTh := model.DailyWearStats{WearingThresholdN: ptrFloat(model.WearingThresholdN)}
	assert.False(t, halfTh.HasRollupStamp(), "只有 wearing_threshold_n 不算印章")

	assert.False(t, (&model.DailyWearStats{}).HasRollupStamp(), "两列皆空 = 无印章")
}

func ptrTime(v time.Time) *time.Time { return &v }
func ptrFloat(v float64) *float64    { return &v }

var _ repo.DailyFrameCounter = (*fakeDailyFrameCounter)(nil)

// TestT366NilCounterKeepsStampedRowsTrustworthy 佐证源未注入只影响「无印章行」的升档，不影响印章行
func TestT366NilCounterKeepsStampedRowsTrustworthy(t *testing.T) {
	ctx := context.Background()
	stamped := model.DailyWearStats{PatientID: "P1", StatDate: t366Day(t, "2026-09-23"), FrameCount: 588}
	t366Stamp(&stamped, time.Date(2026, 9, 23, 16, 10, 0, 0, time.UTC), 0.05)

	svc := newDailyWearSvcWithNow(&fakeDailyWearStore{rows: []model.DailyWearStats{
		stamped,
		{PatientID: "P1", StatDate: t366Day(t, "2026-09-22"), FrameCount: 6},
	}}, time.Date(2026, 9, 24, 10, 0, 0, 0, model.CSTZone()))

	list, appErr := svc.GetDailyWear(ctx, "P1", "2026-09-22", "2026-09-23")
	require.Nil(t, appErr)
	require.Len(t, list, 2)
	byDate := map[string]*model.DailyWearDayDTO{}
	for _, d := range list {
		byDate[d.Date] = d
	}

	assert.Equal(t, model.ProvenanceRollup, byDate["2026-09-23"].Provenance, "有印章不依赖明细佐证")
	require.NotNil(t, byDate["2026-09-23"].AggregatedAt)

	assert.Equal(t, model.ProvenanceUnsupported, byDate["2026-09-22"].Provenance, "无印章 + 无佐证源 ⇒ 不得凭空升档")
	assert.Nil(t, byDate["2026-09-22"].DetailFrameCount, "未查 ≠ 0 帧")
}

// TestT366GetDailyWearDerivesThreeTierProvenance 端到端（service 层 + fake 仓储）三档同现
func TestT366GetDailyWearDerivesThreeTierProvenance(t *testing.T) {
	ctx := context.Background()
	fakeNow := time.Date(2026, 9, 24, 10, 0, 0, 0, model.CSTZone())
	aggAt := time.Date(2026, 9, 23, 16, 10, 0, 0, time.UTC) // 09-24 00:10 CST 跑昨日聚合

	stamped := model.DailyWearStats{
		PatientID: "P1", StatDate: t366Day(t, "2026-09-21"),
		WearMinutes: 300, AvgPressure: 1.2, MaxPressure: 3.4, MaxPoint: "P03",
		FrameCount: 588, AbnormalCount: 1,
	}
	t366Stamp(&stamped, aggAt, 0.05)

	corroborated := model.DailyWearStats{
		PatientID: "P1", StatDate: t366Day(t, "2026-09-22"),
		WearMinutes: 480, FrameCount: 6, AbnormalCount: 0,
	}
	seedStyle := model.DailyWearStats{ // 声明 40 帧，但明细当日只有 3 帧
		PatientID: "P1", StatDate: t366Day(t, "2026-09-23"),
		WearMinutes: 1200, AvgPressure: 22.4, MaxPressure: 47.2, MaxPoint: "P03",
		FrameCount: 40, AbnormalCount: 1,
	}

	store := &fakeDailyWearStore{rows: []model.DailyWearStats{stamped, corroborated, seedStyle}}
	counter := &fakeDailyFrameCounter{counts: map[string]int{
		"2026-09-21": 588,
		"2026-09-22": 6,
		"2026-09-23": 3,
	}}
	svc := NewDailyWearService(store, counter)
	svc.now = func() time.Time { return fakeNow }

	list, appErr := svc.GetDailyWear(ctx, "P1", "2026-09-21", "2026-09-23")
	require.Nil(t, appErr)
	require.Len(t, list, 3)

	byDate := map[string]*model.DailyWearDayDTO{}
	for _, d := range list {
		byDate[d.Date] = d
	}

	rollup := byDate["2026-09-21"]
	require.NotNil(t, rollup)
	assert.Equal(t, model.ProvenanceRollup, rollup.Provenance)
	require.NotNil(t, rollup.AggregatedAt)
	assert.Equal(t, "2026-09-23T16:10:00Z", *rollup.AggregatedAt, "印章时刻按 UTC RFC3339 透出")
	require.NotNil(t, rollup.WearingThresholdN)
	assert.InDelta(t, 0.05, *rollup.WearingThresholdN, 1e-9, "实际生效阈值随行走，复算不必再猜配置")
	require.NotNil(t, rollup.DetailFrameCount)
	assert.Equal(t, 588, *rollup.DetailFrameCount, "聚合行的明细帧数应与声明一致")

	corro := byDate["2026-09-22"]
	require.NotNil(t, corro)
	assert.Equal(t, model.ProvenanceCorroborated, corro.Provenance)
	assert.Nil(t, corro.AggregatedAt, "无印章 ⇒ 印章字段必须是 null")
	assert.Nil(t, corro.WearingThresholdN)

	unsupported := byDate["2026-09-23"]
	require.NotNil(t, unsupported)
	assert.Equal(t, model.ProvenanceUnsupported, unsupported.Provenance)
	require.NotNil(t, unsupported.DetailFrameCount, "明细查到了（3 帧），必须如实回传而不是 null")
	assert.Equal(t, 3, *unsupported.DetailFrameCount)
	assert.Nil(t, unsupported.AggregatedAt)

	assert.Equal(t, 1, counter.calls, "整段区间只统计一次明细帧数（不按行 N+1 查）")
}

// TestT366UnknownSerializesAsJSONNull 反证：未知量在 JSON 里是 null，不是空串/0
func TestT366UnknownSerializesAsJSONNull(t *testing.T) {
	ctx := context.Background()
	svc := NewDailyWearService(
		&fakeDailyWearStore{rows: []model.DailyWearStats{{
			PatientID: "P1", StatDate: t366Day(t, "2026-09-23"),
			WearMinutes: 1200, FrameCount: 40,
		}}},
		&fakeDailyFrameCounter{counts: map[string]int{}}, // 该日 0 帧
	)
	svc.now = func() time.Time { return time.Date(2026, 9, 24, 10, 0, 0, 0, model.CSTZone()) }

	list, appErr := svc.GetDailyWear(ctx, "P1", "2026-09-23", "2026-09-23")
	require.Nil(t, appErr)
	raw, err := json.Marshal(list)
	require.NoError(t, err)
	body := string(raw)

	assert.Contains(t, body, `"provenance":"unsupported"`)
	assert.Contains(t, body, `"aggregatedAt":null`)
	assert.Contains(t, body, `"wearingThresholdN":null`)
	assert.Contains(t, body, `"detailFrameCount":0`, "查明细确认为 0 帧要写 0，不能与「未查」混成 null")
	assert.NotContains(t, body, `"aggregatedAt":""`, "空串会把「不知道」说成「已知为空」")
}

// TestT366DetailCountFailureDegradesNotErrors 佐证源挂掉时：接口照常 200，行降为 unsupported
func TestT366DetailCountFailureDegradesNotErrors(t *testing.T) {
	ctx := context.Background()
	svc := NewDailyWearService(
		&fakeDailyWearStore{rows: []model.DailyWearStats{{
			PatientID: "P1", StatDate: t366Day(t, "2026-09-23"), FrameCount: 6,
		}}},
		&fakeDailyFrameCounter{err: errors.New("detail db down")},
	)
	svc.now = func() time.Time { return time.Date(2026, 9, 24, 10, 0, 0, 0, model.CSTZone()) }

	list, appErr := svc.GetDailyWear(ctx, "P1", "2026-09-23", "2026-09-23")
	require.Nil(t, appErr, "佐证失败不得把 daily-wear 打成 500")
	require.Len(t, list, 1)
	assert.Equal(t, model.ProvenanceUnsupported, list[0].Provenance, "查不到明细不能算佐证成立")
	assert.Nil(t, list[0].DetailFrameCount, "未查 ≠ 0 帧")
}
