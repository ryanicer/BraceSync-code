package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/metrics"
	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// ─────────────────────────────────────────────────────────────
// Fakes（内存实现，白盒单测）
// ─────────────────────────────────────────────────────────────

type fakeRecords struct {
	mu         sync.Mutex
	nextID     int64
	rows       []model.PressureRecord
	insertErr  error
	batchErr   error
	historyErr error
}

func newFakeRecords() *fakeRecords { return &fakeRecords{nextID: 1000} }

func (f *fakeRecords) InsertRecord(_ context.Context, deviceID, patientID string, p repo.PendingFrame) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return 0, false, f.insertErr
	}
	for _, r := range f.rows {
		if r.DeviceID == deviceID && r.Ts.Equal(p.Ts) {
			return 0, false, nil // 幂等命中
		}
	}
	f.nextID++
	rec := model.PressureRecord{
		RecordID: f.nextID, DeviceID: deviceID, PatientID: patientID,
		Ts: p.Ts, Points: p.Points, UploadTime: time.Now().UTC(),
	}
	for _, v := range p.Points {
		if v > rec.MaxPressure {
			rec.MaxPressure = v
		}
	}
	f.rows = append(f.rows, rec)
	return rec.RecordID, true, nil
}

func (f *fakeRecords) BatchInsert(_ context.Context, deviceID, patientID string, frames []repo.PendingFrame) ([]time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.batchErr != nil {
		return nil, f.batchErr
	}
	var accepted []time.Time
	for _, p := range frames {
		dup := false
		for _, r := range f.rows {
			if r.DeviceID == deviceID && r.Ts.Equal(p.Ts) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		f.nextID++
		f.rows = append(f.rows, model.PressureRecord{
			RecordID: f.nextID, DeviceID: deviceID, PatientID: patientID, Ts: p.Ts, Points: p.Points,
		})
		accepted = append(accepted, p.Ts)
	}
	return accepted, nil
}

func (f *fakeRecords) QueryHistory(_ context.Context, patientID string, from, to time.Time, page, pageSize int) ([]model.PressureRecord, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.historyErr != nil {
		return nil, 0, f.historyErr
	}
	var matched []model.PressureRecord
	for _, r := range f.rows {
		if r.PatientID == patientID && !r.Ts.Before(from) && r.Ts.Before(to) {
			matched = append(matched, r)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Ts.After(matched[j].Ts) })
	total := int64(len(matched))
	start := (page - 1) * pageSize
	if start >= len(matched) {
		return nil, total, nil
	}
	end := start + pageSize
	if end > len(matched) {
		end = len(matched)
	}
	return matched[start:end], total, nil
}

type fakeDevices struct {
	bindings  map[string][2]string // deviceID → {patientID, status}
	byPatient map[string][2]string // patientID → {deviceID, status}
	err       error
}

func newFakeDevices() *fakeDevices {
	return &fakeDevices{bindings: map[string][2]string{}, byPatient: map[string][2]string{}}
}

func (f *fakeDevices) bind(deviceID, patientID, status string) {
	f.bindings[deviceID] = [2]string{patientID, status}
	f.byPatient[patientID] = [2]string{deviceID, status}
}

func (f *fakeDevices) GetBinding(_ context.Context, deviceID string) (string, string, bool, error) {
	if f.err != nil {
		return "", "", false, f.err
	}
	v, ok := f.bindings[deviceID]
	return v[0], v[1], ok, nil
}

func (f *fakeDevices) GetDeviceByPatient(_ context.Context, patientID string) (string, string, bool, error) {
	if f.err != nil {
		return "", "", false, f.err
	}
	v, ok := f.byPatient[patientID]
	return v[0], v[1], ok, nil
}

type fakeConfigs struct {
	interval, version int
	err               error
	calls             int
}

func (f *fakeConfigs) GetDeviceConfig(context.Context) (int, int, error) {
	f.calls++
	if f.err != nil {
		return 0, 0, f.err
	}
	return f.interval, f.version, nil
}

type statEntry struct {
	wear   int
	max    float64
	point  string
	abn    int
	expire time.Time
}

type fakeCache struct {
	mu       sync.Mutex
	lastseen map[string]time.Time
	rtFrame  map[string]string
	stat     map[string]*statEntry
	pending  []string
	rollup   []string
	markers  map[string]bool

	errLastSeen error
	errFrame    error
	errStat     error
	errPending  error
	errRollup   error
	errGetStat  error
	errGetFrame error
	errGetLS    error
}

func newFakeCache() *fakeCache {
	return &fakeCache{
		lastseen: map[string]time.Time{}, rtFrame: map[string]string{},
		stat: map[string]*statEntry{}, markers: map[string]bool{},
	}
}

func (f *fakeCache) SetLastSeen(_ context.Context, deviceID string, ts time.Time) error {
	if f.errLastSeen != nil {
		return f.errLastSeen
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastseen[deviceID] = ts
	return nil
}

func (f *fakeCache) GetLastSeen(_ context.Context, deviceID string) (time.Time, bool, error) {
	if f.errGetLS != nil {
		return time.Time{}, false, f.errGetLS
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.lastseen[deviceID]
	return v, ok, nil
}

func (f *fakeCache) SetRealtimeFrame(_ context.Context, deviceID, frameJSON string) error {
	if f.errFrame != nil {
		return f.errFrame
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rtFrame[deviceID] = frameJSON
	return nil
}

func (f *fakeCache) GetRealtimeFrame(_ context.Context, deviceID string) (string, error) {
	if f.errGetFrame != nil {
		return "", f.errGetFrame
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rtFrame[deviceID], nil
}

func (f *fakeCache) ApplyStatToday(_ context.Context, patientID string, wearMinutes int, maxPressure float64, maxPoint string, abnormalDelta int, expireAt time.Time) error {
	if f.errStat != nil {
		return f.errStat
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.stat[patientID]
	if !ok {
		e = &statEntry{}
		f.stat[patientID] = e
	}
	e.wear += wearMinutes
	e.abn += abnormalDelta
	if maxPressure > e.max {
		e.max = maxPressure
		e.point = maxPoint
	}
	e.expire = expireAt
	return nil
}

func (f *fakeCache) GetStatToday(_ context.Context, patientID string) (map[string]string, error) {
	if f.errGetStat != nil {
		return nil, f.errGetStat
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	if e, ok := f.stat[patientID]; ok {
		out["wear_minutes"] = strconv.Itoa(e.wear)
		out["max_pressure"] = strconv.FormatFloat(e.max, 'f', -1, 64)
		out["max_point"] = e.point
		out["abnormal_count"] = strconv.Itoa(e.abn)
	}
	return out, nil
}

func (f *fakeCache) PushAlertPending(_ context.Context, payloadJSON string) error {
	if f.errPending != nil {
		return f.errPending
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, payloadJSON)
	return nil
}

func (f *fakeCache) EnqueueRollup(_ context.Context, patientID, date string, payloadJSON string) (bool, error) {
	if f.errRollup != nil {
		return false, f.errRollup
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := patientID + "|" + date
	if f.markers[key] {
		return false, nil // 幂等跳过
	}
	f.markers[key] = true
	f.rollup = append(f.rollup, payloadJSON)
	return true, nil
}

// DequeueRollup T021：RPOP rollup 队列（从 rollup slice 头部取出）
func (f *fakeCache) DequeueRollup(_ context.Context) (string, error) {
	if f.errRollup != nil {
		return "", f.errRollup
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rollup) == 0 {
		return "", nil
	}
	v := f.rollup[0]
	f.rollup = f.rollup[1:]
	return v, nil
}

type fakeAlerts struct {
	mu    sync.Mutex
	errs  []error
	res   []*AlertEvalResult
	reqs  []*AlertEvalRequest
	calls int
}

func (f *fakeAlerts) push(err error, r *AlertEvalResult) {
	f.errs = append(f.errs, err)
	f.res = append(f.res, r)
}

func (f *fakeAlerts) Evaluate(_ context.Context, req *AlertEvalRequest) (*AlertEvalResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.reqs = append(f.reqs, req)
	if len(f.errs) == 0 {
		return &AlertEvalResult{}, nil
	}
	err := f.errs[0]
	r := f.res[0]
	f.errs, f.res = f.errs[1:], f.res[1:]
	if r == nil {
		r = &AlertEvalResult{}
	}
	return r, err
}

// ─────────────────────────────────────────────────────────────
// 测试基座
// ─────────────────────────────────────────────────────────────

const (
	testDevice  = "PRS-ML05-RC-20260808001"
	testPatient = "P20260001"
)

var fixedNow = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

type testEnv struct {
	svc     *RecordService
	records *fakeRecords
	devices *fakeDevices
	configs *fakeConfigs
	cache   *fakeCache
	alerts  *fakeAlerts
	limiter *RateLimiter
}

// newTestEnv 默认：限流放开、配置 30/1、设备已绑定
func newTestEnv() *testEnv {
	env := &testEnv{
		records: newFakeRecords(),
		devices: newFakeDevices(),
		configs: &fakeConfigs{interval: 30, version: 1},
		cache:   newFakeCache(),
		alerts:  &fakeAlerts{},
		limiter: NewRateLimiter(1e9, 1e9, 1e9, 1e9),
	}
	env.devices.bind(testDevice, testPatient, "online")
	env.svc = NewRecordService(env.records, env.devices, env.configs, env.cache, env.alerts, env.limiter)
	env.svc.now = func() time.Time { return fixedNow }
	return env
}

// pts 生成 20 点压力值：前 n 个取 values，其余 0
func pts(values ...float64) []float64 {
	out := make([]float64, model.PointCount)
	copy(out, values)
	return out
}

func singleReq(ts time.Time, points []float64) *model.SingleFrameRequest {
	return &model.SingleFrameRequest{
		DeviceID:  testDevice,
		Timestamp: ts.Unix(),
		Points:    points,
		Battery:   87,
		Firmware:  "v1.2.0",
	}
}

// ─────────────────────────────────────────────────────────────
// 公共辅助函数
// ─────────────────────────────────────────────────────────────

func TestResolveDeviceID(t *testing.T) {
	id, appErr := resolveDeviceID("HDR", "")
	require.Nil(t, appErr)
	assert.Equal(t, "HDR", id)

	id, appErr = resolveDeviceID("HDR", "HDR")
	require.Nil(t, appErr)
	assert.Equal(t, "HDR", id)

	_, appErr = resolveDeviceID("HDR", "OTHER")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeDeviceIDMismatch, appErr.Code)

	id, appErr = resolveDeviceID("", "BODY")
	require.Nil(t, appErr)
	assert.Equal(t, "BODY", id)

	_, appErr = resolveDeviceID("", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code)
}

func TestValidateTimestamp(t *testing.T) {
	ts, appErr := validateTimestamp(fixedNow.Unix(), fixedNow)
	require.Nil(t, appErr)
	assert.True(t, ts.Equal(fixedNow))

	_, appErr = validateTimestamp(0, fixedNow)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeBadTimestamp, appErr.Code)

	// 早于下界 2026-01-01
	_, appErr = validateTimestamp(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC).Unix(), fixedNow)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeBadTimestamp, appErr.Code)

	// 未来超过 1h
	_, appErr = validateTimestamp(fixedNow.Add(2*time.Hour).Unix(), fixedNow)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeBadTimestamp, appErr.Code)
}

func TestPeriodRange(t *testing.T) {
	// day
	from, to, appErr := periodRange("day", "2026-08-08")
	require.Nil(t, appErr)
	assert.Equal(t, time.Date(2026, 8, 8, 0, 0, 0, 0, model.CSTZone()).UTC(), from)
	assert.Equal(t, time.Date(2026, 8, 9, 0, 0, 0, 0, model.CSTZone()).UTC(), to)

	// week：2026-08-08 是周六 → 周一为 08-03
	from, to, appErr = periodRange("week", "2026-08-08")
	require.Nil(t, appErr)
	assert.Equal(t, time.Date(2026, 8, 3, 0, 0, 0, 0, model.CSTZone()).UTC(), from)
	assert.Equal(t, 7*24*time.Hour, to.Sub(from))

	// month
	from, to, appErr = periodRange("month", "2026-08-15")
	require.Nil(t, appErr)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, model.CSTZone()).UTC(), from)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, model.CSTZone()).UTC(), to)

	_, _, appErr = periodRange("year", "2026-08-08")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeQueryParam, appErr.Code)

	_, _, appErr = periodRange("day", "bad-date")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeQueryParam, appErr.Code)
}

