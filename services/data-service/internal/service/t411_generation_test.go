// T411：日聚合行的「口径代次」判定（corroborated 只证帧数、不证代次）
//
// 缺陷原文（T352 第 40 轮验收 L-3）：corroborated 佐证判据不含任何分钟或均值复算项，
// 既不能判老口径也不能判新口径 ⇒ 聚合行代次归属不可独立复算。
// 本文件的用例把这两问分开钉住：
//  1. 七档判定各自只在有正向证据时成立（打表）；
//  2. provenance 与 wearGeneration 互不顶替（同一行可以「帧数已佐证」+「代次不可归属」）；
//  3. 复算现场（假设阈值 / 假设间隔 / 两代候选值）必须随行下发，不能只给一个结论字符串。
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

const (
	t411N        = 0.05 // 假设佩戴阈值（N）
	t411Interval = 30   // 假设采集间隔（分钟）
)

// t411Day 复算用例统一用的业务日（CST 零点，与 RollupService 写 StatDate 的口径一致）
var t411Day = time.Date(2026, 9, 22, 0, 0, 0, 0, model.CSTZone())

// t411Row 造一行无聚合印章的 daily_wear_stats
func t411Row(minutes int, avg float32, frameCount int) model.DailyWearStats {
	return model.DailyWearStats{
		PatientID: "P1", StatDate: t411Day,
		WearMinutes: minutes, AvgPressure: avg, FrameCount: frameCount,
	}
}

// t411Detail 造一份现存明细复算输入
func t411Detail(frames, wearing int, span float64, avgPointWearing, avgMaxAll float64) repo.WearDayDetail {
	return repo.WearDayDetail{
		Frames: frames, WearingFrames: wearing, WearSpanSeconds: span,
		AvgPointWearing: avgPointWearing, AvgMaxAll: avgMaxAll,
	}
}

func t411Gen(row model.DailyWearStats, d *repo.WearDayDetail) (string, *model.WearGenerationCheck) {
	return deriveWearGeneration(row.HasRollupStamp(), row, d, t411N, t411Interval)
}

// 600 帧、实测约 31 秒一帧：现口径 ≈ 311 分钟，老口径 = 600×30 封顶 1440。
// 两代的日均也不同层（点均值 1.0 vs 峰值均值 2.0），故四种组合都能被区分。
var (
	t411Detail600   = t411Detail(600, 600, 18600, 1.0, 2.0)
	t411CurMinutes  = repo.WearMinutesFromSpan(600, 18600)
	t411LegMinutes  = model.WearMinutesLegacy(600, t411Interval)
	t411CurRow      = t411Row(t411CurMinutes, 1.0, 600)
	t411LegacyRow   = t411Row(t411LegMinutes, 2.0, 600)
	t411UnmatchRow  = t411Row(900, 1.5, 600)
	t411NoWearDet   = t411Detail(5, 0, 0, 0, 0) // 全空载帧：两代分钟都是 0、日均都是 0
	t411NoDetailRow = t411Row(0, 0, 5)
)

func TestT411GenerationCaliberAssumptions(t *testing.T) {
	require.Equal(t, 311, t411CurMinutes, "跨度口径：600 帧 × 约 31 秒 ⇒ 311 分钟")
	require.Equal(t, 1440, t411LegMinutes, "老口径：600×30 须夹到物理日 1440")
	require.NotEqual(t, t411CurMinutes, t411LegMinutes, "两代分钟候选必须可区分，否则下面的用例没有意义")
}

