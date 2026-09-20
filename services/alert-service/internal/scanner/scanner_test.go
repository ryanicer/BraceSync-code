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
	mu         sync.Mutex
	created    []NewAlert
	active     map[string]bool      // device → 存在 active 佩戴中断告警
	recent     map[string]bool      // device → 去重窗口内已有同类型告警
	recentByType map[string]bool    // "device|type" → 已有同类告警（T257 2.6 日扫去重）
	resolveAt  map[string]time.Time // device → resolve 调用时刻
	createErr  error
	failSince  error // T257 2.6：去重查询失败注入
}

func newFakeAlerts() *fakeAlerts {
	return &fakeAlerts{
		active:       map[string]bool{},
		recent:       map[string]bool{},
		recentByType: map[string]bool{},
		resolveAt:    map[string]time.Time{},
	}
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

func (f *fakeAlerts) HasAlertSince(_ context.Context, deviceID string, t engine.AlertType, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSince != nil {
		return false, f.failSince
	}
	if f.recent[deviceID] {
		return true, nil
	}
	return f.recentByType[deviceID+"|"+string(t)], nil
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
func TestScan_WithLogger(t *testing.T) {	now := time.Now()
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

// ─────────────────────────────────────────────────────────────
// T257 2.6：每日「佩戴时长不足」扫描
// ─────────────────────────────────────────────────────────────

type fakeWear struct {
	mu         sync.Mutex
	minutes    map[string]int // patient|date → wear_minutes（缺失 = 无 rollup 行 = 0）
	errFor     map[string]bool
	readPatient []string
}

func newFakeWear() *fakeWear {
	return &fakeWear{minutes: map[string]int{}, errFor: map[string]bool{}}
}

func (f *fakeWear) DailyWearMinutes(_ context.Context, patientID string, bizDay time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := patientID + "|" + bizDay.Format("2006-01-02")
	f.readPatient = append(f.readPatient, patientID)
	if f.errFor[patientID] {
		return 0, errors.New("db down")
	}
	return f.minutes[key], nil
}

func cstDailyFixture(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return loc
}

func TestScanDailyWear_BelowTargetCreatesOneAlertPerPatient(t *testing.T) {
	loc := cstDailyFixture(t)
	day := time.Date(2026, 9, 19, 0, 0, 0, 0, loc)
	devs := newFakeDevices(
		Device{DeviceID: "DEV-B", PatientID: "P001"},
		Device{DeviceID: "DEV-A", PatientID: "P001"}, // 同患者多设备 ⇒ 仍只一条，且取字典序最小设备
		Device{DeviceID: "DEV-C", PatientID: "P002"},
	)
	alerts := newFakeAlerts()
	wear := newFakeWear()
	wear.minutes["P001|2026-09-19"] = 390 // 6.5h < 18h
	wear.minutes["P002|2026-09-19"] = 1200

	s := New(devs, alerts, newFakeLastSeen(), engine.NewDefaultRuleEvaluator())
	s.SetWearStore(wear)
	report, err := s.ScanDailyWear(context.Background(), day, 18)

	require.NoError(t, err)
	assert.Equal(t, 2, report.Patients, "按患者去重")
	assert.Equal(t, 1, report.AlertCreated)
	assert.Equal(t, 1, report.AboveTarget)
	require.Len(t, alerts.created, 1)
	a := alerts.created[0]
	assert.Equal(t, "P001", a.PatientID)
	assert.Equal(t, "DEV-A", a.DeviceID, "多设备患者取字典序最小设备，保证重复扫描命中同一唯一键")
	assert.Equal(t, engine.TypeWearDurationShort, a.Type)
	assert.InDelta(t, 18*60, a.ThresholdValue, 0.001)
	assert.InDelta(t, 390, a.ActualValue, 0.001)
	assert.True(t, a.Ts.Equal(time.Date(2026, 9, 19, 23, 59, 59, 0, loc)),
		"ts 固定为该业务日 23:59:59 ⇒ uk_alerts_natural 天然每日一发")
	assert.Contains(t, a.Detail, "佩戴时长不足")
}

// 同一业务日再跑一轮：去重查询命中 ⇒ 不再读 rollup、不再产生（每日一发）
func TestScanDailyWear_IdempotentWithinBizDay(t *testing.T) {
	loc := cstDailyFixture(t)
	day := time.Date(2026, 9, 19, 0, 0, 0, 0, loc)
	devs := newFakeDevices(Device{DeviceID: "DEV-A", PatientID: "P001"})
	alerts := newFakeAlerts()
	alerts.recentByType["DEV-A|wear_duration_short"] = true
	wear := newFakeWear()

	s := New(devs, alerts, newFakeLastSeen(), engine.NewDefaultRuleEvaluator())
	s.SetWearStore(wear)
	report, err := s.ScanDailyWear(context.Background(), day, 18)

	require.NoError(t, err)
	assert.Equal(t, 1, report.Deduped)
	assert.Zero(t, report.AlertCreated)
	assert.Empty(t, wear.readPatient, "已存在同类告警时不应再读 rollup（快路径）")
	assert.Empty(t, alerts.created)
}

func TestScanDailyWear_NoWearStoreIsNoop(t *testing.T) {
	devs := newFakeDevices(Device{DeviceID: "DEV-A", PatientID: "P001"})
	alerts := newFakeAlerts()
	s := New(devs, alerts, newFakeLastSeen(), engine.NewDefaultRuleEvaluator())

	report, err := s.ScanDailyWear(context.Background(), time.Now(), 18)
	require.NoError(t, err)
	assert.Zero(t, report.Patients)
	assert.Empty(t, alerts.created, "未注入 rollup 来源 ⇒ 规则不启用，不得凭 0 分钟误报")
}

// 目标为 0（规则关闭）与读失败：都不产生告警、不中断整轮
func TestScanDailyWear_TargetZeroAndReadError(t *testing.T) {
	loc := cstDailyFixture(t)
	day := time.Date(2026, 9, 19, 0, 0, 0, 0, loc)
	devs := newFakeDevices(
		Device{DeviceID: "DEV-A", PatientID: "P001"},
		Device{DeviceID: "DEV-B", PatientID: "P002"},
	)
	alerts := newFakeAlerts()
	wear := newFakeWear()
	wear.errFor["P002"] = true

	s := New(devs, alerts, newFakeLastSeen(), engine.NewDefaultRuleEvaluator())
	s.SetWearStore(wear)

	report, err := s.ScanDailyWear(context.Background(), day, 0)
	require.NoError(t, err)
	assert.Empty(t, alerts.created, "targetHours=0 = 规则未启用")
	assert.Zero(t, report.WearErrors, "规则关闭时先判空，不该走到读 rollup")

	// 阈值正常：P001 无 rollup 行（=0 分钟）触发；P002 读失败记错但不中断
	report2, err := s.ScanDailyWear(context.Background(), day, 18)
	require.NoError(t, err)
	assert.Equal(t, 1, report2.WearErrors)
	require.Len(t, alerts.created, 1)
	assert.Equal(t, "P001", alerts.created[0].PatientID, "缺行按 0 分钟处理（与 msg-service 佩戴提醒同语义）")
}

func TestScanDailyWear_ListDevicesError(t *testing.T) {
	devs := newFakeDevices()
	devs.listErr = errors.New("devices unavailable")
	s := New(devs, newFakeAlerts(), newFakeLastSeen(), engine.NewDefaultRuleEvaluator())
	s.SetWearStore(newFakeWear())

	_, err := s.ScanDailyWear(context.Background(), time.Now(), 18)
	assert.Error(t, err, "设备清单不可得 ⇒ 整轮失败，由调度层记录")
}

func TestPreviousBizDay_CutsOnBizZone(t *testing.T) {
	loc := cstDailyFixture(t)
	// 北京时间 2026-09-20 00:10 ⇒ 上一完整业务日 = 09-19 零点（UTC 09-18 16:00）
	got := PreviousBizDay(time.Date(2026, 9, 20, 0, 10, 0, 0, loc), loc)
	assert.True(t, got.Equal(time.Date(2026, 9, 19, 0, 0, 0, 0, loc)), "按业务时区切日，不按 UTC")
	assert.Equal(t, loc, got.Location())

	// UTC 时刻换到东八区已跨天：2026-09-19T16:30Z == 北京 09-20 00:30 ⇒ 上一日 09-19
	fromUTC := time.Date(2026, 9, 19, 16, 30, 0, 0, time.UTC)
	got2 := PreviousBizDay(fromUTC, loc)
	assert.True(t, got2.Equal(time.Date(2026, 9, 19, 0, 0, 0, 0, loc)), "输入 UTC 时刻也要按北京时间判日")
}
