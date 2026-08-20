//go:build integration
// +build integration

// Package repo 集成测试：真实 PG15 + Redis7（testcontainers）
// 对齐：docs/ §3.1 A5/A7/A8/A10 + PRD §8.1 设备状态机
//
//	佩戴中断扫描生成告警 / 去重窗口不重复 / 恢复上报自动 resolve / devices.status 联动
//	A10：alert-service 降级 → 帧入 alert:pending，恢复后补偿评估（幂等/积压不推送）
//
// 运行：make test-integration（需 Docker）
package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/config"
	"github.com/bracesync/bracesync/services/alert-service/internal/consumer"
	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
	"github.com/bracesync/bracesync/services/testhelper"
)

var (
	itPool  *pgxpool.Pool
	itRedis *redis.Client
)

const (
	itDevice  = "DEV-IT-A001"
	itPatient = "P-IT-A001"
	// 第二台设备：状态机 offline 场景（采集间隔调大避开 abnormal）
	itDevice2  = "DEV-IT-A002"
	itPatient2 = "P-IT-A002"
	// 未绑定设备：不应参与扫描
	itDeviceUnbound = "DEV-IT-A099"
)

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		return runIT(m, cfg.DBURL, cfg.RedisURL)
	})
}

// runIT 建连 + 迁移 + 种子 + 执行用例
func runIT(m *testing.M, dbURL, redisURL string) int {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "it: pgxpool: %v\n", err)
		return 1
	}
	itPool = pool
	defer pool.Close()

	applyMigrations(ctx)
	seedITData(ctx)

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "it: redis url: %v\n", err)
		return 1
	}
	itRedis = redis.NewClient(opts)
	defer itRedis.Close()

	return m.Run()
}

// migrationsDir 定位 scripts/db/migrations（相对本文件 4 级上）
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "db", "migrations")
}

// applyMigrations 顺序执行全部 *.up.sql（单一事实源，避免测试 schema 漂移）
func applyMigrations(ctx context.Context) {
	entries, err := os.ReadDir(migrationsDir())
	if err != nil {
		panic("it: read migrations dir: " + err.Error())
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		sqlBytes, readErr := os.ReadFile(filepath.Join(migrationsDir(), name))
		if readErr != nil {
			panic("it: read migration: " + readErr.Error())
		}
		if _, execErr := itPool.Exec(ctx, string(sqlBytes)); execErr != nil {
			panic(fmt.Sprintf("it: apply %s: %v", name, execErr))
		}
	}
}

func seedITData(ctx context.Context) {
	type stmt struct {
		sql  string
		args []any
	}
	stmts := []stmt{
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		  VALUES ($1, 'IT患者A', '\x00'::bytea, 'bb11' || repeat('0', 60), 'active')
		  ON CONFLICT (patient_id) DO NOTHING`, []any{itPatient}},
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		  VALUES ($1, 'IT患者B', '\x00'::bytea, 'bb22' || repeat('0', 60), 'active')
		  ON CONFLICT (patient_id) DO NOTHING`, []any{itPatient2}},
		{`INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		  VALUES ($1, '\x00'::bytea, $2, 'online')
		  ON CONFLICT (device_id) DO NOTHING`, []any{itDevice, itPatient}},
		{`INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		  VALUES ($1, '\x00'::bytea, $2, 'online')
		  ON CONFLICT (device_id) DO NOTHING`, []any{itDevice2, itPatient2}},
		{`INSERT INTO devices (device_id, device_secret_enc, status)
		  VALUES ($1, '\x00'::bytea, 'unbound')
		  ON CONFLICT (device_id) DO NOTHING`, []any{itDeviceUnbound}},
	}
	for _, s := range stmts {
		if _, err := itPool.Exec(ctx, s.sql, s.args...); err != nil {
			panic("it: seed: " + err.Error())
		}
	}
}

// ─────────────────────────────────────────────────────────────
// 测试装配
// ─────────────────────────────────────────────────────────────

