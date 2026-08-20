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

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
	"github.com/bracesync/bracesync/services/data-service/internal/service"
)

// ─────────────────────────────────────────────────────────────
// Fakes（handler 层 httptest 用最小实现）
// ─────────────────────────────────────────────────────────────

type stubRecords struct {
	lastPageSize int
	lastPage     int
	rows         []model.PressureRecord
}

func (s *stubRecords) InsertRecord(_ context.Context, deviceID, patientID string, f repo.PendingFrame) (int64, bool, error) {
	return 42, true, nil
}

func (s *stubRecords) BatchInsert(_ context.Context, _, _ string, frames []repo.PendingFrame) ([]time.Time, error) {
	var ts []time.Time
	for _, f := range frames {
		ts = append(ts, f.Ts)
	}
	return ts, nil
}

func (s *stubRecords) QueryHistory(_ context.Context, patientID string, from, to time.Time, page, pageSize int) ([]model.PressureRecord, int64, error) {
	s.lastPage, s.lastPageSize = page, pageSize
	return s.rows, int64(len(s.rows)), nil
}

type stubDevices struct{ patientByDevice map[string]string }

func (s *stubDevices) GetBinding(_ context.Context, deviceID string) (string, string, bool, error) {
	pid, ok := s.patientByDevice[deviceID]
	if !ok {
		return "", "", false, nil
	}
	return pid, "online", true, nil
}

func (s *stubDevices) GetDeviceByPatient(_ context.Context, patientID string) (string, string, bool, error) {
	for dev, pid := range s.patientByDevice {
		if pid == patientID {
			return dev, "online", true, nil
		}
	}
	return "", "", false, nil
}

type stubConfigs struct{}

func (stubConfigs) GetDeviceConfig(context.Context) (int, int, error) { return 30, 1, nil }

type stubCache struct{}

