// Package service T021：rollup 服务单元测试（实现侧）
package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// ─────────────────────────────────────────────────────────────
// Fakes
// ─────────────────────────────────────────────────────────────

type fakeDailyWearStatsStore struct {
	upserted     []model.DailyWearStats
	aggregated   []model.DailyWearStats
	patientRange map[string][]model.DailyWearStats
	patients     []string
	upsertErr    error
	aggErr       error
	queryErr     error
	listErr      error
}

func newFakeDailyWearStats() *fakeDailyWearStatsStore {
	return &fakeDailyWearStatsStore{patientRange: map[string][]model.DailyWearStats{}}
}

func (f *fakeDailyWearStatsStore) Upsert(_ context.Context, stats []model.DailyWearStats) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserted = append(f.upserted, stats...)
	return nil
}

func (f *fakeDailyWearStatsStore) AggregateDate(_ context.Context, _, _ time.Time, _ int) ([]model.DailyWearStats, error) {
	if f.aggErr != nil {
		return nil, f.aggErr
	}
	return f.aggregated, nil
}

func (f *fakeDailyWearStatsStore) QueryRange(_ context.Context, patientID string, _, _ time.Time) ([]model.DailyWearStats, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.patientRange[patientID], nil
}

func (f *fakeDailyWearStatsStore) ListPatientsWithStats(_ context.Context, _, _ time.Time) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.patients, nil
}

type fakeHealthReportStore struct {
	inserted    []*model.HealthReport
	prev        map[string]*model.HealthReport
	suggestions map[string]string
	insertErr   error
	prevErr     error
	sugErr      error
}

func newFakeHealthReportStore() *fakeHealthReportStore {
	return &fakeHealthReportStore{prev: map[string]*model.HealthReport{}, suggestions: map[string]string{}}
}

func (f *fakeHealthReportStore) InsertReport(_ context.Context, report *model.HealthReport) (bool, error) {
	if f.insertErr != nil {
		return false, f.insertErr
	}
	// 幂等检查
	for _, existing := range f.inserted {
		if existing.PatientID == report.PatientID &&
			existing.ReportType == report.ReportType &&
			existing.PeriodStart.Equal(report.PeriodStart) {
			return false, nil
		}
	}
	f.inserted = append(f.inserted, report)
	return true, nil
}

func (f *fakeHealthReportStore) QueryPreviousReport(_ context.Context, patientID, reportType string, _ time.Time) (*model.HealthReport, error) {
	if f.prevErr != nil {
		return nil, f.prevErr
	}
	key := patientID + "|" + reportType
	return f.prev[key], nil
}

func (f *fakeHealthReportStore) LatestSuggestion(_ context.Context, patientID string) (string, error) {
	if f.sugErr != nil {
		return "", f.sugErr
	}
	return f.suggestions[patientID], nil
}

// fakePartitionStore 分区管理 fake
type fakePartitionStore struct {
	existing  map[string]bool
	created   []string
	ensureErr error
	existsErr error
}

func newFakePartitionStore() *fakePartitionStore {
	return &fakePartitionStore{existing: map[string]bool{}}
}

func (f *fakePartitionStore) EnsurePartition(_ context.Context, ym time.Time) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
	name := "pressure_records_" + ym.Format("200601")
	f.created = append(f.created, name)
	return nil
}

func (f *fakePartitionStore) PartitionExists(_ context.Context, name string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.existing[name], nil
}

// fakeArchiveStore 归档 fake
type fakeArchiveStore struct {
	statuses    map[string]*model.ArchiveStatus
	rowCounts   map[string]int64
	exportErr   error
	upsertErr   error
	countErr    error
	detachErr   error
	detachCalls int
}

func newFakeArchiveStore() *fakeArchiveStore {
	return &fakeArchiveStore{statuses: map[string]*model.ArchiveStatus{}, rowCounts: map[string]int64{}}
}

func (f *fakeArchiveStore) GetArchiveStatus(_ context.Context, name string) (*model.ArchiveStatus, error) {
	return f.statuses[name], nil
}

func (f *fakeArchiveStore) UpsertArchiveStatus(_ context.Context, s *model.ArchiveStatus) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	cp := *s
	f.statuses[s.PartitionName] = &cp
	return nil
}

func (f *fakeArchiveStore) CountPartitionRows(_ context.Context, name string) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.rowCounts[name], nil
}

func (f *fakeArchiveStore) ExportPartitionCSV(_ context.Context, name, _ string) (int64, error) {
	if f.exportErr != nil {
		return 0, f.exportErr
	}
	return f.rowCounts[name], nil
}