// newITScanner 组装连真实 PG/Redis 的扫描器（默认阈值口径：中断 60min / 采集 30min）
func newITScanner(t *testing.T) *scanner.Scanner {
	t.Helper()
	return scanner.New(
		NewDeviceRepo(itPool),
		NewAlertRepo(itPool),
		NewRedisLastSeen(itRedis),
		engine.NewDefaultRuleEvaluator(),
	)
}

// resetIT 用例隔离：清空 alerts、重置设备状态、清 Redis lastseen
func resetIT(ctx context.Context, t *testing.T) {
	t.Helper()
	require.NoError(t, itRedis.FlushDB(ctx).Err())
	_, err := itPool.Exec(ctx, `TRUNCATE TABLE alerts RESTART IDENTITY`)
	require.NoError(t, err)
	_, err = itPool.Exec(ctx, `UPDATE devices SET status = 'online' WHERE device_id IN ($1, $2)`, itDevice, itDevice2)
	require.NoError(t, err)
}

// setLastSeen 模拟 data-service 写入 dev:lastseen:{device_id}（Unix 秒）
func setLastSeen(ctx context.Context, t *testing.T, deviceID string, ts time.Time) {
	t.Helper()
	require.NoError(t, itRedis.Set(ctx, "dev:lastseen:"+deviceID, strconv.FormatInt(ts.Unix(), 10), 0).Err())
}

func countAlerts(ctx context.Context, t *testing.T, deviceID string) int {
	t.Helper()
	var n int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE device_id = $1 AND type = 'wear_interrupt'`, deviceID).Scan(&n))
	return n
}

func deviceStatus(ctx context.Context, t *testing.T, deviceID string) string {
	t.Helper()
	var status string
	require.NoError(t, itPool.QueryRow(ctx, `SELECT status FROM devices WHERE device_id = $1`, deviceID).Scan(&status))
	return status
}

// ─────────────────────────────────────────────────────────────
// A5：lastseen 超阈值 → 扫描生成 wear_interrupt + 状态联动 abnormal
// ─────────────────────────────────────────────────────────────

func TestIT_A5_ScanGeneratesWearInterrupt(t *testing.T) {
	ctx := context.Background()
	resetIT(ctx, t)
	now := time.Now()
	setLastSeen(ctx, t, itDevice, now.Add(-90*time.Minute)) // 90min > 60min 阈值

	report, err := newITScanner(t).Scan(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, report.Scanned, "仅扫描绑定设备（未绑定设备不在清单内）")
	assert.Equal(t, 1, report.AlertCreated)
	assert.Equal(t, 1, countAlerts(ctx, t, itDevice))

	// 告警字段断言
	var (
		patientID         string
		resolvedStatus    string
		threshold, actual float64
		ts                time.Time
	)
	require.NoError(t, itPool.QueryRow(ctx, `
		SELECT patient_id, resolved_status, threshold_value, actual_value, ts
		FROM alerts WHERE device_id = $1 AND type = 'wear_interrupt'`, itDevice).
		Scan(&patientID, &resolvedStatus, &threshold, &actual, &ts))
	assert.Equal(t, itPatient, patientID)
	assert.Equal(t, "active", resolvedStatus)
	assert.InDelta(t, 60.0, threshold, 0.01)
	assert.InDelta(t, 90.0, actual, 1.0)
	assert.WithinDuration(t, now, ts, time.Minute)

	// 状态联动：90min 缺数 = 3×30min 采集周期 → abnormal（PRD §8.1）
	assert.Equal(t, "abnormal", deviceStatus(ctx, t, itDevice))
}

// ─────────────────────────────────────────────────────────────
// A7：去重 —— 同一中断窗口内不重复告警
// ─────────────────────────────────────────────────────────────

func TestIT_A7_DedupWithinWindow(t *testing.T) {
	ctx := context.Background()
	resetIT(ctx, t)
	now := time.Now()
	setLastSeen(ctx, t, itDevice, now.Add(-90*time.Minute))

	s := newITScanner(t)
	_, err := s.Scan(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, countAlerts(ctx, t, itDevice))

	// 第二轮扫描（5min 后 cron 再触发）：active 告警存在 → 不重复
	report, err := s.Scan(ctx)
	require.NoError(t, err)
	assert.Zero(t, report.AlertCreated)
	assert.Equal(t, 1, report.Deduped)
	assert.Equal(t, 1, countAlerts(ctx, t, itDevice), "去重窗口内不重复告警")

	// DB 唯一约束保底：同 (patient,device,type,ts) 直接重复插入被吞
	_, created, err := NewAlertRepo(itPool).CreateAlert(ctx, scanner.NewAlert{
		PatientID: itPatient, DeviceID: itDevice, Type: engine.TypeWearInterrupt,
		Detail: "dup", Ts: queryAlertTs(ctx, t, itDevice),
	})
	require.NoError(t, err)
	assert.False(t, created, "uk_alerts_natural 保底去重")
}

func queryAlertTs(ctx context.Context, t *testing.T, deviceID string) time.Time {
	t.Helper()
	var ts time.Time
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT ts FROM alerts WHERE device_id = $1 AND type = 'wear_interrupt' LIMIT 1`, deviceID).Scan(&ts))
	return ts
}

