// Package engine — 告警规则引擎测试用例（测试先行 / TDD）
//
// 沿革：
//
//	本文件包含告警规则引擎的全部测试用例（src: docs/ §3.1 A1-A10）。
//	T002 阶段用例允许红（Evaluate 为桩），T003 实现后转绿；T257 2.6 摘除压力波动规则，
//	A3 三条翻为「不再产生波动」门禁；T661 把原 if result != nil 弱断言全部补成实牙，
//	KNOWN_RED 标记全数清除（A10 降级队列不属 engine 行为，改 t.Skip 并指路实现位置）。
//
// 覆盖规则类型：
//   - 压力偏高 (pressure_high)
//   - 压力波动 (pressure_fluctuation)（T257 2.6 起引擎不再产生，本组钉回归）
//   - 佩戴中断 (wear_interrupt)
//   - 传感器标定异常 (sensor_drift)
package engine_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// A1: 压力偏高 >45N 触发告警
// ============================================================
func TestA1_PressureHigh_OverThreshold(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold:   45.0,
		FluctuationThresholdPct: 30,
		WearInterruptMinutes:    60,
		SensorDriftThreshold:    2.8,
		DedupWindowMinutes:      30,
		CollectionIntervalMin:   30,
	}

	// 某点压力 47.2N > 45N 阈值 → 应触发 pressure_high
	pressures := [20]float64{}
	pressures[2] = 47.2 // P03
	frame := engine.PressureFrame{
		DeviceID:  "PRS-ML05-RC-20260701001",
		PatientID: "P20260001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   true,
	}

	result := evaluator.Evaluate(frame, nil)

	// T661：T002 桩期的弱断言补成实牙（T003 早已实现，此处钉死完整契约）
	require.NotNil(t, result, "should produce an alert result")
	assert.Equal(t, engine.TypePressureHigh, result.AlertType)
	assert.Equal(t, "P03", result.SensorPoint)
	assert.True(t, result.ShouldAlert)
	assert.Equal(t, "压力偏高：采集点 P03 压力 47.2N 超阈值 45.0N", result.Message)
}

// A1-Boundary: 临界值 45.0N（等于阈值不触发——严格大于）
func TestA2_PressureHigh_AtBoundary(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold:   45.0,
		FluctuationThresholdPct: 30,
	}

	pressures := [20]float64{}
	pressures[2] = 45.0 // 恰好等于阈值
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		PatientID: "P001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   true,
	}

	result := evaluator.Evaluate(frame, nil)

	assert.Nil(t, result, "exactly 45.0N should NOT trigger (> 严格大于)")
}

// A1-Boundary: 压力 44.9N（低于阈值不触发）
func TestPressureHigh_BelowThreshold(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
	}

	pressures := [20]float64{}
	pressures[2] = 44.9
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		PatientID: "P001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   true,
	}

	result := evaluator.Evaluate(frame, nil)

	assert.Nil(t, result, "below-threshold 44.9N should NOT trigger")
}

// A1-Edge: 多传感器点同时超阈值（取最大值）
func TestPressureHigh_MultiplePointsOverThreshold(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
	}

	pressures := [20]float64{}
	pressures[2] = 47.2  // P03
	pressures[5] = 50.1  // P06（最高）
	pressures[15] = 46.0 // P16
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		PatientID: "P001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   true,
	}

	result := evaluator.Evaluate(frame, nil)

	require.NotNil(t, result, "multiple points over threshold should still produce one result")
	assert.Equal(t, engine.TypePressureHigh, result.AlertType)
	assert.Equal(t, "P06", result.SensorPoint, "should report the point with highest pressure")
}