func (f *fakeArchiveStore) DetachAndDropPartition(_ context.Context, _ string) error {
	f.detachCalls++
	return f.detachErr
}

// ─────────────────────────────────────────────────────────────
// RollupService 测试
// ─────────────────────────────────────────────────────────────

func TestRollupService_RunDailyRollup_Success(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = []model.DailyWearStats{
		{PatientID: "P1", FrameCount: 48, WearMinutes: 1440, AvgPressure: 20.5, MaxPressure: 35.0, MaxPoint: "P03"},
		{PatientID: "P2", FrameCount: 10, WearMinutes: 300, AvgPressure: 15.0, MaxPressure: 25.0, MaxPoint: "P06"},
	}
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	svc := NewRollupService(stats, cache, cfg)
	svc.now = func() time.Time { return time.Date(2026, 8, 11, 16, 10, 0, 0, time.UTC) } // CST 08-12 00:10

	svc.RunDailyRollup(context.Background())

	require.Len(t, stats.upserted, 2)
	assert.Equal(t, "P1", stats.upserted[0].PatientID)
	assert.Equal(t, "P2", stats.upserted[1].PatientID)
	// StatDate 应为昨日（CST）
	assert.Equal(t, "2026-08-11", stats.upserted[0].StatDate.Format("2006-01-02"))
}

func TestRollupService_RunDailyRollup_EmptyRecords(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = nil // 空明细
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	svc := NewRollupService(stats, cache, cfg)
	svc.now = func() time.Time { return time.Date(2026, 8, 11, 16, 10, 0, 0, time.UTC) }

	svc.RunDailyRollup(context.Background())
	assert.Empty(t, stats.upserted, "空明细不应 upsert")
}

func TestRollupService_RunDailyRollup_AggregateError(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggErr = errors.New("db error")
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	svc := NewRollupService(stats, cache, cfg)
	svc.now = func() time.Time { return time.Date(2026, 8, 11, 16, 10, 0, 0, time.UTC) }

	svc.RunDailyRollup(context.Background()) // 不应 panic
	assert.Empty(t, stats.upserted)
}

func TestRollupService_RunDailyRollup_UpsertError(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = []model.DailyWearStats{{PatientID: "P1", FrameCount: 1}}
	stats.upsertErr = errors.New("upsert failed")
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	svc := NewRollupService(stats, cache, cfg)
	svc.now = func() time.Time { return time.Date(2026, 8, 11, 16, 10, 0, 0, time.UTC) }

	svc.RunDailyRollup(context.Background()) // 不应 panic
}

func TestRollupService_RunDailyRollup_ConfigFallback(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = []model.DailyWearStats{{PatientID: "P1", FrameCount: 1}}
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 0, version: 0, err: errors.New("config down")}

	svc := NewRollupService(stats, cache, cfg)
	svc.now = func() time.Time { return time.Date(2026, 8, 11, 16, 10, 0, 0, time.UTC) }

	svc.RunDailyRollup(context.Background())
	assert.Len(t, stats.upserted, 1)
}

func TestRollupService_ProcessBackfillQueue_Success(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = []model.DailyWearStats{
		{PatientID: "P1", FrameCount: 20, WearMinutes: 600},
	}
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	// 投递一条补传任务
	task := rollupTask{PatientID: "P1", Date: "2026-08-10", QueuedAt: "2026-08-11T00:00:00Z"}
	payload, _ := json.Marshal(task)
	cache.rollup = append(cache.rollup, string(payload))

	svc := NewRollupService(stats, cache, cfg)
	svc.now = func() time.Time { return fixedNow }

	svc.ProcessBackfillQueue(context.Background())

	assert.Len(t, stats.upserted, 1, "应 upsert 一条重算结果")
	assert.Empty(t, cache.rollup, "队列应被消费")
}

func TestRollupService_ProcessBackfillQueue_EmptyQueue(t *testing.T) {
	stats := newFakeDailyWearStats()
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	svc := NewRollupService(stats, cache, cfg)
	svc.ProcessBackfillQueue(context.Background()) // 不应 panic
	assert.Empty(t, stats.upserted)
}

func TestRollupService_ProcessBackfillQueue_InvalidPayload(t *testing.T) {
	stats := newFakeDailyWearStats()
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	cache.rollup = append(cache.rollup, "{invalid json")

	svc := NewRollupService(stats, cache, cfg)
	svc.ProcessBackfillQueue(context.Background()) // 跳过无效 payload
	assert.Empty(t, stats.upserted)
}