// ─────────────────────────────────────────────────────────────
// A8：设备恢复上报（lastseen 刷新，含补传语义）→ active 告警自动 resolved
// ─────────────────────────────────────────────────────────────

func TestIT_A8_RecoveryAutoResolve(t *testing.T) {
	ctx := context.Background()
	resetIT(ctx, t)
	now := time.Now()
	setLastSeen(ctx, t, itDevice, now.Add(-90*time.Minute))

	s := newITScanner(t)
	_, err := s.Scan(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, countAlerts(ctx, t, itDevice))
	assert.Equal(t, "abnormal", deviceStatus(ctx, t, itDevice))

	// 设备恢复上报：data-service 单帧/补传均把 lastseen 置为当前时刻
	setLastSeen(ctx, t, itDevice, now)
	report, err := newITScanner(t).Scan(ctx) // 下一轮扫描（恢复后首轮）
	require.NoError(t, err)
	assert.EqualValues(t, 1, report.Resolved)

	// 告警置 resolved + resolved_at（与 process_status 正交，process_status 仍为 pending）
	var resolvedStatus, processStatus string
	var resolvedAt *time.Time
	require.NoError(t, itPool.QueryRow(ctx, `
		SELECT resolved_status, process_status, resolved_at
		FROM alerts WHERE device_id = $1 AND type = 'wear_interrupt'`, itDevice).
		Scan(&resolvedStatus, &processStatus, &resolvedAt))
	assert.Equal(t, "resolved", resolvedStatus)
	assert.Equal(t, "pending", processStatus, "resolve 与处理流程正交")
	require.NotNil(t, resolvedAt)

	// 状态联动恢复 online
	assert.Equal(t, "online", deviceStatus(ctx, t, itDevice))

	// 再次扫描幂等：已 resolved 的告警不再被 resolve
	report2, err := newITScanner(t).Scan(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 0, report2.Resolved)
}

// ─────────────────────────────────────────────────────────────
// 状态机：offline 兜底（abnormal 未激活时）+ 未绑定设备不扫描
// ─────────────────────────────────────────────────────────────

func TestIT_StatusOfflineFallback(t *testing.T) {
	ctx := context.Background()
	resetIT(ctx, t)
	now := time.Now()
	// 采集间隔调大为 60min → abnormal 需 180min；130min 缺数 → offline（PRD §8.1 例）
	setLastSeen(ctx, t, itDevice2, now.Add(-130*time.Minute))
	setLastSeen(ctx, t, itDevice, now.Add(-5*time.Minute)) // 对照组保持 online

	eval := engine.NewDefaultRuleEvaluator()
	eval.CollectionIntervalMin = 60
	s := scanner.New(NewDeviceRepo(itPool), NewAlertRepo(itPool), NewRedisLastSeen(itRedis), eval)
	report, err := s.Scan(ctx)
	require.NoError(t, err)

	assert.Equal(t, "offline", deviceStatus(ctx, t, itDevice2))
	assert.Equal(t, "online", deviceStatus(ctx, t, itDevice))
	assert.Equal(t, 1, report.StatusChanged, "仅 DEV002 状态迁移")

	// 未绑定设备即使 lastseen 超阈值也不产生告警
	setLastSeen(ctx, t, itDeviceUnbound, now.Add(-24*time.Hour))
	_, err = s.Scan(ctx)
	require.NoError(t, err)
	assert.Zero(t, countAlerts(ctx, t, itDeviceUnbound))
}

