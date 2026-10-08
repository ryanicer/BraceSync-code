// Package engine — T625 盲区用例：传感器漂移（sensor_drift，T621 规则）的空载/停用/选取口径
//
// 本文件是**新增**测试文件，不改动 engine_test.go（T002 Ella 冻结契约）、
// engine_supplement_test.go（T003 Winner）与 engine_pointrules_test.go（T252）。
// 契约出处：engine.go:74 checkSensorDrift（守卫 :76，触发表达式 :82）。
//
// 现读实现的判定式（engine.go:82）：
//
//	if (p > e.SensorDriftThreshold || p < 0) && math.Abs(p) > maxAbs
//
// 两个要害：
//  1. 「任何负值都触发」——负值不受阈值幅值约束，-0.2 在阈值 2.8 下同样命中；
//     这不是「按幅值对称」的漂移判定，校准后的轻微负偏会被当成传感器故障上报（类 7 同族缺陷，待 T621 收口）。
//  2. 多点位命中时 SensorPoint 取**绝对值最大**的点（不是「最超阈」也不是「首个命中」），
//     负值因此能压过同号幅值更大的正超阈读数——本文件把这个选取口径钉住。
package engine_test

import (
	"math"
	"testing"
	"time"

	"github.com/bracesync/bracesync/services/alert-service/internal/config"
	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t625DriftFrame 组装一帧空载/佩戴读数：只给 PressureOverrides（下标 → N），其余点恒 0（空载本底）。
// PressureHighThreshold 由调用方置 0（该规则不启用），确保 EvaluateAll 的唯一候选就是 sensor_drift，
// 免得规则优先级 pressure_high → sensor_drift → wear_interrupt（types.go:114-118）遮住要钉的那一格。
func t625DriftFrame(wearing bool, overrides map[int]float64) engine.PressureFrame {
	var pressures [20]float64
	for idx, val := range overrides {
		pressures[idx] = val
	}
	return engine.PressureFrame{
		DeviceID:  "DEV-T625",
		PatientID: "P-T625",
		Timestamp: time.Date(2026, 10, 8, 2, 40, 0, 0, time.UTC),
		Pressures: pressures,
		Wearing:   wearing,
	}
}

// t625DriftEvaluator 每次新造评估器：去重窗口（DedupWindowMinutes）是评估器内存态，
// 复用同一个会把后一发抑制成 nil（见 engine.go applyDedup），与本文件的判据无关。
func t625DriftEvaluator(threshold float64) *engine.RuleEvaluator {
	return &engine.RuleEvaluator{
		PressureHighThreshold: 0, // 规则不启用（零值即关闭，engine.go:38）
		WearInterruptMinutes:  0, // 同上：排除 wear_interrupt 干扰
		SensorDriftThreshold:  threshold,
	}
}

// t625OnlyDriftHit 断言命中集合非空且全部是 sensor_drift，返回该条。
func t625OnlyDriftHit(t *testing.T, hits []*engine.AlertResult) *engine.AlertResult {
	t.Helper()
	require.NotEmpty(t, hits, "空载超阈/负值读数应命中规则")
	for _, hit := range hits {
		require.Equal(t, engine.TypeSensorDrift, hit.AlertType,
			"本用例只允许 sensor_drift 命中，实得 %s", hit.AlertType)
	}
	return hits[0]
}

// ─────────────────────────────────────────────────────────────
// 1) 佩戴态（Wearing=true）时漂移规则整体静音：任何负值 / 超阈读数都不产告警
// ─────────────────────────────────────────────────────────────

func TestT625_SensorDrift_WearingFramesAreSilent(t *testing.T) {
	evaluator := t625DriftEvaluator(2.8) // 与 NewDefaultRuleEvaluator 的默认阈值同值

	cases := []struct {
		name      string
		overrides map[int]float64
	}{
		{"负值读数", map[int]float64{3: -5.0}},
		{"正超阈读数", map[int]float64{3: 5.0}},
		{"负值与正超阈并存", map[int]float64{1: 9.0, 6: -7.5}},
		{"整排读数越阈", map[int]float64{0: 3.0, 5: -3.0, 19: 12.0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := t625DriftFrame(true, tc.overrides)
			assert.Empty(t, evaluator.EvaluateAll(frame, nil),
				"Wearing=true 必须在判定前直接返回（engine.go:76 守卫），不得产出 sensor_drift")
			assert.Nil(t, evaluator.Evaluate(frame, nil))
		})
	}

	// 对照：同一组读数换成空载（Wearing=false）后确有命中——
	// 证明上面的空集来自佩戴守卫，而不是「读数本身不足以触发」。
	control := t625DriftFrame(false, map[int]float64{3: -5.0})
	require.NotEmpty(t, evaluator.EvaluateAll(control, nil), "对照帧应命中，否则本用例失去意义")
}

// ─────────────────────────────────────────────────────────────
// 2) SensorDriftThreshold <= 0 = 规则停用（全引擎「零值即关闭」口径，engine.go:76）
// ─────────────────────────────────────────────────────────────

func TestT625_SensorDrift_DisabledWhenThresholdNotPositive(t *testing.T) {
	// 空载 + 明显越阈的读数：规则一旦启用必命中，用它反证「停用」是真的短路
	frame := t625DriftFrame(false, map[int]float64{7: 40.0, 8: -12.0})

	for _, threshold := range []float64{0, -0.1, -2.8} {
		evaluator := t625DriftEvaluator(threshold)
		assert.Empty(t, evaluator.EvaluateAll(frame, nil),
			"阈值 %v 不属正数 ⇒ 规则不启用，不得产出任何告警", threshold)
		assert.Nil(t, evaluator.Evaluate(frame, nil), "阈值 %v 时 Evaluate 应回 nil", threshold)
	}

	// 同一帧把阈值提到最小正数：必须命中，证明「空集」来自停用而非读数不足
	enabled := t625DriftEvaluator(0.1)
	hit := t625OnlyDriftHit(t, enabled.EvaluateAll(frame, nil))
	assert.Equal(t, "P08", hit.SensorPoint, "0.1N 阈值下 -12.0 的绝对值最大，取的仍是幅值点")
}

// ─────────────────────────────────────────────────────────────
// 3) 空载 + |p| 越阈：正 5.0 与负 -5.0 各一首都触发；SensorPoint = 绝对值最大的点
// ─────────────────────────────────────────────────────────────

func TestT625_SensorDrift_PositiveAndNegativeBothTriggerAndPickMaxAbsPoint(t *testing.T) {
	const threshold = 2.8

	// 3a 正超阈：P03 = +5.0
	posFrame := t625DriftFrame(false, map[int]float64{2: 5.0})
	posHit := t625OnlyDriftHit(t, t625DriftEvaluator(threshold).EvaluateAll(posFrame, nil))
	assert.Equal(t, "P03", posHit.SensorPoint)
	assert.InDelta(t, 5.0, posHit.ActualValue, 0.001)
	assert.InDelta(t, threshold, posHit.ThresholdValue, 0.001)
	assert.Equal(t, engine.TypeSensorDrift, posHit.AlertType)
	assert.Equal(t, "medium", posHit.Severity, "漂移严重度现值 medium（engine.go:98）")

	// 3b 负超阈（同幅值）：P07 = -5.0 —— 与 3a 对称命中，钉住「负值也触发」
	negFrame := t625DriftFrame(false, map[int]float64{6: -5.0})
	negHit := t625OnlyDriftHit(t, t625DriftEvaluator(threshold).EvaluateAll(negFrame, nil))
	assert.Equal(t, "P07", negHit.SensorPoint)
	assert.InDelta(t, -5.0, negHit.ActualValue, 0.001)

	// 3c 混合：负值幅值最大（+3.0 @P01 与 -5.0 @P10）→ 取 P10。
	// 正负都越阈时，胜出者由 math.Abs 决定，与规则优先级无关（同一条规则内的逐点比较）。
	mixedFrame := t625DriftFrame(false, map[int]float64{0: 3.0, 9: -5.0})
	mixedHit := t625OnlyDriftHit(t, t625DriftEvaluator(threshold).EvaluateAll(mixedFrame, nil))
	assert.Equal(t, "P10", mixedHit.SensorPoint,
		"选取口径 = 绝对值最大的点，负值不因符号被降权")
	assert.InDelta(t, -5.0, mixedHit.ActualValue, 0.001)

	// 3d 混合反向：正值幅值最大（+4.5 @P01 与 -3.0 @P06）→ 取 P01
	reverseFrame := t625DriftFrame(false, map[int]float64{0: 4.5, 5: -3.0})
	reverseHit := t625OnlyDriftHit(t, t625DriftEvaluator(threshold).EvaluateAll(reverseFrame, nil))
	assert.Equal(t, "P01", reverseHit.SensorPoint)
	assert.InDelta(t, 4.5, reverseHit.ActualValue, 0.001)

	// 3e 等幅值并列（+5.0 @P02 与 -5.0 @P09）：现实现用严格大于（math.Abs(p) > maxAbs），
	// 故保留**先遍历到**的那个点。这是纯实现细节，钉住它是为了「改动选取策略时必须让这里响」。
	tieFrame := t625DriftFrame(false, map[int]float64{1: 5.0, 8: -5.0})
	tieHit := t625OnlyDriftHit(t, t625DriftEvaluator(threshold).EvaluateAll(tieFrame, nil))
	assert.Equal(t, "P02", tieHit.SensorPoint, "等幅值并列时取遍历顺序的第一个点")

	// 3f 不越阈的正读数（1.0N，全部落在阈值内且无负值）→ 无命中，证明 3a-3e 不是「恒触发」
	quietFrame := t625DriftFrame(false, map[int]float64{4: 1.0, 5: math.SmallestNonzeroFloat64})
	assert.Empty(t, t625DriftEvaluator(threshold).EvaluateAll(quietFrame, nil))
}

// ─────────────────────────────────────────────────────────────
// 4) 类 7：校准后的轻微负偏（-0.2N）不应报故障 —— 现实现必报，故 pending
// ─────────────────────────────────────────────────────────────

func TestT625_SensorDrift_SmallNegativeAfterCalibrationShouldNotAlert(t *testing.T) {
	reason := "T621 未合入：checkSensorDrift 需把负值判据从「p < 0 即故障」（engine.go:82）改为按幅值对称 " +
		"|p| > 阈值（或引入 threshold_calibration_offset 容差，user-service 已有该键 handler.go:2028）；" +
		"合入后去掉本行即转绿"
	t.Skip(reason)

	// 期望（PRD 口径）：-0.2N 是校准容差内的正常负偏，不是传感器故障。
	frame := t625DriftFrame(false, map[int]float64{3: -0.2})
	assert.Empty(t, t625DriftEvaluator(2.8).EvaluateAll(frame, nil),
		"校准后 -0.2N 不应产出 sensor_drift")

	// 同帧幅值真越阈的负读数仍需命中：收口不能把负值判据整条删掉
	stillBroken := t625DriftFrame(false, map[int]float64{3: -5.0})
	assert.NotEmpty(t, t625DriftEvaluator(2.8).EvaluateAll(stillBroken, nil))
}

// ─────────────────────────────────────────────────────────────
// 5) 阈值口径冲突：配置键默认 0.3 vs 引擎默认 2.8 —— pending（缺陷待新卡收口）
// ─────────────────────────────────────────────────────────────

// 现状三个「默认值」各处一说（file:line 为 2026-10-08 现读 grep 结果）：
//   - services/alert-service/internal/engine/engine.go:21           SensorDriftThreshold: 2.8（NewDefaultRuleEvaluator）
//   - services/alert-service/internal/config/config.go:61           SensorDriftN: 0.3（DefaultThresholds，注释 :55 却写着
//     「PRD 默认阈值口径（与 engine.NewDefaultRuleEvaluator 一致）」—— 该注释与数值直接矛盾）
//   - services/user-service/internal/handler/handler.go:2048        SensorDriftN: 0.3（settingsDefaults，键 :2021）
//   - services/user-service/internal/handler/handler.go:2173        入参校验区间 [0.1,20]：2.8 合法、0.3 亦合法，
//     ⇒ 校验拦不住口径分裂，两个值都能被写进 sys_configs
//   - scripts/db/migrations/000019_t203_threshold_div10_winner.up.sql:12 把库值 2.8 改成 0.3，
//     但没同步引擎默认值，也没在 engine.go 的阈值语义注释（types.go:65 「漂移 2.8N」）留痕
//
// 后果：config.Manager.Refresh 走 ApplyThresholds（manager.go:149）时线上生效 0.3；
// 任何未经 Refresh 的评估器（NewDefaultRuleEvaluator 直接构造的调用方、单测替身）走 2.8，
// 同一份数据两口径 ⇒ 空载正读数在 (0.3, 2.8] 区间内时报时不报，无固定答案。
func TestT625_SensorDriftThreshold_OneDocumentedDefaultAcrossLayers(t *testing.T) {
	reason := "T625 新卡（阈值口径收口）未合入：threshold_sensor_drift 要只有一个权威默认值" +
		"（engine.go:21 的 2.8 与 config.go:61 / user-service handler.go:2048 的 0.3 收成一个，" +
		"并把 config.go:55 那句「与 engine.NewDefaultRuleEvaluator 一致」的注释与实现对齐）；" +
		"合入后去掉本行即转绿"
	t.Skip(reason)

	engineDefault := engine.NewDefaultRuleEvaluator().SensorDriftThreshold
	configDefault := config.DefaultThresholds().SensorDriftN

	assert.InDelta(t, configDefault, engineDefault, 0.0001,
		"引擎默认值必须与配置层默认值同口径（现值：engine %v vs config %v）", engineDefault, configDefault)
}
