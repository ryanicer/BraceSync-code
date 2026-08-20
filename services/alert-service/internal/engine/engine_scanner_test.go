// Package engine — 定时扫描器佩戴中断判定测试（T008）
//
// 对齐：docs/ §3.1 A5（>60min 无上报 → wear_interrupt）。
// EvaluateWearInterrupt 供 alert-service 定时扫描器调用（引擎 API，不经 Evaluate）。
package engine_test

import (
	"testing"
	"time"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A5：lastseen 距今 61min > 阈值 60min → 触发 wear_interrupt
func TestEvaluateWearInterrupt_BeyondThreshold(t *testing.T) {
	evaluator := &engine.RuleEvaluator{WearInterruptMinutes: 60}
	now := time.Now()

	result := evaluator.EvaluateWearInterrupt("DEV001", now.Add(-61*time.Minute), now)

	require.NotNil(t, result)
	assert.True(t, result.ShouldAlert)
	assert.Equal(t, engine.TypeWearInterrupt, result.AlertType)
	assert.Equal(t, 60.0, result.ThresholdValue)
	assert.InDelta(t, 61.0, result.ActualValue, 0.1)
	assert.Equal(t, "high", result.Severity)
	assert.Contains(t, result.Message, "DEV001")
}

// A5-Boundary：lastseen 距今恰好 60min（=阈值）→ 不触发（严格大于，与 checkWearInterrupt 一致）
func TestEvaluateWearInterrupt_AtBoundary(t *testing.T) {
	evaluator := &engine.RuleEvaluator{WearInterruptMinutes: 60}
	now := time.Now()

	result := evaluator.EvaluateWearInterrupt("DEV001", now.Add(-60*time.Minute), now)

	assert.Nil(t, result, "恰好等于阈值不应触发（严格大于）")
}

// 阈值零值 = 规则未启用（与 Evaluate 规则字段零值语义一致）
func TestEvaluateWearInterrupt_Disabled(t *testing.T) {
	evaluator := &engine.RuleEvaluator{WearInterruptMinutes: 0}
	now := time.Now()

	result := evaluator.EvaluateWearInterrupt("DEV001", now.Add(-24*time.Hour), now)

	assert.Nil(t, result, "WearInterruptMinutes<=0 时规则未启用")
}

// lastseen 新鲜（阈值窗口内）→ 不触发：扫描器据此判定"设备恢复上报"
func TestEvaluateWearInterrupt_FreshLastSeen(t *testing.T) {
	evaluator := &engine.RuleEvaluator{WearInterruptMinutes: 60}
	now := time.Now()

	result := evaluator.EvaluateWearInterrupt("DEV001", now.Add(-5*time.Minute), now)

	assert.Nil(t, result)
}
