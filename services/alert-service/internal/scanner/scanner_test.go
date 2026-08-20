// Package scanner — 佩戴中断定时扫描器测试（TDD / T008）
//
// 对齐：docs/ §3.1 A5/A7/A8 + PRD §8.1 设备状态机。
// 依赖全部经接口注入（fake 实现见本文件），不触真实 PG/Redis。
package scanner

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

// ─────────────────────────────────────────────────────────────
// fake 依赖
// ─────────────────────────────────────────────────────────────

type fakeDevices struct {
	mu       sync.Mutex
	bound    []Device
	listErr  error
	statuses map[string]string // UpdateStatus 落库结果
	failUpd  bool
}

func newFakeDevices(devs ...Device) *fakeDevices {
	return &fakeDevices{bound: devs, statuses: map[string]string{}}
}

func (f *fakeDevices) ListBoundDevices(context.Context) ([]Device, error) {
	return f.bound, f.listErr
}

func (f *fakeDevices) UpdateStatus(_ context.Context, deviceID, status string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failUpd {
		return false, errors.New("db down")
	}
	if f.statuses[deviceID] == status {
		return false, nil // 状态未变化
	}
	f.statuses[deviceID] = status
	return true, nil
}

type fakeAlerts struct {
	mu        sync.Mutex
	created   []NewAlert
	active    map[string]bool      // device → 存在 active 佩戴中断告警
	recent    map[string]bool      // device → 去重窗口内已有同类型告警
	resolveAt map[string]time.Time // device → resolve 调用时刻
	createErr error
}

func newFakeAlerts() *fakeAlerts {
	return &fakeAlerts{active: map[string]bool{}, recent: map[string]bool{}, resolveAt: map[string]time.Time{}}
}

func (f *fakeAlerts) CreateAlert(_ context.Context, alert NewAlert) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return "", false, f.createErr
	}
	f.created = append(f.created, alert)
	return "1", true, nil
}

func (f *fakeAlerts) HasAlertSince(_ context.Context, deviceID string, _ engine.AlertType, _ time.Time) (bool, error) {
	return f.recent[deviceID], nil
}

func (f *fakeAlerts) HasActiveInterrupt(_ context.Context, deviceID string) (bool, error) {
	return f.active[deviceID], nil
}

func (f *fakeAlerts) ResolveActiveInterrupts(_ context.Context, deviceID string, resolvedAt time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.active[deviceID] {
		return 0, nil
	}
	delete(f.active, deviceID)
	f.resolveAt[deviceID] = resolvedAt
	return 1, nil
}

type fakeLastSeen struct {
	mu     sync.Mutex
	values map[string]time.Time
	err    error
}

func newFakeLastSeen() *fakeLastSeen { return &fakeLastSeen{values: map[string]time.Time{}} }

func (f *fakeLastSeen) GetLastSeen(_ context.Context, deviceID string) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return time.Time{}, false, f.err
	}
	v, ok := f.values[deviceID]
	return v, ok, nil
}

// newTestScanner 组装：默认阈值 60min 中断 / 30min 采集间隔（engine 默认口径）
func newTestScanner(devs *fakeDevices, alerts *fakeAlerts, ls *fakeLastSeen, now time.Time) *Scanner {
	s := New(devs, alerts, ls, engine.NewDefaultRuleEvaluator())
	s.now = func() time.Time { return now }
	return s
}

func defaultDevice() Device { return Device{DeviceID: "DEV001", PatientID: "P001", Status: "online"} }

// ─────────────────────────────────────────────────────────────
// A5：lastseen 超阈值 → 生成 wear_interrupt 告警
// ─────────────────────────────────────────────────────────────

func TestScan_InterruptGeneratesAlert(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice())
	alerts := newFakeAlerts()
	ls := newFakeLastSeen()
	ls.values["DEV001"] = now.Add(-90 * time.Minute) // 超 60min 阈值

	report, err := newTestScanner(devs, alerts, ls, now).Scan(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, report.Scanned)
	assert.Equal(t, 1, report.AlertCreated)
	require.Len(t, alerts.created, 1)
	a := alerts.created[0]
	assert.Equal(t, "DEV001", a.DeviceID)
	assert.Equal(t, "P001", a.PatientID)
	assert.Equal(t, engine.TypeWearInterrupt, a.Type)
	assert.Equal(t, 60.0, a.ThresholdValue)
	assert.InDelta(t, 90.0, a.ActualValue, 0.1)
	assert.True(t, a.Ts.Equal(now))
	assert.NotEmpty(t, a.Detail)
}