// 无 lastseen 的绑定设备：跳过（不误报，状态由查询时推导兜底）
func TestIT_NoLastSeenSkipped(t *testing.T) {
	ctx := context.Background()
	resetIT(ctx, t)

	report, err := newITScanner(t).Scan(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, report.MissedLastSeen)
	assert.Zero(t, report.AlertCreated)
	assert.Equal(t, "online", deviceStatus(ctx, t, itDevice), "无 lastseen 不改状态")
}

// ─────────────────────────────────────────────────────────────
// A6（T009）：sys_configs 阈值配置读写 + Manager 联动校验
// ─────────────────────────────────────────────────────────────

func itInt(v int) *int { return &v }

// cleanITConfigs 用例隔离：清空阈值相关配置行（空表 = 解析层回退 PRD 默认）
func cleanITConfigs(ctx context.Context, t *testing.T) {
	t.Helper()
	_, err := itPool.Exec(ctx, `DELETE FROM sys_configs WHERE config_key = ANY($1)`, config.Keys())
	require.NoError(t, err)
}

func TestIT_A6_ConfigRepo_FetchAndUpsert(t *testing.T) {
	ctx := context.Background()
	cleanITConfigs(ctx, t)
	r := NewConfigRepo(itPool)

	m, err := r.FetchAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, m, "空表返回空 map（解析层回退默认）")

	require.NoError(t, r.Upsert(ctx, map[string]string{
		config.KeyCollectInterval: "40",
		config.KeyWearInterrupt:   "80",
	}))
	m, err = r.FetchAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, "40", m[config.KeyCollectInterval])
	assert.Equal(t, "80", m[config.KeyWearInterrupt])

	// 覆盖写：同键更新
	require.NoError(t, r.Upsert(ctx, map[string]string{config.KeyWearInterrupt: "90"}))
	m, err = r.FetchAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, "90", m[config.KeyWearInterrupt])
}

func TestIT_A6_Manager_LinkageValidation(t *testing.T) {
	ctx := context.Background()
	cleanITConfigs(ctx, t)
	mgr := config.NewManager(NewConfigRepo(itPool))

	// 空表加载 → PRD 默认（30/60），联动校验自洽通过
	cur, err := mgr.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, 30, cur.CollectIntervalMinutes)
	assert.Equal(t, 60, cur.WearInterruptMinutes)

	// A6 拒绝分支：仅改采集间隔 40min → 中断 60min < 2×40=80min
	_, err = mgr.Update(ctx, config.ThresholdPatch{CollectIntervalMinutes: itInt(40)})
	require.Error(t, err)
	var verr *config.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, config.ErrCodeThresholdLinkage, verr.Code)

	// 确认拒绝写入：sys_configs 仍无配置行
	m, err := NewConfigRepo(itPool).FetchAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, m, "校验失败不落库")

	// A6 通过分支：联动修改采集间隔 40min + 中断阈值 80min（=2× 边界，PRD ≥ 语义）
	th, err := mgr.Update(ctx, config.ThresholdPatch{
		CollectIntervalMinutes: itInt(40),
		WearInterruptMinutes:   itInt(80),
	})
	require.NoError(t, err)
	assert.Equal(t, 40, th.CollectIntervalMinutes)
	assert.Equal(t, 80, th.WearInterruptMinutes)

	// DB 行已更新（引擎热更新数据源）
	m, err = NewConfigRepo(itPool).FetchAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, "40", m[config.KeyCollectInterval])
	assert.Equal(t, "80", m[config.KeyWearInterrupt])

	// 引擎读取最新配置值
	eval := engine.NewDefaultRuleEvaluator()
	_, err = mgr.Refresh(ctx, eval)
	require.NoError(t, err)
	assert.Equal(t, 80, eval.WearInterruptMinutes)
	assert.Equal(t, 40, eval.CollectionIntervalMin)
}

