package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

type fakeRetryQueue struct {
	items   []string
	pushN   int32
	popN    int32
	pushErr error // 模拟 push 失败
}

func (q *fakeRetryQueue) Push(_ context.Context, payload string) error {
	atomic.AddInt32(&q.pushN, 1)
	if q.pushErr != nil {
		return q.pushErr
	}
	q.items = append(q.items, payload)
	return nil
}

func (q *fakeRetryQueue) Pop(_ context.Context) (string, bool, error) {
	if len(q.items) == 0 {
		return "", false, nil
	}
	atomic.AddInt32(&q.popN, 1)
	item := q.items[0]
	q.items = q.items[1:]
	return item, true, nil
}

func (q *fakeRetryQueue) Run(context.Context, time.Duration) {}

// mockMsgServer 模拟 msg-service /internal/msg/send 端点
type mockMsgServer struct {
	statusCode  int
	respBody    string
	callCount   int32
	lastHeaders http.Header
	lastBody    []byte
}

func (m *mockMsgServer) handler(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt32(&m.callCount, 1)
	m.lastHeaders = r.Header.Clone()
	var body []byte
	body, _ = io.ReadAll(r.Body)
	m.lastBody = body

	if m.statusCode == 0 {
		m.statusCode = http.StatusOK
	}
	w.WriteHeader(m.statusCode)
	if m.respBody != "" {
		_, _ = w.Write([]byte(m.respBody))
	} else {
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"accepted":true}}`))
	}
}

func newMockMsgServer() *mockMsgServer {
	return &mockMsgServer{}
}

func (m *mockMsgServer) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(m.handler))
}

func makeAlert() scanner.NewAlert {
	return scanner.NewAlert{
		AlertID:        "42",
		PatientID:      "P1",
		DeviceID:       "DEV1",
		Type:           engine.TypePressureHigh,
		SensorPoint:    "P03",
		Detail:         "压力偏高：采集点 P03 压力 50.0N 超阈值 45.0N",
		ThresholdValue: 45.0,
		ActualValue:    50.0,
		Ts:             time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	}
}

// ─────────────────────────────────────────────────────────────
// Notify 成功
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_NotifySuccess(t *testing.T) {
	mock := newMockMsgServer()
	srv := mock.server()
	defer srv.Close()

	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
	})
	alert := makeAlert()
	n.Notify(context.Background(), alert)

	assert.EqualValues(t, 1, atomic.LoadInt32(&mock.callCount))
	assert.Equal(t, "alert-service", mock.lastHeaders.Get("X-Internal-Service"))
	assert.Equal(t, "application/json", mock.lastHeaders.Get("Content-Type"))

	// 验证请求体对齐契约
	var req notifyRequest
	require.NoError(t, json.Unmarshal(mock.lastBody, &req))
	assert.Equal(t, "42", req.AlertID)
	assert.Equal(t, "pressure_high", req.Type)
	assert.Equal(t, "P1", req.PatientID)
	assert.Equal(t, "DEV1", req.DeviceID)
	assert.Equal(t, "P03", req.SensorPoint)
	require.NotNil(t, req.ThresholdValue)
	assert.Equal(t, 45.0, *req.ThresholdValue)
	require.NotNil(t, req.ActualValue)
	assert.Equal(t, 50.0, *req.ActualValue)
	assert.Equal(t, "2026-08-10T12:00:00Z", req.Timestamp)
}

// ─────────────────────────────────────────────────────────────
// Notify 失败 → 入重试队列
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_NotifyFailedEnqueueRetry(t *testing.T) {
	// 模拟 msg-service 500
	mock := &mockMsgServer{statusCode: 500, respBody: `{"code":90001,"message":"internal error"}`}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	queue := &fakeRetryQueue{}
	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
		RetryQueue:    queue,
	})

	n.Notify(context.Background(), makeAlert())

	assert.EqualValues(t, 1, atomic.LoadInt32(&mock.callCount))
	assert.Equal(t, int32(1), atomic.LoadInt32(&queue.pushN), "失败后入重试队列")
}

// ─────────────────────────────────────────────────────────────
// Notify 超时 → 不阻塞
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_NotifyTimeoutDoesNotBlock(t *testing.T) {
	// 慢服务器
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	queue := &fakeRetryQueue{}
	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       50 * time.Millisecond, // 极短超时
		RetryQueue:    queue,
	})

	start := time.Now()
	n.Notify(context.Background(), makeAlert())
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 400*time.Millisecond, "超时不阻塞主链路")
	assert.Equal(t, int32(1), atomic.LoadInt32(&queue.pushN), "超时后入重试队列")
}

// ─────────────────────────────────────────────────────────────
// DrainRetryOnce：重试成功
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_DrainRetryOnceSuccess(t *testing.T) {
	// 首次失败，重试时成功
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"code":90001,"message":"error"}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"accepted":true}}`))
	}))
	defer srv.Close()

	queue := &fakeRetryQueue{}
	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
		MaxRetries:    3,
		RetryQueue:    queue,
	})

	// 首次推送失败
	n.Notify(context.Background(), makeAlert())
	assert.Equal(t, 1, callCount)
	require.Equal(t, 1, len(queue.items), "首次失败入队")

	// 排空重试
	processed, err := n.DrainRetryOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, processed)
	assert.Equal(t, 2, callCount, "重试调用 msg-service")
	assert.Empty(t, queue.items, "重试成功后队列清空")
}