func TestEndOfTodayCST(t *testing.T) {
	// UTC 2026-08-08 12:00 = CST 20:00 → 当日 24:00 = CST 08-09 00:00
	end := endOfTodayCST(fixedNow)
	assert.Equal(t, time.Date(2026, 8, 9, 0, 0, 0, 0, model.CSTZone()), end)
	// UTC 凌晨（CST 前一日）：UTC 08-08 15:30 = CST 08-08 23:30 → 仍是 CST 08-09 切日
	end = endOfTodayCST(time.Date(2026, 8, 8, 15, 30, 0, 0, time.UTC))
	assert.Equal(t, time.Date(2026, 8, 9, 0, 0, 0, 0, model.CSTZone()), end)
	// UTC 08-08 17:00 = CST 08-09 01:00 → 切日为 CST 08-10
	end = endOfTodayCST(time.Date(2026, 8, 8, 17, 0, 0, 0, time.UTC))
	assert.Equal(t, time.Date(2026, 8, 10, 0, 0, 0, 0, model.CSTZone()), end)
}

// ─────────────────────────────────────────────────────────────
// 单帧上报
// ─────────────────────────────────────────────────────────────

func TestUploadSingle_Success(t *testing.T) {
	env := newTestEnv()
	ts := fixedNow.Add(-5 * time.Minute)
	points := pts(12.3, 20.1, 30.2) // max=30.2 @P03，>0.5N 佩戴帧

	resp, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(ts, points))
	require.Nil(t, appErr)
	assert.False(t, resp.Duplicated)
	assert.NotEmpty(t, resp.RecordID)
	assert.Equal(t, 30, resp.Config.IntervalMinutes)
	assert.Equal(t, 1, resp.Config.ConfigVersion)

	// 落库
	require.Len(t, env.records.rows, 1)
	assert.Equal(t, testPatient, env.records.rows[0].PatientID)

	// Redis 三写
	assert.True(t, env.cache.lastseen[testDevice].Equal(ts))
	assert.NotEmpty(t, env.cache.rtFrame[testDevice])
	st := env.cache.stat[testPatient]
	require.NotNil(t, st)
	assert.Equal(t, 30, st.wear) // 佩戴帧累加一个采集间隔
	assert.InDelta(t, 30.2, st.max, 0.01)
	assert.Equal(t, "P03", st.point)
	assert.Equal(t, endOfTodayCST(fixedNow), st.expire)

	// 内联告警被调用且非补传
	require.Equal(t, 1, env.alerts.calls)
	assert.False(t, env.alerts.reqs[0].IsBackfill)
	assert.Empty(t, env.cache.pending)
}

