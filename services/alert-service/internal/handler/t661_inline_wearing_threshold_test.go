// T661 H 项（T621 §4.3）：内联 /internal/evaluate 的佩戴判定必须吃 sys_configs 的
// wearing_pressure_threshold（经 SetWearingThreshold 注入），不再写死 0.05N。
// 同一帧（最大压力 3.5N）在不同注入阈值下，sensor_drift 的空载门必须随 Wearing 翻转。
package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/consumer"
	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

// t661DriftOnlyEvaluator 只启用 sensor_drift（阈值 2.8），排除 pressure_high / wear_interrupt 干扰。
func t661DriftOnlyHandler() *Handler {
	return New(&engine.RuleEvaluator{SensorDriftThreshold: 2.8}, &fakeAlerts{}, nil)
}

// t661EvalBodyMax35 帧：P03=3.5N，其余 0（Wearing 由最大压力 3.5N 与注入阈值比较得出）。
func t661EvalBodyMax35(t *testing.T) []byte {
	t.Helper()
	points := make([]float64, 20)
	points[2] = 3.5
	b, err := json.Marshal(consumer.FrameRef{
		DeviceID: "DEV-T661", PatientID: "P-T661",
		Timestamp: time.Now().UTC(), Points: points,
	})
	require.NoError(t, err)
	return b
}

func TestT661_InlineEvaluate_HonorsInjectedWearingThreshold(t *testing.T) {
	body := t661EvalBodyMax35(t)

	// 1) 注入高佩戴阈值 5.0N：3.5 ≤ 5 → 判空载；空载 3.5 > 漂移阈值 2.8 → 命中 sensor_drift。
	//    修复前内联链路写死 0.05N，此帧恒判佩戴、漂移恒静音（运营调参对内联不生效的根因）。
	hHigh := t661DriftOnlyHandler()
	hHigh.SetWearingThreshold(5.0)
	rec := doPost(hHigh, body)
	require.Equal(t, http.StatusOK, rec.Code)
	var high respEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &high))
	require.NotNil(t, high.Data)
	assert.True(t, high.Data.ShouldAlert, "高佩戴阈值下应判空载并触发漂移告警")
	assert.Equal(t, "sensor_drift", high.Data.AlertType)

	// 2) 注入低佩戴阈值 1.0N：3.5 > 1 → 判佩戴；佩戴门直接静音漂移 → 不告警。
	hLow := t661DriftOnlyHandler()
	hLow.SetWearingThreshold(1.0)
	rec = doPost(hLow, body)
	require.Equal(t, http.StatusOK, rec.Code)
	var low respEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &low))
	require.NotNil(t, low.Data)
	assert.False(t, low.Data.ShouldAlert, "低佩戴阈值下应判佩戴，漂移规则整体静音")

	// 3) 不注入（零值）：转换层回退默认 0.05N → 3.5 > 0.05 判佩戴 → 不告警。
	//    钉死「未配置时兜底与补偿链路一致」，防止零值被当成超高阈值。
	hDefault := t661DriftOnlyHandler()
	rec = doPost(hDefault, body)
	require.Equal(t, http.StatusOK, rec.Code)
	var def respEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &def))
	require.NotNil(t, def.Data)
	assert.False(t, def.Data.ShouldAlert, "未注入时回退 0.05N 兜底，判佩戴不告警")
}
