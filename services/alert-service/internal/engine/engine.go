// Package engine — 告警规则引擎实现（T003）
//
// 行为契约：engine_test.go（T002 Ella，禁止修改）——各用例断言是唯一行为契约。
// 阈值口径：压力偏高 45N / 波动 30% / 中断 60min / 漂移 2.8N（架构 §7D.12 / 协议 §4.1）。
package engine

import (
	"fmt"
	"math"
	"time"
)

// NewDefaultRuleEvaluator 返回带默认阈值的评估器（生产调用方使用）。
// 注意：规则字段零值 = 该规则不启用（测试契约），默认值只经由此构造函数注入。
func NewDefaultRuleEvaluator() *RuleEvaluator {
	return &RuleEvaluator{
		PressureHighThreshold:   45,
		FluctuationThresholdPct: 30,
		WearInterruptMinutes:    60,
		SensorDriftThreshold:    2.8,
		DedupWindowMinutes:      30,
		CollectionIntervalMin:   30,
	}
}

// sensorPointName 采集点下标（0 起）→ 点位编号 "P01".."P20"。
func sensorPointName(index int) string {
	return fmt.Sprintf("P%02d", index+1)
}

// checkPressureHigh 压力偏高：任一采集点压力严格大于阈值（A1/A2）。
// 多点超阈值时报告压力最大的点（A1-Edge）。
func (e *RuleEvaluator) checkPressureHigh(frame PressureFrame, _ *PressureFrame) *AlertResult {
	if e.PressureHighThreshold <= 0 {
		return nil // 阈值零值：规则未启用
	}
	maxIdx := -1
	for i, p := range frame.Pressures {
		if p > e.PressureHighThreshold && (maxIdx < 0 || p > frame.Pressures[maxIdx]) {
			maxIdx = i
		}
	}
	if maxIdx < 0 {
		return nil
	}
	point := sensorPointName(maxIdx)
	actual := frame.Pressures[maxIdx]
	return &AlertResult{
		ShouldAlert:    true,
		AlertType:      TypePressureHigh,
		SensorPoint:    point,
		ThresholdValue: e.PressureHighThreshold,
		ActualValue:    actual,
		Severity:       "high",
		Message: fmt.Sprintf("压力偏高：采集点 %s 压力 %.1fN 超阈值 %.1fN",
			point, actual, e.PressureHighThreshold),
	}
}

// checkPressureFluctuation 压力波动：与上一帧同点压力偏差严格大于阈值百分比（A3）。
// 无前一帧不评估（A3-NoPrev）；上一帧读数为 0 时无法计算变化率，跳过该点。
func (e *RuleEvaluator) checkPressureFluctuation(frame PressureFrame, prevFrame *PressureFrame) *AlertResult {
	if e.FluctuationThresholdPct <= 0 || prevFrame == nil {
		return nil
	}
	maxIdx := -1
	maxPct := 0.0
	for i := range frame.Pressures {
		prev := prevFrame.Pressures[i]
		if prev <= 0 {
			continue // T005(Ella) 已闭环：prev=0 → 变化率除零不可算，跳过为正确行为。用例见 engine_supplement_test.go TestSupplement_Fluctuation_PrevZero*
		}
		pct := math.Abs(frame.Pressures[i]-prev) / prev * 100
		if pct > e.FluctuationThresholdPct && pct > maxPct {
			maxPct = pct
			maxIdx = i
		}
	}
	if maxIdx < 0 {
		return nil
	}
	point := sensorPointName(maxIdx)
	return &AlertResult{
		ShouldAlert:    true,
		AlertType:      TypePressureFluctuation,
		SensorPoint:    point,
		ThresholdValue: e.FluctuationThresholdPct,
		ActualValue:    maxPct,
		Severity:       "medium",
		Message: fmt.Sprintf("压力波动：采集点 %s 与上一帧偏差 %.1f%% 超阈值 %.1f%%",
			point, maxPct, e.FluctuationThresholdPct),
	}
}

