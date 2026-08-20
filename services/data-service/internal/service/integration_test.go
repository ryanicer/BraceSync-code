//go:build integration
// +build integration

// Package service 集成测试：真实 PG15 + Redis7（testcontainers）
// 对齐：docs/ §5 设备链路用例
//
//	单帧/批量补传/补传幂等/断网恢复（lastseen）/限流通道隔离（单测覆盖）/降级队列
//
// 运行：make test-integration（需 Docker）
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
	"github.com/bracesync/bracesync/services/testhelper"
)

var (
	itPool  *pgxpool.Pool
	itRedis *redis.Client
)

const (
	itDevice  = "DEV-IT-001"
	itPatient = "P-IT-001"
)

// skipCSTMidnightGuard 当前时刻距 Asia/Shanghai 当日 0 点不足 minHours 时跳过用例：
// 用例内回溯 N 小时的帧会跨 CST 切日，"当日口径"断言（Total/rollup 单日）
// 在该窗口内天然不成立，属确定性边界而非缺陷，跳过优于 flaky。
func skipCSTMidnightGuard(t *testing.T, minHours float64) {
	t.Helper()
	local := time.Now().In(model.CSTZone())
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, model.CSTZone())
	if since := local.Sub(dayStart); since < time.Duration(minHours*float64(time.Hour)) {
		t.Skipf("距 Asia/Shanghai 当日 0 点仅 %s，跳过跨日敏感用例", since.Round(time.Second))
	}
}

// truncateRecords 清空 pressure_records（含全部分区）：
// 各用例共享同一 PG 实例，用例间须隔离，避免前行数据串扰断言。
func truncateRecords(ctx context.Context) {
	if _, err := itPool.Exec(ctx, `TRUNCATE TABLE pressure_records`); err != nil {
		panic("it: truncate pressure_records: " + err.Error())
	}
}

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		return runIT(m, cfg.DBURL, cfg.RedisURL)
	})
}

// runIT 建连 + 迁移/分区/种子 + 执行用例
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
	ensurePartitions(ctx)
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

// ensurePartitions 为当前月及未来 2 个月预建分区（防止 CI 时钟超出初始分区范围）
func ensurePartitions(ctx context.Context) {
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		start := time.Date(now.Year(), now.Month()+time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		ddl := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS pressure_records_%s PARTITION OF pressure_records FOR VALUES FROM ('%s') TO ('%s')`,
			start.Format("200601"), start.Format("2006-01-02"), end.Format("2006-01-02"))
		if _, err := itPool.Exec(ctx, ddl); err != nil {
			if strings.Contains(err.Error(), "would overlap") {
				continue // 已被初始迁移分区覆盖
			}
			panic("it: ensure partition: " + err.Error())
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
		  VALUES ($1, 'IT患者', '\x00'::bytea, 'aa11' || repeat('0', 60), 'active')
		  ON CONFLICT (patient_id) DO NOTHING`, []any{itPatient}},
		{`INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		  VALUES ($1, '\x00'::bytea, $2, 'online')
		  ON CONFLICT (device_id) DO NOTHING`, []any{itDevice, itPatient}},
		{`INSERT INTO sys_configs (config_key, config_value) VALUES
		    ('collect_interval_minutes', '30'),
		    ('device_config_version', '1')
		  ON CONFLICT (config_key) DO NOTHING`, nil},
	}
	for _, s := range stmts {
		if _, err := itPool.Exec(ctx, s.sql, s.args...); err != nil {
			panic("it: seed: " + err.Error())
		}
	}
}

// newITSvc 组装连真实 PG/Redis 的服务（NoopAlerts=模拟 alert-service 降级场景）
func newITSvc(t *testing.T, alerts AlertEvaluator) *RecordService {
	t.Helper()
	if alerts == nil {
		alerts = NoopAlertClient{}
	}
	svc := NewRecordService(
		repo.NewRecordRepo(itPool),
		repo.NewDeviceRepo(itPool),
		repo.NewConfigRepo(itPool),
		repo.NewRedisCache(itRedis),
		alerts,
		NewRateLimiter(1e9, 1e9, 1e9, 1e9),
	)
	svc.now = time.Now
	return svc
}

func itPoints(v float64) []float64 {
	out := make([]float64, model.PointCount)
	out[2] = v // P03 为最大点
	return out
}

// ─────────────────────────────────────────────────────────────
// 单帧上报：落库 + Redis 三写 + 幂等 + 降级队列
// ─────────────────────────────────────────────────────────────