func TestRollupService_ProcessBackfillQueue_InvalidDate(t *testing.T) {
	stats := newFakeDailyWearStats()
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	task := rollupTask{PatientID: "P1", Date: "bad-date"}
	payload, _ := json.Marshal(task)
	cache.rollup = append(cache.rollup, string(payload))

	svc := NewRollupService(stats, cache, cfg)
	svc.ProcessBackfillQueue(context.Background()) // 跳过无效日期
	assert.Empty(t, stats.upserted)
}

func TestRollupService_ProcessBackfillQueue_AggregateError(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggErr = errors.New("aggregate failed")
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	task := rollupTask{PatientID: "P1", Date: "2026-08-10"}
	payload, _ := json.Marshal(task)
	cache.rollup = append(cache.rollup, string(payload))

	svc := NewRollupService(stats, cache, cfg)
	svc.ProcessBackfillQueue(context.Background()) // 记录错误继续
	assert.Empty(t, stats.upserted)
}

func TestRollupService_ProcessBackfillQueue_NoPatientMatch(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = []model.DailyWearStats{
		{PatientID: "P-OTHER", FrameCount: 10},
	}
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	task := rollupTask{PatientID: "P1", Date: "2026-08-10"}
	payload, _ := json.Marshal(task)
	cache.rollup = append(cache.rollup, string(payload))

	svc := NewRollupService(stats, cache, cfg)
	svc.ProcessBackfillQueue(context.Background())
	assert.Empty(t, stats.upserted, "目标患者不在聚合结果中，不应 upsert")
}

func TestRollupService_ProcessBackfillQueue_DequeueError(t *testing.T) {
	stats := newFakeDailyWearStats()
	cache := newFakeCache()
	cache.errRollup = errors.New("redis down")
	cfg := &fakeConfigs{interval: 30, version: 1}

	svc := NewRollupService(stats, cache, cfg)
	svc.ProcessBackfillQueue(context.Background()) // 不应 panic
}

func TestRollupService_TimeZoneDayBoundary(t *testing.T) {
	stats := newFakeDailyWearStats()
	stats.aggregated = []model.DailyWearStats{{PatientID: "P1", FrameCount: 1}}
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	// UTC 2026-08-11 17:00 = CST 2026-08-12 01:00 → "昨日" = 2026-08-11 CST
	svc := NewRollupService(stats, cache, cfg)
	svc.now = func() time.Time { return time.Date(2026, 8, 11, 17, 0, 0, 0, time.UTC) }

	svc.RunDailyRollup(context.Background())
	require.Len(t, stats.upserted, 1)
	assert.Equal(t, "2026-08-11", stats.upserted[0].StatDate.Format("2006-01-02"))
}

// 确保 AggregateDate 接收 from/to 参数（UTC 时间窗口）
func TestRollupService_AggregateDateCalledWithUTCWindow(t *testing.T) {
	var capturedFrom, capturedTo time.Time
	stats := &captureAggStore{capture: func(from, to time.Time) {
		capturedFrom, capturedTo = from, to
	}}
	cache := newFakeCache()
	cfg := &fakeConfigs{interval: 30, version: 1}

	svc := NewRollupService(stats, cache, cfg)
	// CST 2026-08-11 00:10 → 昨日 CST = 2026-08-10
	svc.now = func() time.Time { return time.Date(2026, 8, 10, 16, 10, 0, 0, time.UTC) }

	svc.RunDailyRollup(context.Background())

	// 期望 UTC 窗口：CST 08-10 00:00 → UTC 08-09 16:00 到 CST 08-11 00:00 → UTC 08-10 16:00
	assert.Equal(t, time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC), capturedFrom)
	assert.Equal(t, time.Date(2026, 8, 10, 16, 0, 0, 0, time.UTC), capturedTo)
}

type captureAggStore struct {
	capture func(from, to time.Time)
}

func (c *captureAggStore) Upsert(_ context.Context, _ []model.DailyWearStats) error { return nil }
func (c *captureAggStore) AggregateDate(_ context.Context, from, to time.Time, _ int) ([]model.DailyWearStats, error) {
	c.capture(from, to)
	return nil, nil
}
func (c *captureAggStore) QueryRange(_ context.Context, _ string, _, _ time.Time) ([]model.DailyWearStats, error) {
	return nil, nil
}
func (c *captureAggStore) ListPatientsWithStats(_ context.Context, _, _ time.Time) ([]string, error) {
	return nil, nil
}

// 确保 repo 接口编译正确
var _ repo.DailyWearStatsStore = (*fakeDailyWearStatsStore)(nil)
var _ repo.HealthReportStore = (*fakeHealthReportStore)(nil)
var _ repo.PartitionStore = (*fakePartitionStore)(nil)
var _ repo.ArchiveStore = (*fakeArchiveStore)(nil)
