// Package config — alert-service 阈值配置加载/热更新（T009，A6 落地）
//
// 对齐：PRD §7D.12 系统配置（佩戴中断判定时间必须 ≥2×采集间隔，联动修改时自动校验）；
// 架构 §3.5（错误码分段：9xxxx 系统级）；test-plan §3.1 A6。
//
// 配置源：sys_configs 表（shared DB，schema/默认值见 scripts/db/migrations + seed.sql）。
//   - 读取统一入口：Manager.Current（带缓存 TTL，过期自动重读 DB = 热更新生效）；
//   - 修改入口：Manager.Update（先 ValidateThresholds 联动校验，通过才写库 + 缓存失效；
//     校验失败拒绝写入并返回 *ValidationError，错误码 90712）。
package config

import (
	"fmt"
	"strconv"
)

// sys_configs 配置键（与 scripts/db/seed/seed.sql 一致）
const (
	KeyPressureHigh    = "threshold_pressure_high"
	KeyFluctuationPct  = "threshold_pressure_fluctuation_pct"
	KeyWearInterrupt   = "threshold_wear_interrupt_minutes"
	KeySensorDrift     = "threshold_sensor_drift"
	KeyCollectInterval = "collect_interval_minutes"
)

// ErrCodeThresholdLinkage 阈值联动校验失败错误码（架构 §3.5 错误码分段：9xxxx 系统级）
const ErrCodeThresholdLinkage = 90712

// ValidationError 配置校验错误（Message 为可读拒绝原因，Code 归错误码分段）
type ValidationError struct {
	Code    int
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Thresholds 告警阈值配置快照（PRD §7D.12）
type Thresholds struct {
	PressureHighN          float64 // 压力偏高阈值 (N)，默认 45
	FluctuationPct         float64 // 压力波动幅度阈值 (%)，默认 30
	WearInterruptMinutes   int     // 佩戴中断判定（分钟），默认 60
	SensorDriftN           float64 // 传感器漂移阈值 (N)，默认 2.8
	CollectIntervalMinutes int     // 采集间隔（分钟），默认 30
}

// DefaultThresholds PRD 默认阈值口径（与 engine.NewDefaultRuleEvaluator 一致）
func DefaultThresholds() Thresholds {
	return Thresholds{
		PressureHighN:          45,
		FluctuationPct:         30,
		WearInterruptMinutes:   60,
		SensorDriftN:           2.8,
		CollectIntervalMinutes: 30,
	}
}

// Keys 本包管理的全部 sys_configs 键
func Keys() []string {
	return []string{
		KeyPressureHigh,
		KeyFluctuationPct,
		KeyWearInterrupt,
		KeySensorDrift,
		KeyCollectInterval,
	}
}

// ValidateThresholds 阈值联动校验（纯函数；PRD §7D.12 / test-plan A6）：
// wear_interrupt 阈值 ≥ 2×采集间隔（边界恰好 =2× 通过，PRD "≥" 语义）。
func ValidateThresholds(collectIntervalMin, interruptThresholdMin int) error {
	if collectIntervalMin <= 0 {
		return &ValidationError{
			Code:    ErrCodeThresholdLinkage,
			Message: fmt.Sprintf("采集间隔必须为正数（当前 %d 分钟）", collectIntervalMin),
		}
	}
	if interruptThresholdMin <= 0 {
		return &ValidationError{
			Code:    ErrCodeThresholdLinkage,
			Message: fmt.Sprintf("佩戴中断阈值必须为正数（当前 %d 分钟）", interruptThresholdMin),
		}
	}
	if minRequired := 2 * collectIntervalMin; interruptThresholdMin < minRequired {
		return &ValidationError{
			Code: ErrCodeThresholdLinkage,
			Message: fmt.Sprintf("佩戴中断阈值 %dmin < 2×采集间隔(%dmin)=%dmin，须 ≥%dmin（PRD §7D.12 联动约束）",
				interruptThresholdMin, collectIntervalMin, minRequired, minRequired),
		}
	}
	return nil
}

// ParseThresholds 解析 sys_configs 原始键值为 Thresholds；
// 缺失键与非法值（非数值/非正）回退默认（与 data-service applyConfigValue 语义一致）。
func ParseThresholds(raw map[string]string) Thresholds {
	th := DefaultThresholds()
	if v, ok := parsePositiveFloat(raw[KeyPressureHigh]); ok {
		th.PressureHighN = v
	}
	if v, ok := parsePositiveFloat(raw[KeyFluctuationPct]); ok {
		th.FluctuationPct = v
	}
	if v, ok := parsePositiveInt(raw[KeyWearInterrupt]); ok {
		th.WearInterruptMinutes = v
	}
	if v, ok := parsePositiveFloat(raw[KeySensorDrift]); ok {
		th.SensorDriftN = v
	}
	if v, ok := parsePositiveInt(raw[KeyCollectInterval]); ok {
		th.CollectIntervalMinutes = v
	}
	return th
}

// ToValues 阈值快照反解为 sys_configs 键值（写入用）
func (t Thresholds) ToValues() map[string]string {
	return map[string]string{
		KeyPressureHigh:    strconv.FormatFloat(t.PressureHighN, 'f', -1, 64),
		KeyFluctuationPct:  strconv.FormatFloat(t.FluctuationPct, 'f', -1, 64),
		KeyWearInterrupt:   strconv.Itoa(t.WearInterruptMinutes),
		KeySensorDrift:     strconv.FormatFloat(t.SensorDriftN, 'f', -1, 64),
		KeyCollectInterval: strconv.Itoa(t.CollectIntervalMinutes),
	}
}

// ThresholdPatch 阈值修改补丁（nil = 不修改；非 nil 字段参与联动校验）
type ThresholdPatch struct {
	PressureHighN          *float64
	FluctuationPct         *float64
	WearInterruptMinutes   *int
	SensorDriftN           *float64
	CollectIntervalMinutes *int
}

// IsEmpty 补丁是否无任何修改字段
func (p ThresholdPatch) IsEmpty() bool {
	return p.PressureHighN == nil && p.FluctuationPct == nil &&
		p.WearInterruptMinutes == nil && p.SensorDriftN == nil &&
		p.CollectIntervalMinutes == nil
}

// apply 补丁合并到阈值快照（返回新快照，不改原值）
func (t Thresholds) apply(p ThresholdPatch) Thresholds {
	if p.PressureHighN != nil {
		t.PressureHighN = *p.PressureHighN
	}
	if p.FluctuationPct != nil {
		t.FluctuationPct = *p.FluctuationPct
	}
	if p.WearInterruptMinutes != nil {
		t.WearInterruptMinutes = *p.WearInterruptMinutes
	}
	if p.SensorDriftN != nil {
		t.SensorDriftN = *p.SensorDriftN
	}
	if p.CollectIntervalMinutes != nil {
		t.CollectIntervalMinutes = *p.CollectIntervalMinutes
	}
	return t
}

func parsePositiveFloat(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func parsePositiveInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}
