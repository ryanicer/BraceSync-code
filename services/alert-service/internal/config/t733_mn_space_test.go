// T733 方案A · 量纲链路门禁（alert-service 侧）
//
// 钉死三件事，防止 1000 倍量纲错配再回来：
//  1. 配置层默认与 sys_configs mN 迁移值同口径（5000 / 300 / 50），读键零换算；
//  2. 引擎按 mN 比较：346mN（=0.346N）在 5000mN 阈值下**不告警**、6000mN **告警**
//     —— 修复前阈值是 5N，346 被当成 346N，直接误报并换算出 5401kPa；
//  3. ApplyThresholds 下发到引擎后两侧同量纲（无隐藏的 ×/÷1000）。
package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/config"
	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

// TestT733_DefaultsAreMnSpace 代码默认必须与 sys_configs 的 mN 迁移值逐键同值。
func TestT733_DefaultsAreMnSpace(t *testing.T) {
	th := config.DefaultThresholds()
	assert.Equal(t, 5000.0, th.PressureHighMn, "threshold_pressure_high 迁移 5→5000（mN）")
	assert.Equal(t, 300.0, th.SensorDriftMn, "threshold_sensor_drift 迁移 0.3→300（mN）")
	assert.Equal(t, 50.0, th.WearingMn, "wearing_pressure_threshold 迁移 0.05→50（mN）")
	assert.Equal(t, 1000.0, config.MnPerN, "展示层唯一分母")
}

// TestT733_ParseThresholds_ReadsMnWithoutConversion mN 键值原样解析，禁止再出现 ÷1000。
func TestT733_ParseThresholds_ReadsMnWithoutConversion(t *testing.T) {
	th := config.ParseThresholds(map[string]string{
		config.KeyPressureHigh: "5000",
		config.KeySensorDrift:  "300",
		config.KeyWearingMn:    "50",
	})
	assert.Equal(t, 5000.0, th.PressureHighMn)
	assert.Equal(t, 300.0, th.SensorDriftMn)
	assert.Equal(t, 50.0, th.WearingMn)

	// 回写对称：ToValues 落库仍是 mN（N↔mN 换算只归 user-service 的设置端点）
	vals := th.ToValues()
	assert.Equal(t, "5000", vals[config.KeyPressureHigh])
	assert.Equal(t, "300", vals[config.KeySensorDrift])
	assert.Equal(t, "50", vals[config.KeyWearingMn])
}

// TestT733_EngineJudgesInMnSpace 现网读数 346mN 不得触发压力偏高；6000mN 必须触发。
func TestT733_EngineJudgesInMnSpace(t *testing.T) {
	eval := &engine.RuleEvaluator{}
	config.ApplyThresholds(config.DefaultThresholds(), eval)
	require.Equal(t, 5000.0, eval.PressureHighThreshold, "配置层 mN 直接落到引擎，零换算")

	frame := func(v float64) engine.PressureFrame {
		p := [20]float64{}
		p[2] = v // P03
		return engine.PressureFrame{DeviceID: "DEV-T733", PatientID: "P-T733", Pressures: p, Wearing: true}
	}

	assert.Nil(t, eval.Evaluate(frame(346), nil), "346mN（=0.346N）远低于 5000mN 上限，不得告警")
	hit := eval.Evaluate(frame(6000), nil)
	require.NotNil(t, hit, "6000mN > 5000mN 应告警")
	assert.Equal(t, engine.TypePressureHigh, hit.AlertType)
	assert.InDelta(t, 6000.0, hit.ActualValue, 1e-9, "告警值按 mN 落库（展示层才转 N）")
	assert.InDelta(t, 5000.0, hit.ThresholdValue, 1e-9)
	assert.Contains(t, hit.Message, "6000.0mN", "文案自标量纲 mN，禁止再写 N")
}
