// Package config — alert-service 阈值配置加载/热更新（T009，A6 落地）
//
// 对齐：PRD §7D.12 系统配置（佩戴中断判定时间必须 ≥2×采集间隔，联动修改时自动校验）；
// 架构 §3.5（错误码分段：9xxxx 系统级）；test-plan §3.1 A6。
//
// 配置源：sys_configs 表（shared DB，schema/默认值见 scripts/db/migrations + seed.sql）。
//   - 读取统一入口：Manager.Current（带缓存 TTL，过期自动重读 DB = 热更新生效）；
//   - 修改入口：Manager.Update（先 ValidateThresholds 联动校验，通过才写库 + 缓存失效；
//     校验失败拒绝写入并返回 *ValidationError，错误码 90712）。
//
// 单位口径（T733 方案A，Boss 裁定）：本包所有压力量纲阈值与帧压力同为 **mN**，
// 读配置→引擎判定→落库全程零换算；换算只发生在 HTTP 展示响应层（÷MnPerN）。
package config

import (
	"fmt"
	"strconv"

	"github.com/bracesync/bracesync/services/units"
)

// sys_configs 配置键（键名与 scripts/db/seed/seed.sql 一致）。
// 压力量纲键（KeyPressureHigh / KeySensorDrift / KeyWearingMn）自 T733 配套迁移起**存 mN**
// （5→5000 / 0.3→300 / 0.05→50），本包读出来即 mN，不再做任何换算
// （seed.sql 现仍为 N 口径，迁移落库前以迁移为准 —— 迁移属 data 域，不在本卡改动面）。
const (
	KeyPressureHigh    = "threshold_pressure_high"
	KeyFluctuationPct  = "threshold_pressure_fluctuation_pct"
	KeyWearInterrupt   = "threshold_wear_interrupt_minutes"
	KeySensorDrift     = "threshold_sensor_drift"
	KeyWearingMn       = "wearing_pressure_threshold"
	KeyCollectInterval = "collect_interval_minutes"
	// KeyWearTargetHours 每日佩戴目标小时数：T257 2.6「佩戴时长不足」的阈值来源。
	// 与 §7D.12 系统配置页 dailyWearTargetHours、2.2 告警规则页 dailyWearMinHours、
	// msg-service 佩戴提醒同源同键（PM 裁定不另设第三键）。
	KeyWearTargetHours = "wear_target_hours"
)

// ErrCodeThresholdLinkage 阈值联动校验失败错误码（架构 §3.5 错误码分段：9xxxx 系统级）
const ErrCodeThresholdLinkage = 90712

// MnPerN 毫牛↔牛换算分母（T733 方案A，量纲链路唯一常量）。
//
// 单位口径（Boss 裁定，T733）：固件 1.9 上报 mN，本服务的
// 存储 / 传输 / 聚合 / 告警评估 / 校准减除 空间**全部是 mN，链路上零换算**；
// N 只存在于 HTTP 展示响应层（÷MnPerN 出口换算，见 handler 包）。
// sys_configs 阈值行已由迁移统一为 mN（threshold_pressure_high 5→5000、
// threshold_sensor_drift 0.3→300、wearing_pressure_threshold 0.05→50），
// 故本包读到的键值直接就是 mN，与帧压力同量纲比较。
// 分母唯一真源在 services/units；本常量仅为兼容既有调用点保留的别名。
// 兼容别名：新代码请直接使用 units.MnPerN。
const MnPerN = units.MnPerN

// ValidationError 配置校验错误（Message 为可读拒绝原因，Code 归错误码分段）
type ValidationError struct {
	Code    int
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Thresholds 告警阈值配置快照（PRD §7D.12）
// 🔴 T733 方案A：压力量纲字段一律 **mN**（与帧压力同量纲，判定链路零换算）；
// N 只在 HTTP 展示响应层出现。配置文件（sys_configs）驱动、不硬编码，
// 远期后台运营可调、调参不改代码。
type Thresholds struct {
	PressureHighMn         float64 // 压力偏高阈值 (mN)，T203 ÷10 定 5N，T733 迁移 5→5000
	FluctuationPct         float64 // 压力波动幅度阈值 (%)，默认 30；T257 2.6 起引擎不再读取
	WearInterruptMinutes   int     // 佩戴中断判定（分钟），默认 60
	SensorDriftMn          float64 // 传感器漂移阈值 (mN)，T203 ÷10 定 0.3N，T733 迁移 0.3→300
	WearingMn              float64 // 佩戴判定阈值 (mN)，T203 ÷10 定 0.05N，T733 迁移 0.05→50
	CollectIntervalMinutes int     // 采集间隔（分钟），默认 30
	WearTargetHours        float64 // 每日佩戴目标 (h)，默认 22；T257 2.6 佩戴时长不足阈值
}

// DefaultThresholds PRD 默认阈值口径（与 engine.NewDefaultRuleEvaluator 同量纲：mN）
func DefaultThresholds() Thresholds {
	return Thresholds{
		PressureHighMn:         5000, // T203: 45 → 5；T733 迁移 5N → 5000mN
		FluctuationPct:         30,
		WearInterruptMinutes:   60,
		SensorDriftMn:          300, // T203: 2.8 → 0.3；T733 迁移 0.3N → 300mN
		WearingMn:              50,  // T203: 0.5 → 0.05；T733 迁移 0.05N → 50mN
		CollectIntervalMinutes: 30,
		WearTargetHours:        22, // 与 msg-service WEAR_TARGET_HOURS 默认、§7D.12 默认一致
	}
}

// Keys 本包管理的全部 sys_configs 键
func Keys() []string {
	return []string{
		KeyPressureHigh,
		KeyFluctuationPct,
		KeyWearInterrupt,
		KeySensorDrift,
		KeyWearingMn,
		KeyCollectInterval,
		KeyWearTargetHours,
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
			Message: fmt.Sprintf("设备离线阈值必须为正数（当前 %d 分钟）", interruptThresholdMin),
		}
	}
	if minRequired := 2 * collectIntervalMin; interruptThresholdMin < minRequired {
		return &ValidationError{
			Code: ErrCodeThresholdLinkage,
			Message: fmt.Sprintf("设备离线阈值 %dmin < 2×采集间隔(%dmin)=%dmin，须 ≥%dmin（PRD §7D.12 联动约束）",
				interruptThresholdMin, collectIntervalMin, minRequired, minRequired),
		}
	}
	return nil
}