func TestUploadSingle_NotWearing(t *testing.T) {
	env := newTestEnv()
	resp, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(0.1, 0.2)))
	require.Nil(t, appErr)
	assert.False(t, resp.Duplicated)
	st := env.cache.stat[testPatient]
	require.NotNil(t, st)
	assert.Equal(t, 0, st.wear) // ≤0.5N 非佩戴帧，不累计佩戴分钟
	assert.InDelta(t, 0.2, st.max, 0.001)
}

func TestUploadSingle_IdempotentDuplicate(t *testing.T) {
	env := newTestEnv()
	req := singleReq(fixedNow.Add(-time.Minute), pts(10, 20))

	resp1, appErr := env.svc.UploadSingle(context.Background(), testDevice, req)
	require.Nil(t, appErr)
	assert.False(t, resp1.Duplicated)

	// 重复帧：不重复落库、不重复统计、不重复评估
	resp2, appErr := env.svc.UploadSingle(context.Background(), testDevice, req)
	require.Nil(t, appErr)
	assert.True(t, resp2.Duplicated)
	assert.Len(t, env.records.rows, 1)
	assert.Equal(t, 1, env.alerts.calls)
	assert.Equal(t, 30, env.cache.stat[testPatient].wear)
}

func TestUploadSingle_DeviceErrors(t *testing.T) {
	// 未注册 20404
	env := newTestEnv()
	_, appErr := env.svc.UploadSingle(context.Background(), "UNKNOWN", &model.SingleFrameRequest{DeviceID: "UNKNOWN", Timestamp: fixedNow.Unix(), Points: pts(1)})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeDeviceNotFound, appErr.Code)
	assert.Equal(t, 404, appErr.HTTPStatus)

	// 未绑定 20409
	env.devices.bindings[testDevice] = [2]string{"", "unbound"}
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow, pts(1)))
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeDeviceUnbound, appErr.Code)

	// 查询失败 90001
	env.devices.err = errors.New("db down")
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow, pts(1)))
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code)
}