// ─────────────────────────────────────────────────────────────
// A10（T010）：降级队列 —— 帧入 alert:pending，恢复后补偿评估（幂等/积压不推送）
// ─────────────────────────────────────────────────────────────

// itPendingPayload 构造与 data-service pendingAlertItem 格式一致的队列负载
func itPendingPayload(t *testing.T, queuedAt, frameTS time.Time, maxPressure float64) string {
	t.Helper()
	points := make([]float64, consumer.PointCount)
	points[2] = maxPressure // P03 超阈值 → pressure_high
	b, err := json.Marshal(consumer.PendingItem{
		QueuedAt: queuedAt.UTC(),
		Frame: consumer.FrameRef{
			DeviceID:  itDevice,
			PatientID: itPatient,
			Timestamp: frameTS.UTC(),
			Points:    points,
		},
	})
	require.NoError(t, err)
	return string(b)
}

// itNewConsumer 组装连真实 PG/Redis 的消费者（notifier 用记录型验证推送语义）
func itNewConsumer(notifier consumer.Notifier) *consumer.Consumer {
	return consumer.New(
		NewRedisPendingQueue(itRedis),
		NewRedisEvalDedup(itRedis),
		NewAlertRepo(itPool),
		engine.NewDefaultRuleEvaluator(),
		notifier,
	)
}

type itNotifier struct{ n int }

func (n *itNotifier) Notify(context.Context, scanner.NewAlert) { n.n++ }

func itCountPressureAlerts(ctx context.Context, t *testing.T, deviceID string) int {
	t.Helper()
	var n int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE device_id = $1 AND type = 'pressure_high'`, deviceID).Scan(&n))
	return n
}

// 队列访问层：Pop/LLen FIFO 语义 + 幂等键 SET NX
func TestIT_A10_PendingQueueAndDedupPrimitives(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, itRedis.FlushDB(ctx).Err())

	q := NewRedisPendingQueue(itRedis)
	_, ok, err := q.Pop(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "空队列 ok=false，非错误")

	// data-service 侧 LPUSH；本服务 RPOP → FIFO
	require.NoError(t, itRedis.LPush(ctx, "alert:pending", "first", "second").Err())
	n, err := q.Len(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
	v, ok, err := q.Pop(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "first", v, "LPUSH+RPOP = FIFO（降级顺序补偿）")

	d := NewRedisEvalDedup(itRedis)
	ts := time.Now().Truncate(time.Second)
	first, err := d.MarkEvaluated(ctx, itDevice, ts)
	require.NoError(t, err)
	assert.True(t, first)
	first, err = d.MarkEvaluated(ctx, itDevice, ts)
	require.NoError(t, err)
	assert.False(t, first, "(device_id, timestamp) 幂等键重复置返回 first=false")
	ttl, err := itRedis.TTL(ctx, "alert:evaluated:"+itDevice+":"+strconv.FormatInt(ts.Unix(), 10)).Result()
	require.NoError(t, err)
	assert.True(t, ttl > 47*time.Hour, "幂等键带 TTL 不残留")
}

// 验收①②：模拟 data-service 降级入队 → 消费者自动排空 + 补偿评估生成告警，
// 重复入队幂等不重复告警
func TestIT_A10_CompensationDrainsQueueIdempotently(t *testing.T) {
	ctx := context.Background()
	resetIT(ctx, t)
	now := time.Now()
	frameTS := now.Add(-2 * time.Minute).Truncate(time.Second)

	// 同一帧被重复入队两次（模拟 data-service 重试/重放）+ 一帧正常帧
	payload := itPendingPayload(t, now, frameTS, 50.0)
	require.NoError(t, itRedis.LPush(ctx, "alert:pending",
		payload, payload, itPendingPayload(t, now, frameTS.Add(-30*time.Minute), 12.0)).Err())

	notifier := &itNotifier{}
	c := itNewConsumer(notifier)
	processed, err := c.DrainOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, processed)

	// 队列排空
	qlen, err := itRedis.LLen(ctx, "alert:pending").Result()
	require.NoError(t, err)
	assert.Zero(t, qlen, "服务可用即排空积压")

	// 补偿评估生成告警：超阈值帧 1 条（重复入队被幂等键抑制）；正常帧不产生告警
	assert.Equal(t, 1, itCountPressureAlerts(ctx, t, itDevice))
	assert.Equal(t, 1, notifier.n, "新鲜帧命中 → 推送一次")

	// 告警时刻 = 帧采集时刻；幂等键已置
	var alertTS time.Time
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT ts FROM alerts WHERE device_id = $1 AND type = 'pressure_high'`, itDevice).Scan(&alertTS))
	assert.True(t, alertTS.Equal(frameTS), "告警时刻 = 帧采集时刻")

	// 第二轮（空队列）幂等空转
	processed, err = c.DrainOnce(ctx)
	require.NoError(t, err)
	assert.Zero(t, processed)
	assert.Equal(t, 1, itCountPressureAlerts(ctx, t, itDevice))
}

