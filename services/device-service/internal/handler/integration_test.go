//go:build integration
// +build integration

// Package handler 集成测试：真实 PG15（testcontainers）验证 T137 状态回写入口
// POST /internal/devices/:deviceId/report 端到端把 devices.last_report_at 从 NULL 回填为帧采集时刻。
//
// data-service 上报链路（services/data-service/internal/service/integration_test.go）
// 负责证明"上报后一定会打这个端点"，本文件负责证明"打了这个端点字段真的落库"。
//
// 运行：make test-integration（需 Docker）
package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
	"github.com/bracesync/bracesync/services/device-service/internal/service"
	"github.com/bracesync/bracesync/services/device-service/internal/testutil"
	"github.com/bracesync/bracesync/services/testhelper"
)

var itPool *pgxpool.Pool

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, cfg.DBURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "it: pgxpool: %v\n", err)
			return 1
		}
		itPool = pool
		defer pool.Close()

		applyITMigrations(ctx)
		return m.Run()
	})
}

// itMigrationsDir 定位 scripts/db/migrations（相对本文件 4 级上）
func itMigrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "db", "migrations")
}

// applyITMigrations 顺序执行全部 *.up.sql（单一事实源，避免测试 schema 漂移）
func applyITMigrations(ctx context.Context) {
	entries, err := os.ReadDir(itMigrationsDir())
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
		sqlBytes, readErr := os.ReadFile(filepath.Join(itMigrationsDir(), name))
		if readErr != nil {
			panic("it: read migration: " + readErr.Error())
		}
		if _, execErr := itPool.Exec(ctx, string(sqlBytes)); execErr != nil {
			panic(fmt.Sprintf("it: apply %s: %v", name, execErr))
		}
	}
}

// itEnv 组装走真实 PG 的 handler（store 走 PGStore；fake store 仅占位不参与本用例路径）
func itEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	store := repo.NewPGStore(itPool)
	svc := service.NewDeviceService(store, enc)
	return &testEnv{router: New(svc).Router(), store: testutil.NewFakeStore()}
}

// itRegisterUnreported 注册一台从未上报过的设备（last_report_at 保持建表默认 NULL）
func itRegisterUnreported(t *testing.T, deviceID string) {
	t.Helper()
	store := repo.NewPGStore(itPool)
	_, err := store.RegisterDevice(context.Background(), &model.Device{
		DeviceID: deviceID, Model: model.DefaultModel,
		DeviceSecretEnc: []byte{0x01, 0x02, 0x03}, SecretVersion: 1, Status: model.StatusUnbound,
	})
	require.NoError(t, err)
	_, err = itPool.Exec(context.Background(),
		`UPDATE devices SET last_report_at = NULL, status = 'unbound' WHERE device_id = $1`, deviceID)
	require.NoError(t, err)
}

// itLastReportAt 读 devices.last_report_at；nil = 列仍为空
func itLastReportAt(t *testing.T, deviceID string) *time.Time {
	t.Helper()
	var ts *time.Time
	err := itPool.QueryRow(context.Background(),
		`SELECT last_report_at FROM devices WHERE device_id = $1`, deviceID).Scan(&ts)
	require.NoError(t, err)
	return ts
}

// TestIT_ReportEndpoint_BackfillsLastReportAt T137 实测：一次上报调用前后该字段的取值变化
func TestIT_ReportEndpoint_BackfillsLastReportAt(t *testing.T) {
	ctx := context.Background()
	const deviceID = "DEV-IT-HREPORT-001"
	itRegisterUnreported(t, deviceID)

	require.Nil(t, itLastReportAt(t, deviceID), "未上报过的设备该列须为空（T126 生产实测同形）")
	t.Logf("上报前：devices[%s].last_report_at = NULL", deviceID)

	frameTS := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	env := itEnv(t)
	status, resp := env.do(t, http.MethodPost, "/internal/devices/"+deviceID+"/report",
		map[string]any{"timestamp": frameTS.Unix(), "fault_code": 0}, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 0, resp.Code)

	after := itLastReportAt(t, deviceID)
	require.NotNil(t, after, "回写后须有值")
	t.Logf("上报后：devices[%s].last_report_at = %s", deviceID, after.Format(time.RFC3339Nano))
	assert.True(t, after.Equal(frameTS), "须回填为帧采集时刻，实际 %s 期望 %s", after.Format(time.RFC3339), frameTS.Format(time.RFC3339))

	dev, err := repo.NewPGStore(itPool).GetDevice(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusOnline, dev.Status, "fault_code=0 → online（PRD §8.1 上报即在线）")
}

// TestIT_ReportEndpoint_StaleFrameDoesNotRegress 陈旧补传帧经同一端点不得把字段往回拨
func TestIT_ReportEndpoint_StaleFrameDoesNotRegress(t *testing.T) {
	ctx := context.Background()
	const deviceID = "DEV-IT-HREPORT-002"
	itRegisterUnreported(t, deviceID)
	env := itEnv(t)

	fresh := time.Now().UTC().Truncate(time.Second)
	status, resp := env.do(t, http.MethodPost, "/internal/devices/"+deviceID+"/report",
		map[string]any{"timestamp": fresh.Unix(), "fault_code": 0}, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 0, resp.Code)

	stale := fresh.Add(-3 * time.Hour)
	status, resp = env.do(t, http.MethodPost, "/internal/devices/"+deviceID+"/report",
		map[string]any{"timestamp": stale.Unix(), "fault_code": 9}, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 0, resp.Code)

	after := itLastReportAt(t, deviceID)
	require.NotNil(t, after)
	assert.True(t, after.Equal(fresh), "陈旧帧不得回退 last_report_at，实际 %s", after.Format(time.RFC3339))

	dev, err := repo.NewPGStore(itPool).GetDevice(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusOnline, dev.Status, "陈旧故障帧不得把已在线设备打回 abnormal")
	t.Logf("陈旧帧回写后：devices[%s].last_report_at = %s（未回退）", deviceID, after.Format(time.RFC3339))
}