func TestUploadSingle_ValidationErrors(t *testing.T) {
	env := newTestEnv()

	// points 长度错误 20400
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, &model.SingleFrameRequest{DeviceID: testDevice, Timestamp: fixedNow.Unix(), Points: []float64{1, 2}})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code)

	// timestamp 非法 20402
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, &model.SingleFrameRequest{DeviceID: testDevice, Timestamp: 1, Points: pts(1)})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeBadTimestamp, appErr.Code)

	// 分区缺失 → 20402
	env.records.insertErr = repo.ErrNoPartition
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow, pts(1)))
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeBadTimestamp, appErr.Code)

	// 其他存储错误 → 90001
	env.records.insertErr = errors.New("connection refused")
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow, pts(1)))
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code)
}

func TestUploadSingle_RateLimited(t *testing.T) {
	env := newTestEnv()
	env.svc.limiter = NewRateLimiter(0.5, 1, 0.5, 1) // burst=1
	req := singleReq(fixedNow.Add(-time.Minute), pts(5))

	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, req)
	require.Nil(t, appErr)
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, req)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeRateLimited, appErr.Code)
	assert.Equal(t, 429, appErr.HTTPStatus)
	assert.Equal(t, 2, appErr.RetryAfterSec)
}

func TestUploadSingle_AlertDegradeAndRecover(t *testing.T) {
	env := newTestEnv()
	// 第一次评估失败 → 降级入 pending；第二次命中告警 → abnormal+1
	env.alerts.push(errors.New("timeout"), nil)
	env.alerts.push(nil, &AlertEvalResult{ShouldAlert: true, AlertType: "pressure_high", SensorPoint: "P03"})

	degradeBefore := testutil.ToFloat64(metrics.AlertDegradeTotal)

	req1 := singleReq(fixedNow.Add(-30*time.Minute), pts(50))
	resp, appErr := env.svc.UploadSingle(context.Background(), testDevice, req1)
	require.Nil(t, appErr, "降级不阻塞上报成功返回")
	assert.False(t, resp.Duplicated)
	require.Len(t, env.cache.pending, 1, "降级帧引用写入 alert:pending")
	assert.InDelta(t, degradeBefore+1, testutil.ToFloat64(metrics.AlertDegradeTotal), 0.001, "降级计数指标 +1")

	var item pendingAlertItem
	require.NoError(t, json.Unmarshal([]byte(env.cache.pending[0]), &item))
	assert.Equal(t, testDevice, item.Frame.DeviceID)
	assert.Equal(t, testPatient, item.Frame.PatientID)
	assert.False(t, item.Frame.IsBackfill)

	// 下一帧评估恢复且命中
	req2 := singleReq(fixedNow.Add(-time.Minute), pts(52))
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, req2)
	require.Nil(t, appErr)
	assert.Equal(t, 1, env.cache.stat[testPatient].abn, "命中告警 → 今日异常数 +1")
	assert.Zero(t, env.svc.degradedSinceUnixNano, "恢复后降级窗口关闭")
	assert.Equal(t, 0.0, testutil.ToFloat64(metrics.AlertDegradedSeconds), "恢复后降级时长指标归零")
}

