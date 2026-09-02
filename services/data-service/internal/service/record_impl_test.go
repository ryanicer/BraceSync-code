// Package service T072：GetRealtime DB 优先改造实现侧测试
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// ── fakes ──────────────────────────────────────────────────────

// mockRecordStore 实现 repo.RecordStore（仅 GetLatestRecord 有意义）
type mockRecordStore struct {
	latestRec   model.PressureRecord
	latestExist bool
	latestErr   error
}

func (m *mockRecordStore) InsertRecord(_ context.Context, _, _ string, _ repo.PendingFrame) (int64, bool, error) {
	return 0, false, nil
}
func (m *mockRecordStore) BatchInsert(_ context.Context, _, _ string, _ []repo.PendingFrame) ([]time.Time, error) {
	return nil, nil
}
func (m *mockRecordStore) QueryHistory(_ context.Context, _ string, _, _ time.Time, _, _ int) ([]model.PressureRecord, int64, error) {
	return nil, 0, nil
}
func (m *mockRecordStore) GetLatestRecord(_ context.Context, _ string) (model.PressureRecord, bool, error) {
	return m.latestRec, m.latestExist, m.latestErr
}

// mockDeviceStore 实现 repo.DeviceStore（仅 GetDeviceByPatient 有意义）
type mockDeviceStore struct {
	deviceID string
	status   string
	exist    bool
	err      error
}

func (m *mockDeviceStore) GetBinding(_ context.Context, _ string) (string, string, bool, error) {
	return "", "", false, nil
}
func (m *mockDeviceStore) GetDeviceByPatient(_ context.Context, _ string) (string, string, bool, error) {
	return m.deviceID, m.status, m.exist, m.err
}

// mockCacheStore 实现 repo.CacheStore（GetLastSeen / GetStatToday 有意义）
type mockCacheStore struct {
	lastseen     time.Time
	lastseenOk   bool
	lastseenErr  error
	statToday    map[string]string
	statTodayErr error
}

func (m *mockCacheStore) SetLastSeen(_ context.Context, _ string, _ time.Time) error { return nil }
func (m *mockCacheStore) GetLastSeen(_ context.Context, _ string) (time.Time, bool, error) {
	return m.lastseen, m.lastseenOk, m.lastseenErr
}
func (m *mockCacheStore) SetRealtimeFrame(_ context.Context, _, _ string) error { return nil }
func (m *mockCacheStore) GetRealtimeFrame(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockCacheStore) ApplyStatToday(_ context.Context, _ string, _ int, _ float64, _ string, _ int, _ time.Time) error {
	return nil
}
func (m *mockCacheStore) GetStatToday(_ context.Context, _ string) (map[string]string, error) {
	return m.statToday, m.statTodayErr
}
func (m *mockCacheStore) PushAlertPending(_ context.Context, _ string) error { return nil }
func (m *mockCacheStore) EnqueueRollup(_ context.Context, _, _, _ string) (bool, error) {
	return false, nil
}
func (m *mockCacheStore) DequeueRollup(_ context.Context) (string, error) { return "", nil }

// ── helpers ───────────────────────────────────────────────────

// newTestService 组装仅含 GetRealtime 依赖的 RecordService（其余字段零值）
func newTestService(r repo.RecordStore, d repo.DeviceStore, c repo.CacheStore, now time.Time) *RecordService {
	svc := &RecordService{records: r, devices: d, cache: c, now: func() time.Time { return now }}
	if l, ok := r.(latestRecordStore); ok { // mockRecordStore 实现 GetLatestRecord → DB 优先路径
		svc.latest = l
	}
	return svc
}

// makeRecord 构造一条 pressure_records 领域行（P05 为最大点）
func makeRecord(patientID string, ts time.Time) model.PressureRecord {
	var pts [model.PointCount]float32
	for i := range pts {
		pts[i] = float32(10 + i)
	}
	pts[4] = 50.0 // P05 = max
	return model.PressureRecord{
		RecordID:    1,
		DeviceID:    "DEV-001",
		PatientID:   patientID,
		Ts:          ts,
		Points:      pts,
		MaxPressure: 50.0,
		UploadTime:  ts,
	}
}

// ── tests ──────────────────────────────────────────────────────