// ============================================================
// A3: 压力波动 — T257 2.6（方案A）起规则已从引擎摘除，本组翻为「不再产生」回归门禁。
// ============================================================
func TestA3_PressureFluctuation_OverThreshold(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold:   45.0,
		FluctuationThresholdPct: 30,
	}

	// 上一帧
	prevPressures := [20]float64{}
	prevPressures[2] = 20.0
	prevFrame := &engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: prevPressures,
		Timestamp: time.Now().Add(-30 * time.Minute),
	}

	// 当前帧：P03 从 20N 跳到 30N（+50%）→ 应触发 fluctuation
	currPressures := [20]float64{}
	currPressures[2] = 30.0
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Pressures: currPressures,
		Timestamp: time.Now(),
	}

	result := evaluator.Evaluate(frame, prevFrame)

	// T257 2.6：+50% 跳变也不再产生 pressure_fluctuation（规则已摘除）；
	// 30N 低于压力偏高阈值 45N，故整帧无命中。
	assert.Nil(t, result)
}

// A3-Boundary: 变化刚好 30%（非严格大于，边界行为）
func TestA3_PressureFluctuation_AtBoundary(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		FluctuationThresholdPct: 30,
	}

	// 上一帧 P03 = 20N
	prevPressures := [20]float64{}
	prevPressures[2] = 20.0
	prevFrame := &engine.PressureFrame{DeviceID: "DEV001", Pressures: prevPressures, Timestamp: time.Now().Add(-30 * time.Minute)}

	// 当前帧 P03 = 26N（= 20 * 1.3，刚好 30% 变化）
	currPressures := [20]float64{}
	currPressures[2] = 26.0
	frame := engine.PressureFrame{DeviceID: "DEV001", Pressures: currPressures, Timestamp: time.Now()}

	result := evaluator.Evaluate(frame, prevFrame)

	// T257 2.6：规则已摘除，30% 边界帧同样无任何命中
	assert.Nil(t, result)
}

// A3-NoPrev: 无前一帧时不评估波动
func TestPressureFluctuation_NoPreviousFrame(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		FluctuationThresholdPct: 30,
	}

	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		PatientID: "P001",
		Timestamp: time.Now(),
		Pressures: [20]float64{},
	}

	result := evaluator.Evaluate(frame, nil) // prevFrame = nil

	// T257 2.6：无前一帧且本帧空载零读数（其余规则均未启用）→ 无命中
	assert.Nil(t, result)
}

// ============================================================
// A4: 传感器标定异常 — 空载读数 |p| 严格大于阈值（T661 起双向对称容差）
// 本组用显式 2.8N 构造，钉「判定形状与阈值取值无关」；生产默认 0.3N 另见 t661/t625 组。
// ============================================================
func TestA4_SensorDrift_OverThreshold(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		SensorDriftThreshold: 2.8,
	}

	// 空载时某点读数为 3.5N（>2.8N）→ 应触发 sensor_drift
	pressures := [20]float64{}
	pressures[7] = 3.5 // P08
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		PatientID: "P001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   false, // 空载
	}

	result := evaluator.Evaluate(frame, nil)

	// T661：正向越界文案钉死（漂移超标；前缀仍是「传感器标定异常：」）
	require.NotNil(t, result)
	assert.True(t, result.ShouldAlert)
	assert.Equal(t, engine.TypeSensorDrift, result.AlertType)
	assert.Equal(t, "P08", result.SensorPoint)
	assert.InDelta(t, 3.5, result.ActualValue, 0.001)
	assert.InDelta(t, 2.8, result.ThresholdValue, 0.001)
	assert.Equal(t, "medium", result.Severity)
	assert.Equal(t, "传感器标定异常：空载采集点 P08 读数 3.5N 超阈值 2.8N（漂移超标），通知技师+运营",
		result.Message)
}

// A4-Boundary: 空载时偏移刚好 2.8N
func TestSensorDrift_AtBoundary(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		SensorDriftThreshold: 2.8,
	}

	pressures := [20]float64{}
	pressures[7] = 2.8 // 恰好等于阈值
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   false,
	}

	result := evaluator.Evaluate(frame, nil)

	assert.Nil(t, result, "exactly 2.8N boundary should NOT trigger (|p| > 阈值 严格大于)")
}