func TestUploadSingle_DegradeOver5MinAlarm(t *testing.T) {
	env := newTestEnv()
	env.alerts.push(errors.New("down"), nil)
	// 预置降级已持续 6 分钟 → 触发 >5min 可观测分支
	env.svc.degradedSinceUnixNano = fixedNow.Add(-6 * time.Minute).UnixNano()

	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(5)))
	require.Nil(t, appErr)
	assert.Len(t, env.cache.pending, 1)
	assert.NotZero(t, env.svc.degradedSinceUnixNano)
	assert.GreaterOrEqual(t, testutil.ToFloat64(metrics.AlertDegradedSeconds), 360.0,
		"连续降级时长指标 ≥6min，供 Prometheus 配 >5min 运维告警规则")
}

func TestUploadSingle_PushPendingFailsStillSucceeds(t *testing.T) {
	env := newTestEnv()
	env.alerts.push(errors.New("down"), nil)
	env.cache.errPending = errors.New("redis down")

	resp, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(5)))
	require.Nil(t, appErr, "帧已落库，pending 写入失败不判上报失败")
	assert.False(t, resp.Duplicated)
	assert.Empty(t, env.cache.pending)
}

func TestUploadSingle_RedisFailures(t *testing.T) {
	// lastseen 写失败 → 90001
	env := newTestEnv()
	env.cache.errLastSeen = errors.New("redis down")
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow, pts(5)))
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code)

	// rt:frame 写失败 → 90001
	env = newTestEnv()
	env.cache.errFrame = errors.New("redis down")
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow, pts(5)))
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code)

	// stat:today 写失败 → 90001
	env = newTestEnv()
	env.cache.errStat = errors.New("redis down")
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow, pts(5)))
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code)
}

func TestUploadSingle_ConfigFallback(t *testing.T) {
	env := newTestEnv()
	env.configs.err = errors.New("config read failed")
	resp, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(5)))
	require.Nil(t, appErr)
	assert.Equal(t, 30, resp.Config.IntervalMinutes)
	assert.Equal(t, 1, resp.Config.ConfigVersion)
}

// ─────────────────────────────────────────────────────────────
// 批量补传
// ─────────────────────────────────────────────────────────────

