package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/metrics"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

// ─────────────────────────────────────────────────────────────
// fakes
// ─────────────────────────────────────────────────────────────

type fakeQueue struct {
	items  []string
	popErr error
	lenErr error
}

func (q *fakeQueue) Pop(context.Context) (string, bool, error) {
	if q.popErr != nil {
		return "", false, q.popErr
	}
	if len(q.items) == 0 {
		return "", false, nil
	}
	item := q.items[0]
	q.items = q.items[1:]
	return item, true, nil
}

func (q *fakeQueue) Len(context.Context) (int64, error) {
	if q.lenErr != nil {
		return 0, q.lenErr
	}
	return int64(len(q.items)), nil
}

type fakeDedup struct {
	seen map[string]bool
	err  error
}

func newFakeDedup() *fakeDedup { return &fakeDedup{seen: make(map[string]bool)} }

func (d *fakeDedup) MarkEvaluated(_ context.Context, deviceID string, ts time.Time) (bool, error) {
	if d.err != nil {
		return false, d.err
	}
	key := deviceID + "|" + ts.UTC().Format(time.RFC3339)
	if d.seen[key] {
		return false, nil
	}
	d.seen[key] = true
	return true, nil
}

type fakeAlerts struct {
	created     []scanner.NewAlert
	createErr   error
	createFalse bool
	nextID      int
}

func (a *fakeAlerts) CreateAlert(_ context.Context, alert scanner.NewAlert) (string, bool, error) {
	if a.createErr != nil {
		return "", false, a.createErr
	}
	if a.createFalse {
		return "", false, nil
	}
	a.nextID++
	alert.AlertID = fmt.Sprintf("%d", a.nextID)
	a.created = append(a.created, alert)
	return alert.AlertID, true, nil
}

type recordingNotifier struct{ notified []scanner.NewAlert }

func (n *recordingNotifier) Notify(_ context.Context, alert scanner.NewAlert) {
	n.notified = append(n.notified, alert)
}

// mkPayload 构造队列负载 JSON（与 data-service pendingAlertItem 格式一致）
func mkPayload(t *testing.T, queuedAt time.Time, deviceID, patientID string, ts time.Time, maxPressure float64) string {
	t.Helper()
	points := make([]float64, PointCount)
	points[2] = maxPressure
	item := PendingItem{
		QueuedAt: queuedAt.UTC(),
		Frame: FrameRef{
			DeviceID:  deviceID,
			PatientID: patientID,
			Timestamp: ts.UTC(),
			Points:    points,
		},
	}
	b, err := json.Marshal(&item)
	require.NoError(t, err)
	return string(b)
}

func newTestConsumer(queue *fakeQueue, dedup *fakeDedup, alerts *fakeAlerts, notifier Notifier) *Consumer {
	c := New(queue, dedup, alerts, engine.NewDefaultRuleEvaluator(), notifier)
	return c
}

// ─────────────────────────────────────────────────────────────
// ToPressureFrame
// ─────────────────────────────────────────────────────────────

func TestFrameRef_ToPressureFrame(t *testing.T) {
	points := make([]float64, PointCount)
	points[2] = 50.0
	f := FrameRef{DeviceID: "DEV1", PatientID: "P1", Timestamp: time.Unix(1000, 0), Points: points}
	frame, err := f.ToPressureFrame()
	require.NoError(t, err)
	assert.Equal(t, 50.0, frame.Pressures[2])
	assert.True(t, frame.Wearing, "max 50N > 0.5N → 佩戴帧")

	// 空载帧：全部低于佩戴阈值
	zero := FrameRef{DeviceID: "DEV1", Points: make([]float64, PointCount)}
	frame, err = zero.ToPressureFrame()
	require.NoError(t, err)
	assert.False(t, frame.Wearing)

	// 点数不足
	_, err = FrameRef{Points: []float64{1, 2}}.ToPressureFrame()
	require.Error(t, err)
}

// ─────────────────────────────────────────────────────────────
// DrainOnce：各结果分支
// ─────────────────────────────────────────────────────────────

