//go:build integration
// +build integration

// Package repo T411：WearDetailByCSTDay 真库回归 —— 佐证判据缺的那一半（分钟/均值复算输入）
//
// 本卡要证的是「读侧复算与写侧同源」这一条命题，因此不孤立地测新 SQL，而是把
// 同一批明细帧分别喂给写侧 AggregateDate 和读侧 WearDetailByCSTDay，要求：
//  1. 现口径候选（跨度折算分钟、佩戴帧 20 点全点均值）与写侧落库值逐点对平
//     —— 若两处式子哪天分叉，本用例立刻红，而不是让读侧自称「复算」
//  2. 老口径候选（佩戴帧数 × 采集间隔、全帧峰值均值）必须与现口径**可区分**
//     —— 这就是代次归属能立的唯一依据，两值若恒等则判据无意义
//  3. 阈值作为参数进 SQL：换档位后佩戴帧集合/跨度/均值随之变，AvgMaxAll 不变
//
// 切日与窗口口径沿用 T352（[$1,$2) UTC + 显式 AT TIME ZONE 'Asia/Shanghai'），
// 无帧日不出现在 map 里（调用方据此区分「0 帧」与「未统计」）。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// t411To CST 2026-08-16 全天结束（跨两个业务日，验按日分桶）
var t411To = t352To.AddDate(0, 0, 1)

// TestITT411WearDetailMatchesRollup 主用例：读写同源 + 两代候选可区分
func TestITT411WearDetailMatchesRollup(t *testing.T) {
	ctx := context.Background()
	pool := dashPool
	require.NotNil(t, pool, "集成测试池未初始化")

	const (
		patient = "P-T411-IT1"
		device  = "D-T411-IT1"
	)
	seedT352Patient(ctx, t, pool, patient, device)

	// CST 08-15：3 帧佩戴（01/03/05 点，跨度 4h）+ 1 帧未佩戴（峰值 0.01）
	// CST 08-16：单帧佩戴（无跨度可确立）
	seedT352Frames(ctx, t, pool, patient, device, []t352Frame{
		{"2026-08-15 01:00:00+08", 3.0, 1.0},
		{"2026-08-15 03:00:00+08", 2.0, 1.0},
		{"2026-08-15 05:00:00+08", 1.5, 0.5},
		{"2026-08-15 20:00:00+08", 0.01, 0.0},
		{"2026-08-16 08:00:00+08", 4.0, 1.0},
	})

	details, err := NewRecordRepo(pool).WearDetailByCSTDay(ctx, patient,
		t352From, t411To, model.WearingThresholdN)
	require.NoError(t, err)

	// 按 CST 日分桶：两桶，且无帧日不进 map（08-17 不在返回值里）
	require.Len(t, details, 2)
	d15, ok := details["2026-08-15"]
	require.True(t, ok, "切日必须按 Asia/Shanghai，键为 YYYY-MM-DD")
	d16, ok := details["2026-08-16"]
	require.True(t, ok)

	assert.Equal(t, 4, d15.Frames, "Frames = 该日帧总数（与佩戴与否无关，T366 佐证项原样保留）")
	assert.Equal(t, 3, d15.WearingFrames)
	assert.InDelta(t, 14400.0, d15.WearSpanSeconds, 1.0, "跨度 = 佩戴帧首尾差（未佩戴帧不参与）")

	// 老口径日均（全帧峰值均值）与现口径日均（佩戴帧全点均值）必须不相等 —— 这是代次可判的一手证据
	wantAvgCurrent := (3.0 + 19.0 + 2.0 + 19.0 + 1.5 + 19*0.5) / 20.0 / 3.0
	wantAvgLegacy := (3.0 + 2.0 + 1.5 + 0.01) / 4.0
	assert.InDelta(t, wantAvgCurrent, d15.AvgPointWearing, 1e-4)
	assert.InDelta(t, wantAvgLegacy, d15.AvgMaxAll, 1e-4)
	assert.NotEqual(t, d15.AvgPointWearing, d15.AvgMaxAll,
		"两代日均若相等则本卡判据无法区分口径，须换造例")

	// 现口径候选复算 → 与写侧 AggregateDate 落库值逐点对平（同源，不在 service 另抄式子）
	currentMinutes := WearMinutesFromSpan(d15.WearingFrames, d15.WearSpanSeconds)
	assert.Equal(t, 360, currentMinutes, "跨度 4h×3/2 = 360，与 T352 写侧断言同值")
	legacyMinutes := model.WearMinutesLegacy(d15.WearingFrames, DefaultIntervalMinutes)
	assert.Equal(t, 90, legacyMinutes, "老口径 = 佩戴帧数×采集间隔，与现口径不同值")

	stats, err := NewRollupRepo(pool).AggregateDate(ctx, t352From, t352To, model.WearingThresholdN)
	require.NoError(t, err)
	got := findT352Stat(t, stats, patient)
	assert.Equal(t, currentMinutes, got.WearMinutes, "读侧复算候选必须等于写侧现口径结果")
	assert.InDelta(t, float64(got.AvgPressure), d15.AvgPointWearing, 1e-4,
		"日均同理：两侧同式才谈得上「复算」")
	assert.Equal(t, d15.Frames, got.FrameCount, "帧数佐证项与 T366 口径不漂移")

	// 单帧日：现口径无跨度记 0，老口径按间隔臆造 30 分钟 —— 零值本身就是强判据
	assert.Equal(t, 1, d16.Frames)
	assert.Equal(t, 1, d16.WearingFrames)
	assert.Zero(t, d16.WearSpanSeconds, "单帧不成立跨度")
	assert.Zero(t, WearMinutesFromSpan(d16.WearingFrames, d16.WearSpanSeconds),
		"现口径单帧记 0，不用配置间隔补")
	assert.Equal(t, DefaultIntervalMinutes, model.WearMinutesLegacy(d16.WearingFrames, DefaultIntervalMinutes),
		"老口径单帧非零：两代在单帧日必然分叉")
}