// 边界：lastseen 距今恰好 60min（=阈值）→ 不生成（严格大于）
func TestScan_InterruptAtBoundaryNoAlert(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice())
	alerts := newFakeAlerts()
	ls := newFakeLastSeen()
	ls.values["DEV001"] = now.Add(-60 * time.Minute)

	report, err := newTestScanner(devs, alerts, ls, now).Scan(context.Background())

	require.NoError(t, err)
	assert.Zero(t, report.AlertCreated)
	assert.Empty(t, alerts.created)
}

// ─────────────────────────────────────────────────────────────
// A7：去重窗口 = 1×中断阈值，窗口内不重复告警
// ─────────────────────────────────────────────────────────────

func TestScan_DedupWithinWindow(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice())
	alerts := newFakeAlerts()
	alerts.recent["DEV001"] = true // 窗口内已有同设备同类型告警
	ls := newFakeLastSeen()
	ls.values["DEV001"] = now.Add(-90 * time.Minute)

	report, err := newTestScanner(devs, alerts, ls, now).Scan(context.Background())

	require.NoError(t, err)
	assert.Zero(t, report.AlertCreated)
	assert.Equal(t, 1, report.Deduped)
	assert.Empty(t, alerts.created)
}

func TestScan_DedupActiveAlertExists(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice())
	alerts := newFakeAlerts()
	alerts.active["DEV001"] = true // 已存在未 resolve 的中断告警
	ls := newFakeLastSeen()
	ls.values["DEV001"] = now.Add(-3 * time.Hour)

	report, err := newTestScanner(devs, alerts, ls, now).Scan(context.Background())

	require.NoError(t, err)
	assert.Zero(t, report.AlertCreated)
	assert.Equal(t, 1, report.Deduped)
	assert.Empty(t, alerts.created)
}

// ─────────────────────────────────────────────────────────────
// A8：设备恢复上报 → active 中断告警自动 resolve
// ─────────────────────────────────────────────────────────────

func TestScan_RecoveryResolvesActiveAlert(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice())
	alerts := newFakeAlerts()
	alerts.active["DEV001"] = true // 中断告警仍 active
	ls := newFakeLastSeen()
	ls.values["DEV001"] = now.Add(-5 * time.Minute) // 设备刚恢复上报

	report, err := newTestScanner(devs, alerts, ls, now).Scan(context.Background())

	require.NoError(t, err)
	assert.EqualValues(t, 1, report.Resolved)
	assert.True(t, alerts.resolveAt["DEV001"].Equal(now), "resolved_at 应为扫描时刻")
	assert.Zero(t, report.AlertCreated)
}

func TestScan_FreshDeviceNoResolveCallEffect(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice())
	alerts := newFakeAlerts() // 无 active 告警
	ls := newFakeLastSeen()
	ls.values["DEV001"] = now.Add(-5 * time.Minute)

	report, err := newTestScanner(devs, alerts, ls, now).Scan(context.Background())

	require.NoError(t, err)
	assert.EqualValues(t, 0, report.Resolved) // resolve 无命中，幂等空转
	assert.Zero(t, report.AlertCreated)
}

// ─────────────────────────────────────────────────────────────
// devices.status 状态机联动（PRD §8.1，abnormal > offline）
// ─────────────────────────────────────────────────────────────