func TestDrainOnce_AlertedFrameCreatesAlertAndNotifies(t *testing.T) {
	now := time.Now()
	queue := &fakeQueue{items: []string{mkPayload(t, now, "DEV1", "P1", now.Add(-time.Minute), 50.0)}}
	alerts := &fakeAlerts{}
	notifier := &recordingNotifier{}
	c := newTestConsumer(queue, newFakeDedup(), alerts, notifier)
	c.SetNow(func() time.Time { return now })

	n, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, alerts.created, 1)
	assert.Equal(t, engine.TypePressureHigh, alerts.created[0].Type)
	assert.Equal(t, "P1", alerts.created[0].PatientID)
	assert.True(t, alerts.created[0].Ts.Equal(now.Add(-time.Minute)), "告警时刻 = 帧采集时刻")
	assert.Len(t, notifier.notified, 1, "新鲜帧命中 → 推送")
	assert.EqualValues(t, 1, testutil.ToFloat64(metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeAlerted)))
	assert.EqualValues(t, 0, testutil.ToFloat64(metrics.PendingQueueLength), "排空后长度指标归零")
}

func TestDrainOnce_CleanFrameNoAlert(t *testing.T) {
	now := time.Now()
	queue := &fakeQueue{items: []string{mkPayload(t, now, "DEV1", "P1", now.Add(-time.Minute), 20.0)}}
	alerts := &fakeAlerts{}
	notifier := &recordingNotifier{}
	c := newTestConsumer(queue, newFakeDedup(), alerts, notifier)
	c.SetNow(func() time.Time { return now })

	_, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.Empty(t, alerts.created)
	assert.Empty(t, notifier.notified)
}

func TestDrainOnce_DuplicateFrameIdempotent(t *testing.T) {
	now := time.Now()
	frameTS := now.Add(-time.Minute)
	payload := mkPayload(t, now, "DEV1", "P1", frameTS, 50.0)
	// 同一帧重复入队两次（data-service 重试/队列重放）
	queue := &fakeQueue{items: []string{payload, payload}}
	alerts := &fakeAlerts{}
	c := newTestConsumer(queue, newFakeDedup(), alerts, &recordingNotifier{})
	c.SetNow(func() time.Time { return now })

	_, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.Len(t, alerts.created, 1, "幂等键抑制重复告警")
	assert.EqualValues(t, 1, testutil.ToFloat64(metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeDeduped)))
}

func TestDrainOnce_StaleFrameRecordsButDoesNotPush(t *testing.T) {
	now := time.Now()
	// 入队超过 1h 的积压帧
	queue := &fakeQueue{items: []string{mkPayload(t, now.Add(-2*time.Hour), "DEV1", "P1", now.Add(-2*time.Hour), 50.0)}}
	alerts := &fakeAlerts{}
	notifier := &recordingNotifier{}
	c := newTestConsumer(queue, newFakeDedup(), alerts, notifier)
	c.SetNow(func() time.Time { return now })

	_, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.Len(t, alerts.created, 1, "积压帧仍补告警记录")
	assert.Empty(t, notifier.notified, ">1h 积压帧不推送（避免过时骚扰）")
	assert.EqualValues(t, 1, testutil.ToFloat64(metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeStale)))
}

func TestDrainOnce_InvalidPayloadsDropped(t *testing.T) {
	now := time.Now()
	badJSON := `{not json`
	badPoints := `{"queued_at":"` + now.Format(time.RFC3339) + `","frame":{"device_id":"D","patient_id":"P","timestamp":"` + now.Format(time.RFC3339) + `","points":[1,2]}}`
	noPatient := `{"queued_at":"` + now.Format(time.RFC3339) + `","frame":{"device_id":"D","patient_id":"","timestamp":"` + now.Format(time.RFC3339) + `","points":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}}`

	queue := &fakeQueue{items: []string{badJSON, badPoints, noPatient}}
	alerts := &fakeAlerts{}
	c := newTestConsumer(queue, newFakeDedup(), alerts, nil)
	c.SetNow(func() time.Time { return now })

	n, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Empty(t, alerts.created)
	assert.EqualValues(t, 3, testutil.ToFloat64(metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeDropped)))
}