func TestIT_SingleFrame_IdempotentAndRedisKeys(t *testing.T) {
	ctx := context.Background()
	svc := newITSvc(t, nil)
	require.NoError(t, itRedis.FlushDB(ctx).Err())
	truncateRecords(ctx)

	// 协议口径 timestamp 为 Unix 秒：构造与断言统一用整秒值，
	// 避免 time.Now() 亚秒分量与库内整秒值做等值比较导致查无行。
	ts := time.Unix(time.Now().Add(-5*time.Minute).Unix(), 0)
	req := &model.SingleFrameRequest{DeviceID: itDevice, Timestamp: ts.Unix(), Points: itPoints(32.5), Battery: 90, Firmware: "v1.2.0"}

	resp, appErr := svc.UploadSingle(ctx, itDevice, req)
	require.Nil(t, appErr)
	assert.False(t, resp.Duplicated)
	assert.NotEmpty(t, resp.RecordID)
	assert.Equal(t, 30, resp.Config.IntervalMinutes)

	// 真实落库 pressure_records（psql 可查口径）
	var count int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM pressure_records WHERE device_id = $1 AND ts = $2`, itDevice, ts.UTC()).Scan(&count))
	assert.Equal(t, 1, count)

	// max_pressure 生成列自动计算
	var maxP float32
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT max_pressure FROM pressure_records WHERE device_id = $1 AND ts = $2`, itDevice, ts.UTC()).Scan(&maxP))
	assert.InDelta(t, 32.5, float64(maxP), 0.01)

	// Redis 三 key
	lastseen, err := itRedis.Get(ctx, "dev:lastseen:"+itDevice).Result()
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%d", ts.Unix()), lastseen)

	frame, err := itRedis.Get(ctx, "rt:frame:"+itDevice).Result()
	require.NoError(t, err)
	assert.Contains(t, frame, `"deviceId":"`+itDevice+`"`)
	ttl, err := itRedis.TTL(ctx, "rt:frame:"+itDevice).Result()
	require.NoError(t, err)
	assert.True(t, ttl > time.Hour && ttl <= 2*time.Hour, "rt:frame TTL 2h")

	stat, err := itRedis.HGetAll(ctx, "stat:today:"+itPatient).Result()
	require.NoError(t, err)
	assert.Equal(t, "30", stat["wear_minutes"], "佩戴帧累计一个采集间隔")
	assert.Equal(t, "32.5", stat["max_pressure"])
	assert.Equal(t, "P03", stat["max_point"])

	// 降级场景（NoopAlerts）：帧引用入 alert:pending
	pendingLen, err := itRedis.LLen(ctx, "alert:pending").Result()
	require.NoError(t, err)
	assert.EqualValues(t, 1, pendingLen)

	// 幂等：重复帧不重复落库、不重复统计
	resp2, appErr := svc.UploadSingle(ctx, itDevice, req)
	require.Nil(t, appErr)
	assert.True(t, resp2.Duplicated)
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM pressure_records WHERE device_id = $1`, itDevice).Scan(&count))
	assert.Equal(t, 1, count)
	stat, _ = itRedis.HGetAll(ctx, "stat:today:"+itPatient).Result()
	assert.Equal(t, "30", stat["wear_minutes"], "重复帧不得重复累计佩戴分钟")
	pendingLen, _ = itRedis.LLen(ctx, "alert:pending").Result()
	assert.EqualValues(t, 1, pendingLen, "重复帧不重复评估/入队")
}

// ─────────────────────────────────────────────────────────────
// 批量补传：幂等去重 + 跳过告警 + rollup 重算入队
// ─────────────────────────────────────────────────────────────

func TestIT_BatchBackfill_IdempotentAndRollup(t *testing.T) {
	skipCSTMidnightGuard(t, 3.5) // 帧回溯 3h，切日窗口内 rollup"单日 1 条"断言不成立
	ctx := context.Background()
	svc := newITSvc(t, nil)
	require.NoError(t, itRedis.FlushDB(ctx).Err())
	truncateRecords(ctx)

	now := time.Now()
	mk := func(offset time.Duration, v float64) model.BatchFrame {
		return model.BatchFrame{Timestamp: now.Add(offset).Unix(), Points: itPoints(v), Battery: 80}
	}
	req := &model.BatchRequest{DeviceID: itDevice, Frames: []model.BatchFrame{
		mk(-3*time.Hour, 10), mk(-2*time.Hour, 11), mk(-time.Hour, 12),
	}}

	resp, appErr := svc.UploadBatch(ctx, itDevice, req)
	require.Nil(t, appErr)
	assert.Equal(t, 3, resp.Accepted)
	assert.Equal(t, 0, resp.Duplicated)
	assert.Empty(t, resp.Rejected)

	// 跳过实时告警：alert:pending 无新增
	pendingLen, err := itRedis.LLen(ctx, "alert:pending").Result()
	require.NoError(t, err)
	assert.Zero(t, pendingLen)

	// 受影响日期入 rollup 队列（Asia/Shanghai 同日 → 1 条）
	rollupLen, err := itRedis.LLen(ctx, "rollup:recompute").Result()
	require.NoError(t, err)
	assert.EqualValues(t, 1, rollupLen)

	// 断网恢复语义：lastseen 置为当前时刻
	lastseen, err := itRedis.Get(ctx, "dev:lastseen:"+itDevice).Result()
	require.NoError(t, err)
	assert.NotEmpty(t, lastseen)
	lsSec, convErr := strconv.ParseInt(lastseen, 10, 64)
	require.NoError(t, convErr)
	assert.InDelta(t, now.Unix(), lsSec, 120, "lastseen 应为补传受理时刻（设备刚恢复联网）")

	// 幂等重发：整批去重，不产生重复数据
	resp2, appErr := svc.UploadBatch(ctx, itDevice, req)
	require.Nil(t, appErr)
	assert.Equal(t, 0, resp2.Accepted)
	assert.Equal(t, 3, resp2.Duplicated)

	var count int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM pressure_records WHERE device_id = $1`, itDevice).Scan(&count))
	assert.Equal(t, 3, count, "重发不得产生重复行")

	// rollup 标记幂等：同日重复投递被跳过
	rollupLen, _ = itRedis.LLen(ctx, "rollup:recompute").Result()
	assert.EqualValues(t, 1, rollupLen)
}

