// Package engine — 补充测试（T003 Winner）
//
// engine_test.go（Ella T002）是唯一行为契约，禁止修改；
// 本文件仅覆盖契约用例未触达的实现分支（默认构造 / 佩戴中断触发 / 去重抑制 /
// EvaluateAll 聚合 / 补传帧短路），钉住实现行为并支撑 ≥90% 覆盖率验收。
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
	assert.Equal(t, 30.0, e.FluctuationThresholdPct)
	assert.Equal(t, 60, e.WearInterruptMinutes)
	assert.Equal(t, 2.8, e.SensorDriftThreshold)
	assert.Equal(t, 30, e.DedupWindowMinutes)
	assert.Equal(t, 30, e.CollectionIntervalMin)
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
		PressureHighThreshold:   45.0,
		FluctuationThresholdPct: 30,
		SensorDriftThreshold:    2.8,
		DedupWindowMinutes:      30,
	}

	prevPressures := [20]float64{}
	prevPressures[2] = 20.0
	prevFrame := &engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: prevPressures,
		Timestamp: time.Now().Add(-30 * time.Minute),
		Wearing:   true,
	}

	// P03: 20→50（偏高 + 波动 150%）；P08 空载 3.5（漂移）
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
	require.Len(t, results, 3, "high + fluctuation + drift should all fire")

	types := make([]engine.AlertType, 0, len(results))
	for _, r := range results {
		assert.True(t, r.ShouldAlert)
		types = append(types, r.AlertType)
	}
	assert.Equal(t,
		[]engine.AlertType{engine.TypePressureHigh, engine.TypePressureFluctuation, engine.TypeSensorDrift},
		types, "EvaluateAll keeps rule priority order")

	// Evaluate 返回优先级最高的命中（新评估器避免去重窗口干扰）
	fresh := &engine.RuleEvaluator{
		PressureHighThreshold:   45.0,
		FluctuationThresholdPct: 30,
		SensorDriftThreshold:    2.8,
	}
	first := fresh.Evaluate(frame, prevFrame)
	require.NotNil(t, first)
	assert.Equal(t, engine.TypePressureHigh, first.AlertType)
}

// TestSupplement_Fluctuation_PrevZeroSkips 验证遗留点2：prev=0→curr>0 波动突变跳过。
// 当上一帧某采集点读数为 0 时，变化率不可计算（除零），引擎按 skip 处理。
// 此用例钉住 skip 行为，避免将来误改为触发。
func TestSupplement_Fluctuation_PrevZeroSkips(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		FluctuationThresholdPct: 30,
		PressureHighThreshold:   99, // 设得极高，排除 pressure_high 干扰
	}

	prevPressures := [20]float64{}
	prevPressures[2] = 0.0
	prevFrame := &engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: prevPressures,
		Timestamp: time.Now().Add(-30 * time.Minute),
		Wearing:   true,
	}

	// P03: prev=0 → curr=30 (理论上 +∞%，但除零不可算)
	currPressures := [20]float64{}
	currPressures[2] = 30.0
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: currPressures,
		Timestamp: time.Now(),
		Wearing:   true,
	}

	result := evaluator.Evaluate(frame, prevFrame)
	assert.Nil(t, result, "prev=0 → curr=30: fluctuation check should skip (division by zero), no alert expected")
}

// TestSupplement_Fluctuation_PrevZeroButPressureHighStillFires 验证：
// prev=0 跳过波动检测，但不影响其他规则（如 pressure_high 仍可独立触发）。
func TestSupplement_Fluctuation_PrevZeroButPressureHighStillFires(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		FluctuationThresholdPct: 30,
		PressureHighThreshold:   45,
	}

	prevPressures := [20]float64{}
	prevFrame := &engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: prevPressures, // 全部为 0
		Timestamp: time.Now().Add(-30 * time.Minute),
		Wearing:   true,
	}

	// P03: prev=0 → curr=50 (>45N threshold)
	currPressures := [20]float64{}
	currPressures[2] = 50.0
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: currPressures,
		Timestamp: time.Now(),
		Wearing:   true,
	}

	result := evaluator.Evaluate(frame, prevFrame)
	require.NotNil(t, result, "prev=0→curr=50 > 45N: pressure_high should fire independently")
	assert.Equal(t, engine.TypePressureHigh, result.AlertType,
		"prev=0 skips fluctuation but pressure_high is unaffected")
}

// TestSupplement_Fluctuation_MixedPrevZeros 验证多采集点混合场景：
// 部分点 prev=0（跳过），部分点 prev>0（正常参与波动计算）。
func TestSupplement_Fluctuation_MixedPrevZeros(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		FluctuationThresholdPct: 30,
		PressureHighThreshold:   99, // 排除 pressure_high
	}

	prevPressures := [20]float64{}
	prevPressures[2] = 0.0  // P03: prev=0, 应跳过
	prevPressures[5] = 20.0 // P06: prev=20, 正常参与
	prevFrame := &engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: prevPressures,
		Timestamp: time.Now().Add(-30 * time.Minute),
		Wearing:   true,
	}

	// P03: 0→30 (跳过), P06: 20→15 (变化25% < 30%, 不触发)
	currPressures := [20]float64{}
	currPressures[2] = 30.0
	currPressures[5] = 15.0
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: currPressures,
		Timestamp: time.Now(),
		Wearing:   true,
	}

	// P03 被跳过，P06 变化 25% 不超阈值 → 整体不触发
	result := evaluator.Evaluate(frame, prevFrame)
	assert.Nil(t, result,
		"mixed prev=0/prev>0: only non-zero prev points participate; 25% < 30% threshold, no alert")

	// 反之：P06 变化 40% > 30% → 应触发
	currPressures[5] = 28.0 // 20→28 = +40%
	frame2 := engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: currPressures,
		Timestamp: time.Now(),
		Wearing:   true,
	}
	result2 := evaluator.Evaluate(frame2, prevFrame)
	require.NotNil(t, result2, "P06 20→28 (+40%) exceeds 30% → should fire fluctuation")
	assert.Equal(t, engine.TypePressureFluctuation, result2.AlertType)
	assert.Equal(t, "P06", result2.SensorPoint)
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
