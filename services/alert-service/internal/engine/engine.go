// Package engine — 告警规则引擎实现（T003）
//
// 行为契约：engine_test.go（T002 Ella；T661 起按卡面口径补强断言）——各用例断言是唯一行为契约。
// 阈值口径：压力偏高 45000mN / 中断 60min / 传感器标定异常 300mN
// （T733 方案A：压力量纲统一 mN，与帧压力同量纲，引擎内零换算；
// 与 config.DefaultThresholds、seed+mN 迁移、user-service 设置默认同谱系）。
// T257 2.6（方案A）：按帧规则摘除「压力波动」，新增按自然日的「佩戴时长不足」
// （EvaluateWearDurationShort，由扫描器调用）；波动 30% 阈值仅保留配置键供历史数据解释。
package engine

import (
	"fmt"
	"math"
	"time"
)

// NewDefaultRuleEvaluator 返回带默认阈值的评估器（生产调用方使用）。
// 注意：规则字段零值 = 该规则不启用（测试契约），默认值只经由此构造函数注入。
// 压力量纲默认为 mN（T733 方案A）：45N → 45000mN、0.3N → 300mN，量值放大 1000 倍、判定行为不变。
func NewDefaultRuleEvaluator() *RuleEvaluator {
	return &RuleEvaluator{
		PressureHighThreshold: 45000,
		WearInterruptMinutes:  60,
		SensorDriftThreshold:  300, // T661：兜底默认与 config.DefaultThresholds 收口同值（T203 由 2.8N 调 0.3N；T733 迁 300mN）
		DedupWindowMinutes:    30,
		CollectionIntervalMin: 30,
		// T257 2.6：不再注入 FluctuationThresholdPct（压力波动规则已摘除）。
	}
}

// sensorPointName 采集点下标（0 起）→ 点位编号 "P01".."P20"。
func sensorPointName(index int) string {
	return fmt.Sprintf("P%02d", index+1)
}

// checkPressureHigh 压力偏高：任一采集点压力严格大于阈值（A1/A2）。
// 量纲：帧读数与阈值同为 mN，直接比较、不换算（T733 方案A）。
// 多点超阈值时报告压力最大的点（A1-Edge）。
// T252 2.2：逐点规则可关闭某点监控（monitored=false 跳过）或覆盖其上限（upperMn>0）；
// 未注入逐点规则时全部点位跟随统一上限，判定与 A1/A2 契约完全一致。
func (e *RuleEvaluator) checkPressureHigh(frame PressureFrame, _ *PressureFrame) *AlertResult {
	if e.PressureHighThreshold <= 0 {
		return nil // 阈值零值：规则未启用
	}
	maxIdx := -1
	var maxVal, maxThr float64
	for i, p := range frame.Pressures {
		thr := e.PressureHighThreshold
		if rule, ok := e.pointRule(sensorPointName(i)); ok {
			if !rule.Monitored {
				continue // 该采集点已在 admin 告警规则配置里取消勾选
			}
			if rule.UpperMn > 0 {
				thr = rule.UpperMn
			}
		}
		if p > thr && (maxIdx < 0 || p > maxVal) {
			maxIdx, maxVal, maxThr = i, p, thr
		}
	}
	if maxIdx < 0 {
		return nil
	}
	point := sensorPointName(maxIdx)
	return &AlertResult{
		ShouldAlert:    true,
		AlertType:      TypePressureHigh,
		SensorPoint:    point,
		ThresholdValue: maxThr,
		ActualValue:    maxVal,
		Severity:       "high",
		// 文案按 mN 自标量纲（maxVal/maxThr 为 mN）；换算成 N 属展示层职责，见 handler 出口。
		Message: fmt.Sprintf("压力偏高：采集点 %s 压力 %.1fmN 超阈值 %.1fmN",
			point, maxVal, maxThr),
	}
}