// A4-Negative: 负向越界（应力释放异常）——T661 双向对称容差口径。
// 用生产阈值 0.3N：-1.0N 幅值越界，必须告警且走负向文案。
func TestSensorDrift_NegativeSensorValue(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		SensorDriftThreshold: 0.3,
	}

	pressures := [20]float64{}
	pressures[7] = -1.0 // 应力释放导致的大幅负偏（超容差，需重新校准）
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   false,
	}

	result := evaluator.Evaluate(frame, nil)

	require.NotNil(t, result, "负向幅值超容差必须告警")
	assert.True(t, result.ShouldAlert)
	assert.Equal(t, engine.TypeSensorDrift, result.AlertType)
	assert.Equal(t, "P08", result.SensorPoint)
	assert.InDelta(t, -1.0, result.ActualValue, 0.001)
	assert.InDelta(t, 0.3, result.ThresholdValue, 0.001)
	assert.Contains(t, result.Message, "应力释放异常", "负向文案须区分物理含义，实测：%q", result.Message)
	assert.Contains(t, result.Message, "建议检查佩戴或考虑重新校准")
	assert.True(t, strings.HasPrefix(result.Message, "传感器标定异常："), "前缀门禁（T419）不破")
}

// A4-Negative-WithinTolerance: 容差带内负偏不告警（Boss 物理模型：应力释放的正常负偏）。
// 现网分布（T661 前置 SELECT）：36 条负值样本中 34 条 |p| ≤ 0.2536N，0.3N 容差下止报。
func TestSensorDrift_SmallNegativeWithinTolerance(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		SensorDriftThreshold: 0.3,
	}

	for _, v := range []float64{-0.1, -0.2536} {
		pressures := [20]float64{}
		pressures[7] = v
		frame := engine.PressureFrame{
			DeviceID:  "DEV001",
			Timestamp: time.Now(),
			Pressures: pressures,
			Wearing:   false,
		}
		assert.Nil(t, evaluator.Evaluate(frame, nil),
			"容差带内负偏 %.4fN 是合理物理现象，不得告警", v)
	}
}

// A4-Negative-Boundary: -0.3 恰在边界（|p| == 阈值）不触发，与正向边界同口径。
func TestSensorDrift_NegativeAtBoundary(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		SensorDriftThreshold: 0.3,
	}

	pressures := [20]float64{}
	pressures[7] = -0.3
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   false,
	}

	assert.Nil(t, evaluator.Evaluate(frame, nil))
}

// ============================================================
// A5: 佩戴中断 — >60min 无上报
// T008 转绿：定时扫描 lastseen 场景由 engine.EvaluateWearInterrupt 承载判定
// （扫描器 internal/scanner 只做触发源；DB 全链路见 internal/repo/integration_test.go）
// ============================================================
func TestA5_WearInterrupt_BeyondThreshold(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		WearInterruptMinutes: 60,
	}

	// 最后上报时间 61 分钟前 → 扫描判定应触发 wear_interrupt
	now := time.Now()
	lastReportTime := now.Add(-61 * time.Minute)

	result := evaluator.EvaluateWearInterrupt("DEV001", lastReportTime, now)

	require.NotNil(t, result, ">60min 无上报应触发 wear_interrupt")
	assert.True(t, result.ShouldAlert)
	assert.Equal(t, engine.TypeWearInterrupt, result.AlertType)
	assert.Equal(t, 60.0, result.ThresholdValue)
	assert.InDelta(t, 61.0, result.ActualValue, 0.1)
}

// A5-Edge: 刚好 60min 边界 → 不触发（严格大于，T003 已明确口径）
func TestWearInterrupt_AtBoundary(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		WearInterruptMinutes: 60,
	}

	now := time.Now()
	lastReportTime := now.Add(-60 * time.Minute)

	result := evaluator.EvaluateWearInterrupt("DEV001", lastReportTime, now)

	assert.Nil(t, result, "恰好 60min 边界不应触发（> 严格大于）")
}