func (stubCache) SetLastSeen(context.Context, string, time.Time) error { return nil }
func (stubCache) GetLastSeen(context.Context, string) (time.Time, bool, error) {
	return time.Now(), true, nil
}
func (stubCache) SetRealtimeFrame(context.Context, string, string) error { return nil }
func (stubCache) GetRealtimeFrame(context.Context, string) (string, error) {
	return "", nil
}
func (stubCache) ApplyStatToday(context.Context, string, int, float64, string, int, time.Time) error {
	return nil
}
func (stubCache) GetStatToday(context.Context, string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (stubCache) PushAlertPending(context.Context, string) error { return nil }
func (stubCache) EnqueueRollup(context.Context, string, string, string) (bool, error) {
	return true, nil
}
func (stubCache) DequeueRollup(context.Context) (string, error) { return "", nil }

type stubAlerts struct{}

func (stubAlerts) Evaluate(context.Context, *service.AlertEvalRequest) (*service.AlertEvalResult, error) {
	return &service.AlertEvalResult{}, nil
}

// ─────────────────────────────────────────────────────────────
// 基座
// ─────────────────────────────────────────────────────────────

const (
	hDevice  = "PRS-ML05-RC-20260808001"
	hPatient = "P20260001"
)

type testServer struct {
	router  http.Handler
	records *stubRecords
	svc     *service.RecordService
}

func newTestServer(limiter *service.RateLimiter) *testServer {
	records := &stubRecords{}
	devices := &stubDevices{patientByDevice: map[string]string{hDevice: hPatient}}
	if limiter == nil {
		limiter = service.NewRateLimiter(1e9, 1e9, 1e9, 1e9)
	}
	svc := service.NewRecordService(records, devices, stubConfigs{}, stubCache{}, stubAlerts{}, limiter)
	h := New(svc)
	return &testServer{router: h.Router(), records: records, svc: svc}
}

func doReq(t *testing.T, srv *testServer, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) (code int, data map[string]any) {
	t.Helper()
	var resp struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Code, resp.Data
}

func validFrameBody(ts time.Time) string {
	points := make([]float64, 20)
	for i := range points {
		points[i] = float64(i) + 0.5
	}
	pointsJSON, _ := json.Marshal(points)
	return fmt.Sprintf(`{"device_id":%q,"timestamp":%d,"points":%s,"battery":87,"firmware":"v1.2.0"}`,
		hDevice, ts.Unix(), pointsJSON)
}

// ─────────────────────────────────────────────────────────────
// 用例
// ─────────────────────────────────────────────────────────────

func TestHealthz(t *testing.T) {
	srv := newTestServer(nil)
	w := doReq(t, srv, http.MethodGet, "/healthz", "", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUploadSingle_OK(t *testing.T) {
	srv := newTestServer(nil)
	body := validFrameBody(time.Now().Add(-time.Minute))

	// body device_id 回退（gateway 身份头未就绪的联调路径）
	w := doReq(t, srv, http.MethodPost, "/api/v1/device/records", body, nil)
	require.Equal(t, http.StatusOK, w.Code)
	code, data := decodeBody(t, w)
	assert.Equal(t, 0, code)
	assert.Equal(t, "42", data["record_id"])

	// X-Device-Id 头优先
	w = doReq(t, srv, http.MethodPost, "/api/v1/device/records", body, map[string]string{"X-Device-Id": hDevice})
	assert.Equal(t, http.StatusOK, w.Code)

	// 头体不一致 → 20403
	w = doReq(t, srv, http.MethodPost, "/api/v1/device/records", body, map[string]string{"X-Device-Id": "OTHER"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	code, _ = decodeBody(t, w)
	assert.Equal(t, model.CodeDeviceIDMismatch, code)
}

func TestUploadSingle_InvalidBody(t *testing.T) {
	srv := newTestServer(nil)
	w := doReq(t, srv, http.MethodPost, "/api/v1/device/records", "{bad-json", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	code, _ := decodeBody(t, w)
	assert.Equal(t, model.CodeInvalidParam, code)
}

func TestUploadSingle_DeviceNotFound(t *testing.T) {
	srv := newTestServer(nil)
	points := make([]float64, 20)
	pointsJSON, _ := json.Marshal(points)
	body := fmt.Sprintf(`{"device_id":"NO-SUCH","timestamp":%d,"points":%s,"battery":1}`, time.Now().Unix(), pointsJSON)
	w := doReq(t, srv, http.MethodPost, "/api/v1/device/records", body, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	code, _ := decodeBody(t, w)
	assert.Equal(t, model.CodeDeviceNotFound, code)
}

func TestUploadSingle_RateLimitedWithRetryAfter(t *testing.T) {
	srv := newTestServer(service.NewRateLimiter(0.5, 1, 0.5, 1))
	body := validFrameBody(time.Now().Add(-time.Minute))

	w := doReq(t, srv, http.MethodPost, "/api/v1/device/records", body, nil)
	require.Equal(t, http.StatusOK, w.Code)

	w = doReq(t, srv, http.MethodPost, "/api/v1/device/records", body, nil)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "2", w.Header().Get("Retry-After"))
	code, _ := decodeBody(t, w)
	assert.Equal(t, model.CodeRateLimited, code)
}

func TestUploadBatch_OK(t *testing.T) {
	srv := newTestServer(nil)
	ts := time.Now().Add(-2 * time.Hour)
	points := make([]float64, 20)
	pointsJSON, _ := json.Marshal(points)
	body := fmt.Sprintf(`{"device_id":%q,"frames":[{"timestamp":%d,"points":%s,"battery":80}],"firmware":"v1.2.0"}`,
		hDevice, ts.Unix(), pointsJSON)

	w := doReq(t, srv, http.MethodPost, "/api/v1/device/records/batch", body, nil)
	require.Equal(t, http.StatusOK, w.Code)
	code, data := decodeBody(t, w)
	assert.Equal(t, 0, code)
	assert.EqualValues(t, 1, data["accepted"])
	assert.EqualValues(t, 0, data["duplicated"])
}

func TestUploadBatch_TooManyFrames(t *testing.T) {
	srv := newTestServer(nil)
	var frames []string
	for i := 0; i <= model.MaxBatchFrames; i++ {
		frames = append(frames, fmt.Sprintf(`{"timestamp":%d,"points":[],"battery":1}`, time.Now().Add(-time.Duration(i)*time.Minute).Unix()))
	}
	body := fmt.Sprintf(`{"device_id":%q,"frames":[%s]}`, hDevice, strings.Join(frames, ","))
	w := doReq(t, srv, http.MethodPost, "/api/v1/device/records/batch", body, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	code, _ := decodeBody(t, w)
	assert.Equal(t, model.CodeInvalidParam, code)
}

func TestGetHistory_PaginationClamp(t *testing.T) {
	srv := newTestServer(nil)

	// 缺 date → 30001
	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/records?period=day", "", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	code, _ := decodeBody(t, w)
	assert.Equal(t, model.CodeQueryParam, code)

	// 非法 page → 30001
	w = doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/records?date=2026-08-08&page=0", "", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// pageSize 超上限截断为 100（架构 §3.5）
	w = doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/records?date=2026-08-08&pageSize=500", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 100, srv.records.lastPageSize)
	assert.Equal(t, 1, srv.records.lastPage)

	// 默认分页 page=1 pageSize=20
	w = doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/records?date=2026-08-08", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 20, srv.records.lastPageSize)
	code, data := decodeBody(t, w)
	assert.Equal(t, 0, code)
	assert.NotNil(t, data["list"])
}

func TestGetRealtime_OK(t *testing.T) {
	srv := newTestServer(nil)
	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/realtime", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	code, data := decodeBody(t, w)
	assert.Equal(t, 0, code)
	assert.NotEmpty(t, data["status"])
	assert.NotNil(t, data["pressureRecords"])
	assert.NotNil(t, data["alerts"])
}

// 错误分支兜底：存储异常 → 90001 + HTTP 500
func TestErrorResponseShape(t *testing.T) {
	records := &errRecords{}
	svc := service.NewRecordService(records, &stubDevices{patientByDevice: map[string]string{hDevice: hPatient}},
		stubConfigs{}, stubCache{}, stubAlerts{}, service.NewRateLimiter(1e9, 1e9, 1e9, 1e9))
	router := New(svc).Router()

	body := validFrameBody(time.Now().Add(-time.Minute))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/device/records", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	code, _ := decodeBody(t, w)
	assert.Equal(t, model.CodeInternal, code)
}

type errRecords struct{}

func (errRecords) InsertRecord(context.Context, string, string, repo.PendingFrame) (int64, bool, error) {
	return 0, false, errors.New("db down")
}
func (errRecords) BatchInsert(context.Context, string, string, []repo.PendingFrame) ([]time.Time, error) {
	return nil, errors.New("db down")
}
func (errRecords) QueryHistory(context.Context, string, time.Time, time.Time, int, int) ([]model.PressureRecord, int64, error) {
	return nil, 0, errors.New("db down")
}
