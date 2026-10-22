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
	TypePressureHigh      AlertType = "pressure_high"
	TypeWearInterrupt     AlertType = "wear_interrupt"
	TypeSensorDrift       AlertType = "sensor_drift"
	TypeWearDurationShort AlertType = "wear_duration_short"
	// TypePressureFluctuation T257 2.6（方案A）：自本卡起引擎**不再产生**该类型，
	// 常量保留是因为 DB CHECK 仍允许它（000001 起有存量行），读历史/按类型筛选要用。
	TypePressureFluctuation AlertType = "pressure_fluctuation"
)

// PressureFrame 压力采集帧
type PressureFrame struct {
	DeviceID   string
	PatientID  string
	Timestamp  time.Time
	Pressures  [20]float64 // p01-p20，单位 mN（T733 方案A：固件 1.9 上报 mN，入库/评估链路零换算）
	Battery    int
	Wearing    bool
	IsBackfill bool // 是否为补传帧
	UploadTime time.Time
}

// AlertResult 告警评估结果
//
// 量纲随告警类型而变（T733 方案A 明确）：
//   - pressure_high / sensor_drift：ThresholdValue、ActualValue 为 **mN**（与帧压力同量纲）；
//   - wear_interrupt：分钟；wear_duration_short：分钟；
//   - Message：按上述各自单位自标（压力类文案显式写 "mN"）。
//
// N 只在 HTTP 展示响应层出现（alert-service handler 出口 ÷config.MnPerN）。
type AlertResult struct {
	ShouldAlert    bool
	AlertType      AlertType
	SensorPoint    string // 触发告警的传感器点（如 "P03"）
	ThresholdValue float64
	ActualValue    float64
	Severity       string
	Message        string
}

// PointRule 单个采集点的独立规则（T252 2.2 · admin 告警规则配置 Tab2 网格的一格）。
// 目前只有压力偏高方向有消费者：Monitored=false 跳过该点，UpperMn 覆盖统一上限。
type PointRule struct {
	Monitored bool    // false = 该点不参与压力偏高判定
	UpperMn   float64 // 独立压力上限 (mN)；0 = 跟随统一上限
}

// RuleEvaluator 规则评估器。
//
// 阈值语义（由 engine_test.go 契约决定）：字段为**零值表示该规则不启用**，
// 生产调用方应通过 NewDefaultRuleEvaluator() 获取默认阈值
// （压力偏高 45000mN / 中断 60min / 传感器标定异常 300mN / 去重 30min；
// 传感器标定异常阈值 T661 起与 config.DefaultThresholds 同量纲，生产口径以 sys_configs 热刷新为准）。
//
// 单位口径（T733 方案A）：压力量纲阈值全部为 **mN**，与 PressureFrame.Pressures 同量纲，
// 引擎内零换算；换算只发生在 HTTP 展示响应层。
type RuleEvaluator struct {
	PressureHighThreshold   float64 // 压力偏高阈值 (mN)，默认 45000（=45N 占位口径 ×1000）
	FluctuationThresholdPct float64 // 已停用（T257 2.6 停产生压力波动）；字段保留：冻结契约 engine_test.go 仍按字面量构造
	WearInterruptMinutes    int     // 佩戴中断判定（分钟），默认 60
	SensorDriftThreshold    float64 // 传感器标定异常阈值 (mN)，默认 300（T661 收口 0.3N；T733 迁 mN；双向对称容差，正负读数按 |p| 比此键）
	DedupWindowMinutes      int     // 去重窗口（分钟），默认 30；<=0 表示不去重
	CollectionIntervalMin   int     // 采集间隔（分钟），默认 30

	mu         sync.Mutex
	lastAlerts map[string]time.Time // 去重窗口："deviceID|alertType" → 最近一次告警时间（帧 Timestamp）
	pointRules map[string]PointRule // 逐点独立规则（SetPointRules 注入；nil = 全点位跟随统一上限）
}

// SetPointRules 注入逐采集点规则（配置热更新路径调用；传 nil 清空 = 回退统一阈值）。
// 整图替换、永不原地改，故读取方持锁取出后即可安全使用。
func (e *RuleEvaluator) SetPointRules(rules map[string]PointRule) {
	e.mu.Lock()
	e.pointRules = rules
	e.mu.Unlock()
}

// pointRule 取指定点位独立规则；无条目 = 该点未单独配置（跟随统一上限、默认参与监控）。
func (e *RuleEvaluator) pointRule(point string) (PointRule, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.pointRules[point]
	return r, ok
}

// Evaluate 评估单帧，返回首个命中的告警结果；无命中返回 nil。
// 补传帧（IsBackfill）不参与实时告警评估（A9）。
// 规则优先级：pressure_high → sensor_drift → wear_interrupt。
// T257 2.6：压力波动规则已摘除（方案A 四类）；「佩戴时长不足」不在此按帧评估，
// 它按自然日聚合，判定见 EvaluateWearDurationShort + scanner。
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