func TestScan_StatusTransitions(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name      string
		lastSeen  time.Time
		curStatus string
		want      string
	}{
		{"online：阈值内上报", now.Add(-10 * time.Minute), "offline", "online"},
		{"offline→abnormal：90min 缺数（3×30min）", now.Add(-100 * time.Minute), "online", "abnormal"},
		{"abnormal 优先于 offline：超 2h 仍 abnormal", now.Add(-3 * time.Hour), "online", "abnormal"},
		{"状态不变不写库", now.Add(-10 * time.Minute), "online", "online"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dev := defaultDevice()
			dev.Status = tc.curStatus
			devs := newFakeDevices(dev)
			alerts := newFakeAlerts()
			ls := newFakeLastSeen()
			ls.values["DEV001"] = tc.lastSeen

			report, err := newTestScanner(devs, alerts, ls, now).Scan(context.Background())

			require.NoError(t, err)
			if tc.want == tc.curStatus {
				assert.Empty(t, devs.statuses, "状态不变不应写库")
				assert.Zero(t, report.StatusChanged, "状态不变不应计入变更")
			} else {
				assert.Equal(t, tc.want, devs.statuses["DEV001"], "状态迁移结果")
				assert.Equal(t, 1, report.StatusChanged)
			}
		})
	}
}

func TestDeriveStatus_PriorityAndBoundaries(t *testing.T) {
	// online 口径与 data-service 查询时推导一致（record.go GetRealtime：gap ≤2h → online）
	assert.Equal(t, StatusOnline, DeriveStatus(10*time.Minute, 30))
	assert.Equal(t, StatusOnline, DeriveStatus(2*time.Hour, 60), "恰好 2h 仍在 online 口径内（≤2h）")
	assert.Equal(t, StatusAbnormal, DeriveStatus(90*time.Minute, 30), "恰好 3×采集周期 → abnormal（≥）")
	assert.Equal(t, StatusAbnormal, DeriveStatus(3*time.Hour, 30), "abnormal 优先于 offline")
	assert.Equal(t, StatusOffline, DeriveStatus(130*time.Minute, 60), "1h 间隔：2h+ 未达 abnormal（3h）→ offline")
	assert.Equal(t, StatusAbnormal, DeriveStatus(3*time.Hour, 60), "1h 间隔：3h 满足 abnormal 后迁移")
	assert.Equal(t, StatusOffline, DeriveStatus(5*time.Hour, 0), "采集间隔零值：abnormal 规则不启用 → offline 兜底")
}

// ─────────────────────────────────────────────────────────────
// 异常路径
// ─────────────────────────────────────────────────────────────

func TestScan_NoLastSeenSkipped(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice())
	alerts := newFakeAlerts()
	ls := newFakeLastSeen() // DEV001 无 lastseen

	report, err := newTestScanner(devs, alerts, ls, now).Scan(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, report.MissedLastSeen)
	assert.Zero(t, report.AlertCreated)
	assert.Empty(t, alerts.created)
	assert.Empty(t, devs.statuses, "无 lastseen 不推导状态（查询时实时推导兜底）")
}

func TestScan_RedisErrorContinues(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice(), Device{DeviceID: "DEV002", PatientID: "P002", Status: "online"})
	alerts := newFakeAlerts()
	ls := newFakeLastSeen()
	ls.values["DEV002"] = now.Add(-90 * time.Minute)

	s := newTestScanner(devs, alerts, ls, now)
	s.lastseen = &failOnceLastSeen{inner: ls, fail: map[string]bool{"DEV001": true}}
	report, err := s.Scan(context.Background())

	require.NoError(t, err, "单设备 Redis 读失败不中断整轮扫描")
	assert.Equal(t, 1, report.RedisErrors)
	assert.Equal(t, 1, report.AlertCreated, "其余设备正常扫描")
}

type failOnceLastSeen struct {
	inner *fakeLastSeen
	fail  map[string]bool
}

func (f *failOnceLastSeen) GetLastSeen(ctx context.Context, deviceID string) (time.Time, bool, error) {
	if f.fail[deviceID] {
		return time.Time{}, false, errors.New("redis timeout")
	}
	return f.inner.GetLastSeen(ctx, deviceID)
}

func TestScan_ListDevicesError(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices()
	devs.listErr = errors.New("db down")

	_, err := newTestScanner(devs, newFakeAlerts(), newFakeLastSeen(), now).Scan(context.Background())

	require.Error(t, err, "设备清单读取失败整轮失败（无法扫描）")
}

// ─────────────────────────────────────────────────────────────
// 存储层错误容忍：单点失败记录日志并跳过，不中断整轮
// ─────────────────────────────────────────────────────────────