// checkSensorDrift 传感器漂移：仅空载（Wearing=false）时判定（A4）。
// 空载读数严格大于阈值，或出现负值读数（传感器故障），均触发。
func (e *RuleEvaluator) checkSensorDrift(frame PressureFrame, _ *PressureFrame) *AlertResult {
	if e.SensorDriftThreshold <= 0 || frame.Wearing {
		return nil
	}
	maxIdx := -1
	maxAbs := 0.0
	for i, p := range frame.Pressures {
		if (p > e.SensorDriftThreshold || p < 0) && math.Abs(p) > maxAbs {
			maxAbs = math.Abs(p)
			maxIdx = i
		}
	}
	if maxIdx < 0 {
		return nil
	}
	point := sensorPointName(maxIdx)
	actual := frame.Pressures[maxIdx]
	return &AlertResult{
		ShouldAlert:    true,
		AlertType:      TypeSensorDrift,
		SensorPoint:    point,
		ThresholdValue: e.SensorDriftThreshold,
		ActualValue:    actual,
		Severity:       "medium",
		Message: fmt.Sprintf("传感器漂移：空载采集点 %s 读数 %.1fN 异常（阈值 %.1fN），通知技师+运营",
			point, actual, e.SensorDriftThreshold),
	}
}

// checkWearInterrupt 佩戴中断：相邻两帧间隔严格大于中断窗口。
// 说明：A5 的">60min 无上报"由定时扫描 lastseen 触发（不经 Evaluate），
// 扫描器使用 EvaluateWearInterrupt（T008 落地，判定语义与本函数一致）；
// 本函数保留为上报链路相邻帧间隔近似（T003/T005 已闭环）。
func (e *RuleEvaluator) checkWearInterrupt(frame PressureFrame, prevFrame *PressureFrame) *AlertResult {
	if e.WearInterruptMinutes <= 0 || prevFrame == nil {
		return nil
	}
	gap := frame.Timestamp.Sub(prevFrame.Timestamp)
	if gap <= time.Duration(e.WearInterruptMinutes)*time.Minute {
		return nil // 边界（=阈值）不触发，与其他规则一致采用严格大于
	}
	return &AlertResult{
		ShouldAlert:    true,
		AlertType:      TypeWearInterrupt,
		ThresholdValue: float64(e.WearInterruptMinutes),
		ActualValue:    gap.Minutes(),
		Severity:       "high",
		Message: fmt.Sprintf("佩戴中断：设备 %s 上报间隔 %.0f 分钟超阈值 %d 分钟",
			frame.DeviceID, gap.Minutes(), e.WearInterruptMinutes),
	}
}

// EvaluateWearInterrupt 佩戴中断判定（定时扫描器 API，T008）。
// 扫描器以 Redis lastseen 为触发源：now 与 lastSeen 间隔严格大于中断窗口则命中。
// 判定语义与 checkWearInterrupt 完全一致（严格大于；零值阈值=规则未启用）。
func (e *RuleEvaluator) EvaluateWearInterrupt(deviceID string, lastSeen, now time.Time) *AlertResult {
	if e.WearInterruptMinutes <= 0 {
		return nil // 阈值零值：规则未启用
	}
	gap := now.Sub(lastSeen)
	if gap <= time.Duration(e.WearInterruptMinutes)*time.Minute {
		return nil // 边界（=阈值）不触发，与其他规则一致采用严格大于
	}
	return &AlertResult{
		ShouldAlert:    true,
		AlertType:      TypeWearInterrupt,
		ThresholdValue: float64(e.WearInterruptMinutes),
		ActualValue:    gap.Minutes(),
		Severity:       "high",
		Message: fmt.Sprintf("佩戴中断：设备 %s 上报间隔 %.0f 分钟超阈值 %d 分钟",
			deviceID, gap.Minutes(), e.WearInterruptMinutes),
	}
}

// applyDedup 去重窗口：同设备同类型告警在 DedupWindowMinutes 内不重复产生（A7）。
// DedupWindowMinutes <= 0 时不去重；去重状态为评估器内存态，跨实例持久化由上层仓储负责。
func (e *RuleEvaluator) applyDedup(frame PressureFrame, hits []*AlertResult) []*AlertResult {
	if len(hits) == 0 || e.DedupWindowMinutes <= 0 {
		return hits
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lastAlerts == nil {
		e.lastAlerts = make(map[string]time.Time)
	}
	window := time.Duration(e.DedupWindowMinutes) * time.Minute
	kept := make([]*AlertResult, 0, len(hits))
	for _, hit := range hits {
		key := frame.DeviceID + "|" + string(hit.AlertType)
		if last, ok := e.lastAlerts[key]; ok && frame.Timestamp.Sub(last) < window {
			continue // 窗口内已有同设备同类型告警，抑制
		}
		e.lastAlerts[key] = frame.Timestamp
		kept = append(kept, hit)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