// ─────────────────────────────────────────────────────────────
// 历史查询 + 实时快照（读路径）
// ─────────────────────────────────────────────────────────────

func TestIT_HistoryAndRealtime(t *testing.T) {
	skipCSTMidnightGuard(t, 3) // 帧回溯 150min，切日窗口内"当日 Total=3"断言不成立
	ctx := context.Background()
	svc := newITSvc(t, nil)
	require.NoError(t, itRedis.FlushDB(ctx).Err())
	truncateRecords(ctx)

	now := time.Now()
	// 造当日 3 帧
	for i := 0; i < 3; i++ {
		req := &model.SingleFrameRequest{
			DeviceID: itDevice, Timestamp: now.Add(-time.Duration(90+i*30) * time.Minute).Unix(),
			Points: itPoints(20 + float64(i)), Battery: 85, Firmware: "v1.2.0",
		}
		_, appErr := svc.UploadSingle(ctx, itDevice, req)
		require.Nil(t, appErr)
	}

	today := now.In(model.CSTZone()).Format("2006-01-02")
	page, appErr := svc.GetHistory(ctx, itPatient, "day", today, 1, 2)
	require.Nil(t, appErr)
	assert.Equal(t, int64(3), page.Total)
	assert.Len(t, page.List, 2)
	assert.Equal(t, 2, page.PageSize)
	require.Len(t, page.List[0].Points, model.PointCount)

	page2, appErr := svc.GetHistory(ctx, itPatient, "day", today, 2, 2)
	require.Nil(t, appErr)
	assert.Len(t, page2.List, 1)

	// 实时快照：零 DB 明细，读 Redis
	snap, appErr := svc.GetRealtime(ctx, itPatient)
	require.Nil(t, appErr)
	assert.Equal(t, "online", snap.Status)
	assert.InDelta(t, 1.5, snap.TodayHours, 0.01) // 3 佩戴帧 × 30min
	assert.InDelta(t, 22.0, snap.MaxPressure, 0.01)
	assert.Equal(t, "P03", snap.MaxPoint)
	require.Len(t, snap.PressureRecords, 1)
	assert.Equal(t, itDevice, snap.PressureRecords[0].DeviceID)
}

// ─────────────────────────────────────────────────────────────
// 边界：未注册/未绑定设备
// ─────────────────────────────────────────────────────────────

func TestIT_DeviceBindingErrors(t *testing.T) {
	ctx := context.Background()
	svc := newITSvc(t, nil)

	// 未注册 20404
	req := &model.SingleFrameRequest{DeviceID: "DEV-NO-SUCH", Timestamp: time.Now().Unix(), Points: itPoints(1)}
	_, appErr := svc.UploadSingle(ctx, "DEV-NO-SUCH", req)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeDeviceNotFound, appErr.Code)

	// 未绑定 20409
	_, err := itPool.Exec(ctx,
		`INSERT INTO devices (device_id, device_secret_enc, status) VALUES ('DEV-IT-UNBOUND', '\x00'::bytea, 'unbound') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	req.DeviceID = "DEV-IT-UNBOUND"
	_, appErr = svc.UploadSingle(ctx, "DEV-IT-UNBOUND", req)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeDeviceUnbound, appErr.Code)
}
