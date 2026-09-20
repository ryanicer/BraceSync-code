// Package engine — 补充测试（T003 Winner）
//
// engine_test.go（Ella T002）是唯一行为契约，禁止修改；
// 本文件仅覆盖契约用例未触达的实现分支（默认构造 / 佩戴中断触发 / 去重抑制 /
// EvaluateAll 聚合 / 补传帧短路），钉住实现行为并支撑 ≥90% 覆盖率验收。
//
// T257 2.6：压力波动规则摘除 ⇒ 原 Fluctuation_* 三条用例替换为「不再产生波动告警」的回归门禁；
// 新增 EvaluateWearDurationShort（佩戴时长不足）用例。
package engine_test

import (
	"testing"
	"time"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplement_DefaultEvaluatorThresholds(t *testing.T) {
	e := engine.NewDefaultRuleEvaluator()
	assert.Equal(t, 45.0, e.PressureHighThreshold)
	assert.Equal(t, 60, e.WearInterruptMinutes)
	assert.Equal(t, 2.8, e.SensorDriftThreshold)
	assert.Equal(t, 30, e.DedupWindowMinutes)
	assert.Equal(t, 30, e.CollectionIntervalMin)
	// T257 2.6：默认评估器不再携带波动阈值（规则已摘除，字段仅为冻结契约保留）
	assert.Zero(t, e.FluctuationThresholdPct)
}

func TestSupplement_WearInterrupt_GapTriggers(t *testing.T) {
	evaluator := &engine.RuleEvaluator{WearInterruptMinutes: 60}

	base := time.Now().Truncate(time.Minute)

	prevFrame := &engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: base.Add(-90 * time.Minute),
	}
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: base,
		Wearing:   true,
	}

	result := evaluator.Evaluate(frame, prevFrame)
	require.NotNil(t, result, "90min gap should trigger wear_interrupt")
	assert.Equal(t, engine.TypeWearInterrupt, result.AlertType)
	assert.True(t, result.ShouldAlert)
	assert.Equal(t, 60.0, result.ThresholdValue)
	assert.InDelta(t, 90.0, result.ActualValue, 0.1)
}

func TestSupplement_WearInterrupt_BoundaryNotTrigger(t *testing.T) {
	evaluator := &engine.RuleEvaluator{WearInterruptMinutes: 60}

	base := time.Now().Truncate(time.Minute)

	prevFrame := &engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: base.Add(-60 * time.Minute),
	}
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: base,
		Wearing:   true,
	}

	// 恰好 60min（=阈值）不触发，与其他规则一致采用严格大于
	result := evaluator.Evaluate(frame, prevFrame)
	assert.Nil(t, result, "exactly 60min gap should NOT trigger")
}

func TestSupplement_Dedup_SuppressWithinWindow(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
		DedupWindowMinutes:    30,
	}

	pressures := [20]float64{}
	pressures[2] = 47.2
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   true,
	}

	first := evaluator.Evaluate(frame, nil)
	require.NotNil(t, first, "first alert should fire")
	assert.Equal(t, engine.TypePressureHigh, first.AlertType)

	// 10 分钟后同设备同类型再次超阈值 → 窗口内抑制
	frame.Timestamp = frame.Timestamp.Add(10 * time.Minute)
	second := evaluator.Evaluate(frame, nil)
	assert.Nil(t, second, "same device+type within 30min window should be deduplicated")
}

func TestSupplement_Dedup_AllowAfterWindowExpired(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
		DedupWindowMinutes:    30,
	}

	pressures := [20]float64{}
	pressures[2] = 47.2
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   true,
	}

	require.NotNil(t, evaluator.Evaluate(frame, nil))

	// 31 分钟后窗口已过 → 允许新告警（A7-Edge 语义）
	frame.Timestamp = frame.Timestamp.Add(31 * time.Minute)
	result := evaluator.Evaluate(frame, nil)
	require.NotNil(t, result, "alert after dedup window expired should fire")
	assert.Equal(t, engine.TypePressureHigh, result.AlertType)
}

func TestSupplement_Dedup_DifferentTypesNotSuppressed(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
		SensorDriftThreshold:  2.8,
		DedupWindowMinutes:    30,
	}

	highPressures := [20]float64{}
	highPressures[2] = 47.2
	highFrame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: time.Now(),
		Pressures: highPressures,
		Wearing:   true,
	}
	require.NotNil(t, evaluator.Evaluate(highFrame, nil))

	// 同设备不同类型（空载漂移）不受 pressure_high 去重影响
	driftPressures := [20]float64{}
	driftPressures[7] = 3.5
	driftFrame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: time.Now().Add(5 * time.Minute),
		Pressures: driftPressures,
		Wearing:   false,
	}
	result := evaluator.Evaluate(driftFrame, nil)
	require.NotNil(t, result, "different alert type should not be deduplicated")
	assert.Equal(t, engine.TypeSensorDrift, result.AlertType)
}