func TestT411DeriveWearGenerationTable(t *testing.T) {
	cases := []struct {
		name string
		row  model.DailyWearStats
		d    *repo.WearDayDetail
		want string
	}{
		{"只对上现口径", t411CurRow, &t411Detail600, model.GenRecomputedCurrent},
		{"只对上老口径", t411LegacyRow, &t411Detail600, model.GenRecomputedLegacy},
		{"两代都对上_不可区分", t411NoDetailRow, &t411NoWearDet, model.GenAmbiguous},
		{"两代都对不上", t411UnmatchRow, &t411Detail600, model.GenUnmatched},
		{"查明细_该日无帧", t411Row(300, 1.5, 10), &repo.WearDayDetail{}, model.GenNoDetail},
		{"未查明细", t411CurRow, nil, model.GenUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ck := t411Gen(c.row, c.d)
			assert.Equal(t, c.want, got)
			if c.want == model.GenUnknown {
				assert.Nil(t, ck, "未做复算时不得下发一个空的复算现场（会被读成「算了且不符」）")
				return
			}
			require.NotNil(t, ck)
			assert.InDelta(t, t411N, ck.AssumedWearingThresholdN, 0, "假设阈值随行下发")
			assert.Equal(t, t411Interval, ck.AssumedIntervalMinutes, "假设采集间隔随行下发")
		})
	}
}

func TestT411GenerationMatchRequiresBothMinutesAndMean(t *testing.T) {
	// 只对上分钟、均值差 0.1%（超相对容差 1e-4）⇒ 不得命中任一代
	partial := t411Row(t411CurMinutes, 1.0*1.001, 600)
	got, ck := t411Gen(partial, &t411Detail600)
	assert.False(t, ck.MatchCurrent, "分钟对上但均值没对上 ⇒ 现口径不成立")
	assert.False(t, ck.MatchLegacy)
	assert.Equal(t, model.GenUnmatched, got)

	// 容差内（real 列窄化的位差）仍算对上
	near := t411Row(t411CurMinutes, float32(1.0*(1+1e-7)), 600)
	got2, ck2 := t411Gen(near, &t411Detail600)
	assert.True(t, ck2.MatchCurrent)
	assert.Equal(t, model.GenRecomputedCurrent, got2)
}

func TestT411GenerationDoesNotOverstateFrameCorroboration(t *testing.T) {
	// 帧数与明细不符：复算现场要显式记下来，供代次结论复核
	row := t411Row(t411CurMinutes, 1.0, 599) // 行声明 599 帧，明细 600 帧
	_, ck := t411Gen(row, &t411Detail600)
	assert.False(t, ck.FrameCountMatchesDetail)
	assert.Equal(t, 600, ck.DetailFrames)
	assert.True(t, ck.MatchCurrent, "分钟与均值仍可对上（差一帧是补传形状），但帧数佐证已不成立")
}

// TestT411StampedRowAttributesBySealEvenWhenRecomputeMisses 印章行的代次靠印章推定，
// 复算算不符不把它改判成 unmatched——但复算现场照发，看得见不一致。
func TestT411StampedRowAttributesBySealEvenWhenRecomputeMisses(t *testing.T) {
	row := t411Row(t411UnmatchRow.WearMinutes, t411UnmatchRow.AvgPressure, 600)
	t366Stamp(&row, time.Date(2026, 9, 22, 16, 10, 0, 0, time.UTC), t411N)
	require.True(t, row.HasRollupStamp())

	got, ck := t411Gen(row, &t411Detail600)
	assert.Equal(t, model.GenSealed, got)
	require.NotNil(t, ck)
	assert.False(t, ck.MatchCurrent, "复算不符要看得见，不能因为印章就一起粉饰掉")
	assert.False(t, ck.MatchLegacy)
}

// fakeWearConfig T411：sys_configs 假设值来源替身
type fakeWearConfig struct {
	wearingN float64
	interval int
}

func (f *fakeWearConfig) GetPressureThresholds(context.Context) (model.PressureThresholds, error) {
	return model.PressureThresholds{WearingN: f.wearingN}, nil
}

func (f *fakeWearConfig) GetDeviceConfig(context.Context) (int, int, error) {
	return f.interval, 1, nil
}