func TestUploadBatch_SuccessWithMixedFrames(t *testing.T) {
	env := newTestEnv()
	// 预置一帧制造幂等去重
	preTS := fixedNow.Add(-24 * time.Hour)
	_, inserted, err := env.records.InsertRecord(context.Background(), testDevice, testPatient, repo.PendingFrame{Ts: preTS, Points: [model.PointCount]float32{1}})
	require.NoError(t, err)
	require.True(t, inserted)

	req := &model.BatchRequest{
		DeviceID: testDevice,
		Firmware: "v1.2.0",
		Frames: []model.BatchFrame{
			{Timestamp: fixedNow.Add(-24 * time.Hour).Unix(), Points: pts(11), Battery: 80},         // 幂等去重
			{Timestamp: fixedNow.Add(-23 * time.Hour).Unix(), Points: pts(12), Battery: 79},         // accepted
			{Timestamp: fixedNow.Add(-22 * time.Hour).Unix(), Points: []float64{1, 2}, Battery: 78}, // rejected：points 长度
			{Timestamp: fixedNow.Add(-8 * 24 * time.Hour).Unix(), Points: pts(13), Battery: 77},     // rejected：>7 天
			{Timestamp: 1, Points: pts(14), Battery: 76},                                            // rejected：timestamp 非法
		},
	}

	resp, appErr := env.svc.UploadBatch(context.Background(), testDevice, req)
	require.Nil(t, appErr)
	assert.Equal(t, 1, resp.Accepted)
	assert.Equal(t, 1, resp.Duplicated)
	require.Len(t, resp.Rejected, 3)
	assert.Equal(t, 2, resp.Rejected[0].Index)
	assert.Equal(t, model.CodeInvalidParam, resp.Rejected[0].Code)
	assert.Equal(t, 3, resp.Rejected[1].Index)
	assert.Equal(t, model.CodeBadTimestamp, resp.Rejected[1].Code)
	assert.Equal(t, 4, resp.Rejected[2].Index)

	// 补传跳过实时告警评估
	assert.Zero(t, env.alerts.calls, "batch must skip inline alert evaluation")
	assert.Empty(t, env.cache.pending)

	// lastseen 置为当前时刻（设备刚恢复联网）
	assert.True(t, env.cache.lastseen[testDevice].Equal(fixedNow))

	// 受影响日期投递 rollup（accepted 帧与预置帧不同日：仅 accepted 参与）
	require.Len(t, env.cache.rollup, 1)
	var task rollupTask
	require.NoError(t, json.Unmarshal([]byte(env.cache.rollup[0]), &task))
	assert.Equal(t, testPatient, task.PatientID)
	assert.Equal(t, fixedNow.Add(-23*time.Hour).In(model.CSTZone()).Format("2006-01-02"), task.Date)

	// 补传不触碰 stat:today（历史数据归 rollup 重算）
	assert.Empty(t, env.cache.stat)
}

func TestUploadBatch_Limits(t *testing.T) {
	env := newTestEnv()

	_, appErr := env.svc.UploadBatch(context.Background(), testDevice, &model.BatchRequest{DeviceID: testDevice})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code)

	tooMany := make([]model.BatchFrame, model.MaxBatchFrames+1)
	for i := range tooMany {
		tooMany[i] = model.BatchFrame{Timestamp: fixedNow.Add(-time.Duration(i) * time.Minute).Unix(), Points: pts(1)}
	}
	_, appErr = env.svc.UploadBatch(context.Background(), testDevice, &model.BatchRequest{DeviceID: testDevice, Frames: tooMany})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code)
}

func TestUploadBatch_RateLimitIndependentChannel(t *testing.T) {
	env := newTestEnv()
	env.svc.limiter = NewRateLimiter(1e9, 1e9, 0.5, 1) // 补传 burst=1，实时放开

	frames := []model.BatchFrame{{Timestamp: fixedNow.Add(-time.Hour).Unix(), Points: pts(1)}}
	_, appErr := env.svc.UploadBatch(context.Background(), testDevice, &model.BatchRequest{DeviceID: testDevice, Frames: frames})
	require.Nil(t, appErr)
	_, appErr = env.svc.UploadBatch(context.Background(), testDevice, &model.BatchRequest{DeviceID: testDevice, Frames: frames})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeRateLimited, appErr.Code)

	// 实时通道不受补传限流影响
	_, appErr = env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(5)))
	require.Nil(t, appErr, "batch rate limit must not affect realtime channel")
}

func TestUploadBatch_RollupIdempotentEnqueue(t *testing.T) {
	env := newTestEnv()
	mk := func(offset time.Duration) model.BatchFrame {
		return model.BatchFrame{Timestamp: fixedNow.Add(offset).Unix(), Points: pts(1)}
	}
	// 同一天的两帧 → 仅一条 rollup 任务
	req := &model.BatchRequest{DeviceID: testDevice, Frames: []model.BatchFrame{mk(-2 * time.Hour), mk(-time.Hour)}}
	_, appErr := env.svc.UploadBatch(context.Background(), testDevice, req)
	require.Nil(t, appErr)
	assert.Len(t, env.cache.rollup, 1)

	// 再次补传同一天（新帧）→ 标记已存在，幂等跳过
	req2 := &model.BatchRequest{DeviceID: testDevice, Frames: []model.BatchFrame{mk(-30 * time.Minute)}}
	_, appErr = env.svc.UploadBatch(context.Background(), testDevice, req2)
	require.Nil(t, appErr)
	assert.Len(t, env.cache.rollup, 1, "same-date rollup task must be deduplicated")
}

