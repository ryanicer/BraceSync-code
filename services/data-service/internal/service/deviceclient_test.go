package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// ─────────────────────────────────────────────────────────────
// T137：devices.last_report_at 回写链路与 HTTPDeviceClient
// ─────────────────────────────────────────────────────────────

// recordingReporter 记录收到的回写请求（device-service /internal 契约替身）
type recordingReporter struct {
	calls []reportCall
	err   error
}

type reportCall struct {
	DeviceID  string
	Timestamp time.Time
	FaultCode int
}

func (r *recordingReporter) Report(_ context.Context, deviceID string, ts time.Time, faultCode int) error {
	r.calls = append(r.calls, reportCall{DeviceID: deviceID, Timestamp: ts, FaultCode: faultCode})
	return r.err
}

// ── HTTPDeviceClient：请求形状与错误映射 ──────────────────────

func TestHTTPDeviceClient_RequestShape(t *testing.T) {
	var gotPath, gotCT string
	var gotBody deviceReportRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":null}`))
	}))
	defer srv.Close()

	ts := time.Date(2026, 9, 12, 3, 26, 40, 0, time.UTC)
	client := NewHTTPDeviceClient(srv.URL, time.Second)
	require.NoError(t, client.Report(context.Background(), "DEV-001", ts, 7))

	assert.Equal(t, "/internal/devices/DEV-001/report", gotPath, "须打到 device-service 既有的状态校正端点")
	assert.Equal(t, "application/json", gotCT)
	assert.Equal(t, ts.Unix(), gotBody.Timestamp, "timestamp 口径为帧采集时刻 Unix 秒")
	assert.Equal(t, 7, gotBody.FaultCode)
}

func TestHTTPDeviceClient_DeviceIDEscaped(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	client := NewHTTPDeviceClient(srv.URL, time.Second)
	require.NoError(t, client.Report(context.Background(), "dev/../x", time.Unix(1, 0), 0))
	assert.Equal(t, "/internal/devices/dev%2F..%2Fx/report", gotURI, "device_id 须经路径转义，不得逃逸出 /report")
}

func TestHTTPDeviceClient_Errors(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"非200", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }},
		{"业务码非0", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"code":40404,"message":"device not registered"}`))
		}},
		{"响应非JSON", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`not-json`)) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(c.handler)
			defer srv.Close()
			client := NewHTTPDeviceClient(srv.URL, time.Second)
			err := client.Report(context.Background(), "DEV-001", time.Now(), 0)
			require.Error(t, err)
		})
	}
}

func TestHTTPDeviceClient_TimeoutAndConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	start := time.Now()
	client := NewHTTPDeviceClient(srv.URL, 50*time.Millisecond)
	require.Error(t, client.Report(context.Background(), "DEV-001", time.Now(), 0))
	assert.Less(t, time.Since(start), 200*time.Millisecond, "熔断超时须先于服务端响应触发")

	broken := NewHTTPDeviceClient("http://127.0.0.1:1", 200*time.Millisecond)
	require.Error(t, broken.Report(context.Background(), "DEV-001", time.Now(), 0))
}

// ── 上报/补传链路触发回写 ─────────────────────────────────────

func TestUploadSingle_WritesBackDeviceReport(t *testing.T) {
	env := newTestEnv()
	rep := &recordingReporter{}
	env.svc.SetDeviceReporter(rep, 0)

	frameTS := fixedNow.Add(-3 * time.Minute)
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, &model.SingleFrameRequest{
		DeviceID: testDevice, Timestamp: frameTS.Unix(), Points: pts(32.5), Battery: 90, FaultCode: 3,
	})
	require.Nil(t, appErr)

	require.Len(t, rep.calls, 1)
	assert.Equal(t, testDevice, rep.calls[0].DeviceID)
	assert.True(t, rep.calls[0].Timestamp.Equal(frameTS), "回写须用帧采集时刻，由 device-service 单调推进")
	assert.Equal(t, 3, rep.calls[0].FaultCode)
}

func TestUploadBatch_WritesBackNewestValidFrame(t *testing.T) {
	env := newTestEnv()
	rep := &recordingReporter{}
	env.svc.SetDeviceReporter(rep, 0)

	oldest := fixedNow.Add(-3 * time.Hour)
	newest := fixedNow.Add(-time.Hour)
	_, appErr := env.svc.UploadBatch(context.Background(), testDevice, &model.BatchRequest{
		DeviceID: testDevice,
		Frames: []model.BatchFrame{
			{Timestamp: oldest.Unix(), Points: pts(11), Battery: 80},
			{Timestamp: newest.Unix(), Points: pts(12), Battery: 79, FaultCode: 5},
			{Timestamp: fixedNow.Add(-2 * time.Hour).Unix(), Points: []float64{1, 2}}, // rejected：points 长度
		},
	})
	require.Nil(t, appErr)

	require.Len(t, rep.calls, 1, "一批补传只回写一次")
	assert.True(t, rep.calls[0].Timestamp.Equal(newest), "取通过校验帧中最新的一帧")
	assert.Equal(t, 5, rep.calls[0].FaultCode)
}

func TestUploadBatch_AllFramesRejected_NoWriteback(t *testing.T) {
	env := newTestEnv()
	rep := &recordingReporter{}
	env.svc.SetDeviceReporter(rep, 0)

	_, appErr := env.svc.UploadBatch(context.Background(), testDevice, &model.BatchRequest{
		DeviceID: testDevice,
		Frames:   []model.BatchFrame{{Timestamp: fixedNow.Add(-time.Hour).Unix(), Points: []float64{1}}},
	})
	require.Nil(t, appErr)
	assert.Empty(t, rep.calls, "无一帧可落库时不得推进 last_report_at")
}

// TestUpload_ReportFailureDoesNotBlockUpload 回写失败只告警：上报成功率优先（架构 §3.4）
func TestUpload_ReportFailureDoesNotBlockUpload(t *testing.T) {
	env := newTestEnv()
	env.svc.SetDeviceReporter(&recordingReporter{err: errors.New("device-service down")}, 0)

	single, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(20)))
	require.Nil(t, appErr)
	require.False(t, single.Duplicated)
	require.Len(t, env.records.rows, 1, "帧仍须落库")

	batch, appErr := env.svc.UploadBatch(context.Background(), testDevice, &model.BatchRequest{
		DeviceID: testDevice,
		Frames:   []model.BatchFrame{{Timestamp: fixedNow.Add(-2 * time.Hour).Unix(), Points: pts(21)}},
	})
	require.Nil(t, appErr)
	assert.Equal(t, 1, batch.Accepted)
}

// TestUpload_WithoutReporterKeepsLegacyBehavior 未配置 DEVICE_SERVICE_URL 时链路照旧成功
func TestUpload_WithoutReporterKeepsLegacyBehavior(t *testing.T) {
	env := newTestEnv()
	require.Nil(t, env.svc.deviceReport)

	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(20)))
	require.Nil(t, appErr)
}

func TestNewestFrame(t *testing.T) {
	base := time.Unix(1700000000, 0)
	frames := []repo.PendingFrame{{Ts: base}, {Ts: base.Add(time.Hour), FaultCode: 2}, {Ts: base.Add(30 * time.Minute)}}

	newest, ok := newestFrame(frames)
	require.True(t, ok)
	assert.True(t, newest.Ts.Equal(base.Add(time.Hour)))
	assert.Equal(t, 2, newest.FaultCode)

	_, ok = newestFrame(nil)
	assert.False(t, ok)
}