// ─────────────────────────────────────────────────────────────
// DrainRetryOnce：超过重试上限丢弃
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_DrainRetryOnceMaxRetriesExhausted(t *testing.T) {
	// 始终失败
	mock := &mockMsgServer{statusCode: 500, respBody: `{"code":90001,"message":"error"}`}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	queue := &fakeRetryQueue{}
	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
		MaxRetries:    2, // 最多 2 次
		RetryQueue:    queue,
	})

	// 首次推送失败 → 入队 attempt=0
	n.Notify(context.Background(), makeAlert())
	require.Equal(t, 1, len(queue.items))

	// DrainRetryOnce 排空一轮：
	// pop attempt=0 → 失败 → 重入队 attempt=1（同轮内继续处理）
	// pop attempt=1 → 失败 → attempt+1=2 >= maxRetry=2 → 丢弃
	processed, err := n.DrainRetryOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, processed, "排空 2 条（attempt=0 + attempt=1）")
	assert.Empty(t, queue.items, "超过重试上限后队列清空")
}

// ─────────────────────────────────────────────────────────────
// DrainRetryOnce：无队列配置不报错
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_DrainRetryOnceNoQueue(t *testing.T) {
	n := NewHTTPNotifier(HTTPNotifierConfig{MsgServiceURL: "http://localhost:1"})
	processed, err := n.DrainRetryOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, processed)
}

// ─────────────────────────────────────────────────────────────
// RunRetry：ctx 取消退出
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_RunRetryStopsOnCancel(t *testing.T) {
	mock := newMockMsgServer()
	srv := mock.server()
	defer srv.Close()

	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		n.RunRetry(ctx, 10*time.Millisecond)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunRetry did not stop on ctx cancel")
	}
}

// ─────────────────────────────────────────────────────────────
// buildRequest：零值字段省略
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_BuildRequestOmitsZeroFields(t *testing.T) {
	n := &HTTPNotifier{}
	alert := scanner.NewAlert{
		AlertID:   "1",
		PatientID: "P1",
		DeviceID:  "DEV1",
		Type:      engine.TypeWearInterrupt,
		Ts:        time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	}
	req := n.buildRequest(alert)
	assert.Equal(t, "", req.SensorPoint)
	assert.Nil(t, req.ThresholdValue)
	assert.Nil(t, req.ActualValue)
	assert.Equal(t, "2026-08-10T12:00:00Z", req.Timestamp)
}