func TestDrainOnce_StoreErrorsCountedNotPanicked(t *testing.T) {
	now := time.Now()

	// 幂等键写失败
	queue := &fakeQueue{items: []string{mkPayload(t, now, "DEV1", "P1", now.Add(-time.Minute), 50.0)}}
	dedup := newFakeDedup()
	dedup.err = errors.New("redis down")
	c := newTestConsumer(queue, dedup, &fakeAlerts{}, nil)
	c.SetNow(func() time.Time { return now })
	_, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, testutil.ToFloat64(metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeError)))

	// 落库失败
	queue2 := &fakeQueue{items: []string{mkPayload(t, now, "DEV2", "P1", now.Add(-time.Minute), 50.0)}}
	alerts := &fakeAlerts{createErr: errors.New("pg down")}
	c2 := newTestConsumer(queue2, newFakeDedup(), alerts, nil)
	c2.SetNow(func() time.Time { return now })
	_, err = c2.DrainOnce(context.Background())
	require.NoError(t, err)

	// 唯一约束保底命中（created=false）计 deduped
	queue3 := &fakeQueue{items: []string{mkPayload(t, now, "DEV3", "P1", now.Add(-time.Minute), 50.0)}}
	alerts3 := &fakeAlerts{createFalse: true}
	c3 := newTestConsumer(queue3, newFakeDedup(), alerts3, nil)
	c3.SetNow(func() time.Time { return now })
	_, err = c3.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.Empty(t, alerts3.created)
}

func TestDrainOnce_PopErrorReturned(t *testing.T) {
	queue := &fakeQueue{popErr: errors.New("redis down")}
	c := newTestConsumer(queue, newFakeDedup(), &fakeAlerts{}, nil)
	_, err := c.DrainOnce(context.Background())
	require.Error(t, err)
}

func TestDrainOnce_MaxBatchLimit(t *testing.T) {
	now := time.Now()
	var items []string
	for i := 0; i < 5; i++ {
		items = append(items, mkPayload(t, now, "DEV1", "P1", now.Add(-time.Duration(i+1)*time.Minute), 50.0))
	}
	queue := &fakeQueue{items: items}
	alerts := &fakeAlerts{}
	c := newTestConsumer(queue, newFakeDedup(), alerts, nil)
	c.SetNow(func() time.Time { return now })
	c.maxBatch = 3

	n, err := c.DrainOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, n, "单轮上限 maxBatch")
	assert.Len(t, queue.items, 2, "剩余留给下一轮")
}

func TestDrainOnce_QueueLengthMetricRefreshed(t *testing.T) {
	queue := &fakeQueue{items: []string{"x", "y"}, lenErr: nil}
	// pop 报错提前结束，但长度指标仍刷新
	queue.popErr = errors.New("boom")
	c := newTestConsumer(queue, newFakeDedup(), &fakeAlerts{}, nil)
	_, _ = c.DrainOnce(context.Background())
	assert.EqualValues(t, 2, testutil.ToFloat64(metrics.PendingQueueLength))
}

// ─────────────────────────────────────────────────────────────
// Run：常驻循环随 ctx 取消退出
// ─────────────────────────────────────────────────────────────

func TestRun_StopsOnContextCancelAndDrains(t *testing.T) {
	now := time.Now()
	queue := &fakeQueue{items: []string{mkPayload(t, now, "DEV1", "P1", now.Add(-time.Minute), 50.0)}}
	alerts := &fakeAlerts{}
	c := newTestConsumer(queue, newFakeDedup(), alerts, nil)
	c.SetNow(func() time.Time { return now })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx, 10*time.Millisecond)
		close(done)
	}()

	require.Eventually(t, func() bool { return len(alerts.created) == 1 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancel")
	}
}

func TestNew_DefaultsAndNilNotifier(t *testing.T) {
	c := New(&fakeQueue{}, newFakeDedup(), &fakeAlerts{}, engine.NewDefaultRuleEvaluator(), nil)
	assert.NotNil(t, c.notifier, "nil notifier → NoopNotifier")
	assert.Equal(t, DefaultStaleThreshold, c.staleThreshold)
	assert.Equal(t, DefaultMaxBatch, c.maxBatch)
	c.SetStaleThreshold(2 * time.Hour)
	assert.Equal(t, 2*time.Hour, c.staleThreshold)
	c.SetLogger(c.log) // 覆盖 SetLogger 路径
}