// checkSensorDrift 传感器标定异常：仅空载（Wearing=false）时判定（A4）。
//
// 量纲：空载读数与阈值同为 mN（阈值 = sys_configs threshold_sensor_drift，T733 迁移 0.3→300），
// 判定链路零换算。
//
// T661（Boss 物理模型，T621 调研收敛）：读侧减校准基线后，压力片应力释放会产出负值，
// 容差带内的负偏是合理物理现象而非故障。判据改为**双向对称容差**——读数绝对值严格
// 大于标定阈值（复用 threshold_sensor_drift 单键，正负同向）才告警：
//   - 正向越界 = 漂移超标；
//   - 负向越界 = 应力释放异常，提示检查佩戴/考虑重新校准。
//
// 边界（|p| 恰好等于阈值）不触发，与全引擎严格大于口径一致。
func (e *RuleEvaluator) checkSensorDrift(frame PressureFrame, _ *PressureFrame) *AlertResult {
	if e.SensorDriftThreshold <= 0 || frame.Wearing {
		return nil
	}
	maxIdx := -1
	maxAbs := 0.0
	for i, p := range frame.Pressures {
		// 对称容差：正/负双向都按绝对值比同一枚阈值（T661）。
		if a := math.Abs(p); a > e.SensorDriftThreshold && a > maxAbs {
			maxAbs = a
			maxIdx = i
		}
	}
	if maxIdx < 0 {
		return nil
	}
	point := sensorPointName(maxIdx)
	actual := frame.Pressures[maxIdx]
	message := ""
	if actual > 0 {
		message = fmt.Sprintf("传感器标定异常：空载采集点 %s 读数 %.1fmN 超阈值 %.1fmN（漂移超标），通知技师+运营",
			point, actual, e.SensorDriftThreshold)
	} else {
		message = fmt.Sprintf("传感器标定异常：空载采集点 %s 读数 %.1fmN 负向超出容差 %.1fmN（应力释放异常），建议检查佩戴或考虑重新校准，通知技师+运营",
			point, actual, e.SensorDriftThreshold)
	}
	return &AlertResult{
		ShouldAlert:    true,
		AlertType:      TypeSensorDrift,
		SensorPoint:    point,
		ThresholdValue: e.SensorDriftThreshold,
		ActualValue:    actual,
		Severity:       "medium",
		Message:        message,
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
		Message: fmt.Sprintf("设备离线：设备 %s 上报间隔 %.0f 分钟超阈值 %d 分钟",
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
		Message: fmt.Sprintf("设备离线：设备 %s 上报间隔 %.0f 分钟超阈值 %d 分钟",
			deviceID, gap.Minutes(), e.WearInterruptMinutes),
	}
}

// EvaluateWearDurationShort 佩戴时长不足判定（T257 2.6 新增第四类，定时扫描器 API）。
//
// 口径（PM 2026-09-20 裁定 Q1=方案A）：设计稿文案为「当日累计佩戴时长 < dailyWearMinHours」，
// 按**自然日**算，阈值复用 2.2 的 wear_target_hours 键（不另设）。
// 判定对象由调用方给定（扫描器传「上一个完整自然日」）——不按「今天到此刻」判，
// 否则每天 00:00–阈值时刻之间人人不达标，等于天天误报。
//
// wearMinutes < 0 = 当日无 daily_wear_stats 行（当天没有任何上报），与 0 同义按不足处理；
// targetHours <= 0 = 规则未启用（与其他规则「零值即关闭」口径一致）。
func (e *RuleEvaluator) EvaluateWearDurationShort(patientID string, bizDay time.Time,
	wearMinutes float64, targetHours float64) *AlertResult {
	if targetHours <= 0 {
		return nil
	}
	need := targetHours * 60
	if wearMinutes >= need {
		return nil // 边界（=目标）不触发，与其他规则一致采用严格大于/小于
	}
	return &AlertResult{
		ShouldAlert:    true,
		AlertType:      TypeWearDurationShort,
		ThresholdValue: need,
		ActualValue:    wearMinutes,
		Severity:       "medium",
		Message: fmt.Sprintf("佩戴时长不足：%s 于 %s 累计佩戴 %.1f 小时，低于目标 %.1f 小时",
			patientID, bizDay.Format("2006-01-02"), wearMinutes/60, targetHours),
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