// A6: 采集间隔改 40min 时中断阈值须 ≥80min（阈值联动校验）
func TestA6_WearInterrupt_ThresholdLinkage(t *testing.T) {
	collectionInterval := 40 // 分钟
	requiredMinInterrupt := 2 * collectionInterval

	assert.Equal(t, 80, requiredMinInterrupt,
		"wear interrupt threshold must be >= 2× collection interval")

	// T661：T002 期的占位假断言删除。60 < 80 这格的拒绝动作不属 engine，
	// 由 config.ValidateThresholds 承载（纯函数，config 包有专测钉 90712 拒因）。
	assert.False(t, 60 >= requiredMinInterrupt, "60min 不满足 2×40min，应由配置校验层拒绝")
	assert.True(t, 80 >= requiredMinInterrupt, "恰好 2× 边界放行（PRD ≥ 语义）")
}

// ============================================================
// A7: 去重窗口 — 同类型同设备不重复告警
// ============================================================
func TestA7_DedupWindow(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
		DedupWindowMinutes:    30,
	}

	// 设备 30 分钟内已有同类型告警 → 不应再生成
	pressures := [20]float64{}
	pressures[2] = 47.2
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: time.Now(),
		Pressures: pressures,
	}

	first := evaluator.Evaluate(frame, nil)
	require.NotNil(t, first, "首次超阈值应产生告警")

	// 10 分钟后同设备同类型再次超阈值 → 30min 窗口内抑制
	frame.Timestamp = frame.Timestamp.Add(10 * time.Minute)
	second := evaluator.Evaluate(frame, nil)
	assert.Nil(t, second, "same type+device within 30min window should be deduplicated")
}

// A7-Edge: 去重窗口刚好过期（应触发新告警）
func TestDedupWindow_Expired(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
		DedupWindowMinutes:    30,
	}

	// 模拟：上一次同类型告警在 31 分钟前 → 去重窗口已过 → 应允许新告警
	pressures := [20]float64{}
	pressures[2] = 47.2
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		Timestamp: time.Now(),
		Pressures: pressures,
	}

	first := evaluator.Evaluate(frame, nil)
	require.NotNil(t, first, "首次超阈值应产生告警")

	// 31 分钟后窗口已过 → 允许新告警（A7-Edge 语义）
	frame.Timestamp = frame.Timestamp.Add(31 * time.Minute)
	result := evaluator.Evaluate(frame, nil)
	require.NotNil(t, result, "dedup window expired (31min > 30min), should allow new alert")
	assert.True(t, result.ShouldAlert)
}

// ============================================================
// A8: 恢复上报 → active 中断告警自动置 resolved
// T008 转绿：扫描器检测 lastseen 新鲜（恢复上报，含补传——data-service 补传
// 同样刷新 lastseen）→ 自动 resolve。此处以 scanner + 内存 fake 走通契约；
// 真实 PG/Redis 全链路见 internal/repo/integration_test.go TestIT_A8_RecoveryAutoResolve。
// ============================================================

// a8 契约 fake：scanner 三个依赖接口的最小内存实现
type a8Devices struct {
	list     []scanner.Device
	statuses map[string]string
}

func (f *a8Devices) ListBoundDevices(context.Context) ([]scanner.Device, error) { return f.list, nil }

func (f *a8Devices) UpdateStatus(_ context.Context, deviceID, status string) (bool, error) {
	if f.statuses == nil {
		f.statuses = map[string]string{}
	}
	f.statuses[deviceID] = status
	return true, nil
}

type a8Alerts struct {
	activeInterrupt map[string]bool
	resolvedAt      map[string]time.Time
}

func (f *a8Alerts) CreateAlert(context.Context, scanner.NewAlert) (string, bool, error) {
	return "", false, nil
}

