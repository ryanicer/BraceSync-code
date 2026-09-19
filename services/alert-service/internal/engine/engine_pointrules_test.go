// Package engine — 逐采集点规则测试（T252 2.2）
//
// 钉住 admin「告警规则配置」Tab2 网格两列在引擎侧的生效语义：
//   - monitored=false ⇒ 该点不参与压力偏高判定；
//   - upperN>0 ⇒ 该点用自己的上限，未配置的点仍跟随统一上限；
//   - 未注入逐点规则（SetPointRules(nil)）⇒ 与 A1/A2 契约行为完全一致。
//
// engine_test.go（T002 Ella）仍是唯一行为契约，本文件只覆盖其未触达的新分支。
package engine_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

func pressureFrame(at map[int]float64) engine.PressureFrame {
	frame := engine.PressureFrame{
		DeviceID:  "PRS-ML05-RC-20260701001",
		PatientID: "P20260001",
		Timestamp: time.Now(),
		Wearing:   true,
	}
	for idx, v := range at {
		frame.Pressures[idx] = v
	}
	return frame
}

func newUnified45() *engine.RuleEvaluator {
	return &engine.RuleEvaluator{PressureHighThreshold: 45}
}

func TestPointRules_UnmonitoredPointSkipped(t *testing.T) {
	evaluator := newUnified45()
	evaluator.SetPointRules(map[string]engine.PointRule{
		"P03": {Monitored: false}, // 未勾选：47N 也不告警
	})
	result := evaluator.Evaluate(pressureFrame(map[int]float64{2: 47}), nil)
	assert.Nil(t, result, "取消监控的点位超阈值不应触发")

	// 同帧其余点位仍按统一上限判定
	result = evaluator.Evaluate(pressureFrame(map[int]float64{2: 47, 3: 50}), nil)
	require.NotNil(t, result)
	assert.Equal(t, "P04", result.SensorPoint)
	assert.Equal(t, 45.0, result.ThresholdValue)
}

func TestPointRules_PerPointUpperOverridesUnified(t *testing.T) {
	evaluator := newUnified45()
	evaluator.SetPointRules(map[string]engine.PointRule{
		"P01": {Monitored: true, UpperN: 20}, // 收紧
		"P02": {Monitored: true, UpperN: 60}, // 放宽
	})

	// 30N：仅 P01 的独立上限被突破
	result := evaluator.Evaluate(pressureFrame(map[int]float64{0: 30, 1: 30}), nil)
	require.NotNil(t, result)
	assert.Equal(t, engine.TypePressureHigh, result.AlertType)
	assert.Equal(t, "P01", result.SensorPoint)
	assert.Equal(t, 20.0, result.ThresholdValue, "ThresholdValue 应为该点生效值而非统一值")
	assert.Equal(t, 30.0, result.ActualValue)

	// 50N：放宽到 60 的 P02 不触发，未配置的点仍按统一 45
	result = evaluator.Evaluate(pressureFrame(map[int]float64{1: 50}), nil)
	assert.Nil(t, result)
	result = evaluator.Evaluate(pressureFrame(map[int]float64{1: 50, 4: 50}), nil)
	require.NotNil(t, result)
	assert.Equal(t, "P05", result.SensorPoint)
}

func TestPointRules_UpperZeroFollowsUnified(t *testing.T) {
	evaluator := newUnified45()
	evaluator.SetPointRules(map[string]engine.PointRule{
		"P01": {Monitored: true}, // 只改勾选、没设独立阈值
	})
	result := evaluator.Evaluate(pressureFrame(map[int]float64{0: 46}), nil)
	require.NotNil(t, result)
	assert.Equal(t, 45.0, result.ThresholdValue)
}

func TestPointRules_PicksHighestPressureAmongBreached(t *testing.T) {
	evaluator := newUnified45()
	evaluator.SetPointRules(map[string]engine.PointRule{
		"P01": {Monitored: true, UpperN: 50}, // 30N 未破
		"P02": {Monitored: true, UpperN: 10}, // 25N 已破
		"P05": {Monitored: true, UpperN: 20}, // 40N 已破（与 P02 比较时取压力更大者）
	})
	result := evaluator.Evaluate(pressureFrame(map[int]float64{0: 30, 1: 25, 4: 40}), nil)
	require.NotNil(t, result)
	assert.Equal(t, "P05", result.SensorPoint)
	assert.Equal(t, 20.0, result.ThresholdValue)
}

func TestPointRules_ClearRestoresUnifiedBehavior(t *testing.T) {
	evaluator := newUnified45()
	evaluator.SetPointRules(map[string]engine.PointRule{"P03": {Monitored: false}})
	require.Nil(t, evaluator.Evaluate(pressureFrame(map[int]float64{2: 47}), nil))

	evaluator.SetPointRules(nil)
	result := evaluator.Evaluate(pressureFrame(map[int]float64{2: 47}), nil)
	require.NotNil(t, result, "清空逐点规则后应回到 A1 契约行为")
	assert.Equal(t, "P03", result.SensorPoint)
	assert.Equal(t, 45.0, result.ThresholdValue)
}