// 验收③：>1h 积压帧仅补告警记录不推送
func TestIT_A10_StaleFrameRecordsWithoutPush(t *testing.T) {
	ctx := context.Background()
	resetIT(ctx, t)
	now := time.Now()

	// 入队时刻在 2h 前（积压 >1h）
	require.NoError(t, itRedis.LPush(ctx, "alert:pending",
		itPendingPayload(t, now.Add(-2*time.Hour), now.Add(-2*time.Hour), 50.0)).Err())

	notifier := &itNotifier{}
	c := itNewConsumer(notifier)
	processed, err := c.DrainOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, processed)

	assert.Equal(t, 1, itCountPressureAlerts(ctx, t, itDevice), "积压帧仍补告警记录")
	assert.Zero(t, notifier.n, ">1h 积压帧不推送（避免过时骚扰）")
}

// ─────────────────────────────────────────────────────────────
// T019：告警通知联调 — alert→msg 推送链路端到端
// ─────────────────────────────────────────────────────────────

// TestIT_T019_AlertNotifyEndToEnd 告警生成 → HTTPNotifier → mock msg-service 接收验证
//
// 链路：
//  1. 构造超阈值帧入 alert:pending
//  2. consumer 补偿评估 → 告警落库（PG alerts 表）
//  3. HTTPNotifier POST /internal/msg/send → mock msg-service 接收
//  4. 验证：告警记录含 sensor_point + alert_id；mock 收到合法请求体
func TestIT_T019_AlertNotifyEndToEnd(t *testing.T) {
	ctx := context.Background()
	resetIT(ctx, t)
	now := time.Now()

	// 启动 mock msg-service（接收并验证请求）
	var receivedBody atomic.Value
	var receivedHeader atomic.Value
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader.Store(r.Header.Clone())
		body, _ := io.ReadAll(r.Body)
		receivedBody.Store(string(body))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"accepted":true,"recordId":"rec-001"}}`))
	}))
	defer mockSrv.Close()

	// 构造 HTTPNotifier 指向 mock
	retryQueue := NewRedisNotifyRetryQueue(itRedis)
	notifier := consumer.NewHTTPNotifier(consumer.HTTPNotifierConfig{
		MsgServiceURL: mockSrv.URL,
		Timeout:       2 * time.Second,
		MaxRetries:    3,
		RetryQueue:    retryQueue,
	})

	// 构造超阈值帧（50N > 45N → pressure_high，P03 点）
	points := make([]float64, 20)
	points[2] = 50.0 // P03
	frameTS := now.Add(-time.Minute)
	payload := consumer.PendingItem{
		QueuedAt: now.UTC(),
		Frame: consumer.FrameRef{
			DeviceID:  itDevice,
			PatientID: itPatient,
			Timestamp: frameTS.UTC(),
			Points:    points,
		},
	}
	b, _ := json.Marshal(&payload)
	require.NoError(t, itRedis.LPush(ctx, "alert:pending", string(b)).Err())

	// 消费
	c := consumer.New(
		NewRedisPendingQueue(itRedis),
		NewRedisEvalDedup(itRedis),
		NewAlertRepo(itPool),
		engine.NewDefaultRuleEvaluator(),
		notifier,
	)
	processed, err := c.DrainOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, processed)

	// 验证 1：PG 告警记录落库（含 sensor_point）
	var (
		alertID     int64
		sensorPoint string
		alertType   string
	)
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT alert_id, type, COALESCE(sensor_point, '') FROM alerts
		 WHERE device_id = $1 AND type = 'pressure_high'
		 ORDER BY alert_id DESC LIMIT 1`, itDevice).Scan(&alertID, &alertType, &sensorPoint))
	assert.Greater(t, alertID, int64(0), "alert_id 已生成")
	assert.Equal(t, "pressure_high", alertType)
	assert.Equal(t, "P03", sensorPoint, "sensor_point 已落库")

	// 验证 2：mock msg-service 收到请求
	bodyStr, ok := receivedBody.Load().(string)
	require.True(t, ok, "mock msg-service 收到请求")
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal([]byte(bodyStr), &reqBody))
	assert.Equal(t, fmt.Sprintf("%d", alertID), reqBody["alertId"], "alertId 对齐 PG 主键")
	assert.Equal(t, "pressure_high", reqBody["type"])
	assert.Equal(t, itPatient, reqBody["patientId"])
	assert.Equal(t, itDevice, reqBody["deviceId"])
	assert.Equal(t, "P03", reqBody["sensorPoint"])

	// 验证 3：X-Internal-Service 鉴权头
	hdr, ok := receivedHeader.Load().(http.Header)
	require.True(t, ok)
	assert.Equal(t, "alert-service", hdr.Get("X-Internal-Service"))

	// 验证 4：重试队列为空（推送成功不入队）
	qLen, err := itRedis.LLen(ctx, "alert:notify_pending").Result()
	require.NoError(t, err)
	assert.Zero(t, qLen, "推送成功，重试队列空")
}