func TestGetRealtimeDBFirst_HasData(t *testing.T) {
	now := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)
	recTs := now.Add(-1 * time.Hour) // 1h ago ≤ 2h → online

	svc := newTestService(
		&mockRecordStore{latestRec: makeRecord("P1", recTs), latestExist: true},
		&mockDeviceStore{deviceID: "DEV-001", status: "active", exist: true},
		&mockCacheStore{
			lastseen:   now.Add(-30 * time.Minute), // 30min ago ≤ 2h
			lastseenOk: true,
			statToday:  map[string]string{"wear_minutes": "360", "abnormal_count": "2"},
		},
		now,
	)

	snap, appErr := svc.GetRealtime(context.Background(), "P1")
	require.Nil(t, appErr)
	require.NotNil(t, snap)

	assert.Equal(t, "online", snap.Status)
	require.Len(t, snap.PressureRecords, 1)
	assert.Equal(t, "DEV-001", snap.PressureRecords[0].DeviceID)
	assert.Len(t, snap.PressureRecords[0].Points, model.PointCount)
	assert.Greater(t, snap.MaxPressure, 0.0)
	assert.Regexp(t, `^P\d{2}$`, snap.MaxPoint)
	assert.Len(t, snap.PressureHeatmap, model.PointCount)
	assert.Equal(t, 6.0, snap.TodayHours) // 360min / 60 = 6h
	assert.Equal(t, 2, snap.Events)
}

func TestGetRealtimeDBFirst_NoRecords(t *testing.T) {
	now := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)

	svc := newTestService(
		&mockRecordStore{latestExist: false},
		&mockDeviceStore{deviceID: "DEV-001", status: "active", exist: true},
		&mockCacheStore{},
		now,
	)

	snap, appErr := svc.GetRealtime(context.Background(), "P1")
	require.Nil(t, appErr)
	require.NotNil(t, snap)

	assert.Equal(t, "offline", snap.Status)
	assert.Empty(t, snap.PressureRecords)
	assert.Equal(t, 0.0, snap.MaxPressure)
	assert.Empty(t, snap.MaxPoint)
	assert.Len(t, snap.PressureHeatmap, model.PointCount) // seed 兜底
}

func TestGetRealtimeDBFirst_NoDevice(t *testing.T) {
	now := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)

	svc := newTestService(
		&mockRecordStore{},
		&mockDeviceStore{exist: false},
		&mockCacheStore{},
		now,
	)

	snap, appErr := svc.GetRealtime(context.Background(), "P1")
	require.Nil(t, appErr)
	require.NotNil(t, snap)

	assert.Equal(t, "offline", snap.Status)
	assert.Empty(t, snap.PressureRecords)
	assert.Len(t, snap.PressureHeatmap, model.PointCount) // seed
}

func TestGetRealtimeDBFirst_FieldMapping(t *testing.T) {
	now := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)
	recTs := now.Add(-1 * time.Hour)

	svc := newTestService(
		&mockRecordStore{latestRec: makeRecord("P1", recTs), latestExist: true},
		&mockDeviceStore{deviceID: "DEV-001", status: "active", exist: true},
		&mockCacheStore{lastseen: now.Add(-30 * time.Minute), lastseenOk: true},
		now,
	)

	snap, appErr := svc.GetRealtime(context.Background(), "P1")
	require.Nil(t, appErr)

	// 序列化后校验 JSON 字段名对齐前端 RealtimeSnapshot 契约
	raw, err := json.Marshal(snap)
	require.NoError(t, err)
	body := string(raw)

	for _, key := range []string{
		`"status"`, `"todayHours"`, `"maxPressure"`, `"maxPoint"`,
		`"events"`, `"pressureRecords"`, `"alerts"`, `"pressureHeatmap"`,
	} {
		assert.Contains(t, body, key, "missing JSON field %s in RealtimeSnapshot", key)
	}
}

func TestGetRealtimeDBFirst_AbnormalDevice(t *testing.T) {
	now := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)
	recTs := now.Add(-1 * time.Hour)

	svc := newTestService(
		&mockRecordStore{latestRec: makeRecord("P1", recTs), latestExist: true},
		&mockDeviceStore{deviceID: "DEV-001", status: "abnormal", exist: true},
		&mockCacheStore{lastseen: now.Add(-30 * time.Minute), lastseenOk: true},
		now,
	)

	snap, appErr := svc.GetRealtime(context.Background(), "P1")
	require.Nil(t, appErr)
	require.NotNil(t, snap)

	assert.Equal(t, "abnormal", snap.Status)
	require.Len(t, snap.PressureRecords, 1) // abnormal 仍有数据
}
