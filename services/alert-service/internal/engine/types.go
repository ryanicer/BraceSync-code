// Package engine — 告警规则引擎接口定义
//
// 行为契约：engine_test.go（T002 Ella，禁止修改）；业务逻辑实现见 engine.go（T003）。
// 测试用例 src: docs/ §3.1（A1-A10）
// 目标：≥90% 分支覆盖
//
// 使用方式（测试中）：
//
//	evaluator := &RuleEvaluator{...}
//	result := evaluator.Evaluate(...)
package engine

import (
	"sync"
	"time"
)

// AlertType 告警类型
type AlertType string

const (
	TypePressureHigh        AlertType = "pressure_high"
	TypePressureFluctuation AlertType = "pressure_fluctuation"
	TypeWearInterrupt       AlertType = "wear_interrupt"
	TypeSensorDrift         AlertType = "sensor_drift"
)

// PressureFrame 压力采集帧
type PressureFrame struct {
	DeviceID   string
	PatientID  string
	Timestamp  time.Time
	Pressures  [20]float64 // p01-p20，单位 N
	Battery    int
	Wearing    bool
	IsBackfill bool // 是否为补传帧
	UploadTime time.Time
}

// AlertResult 告警评估结果
type AlertResult struct {
	ShouldAlert    bool
	AlertType      AlertType
	SensorPoint    string // 触发告警的传感器点（如 "P03"）
	ThresholdValue float64
	ActualValue    float64
	Severity       string
	Message        string
}

// RuleEvaluator 规则评估器。
//
// 阈值语义（由 engine_test.go 契约决定）：字段为**零值表示该规则不启用**，
// 生产调用方应通过 NewDefaultRuleEvaluator() 获取默认阈值
// （压力偏高 45N / 波动 30% / 中断 60min / 漂移 2.8N / 去重 30min）。
type RuleEvaluator struct {
	PressureHighThreshold   float64 // 压力偏高阈值 (N)，默认 45
	FluctuationThresholdPct float64 // 波动阈值 (%)，默认 30
	WearInterruptMinutes    int     // 佩戴中断判定（分钟），默认 60
	SensorDriftThreshold    float64 // 传感器漂移阈值 (N)，默认 2.8
	DedupWindowMinutes      int     // 去重窗口（分钟），默认 30；<=0 表示不去重
	CollectionIntervalMin   int     // 采集间隔（分钟），默认 30

	mu         sync.Mutex
	lastAlerts map[string]time.Time // 去重窗口："deviceID|alertType" → 最近一次告警时间（帧 Timestamp）
}

// Evaluate 评估单帧，返回首个命中的告警结果；无命中返回 nil。
// 补传帧（IsBackfill）不参与实时告警评估（A9）。
// 规则优先级：pressure_high → pressure_fluctuation → sensor_drift → wear_interrupt。
func (e *RuleEvaluator) Evaluate(frame PressureFrame, prevFrame *PressureFrame) *AlertResult {
	results := e.EvaluateAll(frame, prevFrame)
	if len(results) == 0 {
		return nil
	}
	return results[0]
}

// EvaluateAll 执行全部已启用规则，返回命中的告警结果列表（可为空），窗口内去重。
func (e *RuleEvaluator) EvaluateAll(frame PressureFrame, prevFrame *PressureFrame) []*AlertResult {
	if frame.IsBackfill {
		// A9：补传帧跳过实时告警评估
		return nil
	}
	rules := []func(PressureFrame, *PressureFrame) *AlertResult{
		e.checkPressureHigh,
		e.checkPressureFluctuation,
		e.checkSensorDrift,
		e.checkWearInterrupt,
	}
	var hits []*AlertResult
	for _, rule := range rules {
		if r := rule(frame, prevFrame); r != nil {
			hits = append(hits, r)
		}
	}
	return e.applyDedup(frame, hits)
}
