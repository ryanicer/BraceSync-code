// T419 G-6 后端可见层防回潮门禁 —— 告警正文 Message 前缀。
//
// 单独成文件（不改 engine_test.go）：Ella 的 T002 契约用例禁止修改，
// 本文件只钉住「展示文案」这一层，判定语义与 AlertType 码值由契约用例覆盖。
// 口径来源：docs/design/admin/告警管理.html:253 明细行写作「传感器标定异常：空载采集点 …」，
// PRD §7D.12（V3.20）同步把 sensor_drift 显示名收口为「传感器标定异常」。
package engine_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

func TestT419_SensorDriftMessage_UseCalibrationTerm(t *testing.T) {
	evaluator := &engine.RuleEvaluator{SensorDriftThreshold: 2.8}

	pressures := [20]float64{}
	pressures[7] = 3.5 // P08，空载超阈值
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		PatientID: "P001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   false,
	}

	result := evaluator.Evaluate(frame, nil)
	require.NotNil(t, result)
	assert.True(t, result.ShouldAlert)
	// 码值不动：只改前缀措辞
	assert.Equal(t, engine.TypeSensorDrift, result.AlertType)
	assert.Equal(t, "P08", result.SensorPoint)

	assert.True(t, strings.HasPrefix(result.Message, "传感器标定异常："),
		"告警正文前缀须与设计稿明细行一致，实测：%q", result.Message)
	assert.NotContains(t, result.Message, "传感器漂移", "旧词回潮：§7D.12 已收口为「传感器标定异常」")
}

// TestT419_SensorDriftMessage_NegativeDirection T661：负向越界走独立文案
// （应力释放异常 + 检查佩戴/重新校准建议），与正向漂移超标分叉，但共用前缀与类型码。
func TestT419_SensorDriftMessage_NegativeDirection(t *testing.T) {
	evaluator := &engine.RuleEvaluator{SensorDriftThreshold: 0.3}

	pressures := [20]float64{}
	pressures[15] = -1.4 // P16，空载负向越界（现网真实故障样本量级）
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		PatientID: "P001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   false,
	}

	result := evaluator.Evaluate(frame, nil)
	require.NotNil(t, result)
	assert.True(t, result.ShouldAlert)
	assert.Equal(t, engine.TypeSensorDrift, result.AlertType, "类型码不随方向分叉")
	assert.Equal(t, "P16", result.SensorPoint)
	assert.InDelta(t, -1.4, result.ActualValue, 0.001)

	assert.True(t, strings.HasPrefix(result.Message, "传感器标定异常："),
		"正负两条文案共用前缀，实测：%q", result.Message)
	assert.Contains(t, result.Message, "应力释放异常")
	assert.Contains(t, result.Message, "建议检查佩戴或考虑重新校准")
	assert.NotContains(t, result.Message, "漂移超标", "负向不得套正向措辞")
	assert.NotContains(t, result.Message, "传感器漂移")
}

// TestT419_SensorDriftMessage_ToleranceBandNegativeSilent T661 验收口径：
// -0.1N 噪声在 0.3N 容差内，不产生告警（正向文案门禁的负向对照）。
func TestT419_SensorDriftMessage_ToleranceBandNegativeSilent(t *testing.T) {
	evaluator := &engine.RuleEvaluator{SensorDriftThreshold: 0.3}

	pressures := [20]float64{}
	pressures[7] = -0.1
	frame := engine.PressureFrame{
		DeviceID:  "DEV001",
		PatientID: "P001",
		Timestamp: time.Now(),
		Pressures: pressures,
		Wearing:   false,
	}

	assert.Nil(t, evaluator.Evaluate(frame, nil))
}

func TestT419_WearInterruptMessage_AlwaysUsedDeviceOfflineTerm(t *testing.T) {
	// 该条在 38cdcb1 之前就是「设备离线：」，此处补一条门禁，
	// 使四类正文的前缀口径在同一批用例里可对照。
	evaluator := &engine.RuleEvaluator{WearInterruptMinutes: 60}
	base := time.Now().Truncate(time.Minute)

	frame := engine.PressureFrame{DeviceID: "DEV001", Timestamp: base}
	prev := &engine.PressureFrame{DeviceID: "DEV001", Timestamp: base.Add(-90 * time.Minute)}

	result := evaluator.Evaluate(frame, prev)
	require.NotNil(t, result)
	assert.Equal(t, engine.TypeWearInterrupt, result.AlertType)
	assert.True(t, strings.HasPrefix(result.Message, "设备离线："), "实测：%q", result.Message)
	assert.NotContains(t, result.Message, "佩戴中断")
}