// TestIT_T019_NotifyFailureRetryQueue 推送失败 → 入重试队列 → 重试成功
func TestIT_T019_NotifyFailureRetryQueue(t *testing.T) {
	ctx := context.Background()
	resetIT(ctx, t)
	now := time.Now()

	callCount := 0
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			// 首次返回 500
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":90001,"message":"internal error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"accepted":true}}`))
	}))
	defer mockSrv.Close()

	retryQueue := NewRedisNotifyRetryQueue(itRedis)
	notifier := consumer.NewHTTPNotifier(consumer.HTTPNotifierConfig{
		MsgServiceURL: mockSrv.URL,
		Timeout:       2 * time.Second,
		MaxRetries:    3,
		RetryQueue:    retryQueue,
	})

	// 构造帧入队列
	points := make([]float64, 20)
	points[2] = 50.0
	payload := consumer.PendingItem{
		QueuedAt: now.UTC(),
		Frame: consumer.FrameRef{
			DeviceID:  itDevice,
			PatientID: itPatient,
			Timestamp: now.Add(-time.Minute).UTC(),
			Points:    points,
		},
	}
	b, _ := json.Marshal(&payload)
	require.NoError(t, itRedis.LPush(ctx, "alert:pending", string(b)).Err())

	c := consumer.New(
		NewRedisPendingQueue(itRedis),
		NewRedisEvalDedup(itRedis),
		NewAlertRepo(itPool),
		engine.NewDefaultRuleEvaluator(),
		notifier,
	)
	processed, err := c.DrainOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, processed)
	assert.Equal(t, 1, callCount, "首次推送被调用")

	// 推送失败 → 重试队列有 1 条
	qLen, err := itRedis.LLen(ctx, "alert:notify_pending").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), qLen, "推送失败后入重试队列")

	// 重试排空 → 第二次调用成功
	retryProcessed, err := notifier.DrainRetryOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, retryProcessed)
	assert.Equal(t, 2, callCount, "重试触发第二次推送")

	// 重试队列清空
	qLen, err = itRedis.LLen(ctx, "alert:notify_pending").Result()
	require.NoError(t, err)
	assert.Zero(t, qLen, "重试成功后队列清空")
}