func (f *a8Alerts) HasAlertSince(context.Context, string, engine.AlertType, time.Time) (bool, error) {
	return false, nil
}

func (f *a8Alerts) HasActiveInterrupt(_ context.Context, deviceID string) (bool, error) {
	return f.activeInterrupt[deviceID], nil
}

func (f *a8Alerts) ResolveActiveInterrupts(_ context.Context, deviceID string, resolvedAt time.Time) (int64, error) {
	if !f.activeInterrupt[deviceID] {
		return 0, nil
	}
	delete(f.activeInterrupt, deviceID)
	if f.resolvedAt == nil {
		f.resolvedAt = map[string]time.Time{}
	}
	f.resolvedAt[deviceID] = resolvedAt
	return 1, nil
}

type a8LastSeen struct{ values map[string]time.Time }

func (f *a8LastSeen) GetLastSeen(_ context.Context, deviceID string) (time.Time, bool, error) {
	v, ok := f.values[deviceID]
	return v, ok, nil
}

func TestA8_ResolveOnRecovery(t *testing.T) {
	// 设备恢复上报 → 活跃的 wear_interrupt 告警应自动置 resolved
	devices := &a8Devices{list: []scanner.Device{
		{DeviceID: "DEV001", PatientID: "P001", Status: "offline"},
	}}
	alerts := &a8Alerts{activeInterrupt: map[string]bool{"DEV001": true}}
	lastseen := &a8LastSeen{values: map[string]time.Time{
		"DEV001": time.Now().Add(-5 * time.Minute), // 设备 5min 前恢复上报
	}}

	s := scanner.New(devices, alerts, lastseen, &engine.RuleEvaluator{
		WearInterruptMinutes:  60,
		CollectionIntervalMin: 30,
	})
	report, err := s.Scan(context.Background())

	require.NoError(t, err)
	assert.EqualValues(t, 1, report.Resolved, "恢复上报应自动 resolve active 中断告警")
	assert.False(t, alerts.activeInterrupt["DEV001"], "告警不再是 active")
	assert.WithinDuration(t, time.Now(), alerts.resolvedAt["DEV001"], time.Minute, "resolved_at 已写入")
	assert.Equal(t, "online", devices.statuses["DEV001"], "恢复后状态机联动回 online")
}

// ============================================================
// A9: 补传帧跳过实时告警评估
// ============================================================
func TestA9_BackfillSkip(t *testing.T) {
	evaluator := &engine.RuleEvaluator{
		PressureHighThreshold: 45.0,
	}

	pressures := [20]float64{}
	pressures[2] = 50.0 // 明显超阈值
	frame := engine.PressureFrame{
		DeviceID:   "DEV001",
		Timestamp:  time.Now().Add(-24 * time.Hour),
		Pressures:  pressures,
		IsBackfill: true, // 补传帧标记
	}

	result := evaluator.Evaluate(frame, nil)

	// A9：补传帧短路在 EvaluateAll 入口，50N 明显超阈值也不得产生实时告警
	assert.Nil(t, result, "backfill frames should not trigger real-time alerts")
}

// ============================================================
// A10: alert-service 降级 → 帧入 pending 队列
// ============================================================
func TestA10_AlertServiceDegradation(t *testing.T) {
	// T661：T002 桩期的 KNOWN_RED 占位清除。降级队列行为从来不属于 engine 包
	// （引擎只做单帧判定），最终架构落在两处并各有专测：
	//   - 入队：data-service 内联评估失败时 LPUSH alert:pending（data-service 侧）
	//   - 补偿：alert-service/internal/consumer.Consumer.DrainOnce（consumer_test 专测）
	// 故此处显式 Skip 指路，不用无断言空测假装覆盖。
	t.Skip("A10 行为属 data-service 入队 + alert-service consumer 补偿，engine 包无对应判定可测；见 consumer.Consumer.DrainOnce")
}