// ─────────────────────────────────────────────────────────────
// accepted=false 视为失败
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_AcceptedFalseTreatedAsFailure(t *testing.T) {
	mock := &mockMsgServer{
		statusCode: 200,
		respBody:   `{"code":0,"message":"success","data":{"accepted":false}}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	queue := &fakeRetryQueue{}
	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
		RetryQueue:    queue,
	})

	n.Notify(context.Background(), makeAlert())
	assert.Equal(t, int32(1), atomic.LoadInt32(&queue.pushN), "accepted=false 入重试队列")
}

// ─────────────────────────────────────────────────────────────
// Prometheus 指标
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_PrometheusMetrics(t *testing.T) {
	mock := newMockMsgServer()
	srv := mock.server()
	defer srv.Close()

	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
	})

	// 重置指标（避免其他测试污染）
	n.Notify(context.Background(), makeAlert())

	v := testutil.ToFloat64(metrics.NotifyTotal.WithLabelValues(metrics.OutcomeSent))
	assert.GreaterOrEqual(t, v, float64(1), "成功推送 sent 指标 ≥1")
}

// ─────────────────────────────────────────────────────────────
// 无重试队列时 Notify 失败仅日志
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_NotifyFailedNoRetryQueue(t *testing.T) {
	mock := &mockMsgServer{statusCode: 500, respBody: `{"code":90001,"message":"error"}`}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
		RetryQueue:    nil, // 无重试队列
	})

	// 不应 panic
	n.Notify(context.Background(), makeAlert())
	assert.EqualValues(t, 1, atomic.LoadInt32(&mock.callCount))
}

// ─────────────────────────────────────────────────────────────
// 重试队列 push 失败 → 日志丢弃
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_NotifyFailedPushError(t *testing.T) {
	mock := &mockMsgServer{statusCode: 500, respBody: `{"code":90001,"message":"error"}`}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	queue := &fakeRetryQueue{pushErr: errors.New("redis down")}
	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
		RetryQueue:    queue,
	})

	// 不应 panic，push 失败仅日志
	n.Notify(context.Background(), makeAlert())
	assert.EqualValues(t, 1, atomic.LoadInt32(&mock.callCount))
	assert.Equal(t, int32(1), atomic.LoadInt32(&queue.pushN), "push 被调用但失败")
	assert.Empty(t, queue.items, "push 失败不入队")
}

// ─────────────────────────────────────────────────────────────
// DrainRetryOnce：Pop 错误
// ─────────────────────────────────────────────────────────────

type errRetryQueue struct{ popErr error }

func (q *errRetryQueue) Push(context.Context, string) error { return nil }
func (q *errRetryQueue) Pop(context.Context) (string, bool, error) {
	return "", false, q.popErr
}
func (q *errRetryQueue) Run(context.Context, time.Duration) {}

func TestHTTPNotifier_DrainRetryOncePopError(t *testing.T) {
	mock := newMockMsgServer()
	srv := mock.server()
	defer srv.Close()

	queue := &errRetryQueue{popErr: errors.New("redis timeout")}
	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
		RetryQueue:    queue,
	})

	_, err := n.DrainRetryOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis timeout")
}

// ─────────────────────────────────────────────────────────────
// doSend：连接拒绝
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_DoSendConnectionRefused(t *testing.T) {
	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: "http://127.0.0.1:1", // 无监听
		Timeout:       100 * time.Millisecond,
	})

	err := n.doSend(context.Background(), []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http post")
}

// ─────────────────────────────────────────────────────────────
// doSend：非 200 响应
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_DoSendNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer srv.Close()

	n := NewHTTPNotifier(HTTPNotifierConfig{MsgServiceURL: srv.URL, Timeout: time.Second})
	err := n.doSend(context.Background(), []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

// ─────────────────────────────────────────────────────────────
// doSend：响应 body 非法 JSON
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_DoSendInvalidResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	n := NewHTTPNotifier(HTTPNotifierConfig{MsgServiceURL: srv.URL, Timeout: time.Second})
	err := n.doSend(context.Background(), []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}

// ─────────────────────────────────────────────────────────────
// doSend：msg-service 返回非零 code
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_DoSendNonZeroCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":50400,"message":"invalid type","data":null}`))
	}))
	defer srv.Close()

	n := NewHTTPNotifier(HTTPNotifierConfig{MsgServiceURL: srv.URL, Timeout: time.Second})
	err := n.doSend(context.Background(), []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error code=50400")
}

// ─────────────────────────────────────────────────────────────
// DrainRetryOnce：非法 JSON 负载
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifier_DrainRetryOnceInvalidPayload(t *testing.T) {
	mock := newMockMsgServer()
	srv := mock.server()
	defer srv.Close()

	queue := &fakeRetryQueue{items: []string{`{not json`}}
	n := NewHTTPNotifier(HTTPNotifierConfig{
		MsgServiceURL: srv.URL,
		Timeout:       time.Second,
		RetryQueue:    queue,
	})

	processed, err := n.DrainRetryOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, processed, "非法负载仍计为已处理（跳过）")
}

// ─────────────────────────────────────────────────────────────
// NoopNotifier 接口兼容
// ─────────────────────────────────────────────────────────────

func TestNoopNotifierImplementsInterface(t *testing.T) {
	var n Notifier = NoopNotifier{}
	// 不应 panic
	n.Notify(context.Background(), makeAlert())
}