func TestUploadBatch_DeviceErrors(t *testing.T) {
	env := newTestEnv()
	frames := []model.BatchFrame{{Timestamp: fixedNow.Add(-time.Hour).Unix(), Points: pts(1)}}

	_, appErr := env.svc.UploadBatch(context.Background(), "UNKNOWN", &model.BatchRequest{DeviceID: "UNKNOWN", Frames: frames})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeDeviceNotFound, appErr.Code)

	env.devices.bindings[testDevice] = [2]string{"", "unbound"}
	_, appErr = env.svc.UploadBatch(context.Background(), testDevice, &model.BatchRequest{DeviceID: testDevice, Frames: frames})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeDeviceUnbound, appErr.Code)
}

// ─────────────────────────────────────────────────────────────
// 历史查询 + 实时快照
// ─────────────────────────────────────────────────────────────

func TestGetHistory_PaginationAndFilter(t *testing.T) {
	env := newTestEnv()
	// 造 3 帧当日数据 + 1 帧他日数据
	for i := 0; i < 3; i++ {
		req := singleReq(fixedNow.Add(-time.Duration(90+i*30)*time.Minute), pts(float64(10+i)))
		_, appErr := env.svc.UploadSingle(context.Background(), testDevice, req)
		require.Nil(t, appErr)
	}
	outReq := singleReq(fixedNow.Add(-48*time.Hour), pts(99))
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, outReq)
	require.Nil(t, appErr)

	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", 1, 2)
	require.Nil(t, appErr)
	assert.Equal(t, int64(3), page.Total)
	assert.Len(t, page.List, 2)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 2, page.PageSize)
	// ts DESC：最新帧在前
	assert.True(t, page.List[0].Timestamp > page.List[1].Timestamp)
	// DTO 结构：20 个 SensorPoint
	require.Len(t, page.List[0].Points, model.PointCount)
	assert.Equal(t, "P01", page.List[0].Points[0].PointID)

	page2, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", 2, 2)
	require.Nil(t, appErr)
	assert.Len(t, page2.List, 1)

	// week 范围覆盖他日帧
	pageW, appErr := env.svc.GetHistory(context.Background(), testPatient, "week", "2026-08-08", 1, 100)
	require.Nil(t, appErr)
	assert.Equal(t, int64(4), pageW.Total)

	// 错误传播
	env.records.historyErr = errors.New("query failed")
	_, appErr = env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", 1, 20)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code)

	// 非法参数
	_, appErr = env.svc.GetHistory(context.Background(), testPatient, "year", "2026-08-08", 1, 20)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeQueryParam, appErr.Code)
}

func TestGetRealtime_FullSnapshot(t *testing.T) {
	env := newTestEnv()
	// 先上报一帧生成缓存
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(10, 25.5)))
	require.Nil(t, appErr)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	assert.Equal(t, "online", snap.Status) // lastseen 距今 1min ≤ 2h
	assert.InDelta(t, 0.5, snap.TodayHours, 0.01)
	assert.InDelta(t, 25.5, snap.MaxPressure, 0.01)
	assert.Equal(t, "P02", snap.MaxPoint)
	assert.Zero(t, snap.Events)
	require.Len(t, snap.PressureRecords, 1)
	assert.Equal(t, testDevice, snap.PressureRecords[0].DeviceID)
	assert.Len(t, snap.PressureRecords[0].Points, model.PointCount)
	assert.NotNil(t, snap.Alerts)
}

func TestGetRealtime_NoDevice(t *testing.T) {
	env := newTestEnv()
	snap, appErr := env.svc.GetRealtime(context.Background(), "P-NO-DEVICE")
	require.Nil(t, appErr)
	assert.Equal(t, "offline", snap.Status)
	assert.Empty(t, snap.PressureRecords)
}

func TestGetRealtime_StatusVariants(t *testing.T) {
	// abnormal 优先于 online
	env := newTestEnv()
	env.devices.byPatient[testPatient] = [2]string{testDevice, "abnormal"}
	env.cache.lastseen[testDevice] = fixedNow
	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	assert.Equal(t, "abnormal", snap.Status)

	// lastseen 超过 2h → offline
	env = newTestEnv()
	env.cache.lastseen[testDevice] = fixedNow.Add(-3 * time.Hour)
	snap, appErr = env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	assert.Equal(t, "offline", snap.Status)

	// 无 lastseen → offline
	env = newTestEnv()
	snap, appErr = env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	assert.Equal(t, "offline", snap.Status)
}