func TestSupplement_EvaluateAll_AggregatesMultipleHits(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
		SensorDriftThreshold:  2.8,
		DedupWindowMinutes:    30,
	}

	prevPressures := [20]float64{}
	prevPressures[2] = 20.0
	prevFrame := &engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: prevPressures,
		Timestamp: time.Now().Add(-30 * time.Minute),
		Wearing:   true,
	}

	// P03: 20→50（偏高；波动规则已摘除故不再有第二条）；P08 空载 3.5（漂移）
	pressures := [20]float64{}
	pressures[2] = 50.0
	pressures[7] = 3.5
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: pressures,
		Timestamp: time.Now(),
		Wearing:   false,
	}

	results := evaluator.EvaluateAll(frame, prevFrame)
	require.Len(t, results, 2, "high + drift 命中；波动已停产生 ⇒ 3 条变 2 条")

	types := make([]engine.AlertType, 0, len(results))
	for _, r := range results {
		assert.True(t, r.ShouldAlert)
		types = append(types, r.AlertType)
	}
	assert.Equal(t,
		[]engine.AlertType{engine.TypePressureHigh, engine.TypeSensorDrift},
		types, "EvaluateAll keeps rule priority order")

	// Evaluate 返回优先级最高的命中（新评估器避免去重窗口干扰）
	fresh := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
		SensorDriftThreshold:  2.8,
	}
	first := fresh.Evaluate(frame, prevFrame)
	require.NotNil(t, first)
	assert.Equal(t, engine.TypePressureHigh, first.AlertType)
}

// TestSupplement_PressureFluctuation_NotProducedAnymore T257 2.6 回归门禁：
// 即使相邻帧变化率远超 30%，引擎也不再产生 pressure_fluctuation（方案A 四类）。
// 原 3 条 Fluctuation_* 用例钉的是「除零跳过」等实现细节，规则摘除后一并删除。
func TestSupplement_PressureFluctuation_NotProducedAnymore(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold:   99, // 排除 pressure_high 干扰
		FluctuationThresholdPct: 30, // 故意仍设阈值：证明「有阈值也不产生」
	}

	prevPressures := [20]float64{}
	prevPressures[5] = 20.0
	prevFrame := &engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: prevPressures,
		Timestamp: time.Now().Add(-30 * time.Minute),
		Wearing:   true,
	}
	currPressures := [20]float64{}
	currPressures[5] = 28.0 // 20→28 = +40%，旧规则必触发
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: currPressures,
		Timestamp: time.Now(),
		Wearing:   true,
	}

	assert.Empty(t, evaluator.EvaluateAll(frame, prevFrame), "波动规则已摘除")
	assert.Nil(t, evaluator.Evaluate(frame, prevFrame))
}

// ─────────────────────────────────────────────────────────────
// T257 2.6 EvaluateWearDurationShort（佩戴时长不足，按自然日）
// ─────────────────────────────────────────────────────────────

func TestSupplement_WearDurationShort_TriggerAndBoundary(t *testing.T) {
	evaluator := engine.NewDefaultRuleEvaluator()
	bizDay := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)

	// 设计稿样例：阈值 18h / 实际 6.5h ⇒ 触发
	res := evaluator.EvaluateWearDurationShort("P001", bizDay, 6.5*60, 18)
	require.NotNil(t, res, "6.5h < 18h 应触发")
	assert.Equal(t, engine.TypeWearDurationShort, res.AlertType)
	assert.True(t, res.ShouldAlert)
	assert.InDelta(t, 18*60, res.ThresholdValue, 0.001, "阈值以分钟口径落库")
	assert.InDelta(t, 6.5*60, res.ActualValue, 0.001)
	assert.Contains(t, res.Message, "2026-09-19", "detail 要写明业务日，否则事后无法核对")
	assert.Contains(t, res.Message, "6.5 小时")

	// 边界：恰好等于目标不触发（其他规则同为严格大于/小于口径）
	assert.Nil(t, evaluator.EvaluateWearDurationShort("P001", bizDay, 18*60, 18))
	// 达标
	assert.Nil(t, evaluator.EvaluateWearDurationShort("P001", bizDay, 19*60, 18))
	// 阈值 0 = 规则未启用（与全引擎「零值即关闭」口径一致）
	assert.Nil(t, evaluator.EvaluateWearDurationShort("P001", bizDay, 0, 0))
	// 无当日统计行（-1）按不足处理
	below := evaluator.EvaluateWearDurationShort("P001", bizDay, -1, 18)
	require.NotNil(t, below, "整日无上报记录也应视为时长不足")
	assert.InDelta(t, -1, below.ActualValue, 0.001)
}

func TestSupplement_EvaluateAll_BackfillReturnsEmpty(t *testing.T) {
	evaluator := engine.NewDefaultRuleEvaluator()

	pressures := [20]float64{}
	pressures[2] = 50.0
	frame := engine.PressureFrame{
		DeviceID:   "DEV001",
		Timestamp:  time.Now().Add(-24 * time.Hour),
		Pressures:  pressures,
		IsBackfill: true,
	}

	assert.Empty(t, evaluator.EvaluateAll(frame, nil), "backfill frames skip real-time evaluation")
	assert.Nil(t, evaluator.Evaluate(frame, nil))
}