type errAlerts struct {
	failActive  bool
	failSince   bool
	failCreate  bool
	failResolve bool
	dupCreate   bool // CreateAlert 返回 created=false（唯一约束保底命中）
}

func (f *errAlerts) CreateAlert(context.Context, NewAlert) (string, bool, error) {
	if f.failCreate {
		return "", false, errors.New("insert failed")
	}
	if f.dupCreate {
		return "", false, nil
	}
	return "1", true, nil
}

func (f *errAlerts) HasAlertSince(context.Context, string, engine.AlertType, time.Time) (bool, error) {
	if f.failSince {
		return false, errors.New("query failed")
	}
	return false, nil
}

func (f *errAlerts) HasActiveInterrupt(context.Context, string) (bool, error) {
	if f.failActive {
		return false, errors.New("query failed")
	}
	return false, nil
}

func (f *errAlerts) ResolveActiveInterrupts(context.Context, string, time.Time) (int64, error) {
	if f.failResolve {
		return 0, errors.New("update failed")
	}
	return 0, nil
}

func interruptFixture(t *testing.T, now time.Time, alerts AlertStore) (*Scanner, *fakeDevices) {
	t.Helper()
	devs := newFakeDevices(defaultDevice())
	ls := newFakeLastSeen()
	ls.values["DEV001"] = now.Add(-90 * time.Minute)
	s := New(devs, alerts, ls, engine.NewDefaultRuleEvaluator())
	s.now = func() time.Time { return now }
	return s, devs
}

func TestScan_AlertStoreErrorsTolerated(t *testing.T) {
	now := time.Now()

	t.Run("HasActiveInterrupt 失败 → 跳过该设备", func(t *testing.T) {
		s, _ := interruptFixture(t, now, &errAlerts{failActive: true})
		report, err := s.Scan(context.Background())
		require.NoError(t, err)
		assert.Zero(t, report.AlertCreated)
	})

	t.Run("HasAlertSince 失败 → 跳过该设备", func(t *testing.T) {
		s, _ := interruptFixture(t, now, &errAlerts{failSince: true})
		report, err := s.Scan(context.Background())
		require.NoError(t, err)
		assert.Zero(t, report.AlertCreated)
	})

	t.Run("CreateAlert 失败 → 跳过该设备", func(t *testing.T) {
		s, _ := interruptFixture(t, now, &errAlerts{failCreate: true})
		report, err := s.Scan(context.Background())
		require.NoError(t, err)
		assert.Zero(t, report.AlertCreated)
	})

	t.Run("CreateAlert 唯一约束保底命中 → 计 Deduped", func(t *testing.T) {
		s, _ := interruptFixture(t, now, &errAlerts{dupCreate: true})
		report, err := s.Scan(context.Background())
		require.NoError(t, err)
		assert.Zero(t, report.AlertCreated)
		assert.Equal(t, 1, report.Deduped)
	})

	t.Run("ResolveActiveInterrupts 失败 → 不计 Resolved", func(t *testing.T) {
		devs := newFakeDevices(defaultDevice())
		ls := newFakeLastSeen()
		ls.values["DEV001"] = now.Add(-5 * time.Minute)
		s := New(devs, &errAlerts{failResolve: true}, ls, engine.NewDefaultRuleEvaluator())
		s.now = func() time.Time { return now }
		report, err := s.Scan(context.Background())
		require.NoError(t, err)
		assert.EqualValues(t, 0, report.Resolved)
	})

	t.Run("UpdateStatus 失败 → 不计 StatusChanged", func(t *testing.T) {
		s, devs := interruptFixture(t, now, newFakeAlerts())
		devs.failUpd = true
		report, err := s.Scan(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, report.AlertCreated, "状态写失败不影响告警生成")
		assert.Zero(t, report.StatusChanged)
	})
}

// SetLogger 注入后各路径仍正常（覆盖日志分支）
func TestScan_WithLogger(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice())
	alerts := newFakeAlerts()
	ls := newFakeLastSeen()
	ls.values["DEV001"] = now.Add(-90 * time.Minute)

	s := newTestScanner(devs, alerts, ls, now)
	s.SetLogger(zerolog.New(io.Discard))
	report, err := s.Scan(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, report.AlertCreated)
}