func TestGetRealtime_InvalidFrameJSONIgnored(t *testing.T) {
	env := newTestEnv()
	env.cache.rtFrame[testDevice] = "{invalid-json"
	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	assert.Empty(t, snap.PressureRecords)
}

func TestGetRealtime_StoreErrors(t *testing.T) {
	env := newTestEnv()
	env.devices.err = errors.New("db down")
	_, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code)

	env = newTestEnv()
	env.cache.errGetFrame = errors.New("redis down")
	_, appErr = env.svc.GetRealtime(context.Background(), testPatient)
	require.NotNil(t, appErr)

	env = newTestEnv()
	env.cache.errGetStat = errors.New("redis down")
	_, appErr = env.svc.GetRealtime(context.Background(), testPatient)
	require.NotNil(t, appErr)

	env = newTestEnv()
	env.cache.errGetLS = errors.New("redis down")
	// lastseen 读失败被容忍（降级为 offline 判定路径之外）
	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	assert.Equal(t, "offline", snap.Status)
}

// ─── T056：PressureHeatmap 契约单测 ───

func TestGetRealtime_HeatmapFromFrame(t *testing.T) {
	env := newTestEnv()
	// 上报一帧（25.5N 在 P02 为最大值）
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(10, 25.5, 8, 5)))
	require.Nil(t, appErr)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	require.Len(t, snap.PressureHeatmap, model.PointCount)

	// P01、P02 数值与上报一致
	assert.InDelta(t, 10.0, snap.PressureHeatmap[0].PressureValue, 0.001)
	assert.InDelta(t, 25.5, snap.PressureHeatmap[1].PressureValue, 0.001)
	// 结构信息（RrCc / PointID）
	assert.Equal(t, "P01", snap.PressureHeatmap[0].PointID)
	assert.Equal(t, 1, snap.PressureHeatmap[0].Row)
	assert.Equal(t, 1, snap.PressureHeatmap[0].Col)
	assert.Equal(t, "R1C1", snap.PressureHeatmap[0].Label)
	// P02（下标 1）是最大点
	assert.True(t, snap.PressureHeatmap[1].IsMax)
	// IsMax 唯一
	isMaxCount := 0
	for _, p := range snap.PressureHeatmap {
		if p.IsMax {
			isMaxCount++
		}
	}
	assert.Equal(t, 1, isMaxCount)
}

func TestGetRealtime_HeatmapSeedFallback(t *testing.T) {
	// 绑定设备 + rt:frame 为空 → 走 seed 兜底
	env := newTestEnv()
	env.cache.rtFrame[testDevice] = ""

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	require.Len(t, snap.PressureHeatmap, model.PointCount)

	// seed 有合理范围（≥10N，非全 0）
	minV := snap.PressureHeatmap[0].PressureValue
	for _, p := range snap.PressureHeatmap {
		if p.PressureValue < minV {
			minV = p.PressureValue
		}
		assert.NotEmpty(t, p.PointID)
		assert.NotEmpty(t, p.Label)
	}
	assert.GreaterOrEqual(t, minV, 8.0) // SeedHeatmap 基础 12N，扣除误差不会太低

	// IsMax 唯一
	isMaxCount := 0
	for _, p := range snap.PressureHeatmap {
		if p.IsMax {
			isMaxCount++
		}
	}
	assert.Equal(t, 1, isMaxCount)
}

func TestGetRealtime_HeatmapInvalidPointsFallback(t *testing.T) {
	// rt:frame 的 Points 长度不足 20 → seed
	env := newTestEnv()
	shortFrame := realtimeFrame{
		DeviceID:  testDevice,
		PatientID: testPatient,
		Timestamp: fixedNow,
		Points:    make([]float64, 10), // 短于 PointCount
		Battery:   50,
	}
	b, err := json.Marshal(&shortFrame)
	require.NoError(t, err)
	env.cache.rtFrame[testDevice] = string(b)
	env.cache.lastseen[testDevice] = fixedNow // 保持 online

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	require.Len(t, snap.PressureHeatmap, model.PointCount)

	// seed 不会全部等于 0
	anyNonZero := false
	for _, p := range snap.PressureHeatmap {
		if p.PressureValue > 0.1 {
			anyNonZero = true
			break
		}
	}
	assert.True(t, anyNonZero, "seed heatmap should have non-zero values")

	// IsMax 唯一
	isMaxCount := 0
	for _, p := range snap.PressureHeatmap {
		if p.IsMax {
			isMaxCount++
		}
	}
	assert.Equal(t, 1, isMaxCount)
}