// ParseThresholds 解析 sys_configs 原始键值为 Thresholds；
// 缺失键与非法值（非数值/非正）回退默认（与 data-service applyConfigValue 语义一致）。
// 压力量纲键值按 **mN** 原样收下，本函数不做任何单位换算（T733 方案A）。
func ParseThresholds(raw map[string]string) Thresholds {
	th := DefaultThresholds()
	if v, ok := parsePositiveFloat(raw[KeyPressureHigh]); ok {
		th.PressureHighMn = v
	}
	if v, ok := parsePositiveFloat(raw[KeyFluctuationPct]); ok {
		th.FluctuationPct = v
	}
	if v, ok := parsePositiveInt(raw[KeyWearInterrupt]); ok {
		th.WearInterruptMinutes = v
	}
	if v, ok := parsePositiveFloat(raw[KeySensorDrift]); ok {
		th.SensorDriftMn = v
	}
	if v, ok := parsePositiveFloat(raw[KeyWearingMn]); ok {
		th.WearingMn = v
	}
	if v, ok := parsePositiveInt(raw[KeyCollectInterval]); ok {
		th.CollectIntervalMinutes = v
	}
	if v, ok := parsePositiveFloat(raw[KeyWearTargetHours]); ok {
		th.WearTargetHours = v
	}
	return th
}

// ToValues 阈值快照反解为 sys_configs 键值（写入用，压力量纲按 **mN** 原样落库，零换算）。
// 🔴 不含 KeyWearTargetHours：该键写权限在 §7D.12 系统配置页（user-service），
// 本服务只读 —— 列进来会让任一内部补丁顺带覆写运营后台刚改的目标时长。
// 🔴 本服务无生产写路径（Manager.Update 仅测试调用）：面向运营的 N↔mN 换算在
// user-service 设置端点（UI 说 N，库存 mN），此处保持 mN 进出对称。
func (t Thresholds) ToValues() map[string]string {
	return map[string]string{
		KeyPressureHigh:    strconv.FormatFloat(t.PressureHighMn, 'f', -1, 64),
		KeyFluctuationPct:  strconv.FormatFloat(t.FluctuationPct, 'f', -1, 64),
		KeyWearInterrupt:   strconv.Itoa(t.WearInterruptMinutes),
		KeySensorDrift:     strconv.FormatFloat(t.SensorDriftMn, 'f', -1, 64),
		KeyWearingMn:       strconv.FormatFloat(t.WearingMn, 'f', -1, 64),
		KeyCollectInterval: strconv.Itoa(t.CollectIntervalMinutes),
	}
}

// ThresholdPatch 阈值修改补丁（nil = 不修改；非 nil 字段参与联动校验）。
// 压力量纲字段同为 **mN**（与 Thresholds 快照同量纲，补丁直接合并、不换算）。
type ThresholdPatch struct {
	PressureHighMn         *float64
	FluctuationPct         *float64
	WearInterruptMinutes   *int
	SensorDriftMn          *float64
	WearingMn              *float64
	CollectIntervalMinutes *int
}

// IsEmpty 补丁是否无任何修改字段
func (p ThresholdPatch) IsEmpty() bool {
	return p.PressureHighMn == nil && p.FluctuationPct == nil &&
		p.WearInterruptMinutes == nil && p.SensorDriftMn == nil &&
		p.WearingMn == nil && p.CollectIntervalMinutes == nil
}

// apply 补丁合并到阈值快照（返回新快照，不改原值）
func (t Thresholds) apply(p ThresholdPatch) Thresholds {
	if p.PressureHighMn != nil {
		t.PressureHighMn = *p.PressureHighMn
	}
	if p.FluctuationPct != nil {
		t.FluctuationPct = *p.FluctuationPct
	}
	if p.WearInterruptMinutes != nil {
		t.WearInterruptMinutes = *p.WearInterruptMinutes
	}
	if p.SensorDriftMn != nil {
		t.SensorDriftMn = *p.SensorDriftMn
	}
	if p.WearingMn != nil {
		t.WearingMn = *p.WearingMn
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
