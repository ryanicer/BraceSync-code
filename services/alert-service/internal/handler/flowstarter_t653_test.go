// T653：告警入库后按类型绑定自动建流程实例 —— handler.evaluate 接线测试。
//
// 契约（PRD V3.44 R8 甲）：
//   - 仅 CreateAlert 返回 created=true（新建）时调一次 AutoStart(alertID, alertType)；
//   - 去重命中（created=false）/ 未命中（无告警）/ 持久化失败均不调；
//   - 通知链与流程链各自独立（本测试只校验触发条件，失败不阻塞由 flowstarter 客户端包自测）。
package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type t653RecordStarter struct {
	calls []t653StartCall
}

type t653StartCall struct {
	alertID   string
	alertType string
}

func (s *t653RecordStarter) AutoStart(_ context.Context, alertID, alertType string) {
	s.calls = append(s.calls, t653StartCall{alertID: alertID, alertType: alertType})
}

func TestT653_Evaluate_NewAlertTriggersFlowAutoStart(t *testing.T) {
	alerts := &fakeAlerts{}
	h := newTestHandler(alerts, nil)
	starter := &t653RecordStarter{}
	h.SetFlowStarter(starter)

	rec := doPost(h, evalBody(t, "DEV1", "P1", time.Now().Add(-time.Minute).UTC(), 50.0))
	require.Equal(t, 200, rec.Code)

	require.Len(t, starter.calls, 1, "新建告警应恰好触发一次自动建流程")
	assert.Equal(t, "1", starter.calls[0].alertID, "alertID = 落库回填的 ID")
	assert.Equal(t, "pressure_high", starter.calls[0].alertType)
}

func TestT653_Evaluate_DedupHitDoesNotTriggerFlowAutoStart(t *testing.T) {
	alerts := &fakeAlerts{createFalse: true} // CreateAlert 返回 created=false（去重窗口内已有活动告警）
	h := newTestHandler(alerts, nil)
	starter := &t653RecordStarter{}
	h.SetFlowStarter(starter)

	rec := doPost(h, evalBody(t, "DEV1", "P1", time.Now().Add(-time.Minute).UTC(), 50.0))
	require.Equal(t, 200, rec.Code)
	assert.Empty(t, starter.calls, "一告警一实例：去重命中不重复触发，否则可能给同一告警建出第二个实例")
}

func TestT653_Evaluate_MissDoesNotTriggerFlowAutoStart(t *testing.T) {
	alerts := &fakeAlerts{}
	h := newTestHandler(alerts, nil)
	starter := &t653RecordStarter{}
	h.SetFlowStarter(starter)

	rec := doPost(h, evalBody(t, "DEV1", "P1", time.Now().UTC(), 20.0))
	require.Equal(t, 200, rec.Code)
	assert.Empty(t, alerts.created)
	assert.Empty(t, starter.calls, "未命中阈值不产生告警，自然不触发流程")
}

func TestT653_Evaluate_DefaultStarterIsNoop(t *testing.T) {
	// 不注入 starter（旧装配/未配 USER_SERVICE_URL）时整条链路照常工作
	alerts := &fakeAlerts{}
	notifier := &recordingNotifier{}
	h := newTestHandler(alerts, notifier)

	rec := doPost(h, evalBody(t, "DEV1", "P1", time.Now().Add(-time.Minute).UTC(), 50.0))
	require.Equal(t, 200, rec.Code)
	require.Len(t, alerts.created, 1)
	require.Len(t, notifier.notified, 1, "通知链不受流程链缺失影响")
}
