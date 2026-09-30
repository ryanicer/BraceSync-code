// T498 来源印章在补偿链路（alert:pending 降级队列）的留存。
//
// 为什么单独钉一条：内联那腿失败时，data-service 把同一帧 LPUSH 进 alert:pending，
// 由这里 RPOP 后补偿评估并落库。若负载里的 ingest_source 在这一腿被丢掉，
// 「同一条注入帧触发的告警」就会出现：alert-service 在线时有来源、恰好在重启窗口没有来源。
// 溯源口径取决于当时谁可达，等于没有口径。
//
// 另一件：processItem 抄的是 item.Frame.IngestSource 而不是 frame.IngestSource ——
// 后者是 engine.PressureFrame（引擎输入结构，不含来源）。写成 frame.* 编译不过，
// 但一旦有人给引擎结构加了同名域，静默换源就会把契约面改成引擎面，故在此钉住读取来源。
package consumer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sourcePayload 构造带（或不带）ingest_source 的队列负载。
// hasKey=false 时该键完全不出现 —— 对应「旧版 data-service 入队的存量负载」。
func sourcePayload(t *testing.T, now time.Time, source string, hasKey bool) string {
	t.Helper()
	points := make([]float64, PointCount)
	points[2] = 50.0 // 超 pressure_high 默认阈值，保证命中
	item := PendingItem{
		QueuedAt: now.UTC(),
		Frame: FrameRef{
			DeviceID:  "DEV1",
			PatientID: "P1",
			Timestamp: now.Add(-time.Minute).UTC(),
			Points:    points,
		},
	}
	if !hasKey {
		b, err := json.Marshal(&item)
		require.NoError(t, err)
		// omitempty 已让空来源不出现该键，这里再显式确认一次，免得结构加了默认值就把这一格测成假绿
		assert.NotContains(t, string(b), "ingest_source")
		return string(b)
	}
	item.Frame.IngestSource = source
	b, err := json.Marshal(&item)
	require.NoError(t, err)
	return string(b)
}

func TestT498_CompensationKeepsFrameSource(t *testing.T) {
	cases := []struct {
		name        string
		source      string
		hasKey      bool
		wantInAlert string
	}{
		{"注入帧降级后补偿：mock 不丢", "mock", true, "mock"},
		{"真实帧降级后补偿：real 不丢", "real", true, "real"},
		{"存量负载无该键：落空串（repo 侧转 NULL）", "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now := time.Now()
			queue := &fakeQueue{items: []string{sourcePayload(t, now, c.source, c.hasKey)}}
			alerts := &fakeAlerts{}
			notifier := &recordingNotifier{}
			c2 := newTestConsumer(queue, newFakeDedup(), alerts, notifier)
			c2.SetNow(func() time.Time { return now })

			drained, err := c2.DrainOnce(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 1, drained, "负载没被处理 = 下面的抄写断言全在空转")

			require.Len(t, alerts.created, 1, "本用例的前提是补偿命中并落库")
			assert.Equal(t, c.wantInAlert, alerts.created[0].IngestSource,
				"补偿腿丢了来源印章：同一条告警有没有来源取决于 alert-service 当时是否可达")
			assert.Equal(t, "DEV1", alerts.created[0].DeviceID)
			assert.Len(t, notifier.notified, 1, "新鲜帧（未超 staleThreshold）补偿命中后仍推送")
		})
	}
}

// TestT498_PendingItemJSONRoundTrip 负载的两端各自定义结构（data-service 写、本包读），
// 键名靠约定 ⇒ 在此钉住「本包读得到 ingest_source」。
// data-service 侧那条同名断言在 service/mock_ingest_t498_test.go，两处一起才夹住这个键。
func TestT498_PendingItemJSONRoundTrip(t *testing.T) {
	var decoded PendingItem
	require.NoError(t, json.Unmarshal([]byte(
		`{"queued_at":"2026-09-30T08:00:00Z","frame":{"device_id":"DEV1","patient_id":"P1",`+
			`"timestamp":"2026-09-30T07:59:00Z","points":[0,0,50,0,0,0,0,0,0,0,`+
			`0,0,0,0,0,0,0,0,0,0],"ingest_source":"mock"}}`), &decoded))
	assert.Equal(t, "mock", decoded.Frame.IngestSource)
	assert.Len(t, decoded.Frame.Points, PointCount)

	// 反证：未知键不影响解析、缺键读到空串（不得有隐式默认值）
	var legacy PendingItem
	require.NoError(t, json.Unmarshal([]byte(
		`{"queued_at":"2026-09-30T08:00:00Z","frame":{"device_id":"DEV1","patient_id":"P1",`+
			`"timestamp":"2026-09-30T07:59:00Z","points":[0,0,50,0,0,0,0,0,0,0,`+
			`0,0,0,0,0,0,0,0,0,0]}}`), &legacy))
	assert.Empty(t, legacy.Frame.IngestSource, "缺键时不得补默认值：默认成 real 就是把未知读成真实")
}
