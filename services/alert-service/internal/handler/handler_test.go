package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/consumer"
	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

// ─────────────────────────────────────────────────────────────
// fakes
// ─────────────────────────────────────────────────────────────

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

func newTestHandler(alerts *fakeAlerts, notifier consumer.Notifier) *Handler {
	return New(engine.NewDefaultRuleEvaluator(), alerts, notifier)
}

// evalBody 构造 /internal/evaluate 请求体（与 data-service AlertEvalRequest 字段一致）
func evalBody(t *testing.T, deviceID, patientID string, ts time.Time, maxPressure float64) []byte {
	t.Helper()
	points := make([]float64, 20)
	points[2] = maxPressure
	b, err := json.Marshal(consumer.FrameRef{DeviceID: deviceID, PatientID: patientID, Timestamp: ts, Points: points})
	require.NoError(t, err)
	return b
}

type respEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    *evalResultData `json:"data"`
}

func doPost(h *Handler, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	return rec
}

// ─────────────────────────────────────────────────────────────
// /internal/evaluate
// ─────────────────────────────────────────────────────────────

func TestEvaluate_HitCreatesAlertAndReturnsResult(t *testing.T) {
	alerts := &fakeAlerts{}
	notifier := &recordingNotifier{}
	h := newTestHandler(alerts, notifier)
	ts := time.Now().Add(-time.Minute).UTC()

	rec := doPost(h, evalBody(t, "DEV1", "P1", ts, 50.0))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp respEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	require.NotNil(t, resp.Data)
	assert.True(t, resp.Data.ShouldAlert)
	assert.Equal(t, "pressure_high", resp.Data.AlertType)
	assert.Equal(t, "P03", resp.Data.SensorPoint)

	// 命中即落库（告警记录）+ 推送
	require.Len(t, alerts.created, 1)
	assert.Equal(t, engine.TypePressureHigh, alerts.created[0].Type)
	assert.True(t, alerts.created[0].Ts.Equal(ts), "告警时刻 = 帧采集时刻")
	assert.Len(t, notifier.notified, 1)
}

func TestEvaluate_MissReturnsShouldAlertFalse(t *testing.T) {
	alerts := &fakeAlerts{}
	h := newTestHandler(alerts, nil)

	rec := doPost(h, evalBody(t, "DEV1", "P1", time.Now().UTC(), 20.0))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp respEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	require.NotNil(t, resp.Data)
	assert.False(t, resp.Data.ShouldAlert)
	assert.Empty(t, alerts.created)
}

func TestEvaluate_InvalidRequests(t *testing.T) {
	h := newTestHandler(&fakeAlerts{}, nil)

	// 非法 JSON
	rec := doPost(h, []byte(`{bad`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// points 长度错误
	bad, err := json.Marshal(consumer.FrameRef{DeviceID: "D", PatientID: "P", Timestamp: time.Now(), Points: []float64{1}})
	require.NoError(t, err)
	rec = doPost(h, bad)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// 缺 patient_id
	missing, err := json.Marshal(consumer.FrameRef{DeviceID: "D", Timestamp: time.Now(), Points: make([]float64, 20)})
	require.NoError(t, err)
	rec = doPost(h, missing)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// 方法不允许（Go 1.22 ServeMux 方法路由）
	req := httptest.NewRequest(http.MethodGet, "/internal/evaluate", nil)
	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestEvaluate_PersistFailureReturnsNonZeroCode(t *testing.T) {
	// 落库失败 → 非 0 码，data-service 侧视为不可用降级入队（告警不丢）
	alerts := &fakeAlerts{createErr: errors.New("pg down")}
	h := newTestHandler(alerts, nil)

	rec := doPost(h, evalBody(t, "DEV1", "P1", time.Now().UTC(), 50.0))
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	var resp respEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEqual(t, 0, resp.Code)

	// 唯一约束保底场景（created=false）仍视为成功响应
	alerts2 := &fakeAlerts{createFalse: true}
	h2 := newTestHandler(alerts2, nil)
	rec = doPost(h2, evalBody(t, "DEV1", "P1", time.Now().UTC(), 50.0))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.True(t, resp.Data.ShouldAlert)
}

// ─────────────────────────────────────────────────────────────
// /healthz + /metrics
// ─────────────────────────────────────────────────────────────

func TestHealthzAndMetricsEndpoints(t *testing.T) {
	h := newTestHandler(&fakeAlerts{}, nil)
	router := h.Router()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"ok"`)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "alert_pending_queue_length"),
		"队列长度指标可被 Prometheus 采集")
}

func TestNew_NilNotifierUsesNoop(t *testing.T) {
	h := New(engine.NewDefaultRuleEvaluator(), &fakeAlerts{}, nil)
	assert.NotNil(t, h.notifier)
	h.SetLogger(h.log) // 覆盖 SetLogger 路径
}