func TestT411ServiceUsesConfiguredAssumptions(t *testing.T) {
	ctx := context.Background()
	src := &fakeDailyWearSource{details: map[string]repo.WearDayDetail{"2026-09-22": t411Detail600}}
	svc := NewDailyWearService(
		&fakeDailyWearStore{rows: []model.DailyWearStats{t411LegacyRow}},
		src, &fakeWearConfig{wearingN: 0.4, interval: 7},
	)
	svc.now = func() time.Time { return time.Date(2026, 9, 24, 10, 0, 0, 0, model.CSTZone()) }

	list, appErr := svc.GetDailyWear(ctx, "P1", "2026-09-22", "2026-09-22")
	require.Nil(t, appErr)
	require.Len(t, list, 1)

	assert.InDelta(t, 0.4, src.gotN, 0, "复算用的阈值必须来自注入的配置源，不是写死的 model 默认")
	require.NotNil(t, list[0].WearRecompute)
	assert.InDelta(t, 0.4, list[0].WearRecompute.AssumedWearingThresholdN, 0)
	assert.Equal(t, 7, list[0].WearRecompute.AssumedIntervalMinutes)
	// 间隔改成 7 后老口径候选 = 600×7=4200，夹到 1440 仍等于行值 ⇒ 依旧判老口径
	assert.Equal(t, 1440, list[0].WearRecompute.ExpectedWearMinutesLegacy)
	assert.Equal(t, model.GenRecomputedLegacy, list[0].WearGeneration)
}

// TestT411CorroboratedRowCanStillBeUnattributed 本卡的核心语义：
// 「帧数被明细佐证」与「代次可归属」是两问，同一行可以是 corroborated + legacy_recomputed。
// T352 L-3 报的正是这个缺口：此前读到 corroborated 就以为数值可信，实则不知是哪一代算的。
func TestT411CorroboratedRowCanStillBeUnattributed(t *testing.T) {
	ctx := context.Background()
	src := &fakeDailyWearSource{details: map[string]repo.WearDayDetail{"2026-09-22": t411Detail600}}
	svc := NewDailyWearService(&fakeDailyWearStore{rows: []model.DailyWearStats{t411LegacyRow}}, src, nil)
	svc.now = func() time.Time { return time.Date(2026, 9, 24, 10, 0, 0, 0, model.CSTZone()) }

	list, appErr := svc.GetDailyWear(ctx, "P1", "2026-09-22", "2026-09-22")
	require.Nil(t, appErr)
	require.Len(t, list, 1)

	assert.Equal(t, model.ProvenanceCorroborated, list[0].Provenance, "帧数佐证照旧升档（本卡不收紧 T366 判据）")
	assert.Equal(t, model.GenRecomputedLegacy, list[0].WearGeneration, "代次另判：这行是老口径算出来的")
	assert.Nil(t, list[0].WearingThresholdN, "无印章 ⇒ 阈值仍不得伪装成已知")
}

// TestT411GenerationSerializesExplicitly 七档与「未复算」在 JSON 里的形状：
// unknown 必须是 wearRecompute:null（「没算」），不得退化成一个全零对象（会被读成「算了且不符」）。
func TestT411GenerationSerializesExplicitly(t *testing.T) {
	ctx := context.Background()
	svc := NewDailyWearService(
		&fakeDailyWearStore{rows: []model.DailyWearStats{t411LegacyRow}}, nil, nil)
	svc.now = func() time.Time { return time.Date(2026, 9, 24, 10, 0, 0, 0, model.CSTZone()) }

	list, appErr := svc.GetDailyWear(ctx, "P1", "2026-09-22", "2026-09-22")
	require.Nil(t, appErr)

	raw, err := json.Marshal(list)
	require.NoError(t, err)
	body := string(raw)
	assert.Contains(t, body, `"wearGeneration":"unknown"`)
	assert.Contains(t, body, `"wearRecompute":null`)
	assert.Contains(t, body, `"detailFrameCount":null`)
}
