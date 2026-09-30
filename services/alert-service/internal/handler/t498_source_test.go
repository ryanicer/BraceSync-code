// T498 来源印章在内联评估链路的落点（alert-service 侧）。
//
// 卡面验收 3：「注入帧能触发对应告警，且该告警可溯源」。可溯源有两个必要条件：
//  1. data-service 把来源放进请求（service 层用例已测）；
//  2. 这里收到后必须原样抄进落库那条告警 —— 本文件测的就是第 2 条。
//
// 为什么三格都钉：real / mock 两格丢任一格，库里那一行就无从判读；
// 「请求里根本没这个键」那一格是向后兼容腿 —— data-service 与 alert-service 分两轮部署，
// 旧版调用方不发这一字段时告警必须照常落库、并落 NULL（不臆断成 real）。
// 把空串读成 real 是最坏的一种：注入帧会和真帧长得一模一样，而这是本卡唯一要防的事。
package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// evalBodyWithSource 构造 /internal/evaluate 请求体。
// sourceExists=false 时**不写这个键**（不是写成空串）：两者在 JSON 面上不是一回事，
// 前者是「旧版调用方」，后者是「新版但来源未知」。
func evalBodyWithSource(t *testing.T, source string, sourceExists bool) []byte {
	t.Helper()
	points := make([]float64, 20)
	points[2] = 50.0 // 超 pressure_high 默认阈值，保证命中
	body := map[string]any{
		"device_id":  "DEV1",
		"patient_id": "P1",
		"timestamp":  time.Now().Add(-time.Minute).UTC(),
		"points":     points,
	}
	if sourceExists {
		body["ingest_source"] = source
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)
	return b
}

func TestT498_EvaluateCopiesSourceIntoAlert(t *testing.T) {
	cases := []struct {
		name        string
		bodySource  string
		bodyHasKey  bool
		wantInAlert string
	}{
		{"注入帧：mock 一路抄到告警", "mock", true, "mock"},
		{"真实帧：real 一路抄到告警", "real", true, "real"},
		{"旧版调用方不带键：落空串（repo 侧转 NULL）", "", false, ""},
		{"新版调用方带空串：同样落空串，不补成 real", "", true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			alerts := &fakeAlerts{}
			h := newTestHandler(alerts, &recordingNotifier{})

			rec := doPost(h, evalBodyWithSource(t, c.bodySource, c.bodyHasKey))
			require.Equal(t, 200, rec.Code)

			var resp respEnvelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.Equal(t, 0, resp.Code, "带不带来源都不得影响评估结果")
			require.NotNil(t, resp.Data)
			assert.True(t, resp.Data.ShouldAlert, "本组用例的前提是命中，没命中则抄写无从谈起")

			require.Len(t, alerts.created, 1)
			assert.Equal(t, c.wantInAlert, alerts.created[0].IngestSource,
				"来源印章必须在落库那一条上：丢了它，这一行在 real 与 mock 之间不可判")
			assert.Equal(t, "P1", alerts.created[0].PatientID, "对拍：确实抄的是这帧的告警")
		})
	}
}

// TestT498_EvaluateMissDoesNotCreateRow 反证前提：未命中不落库，
// 免得「created 为空」被读成「命中了但没抄来源」。
func TestT498_EvaluateMissDoesNotCreateRow(t *testing.T) {
	alerts := &fakeAlerts{}
	h := newTestHandler(alerts, nil)

	points := make([]float64, 20)
	points[2] = 5.0 // 低于 pressure_high 阈值
	b, err := json.Marshal(map[string]any{
		"device_id":     "DEV1",
		"patient_id":    "P1",
		"timestamp":     time.Now().UTC(),
		"points":        points,
		"ingest_source": "mock",
	})
	require.NoError(t, err)

	rec := doPost(h, b)
	require.Equal(t, 200, rec.Code)
	var resp respEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.Data.ShouldAlert)
	assert.Empty(t, alerts.created)
}