// TestITT411WearDetailThresholdIsParameterized 阈值假设值进 SQL：换档位只动佩戴侧字段，
// AvgMaxAll（全帧、无 FILTER）保持不变
func TestITT411WearDetailThresholdIsParameterized(t *testing.T) {
	ctx := context.Background()
	pool := dashPool
	require.NotNil(t, pool)

	const (
		patient = "P-T411-IT2"
		device  = "D-T411-IT2"
	)
	seedT352Patient(ctx, t, pool, patient, device)
	seedT352Frames(ctx, t, pool, patient, device, []t352Frame{
		{"2026-08-15 01:00:00+08", 3.0, 1.0},
		{"2026-08-15 03:00:00+08", 2.0, 1.0},
		{"2026-08-15 05:00:00+08", 1.5, 0.5},
		{"2026-08-15 20:00:00+08", 0.01, 0.0},
	})

	base, err := NewRecordRepo(pool).WearDetailByCSTDay(ctx, patient,
		t352From, t352To, model.WearingThresholdN)
	require.NoError(t, err)

	high, err := NewRecordRepo(pool).WearDetailByCSTDay(ctx, patient, t352From, t352To, 1.6)
	require.NoError(t, err)

	require.Contains(t, high, "2026-08-15")
	d := high["2026-08-15"]
	assert.Equal(t, 4, d.Frames, "帧总数与阈值无关")
	assert.Equal(t, 2, d.WearingFrames, "1.5 N 那帧被排除")
	assert.InDelta(t, 7200.0, d.WearSpanSeconds, 1.0)
	assert.Equal(t, 240, WearMinutesFromSpan(d.WearingFrames, d.WearSpanSeconds),
		"跨度 2h×2/1 = 240 分钟，与 T352 阈值断言同值")
	assert.InDelta(t, (22.0+21.0)/20.0/2.0, d.AvgPointWearing, 1e-4, "日均只算阈值以上帧")
	assert.InDelta(t, base["2026-08-15"].AvgMaxAll, d.AvgMaxAll, 1e-4,
		"老口径日均无佩戴过滤，不随假设阈值变")

	// 阈值高于所有帧峰值：佩戴集为空 → 跨度/日均归零，但帧数仍在（区别于「未统计」）
	none, err := NewRecordRepo(pool).WearDetailByCSTDay(ctx, patient, t352From, t352To, 99.0)
	require.NoError(t, err)
	assert.Equal(t, 4, none["2026-08-15"].Frames)
	assert.Zero(t, none["2026-08-15"].WearingFrames)
	assert.Zero(t, none["2026-08-15"].WearSpanSeconds)
	assert.Zero(t, none["2026-08-15"].AvgPointWearing)
}

// TestITT411WearDetailEmptyDayIsAbsent 区间内无帧的日期不出现在 map 里：
// 「0 帧」与「未统计」在读侧必须是两种形状（unsupported 判据依赖这一区分）
func TestITT411WearDetailEmptyDayIsAbsent(t *testing.T) {
	ctx := context.Background()
	pool := dashPool
	require.NotNil(t, pool)

	const (
		patient = "P-T411-IT3"
		device  = "D-T411-IT3"
	)
	seedT352Patient(ctx, t, pool, patient, device)
	seedT352Frames(ctx, t, pool, patient, device, []t352Frame{
		{"2026-08-15 01:00:00+08", 3.0, 1.0},
	})

	// 窗口只含 08-15 一天；相邻日不得出现在返回值里
	details, err := NewRecordRepo(pool).WearDetailByCSTDay(ctx, patient,
		t352From, t352To, model.WearingThresholdN)
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.NotContains(t, details, "2026-08-16", "窗口外不得凭空多出一天")

	// 完全无帧的患者：空 map（不是含零值的 map）
	empty, err := NewRecordRepo(pool).WearDetailByCSTDay(ctx, "P-T411-NOSUCH",
		t352From, t411To, model.WearingThresholdN)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
