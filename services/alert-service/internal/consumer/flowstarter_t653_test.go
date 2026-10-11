// T653：补偿消费链路上的自动建流程触发条件。
//
// 与内联评估同契约，外加一条补偿链特有语义：
// 陈旧积压帧（>1h）只跳过通知，告警既已补录，流程实例照建 —— AutoStart 在 stale 分支之前。
package consumer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type t653RecordStarter struct{ calls []t653StartCall }

type t653StartCall struct{ alertID, alertType string }

func (s *t653RecordStarter) AutoStart(_ context.Context, alertID, alertType string) {
	s.calls = append(s.calls, t653StartCall{alertID: alertID, alertType: alertType})
}

func TestT653_DrainOnce_NewAlertTriggersFlowAutoStart(t *testing.T) {
	now := time.Now()
	queue := &fakeQueue{items: []string{mkPayload(t, now, "DEV1", "P1", now.Add(-time.Minute), 50.0)}}
	c := newTestConsumer(queue, newFakeDedup(), &fakeAlerts{}, &recordingNotifier{})
	c.SetNow(func() time.Time { return now })
	starter := &t653RecordStarter{}
	c.SetFlowStarter(starter)

	_, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	require.Len(t, starter.calls, 1)
	assert.Equal(t, "1", starter.calls[0].alertID)
	assert.Equal(t, "pressure_high", starter.calls[0].alertType)
}

func TestT653_DrainOnce_StaleFrameStillTriggersFlowAutoStart(t *testing.T) {
	now := time.Now()
	queue := &fakeQueue{items: []string{mkPayload(t, now.Add(-2*time.Hour), "DEV1", "P1", now.Add(-2*time.Hour), 50.0)}}
	notifier := &recordingNotifier{}
	c := newTestConsumer(queue, newFakeDedup(), &fakeAlerts{}, notifier)
	c.SetNow(func() time.Time { return now })
	starter := &t653RecordStarter{}
	c.SetFlowStarter(starter)

	_, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.Empty(t, notifier.notified, "陈旧帧不推送")
	require.Len(t, starter.calls, 1, "但告警已补录，流程实例照建（自动建实例与通知解耦）")
	assert.Equal(t, "pressure_high", starter.calls[0].alertType)
}

func TestT653_DrainOnce_DedupDoesNotTriggerFlowAutoStart(t *testing.T) {
	now := time.Now()
	queue := &fakeQueue{items: []string{mkPayload(t, now, "DEV1", "P1", now.Add(-time.Minute), 50.0)}}
	c := newTestConsumer(queue, newFakeDedup(), &fakeAlerts{createFalse: true}, &recordingNotifier{})
	c.SetNow(func() time.Time { return now })
	starter := &t653RecordStarter{}
	c.SetFlowStarter(starter)

	_, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.Empty(t, starter.calls, "created=false（去重/已有活动告警）不重复建实例")
}
