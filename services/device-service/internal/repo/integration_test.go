//go:build integration
// +build integration

// Package repo 集成测试：真实 PG15（testcontainers）验证 SQL 与约束
//
// 对齐：docs/ §1（集成层）· T015 验收标准 1–4：
//
//	注册幂等 / 绑定互斥（uk_bindings_active）/ 换绑解绑历史 / 状态机单调推进 /
//	baselines offset_values CHECK(=20) / uk_install_baseline
//
// 运行：make test-integration（需 Docker）
package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/testhelper"
)

var itPool *pgxpool.Pool

const (
	itPatient  = "P-DEV-IT-001"
	itPatient2 = "P-DEV-IT-002"
	itTech     = "TECH-DEV-IT-001"
)

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		return runIT(m, cfg.DBURL)
	})
}

// runIT 建连 + 迁移 + 种子 + 执行用例（device-service 仅依赖 PG，不使用 Redis）
func runIT(m *testing.M, dbURL string) int {
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
	return m.Run()
}

// migrationsDir 定位 scripts/db/migrations（相对本文件 4 级上）
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "db", "migrations")
}

// applyMigrations 顺序执行全部 *.up.sql（单一事实源，含 000002_p0_fixes 的 device_bindings）
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
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		  VALUES ($1, 'IT患者甲', '\x00'::bytea, 'da01' || repeat('0', 60), 'active')
		  ON CONFLICT (patient_id) DO NOTHING`, []any{itPatient}},
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		  VALUES ($1, 'IT患者乙', '\x00'::bytea, 'da02' || repeat('0', 60), 'active')
		  ON CONFLICT (patient_id) DO NOTHING`, []any{itPatient2}},
		{`INSERT INTO technicians (tech_id, name, phone_enc, phone_hash)
		  VALUES ($1, 'IT技师', '\x00'::bytea, 'da03' || repeat('0', 60))
		  ON CONFLICT (tech_id) DO NOTHING`, []any{itTech}},
	}
	for _, s := range stmts {
		if _, err := itPool.Exec(ctx, s.sql, s.args...); err != nil {
			panic("it: seed: " + err.Error())
		}
	}
}

func newITStore() *PGStore { return NewPGStore(itPool) }

func itSecretEnc() []byte { return []byte{0x01, 0x02, 0x03} } // 测试密文占位（真实密文由 service 层 AES-GCM 产出）

func itRegister(ctx context.Context, t *testing.T, store Store, deviceID string) {
	t.Helper()
	_, err := store.RegisterDevice(ctx, &model.Device{
		DeviceID: deviceID, Model: model.DefaultModel,
		DeviceSecretEnc: itSecretEnc(), SecretVersion: 1, Status: model.StatusUnbound,
	})
	require.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────
// 注册幂等
// ─────────────────────────────────────────────────────────────

func TestIT_Register_Idempotent(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	dev := &model.Device{
		DeviceID: "DEV-IT-REG-001", Model: model.DefaultModel,
		DeviceSecretEnc: itSecretEnc(), SecretVersion: 1, Status: model.StatusUnbound,
	}

	created, err := store.RegisterDevice(ctx, dev)
	require.NoError(t, err)
	assert.True(t, created)

	// 重复注册：幂等，不覆盖既有密钥
	dev2 := &model.Device{
		DeviceID: "DEV-IT-REG-001", Model: model.DefaultModel,
		DeviceSecretEnc: []byte{0xff, 0xff}, SecretVersion: 1, Status: model.StatusUnbound,
	}
	created, err = store.RegisterDevice(ctx, dev2)
	require.NoError(t, err)
	assert.False(t, created)

	got, err := store.GetDevice(ctx, "DEV-IT-REG-001")
	require.NoError(t, err)
	assert.Equal(t, itSecretEnc(), got.DeviceSecretEnc, "幂等注册不得覆盖密钥密文")
	assert.Equal(t, model.StatusUnbound, got.Status)
}

// ─────────────────────────────────────────────────────────────
// 绑定互斥 / 换绑历史 / 解绑幂等
// ─────────────────────────────────────────────────────────────

func TestIT_Bind_Rebind_Unbind(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	const deviceID = "DEV-IT-BIND-001"
	itRegister(ctx, t, store, deviceID)

	// 首绑：active binding + devices 冗余同事务更新
	prev, err := store.Bind(ctx, BindParams{DeviceID: deviceID, PatientID: itPatient, OperatorID: itTech})
	require.NoError(t, err)
	assert.Nil(t, prev, "首绑无旧绑定")
	dev, err := store.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	require.NotNil(t, dev.PatientID)
	assert.Equal(t, itPatient, *dev.PatientID)
	assert.Equal(t, model.StatusOffline, dev.Status, "unbound→offline")
	require.NotNil(t, dev.BindTime)

	// 同患者幂等
	prev, err = store.Bind(ctx, BindParams{DeviceID: deviceID, PatientID: itPatient, OperatorID: itTech})
	require.NoError(t, err)
	assert.Nil(t, prev)
	bindings, err := store.ListBindings(ctx, deviceID)
	require.NoError(t, err)
	assert.Len(t, bindings, 1, "幂等绑定不得新增行")

	// 自动换绑（对齐 Ella H6 契约）：Bind 他患者 → 旧行关闭 reason=rebind，新行 active
	prev, err = store.Bind(ctx, BindParams{DeviceID: deviceID, PatientID: itPatient2, OperatorID: itTech})
	require.NoError(t, err)
	require.NotNil(t, prev)
	assert.Equal(t, itPatient, prev.PatientID)
	bindings, err = store.ListBindings(ctx, deviceID)
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	active := 0
	for _, b := range bindings {
		if b.UnbindAt == nil {
			active++
			assert.Equal(t, itPatient2, b.PatientID)
		} else {
			require.NotNil(t, b.Reason)
			assert.Equal(t, model.ReasonRebind, *b.Reason)
			require.NotNil(t, b.OperatorID)
			assert.Equal(t, itTech, *b.OperatorID)
		}
	}
	assert.Equal(t, 1, active, "同设备仅一个 active binding")

	// DB 层互斥兜底（当前存在 active binding）：直接插第二条必须被唯一索引拒绝
	_, err = itPool.Exec(ctx,
		`INSERT INTO device_bindings (device_id, patient_id) VALUES ($1, $2)`, deviceID, itPatient)
	require.Error(t, err, "uk_bindings_active 必须拒绝第二条 active binding")

	// 解绑：hadActive=true，devices 冗余清空
	hadActive, err := store.Unbind(ctx, deviceID, "OP-IT")
	require.NoError(t, err)
	assert.True(t, hadActive)
	dev, err = store.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	assert.Nil(t, dev.PatientID)
	assert.Nil(t, dev.BindTime)
	assert.Equal(t, model.StatusUnbound, dev.Status)

	// 解绑幂等：重复解绑 hadActive=false，不报错
	hadActive, err = store.Unbind(ctx, deviceID, "OP-IT")
	require.NoError(t, err)
	assert.False(t, hadActive)

	// 无 active binding 时 Rebind 拒绝（应先走 Bind）
	_, err = store.Rebind(ctx, BindParams{DeviceID: deviceID, PatientID: itPatient, OperatorID: itTech})
	assert.ErrorIs(t, err, ErrConflict, "无 active binding 应拒绝 Rebind")

	// 解绑后可重新 Bind（闭环）
	_, err = store.Bind(ctx, BindParams{DeviceID: deviceID, PatientID: itPatient, OperatorID: itTech})
	require.NoError(t, err)

	// 历史完整：全部行可追溯（unbind_at/reason/operator 完整），仅最新一条 active
	bindings, err = store.ListBindings(ctx, deviceID)
	require.NoError(t, err)
	assert.Len(t, bindings, 3)
	closedCount := 0
	for _, b := range bindings {
		require.NotNil(t, b.Reason)
		if b.UnbindAt != nil {
			closedCount++
		}
	}
	assert.Equal(t, 2, closedCount, "两条已关闭绑定历史完整")
}

// ─────────────────────────────────────────────────────────────
// 状态机落库：单调推进 + 最新帧口径
// ─────────────────────────────────────────────────────────────

func TestIT_Touch_Monotonic(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	const deviceID = "DEV-IT-TOUCH-001"
	itRegister(ctx, t, store, deviceID)

	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, store.Touch(ctx, deviceID, now, model.StatusOnline))
	dev, err := store.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusOnline, dev.Status)
	require.NotNil(t, dev.LastReportAt)
	assert.True(t, dev.LastReportAt.Equal(now))
	firstUpdated := dev.UpdatedAt

	// 故障 → abnormal
	require.NoError(t, store.Touch(ctx, deviceID, now.Add(time.Minute), model.StatusAbnormal))
	dev, _ = store.GetDevice(ctx, deviceID)
	assert.Equal(t, model.StatusAbnormal, dev.Status)

	// 陈旧帧：last_report_at 不回退，状态不被覆盖
	require.NoError(t, store.Touch(ctx, deviceID, now.Add(-time.Hour), model.StatusOnline))
	dev, _ = store.GetDevice(ctx, deviceID)
	assert.Equal(t, model.StatusAbnormal, dev.Status, "陈旧帧不得覆盖最新状态")
	require.NotNil(t, dev.LastReportAt)
	assert.True(t, dev.LastReportAt.Equal(now.Add(time.Minute)), "last_report_at 单调不回退")
	assert.True(t, dev.UpdatedAt.After(firstUpdated) || dev.UpdatedAt.Equal(firstUpdated), "updated_at 行级刷新")

	// 未注册设备
	err = store.Touch(ctx, "DEV-IT-NO-SUCH", now, model.StatusOnline)
	assert.ErrorIs(t, err, ErrNotFound)
}

// ─────────────────────────────────────────────────────────────
// 安装 + 基线闭环（offset_values CHECK=20 / uk_install_baseline）
// ─────────────────────────────────────────────────────────────

func TestIT_Install_Baseline(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	const deviceID = "DEV-IT-INST-001"
	itRegister(ctx, t, store, deviceID)
	_, err := store.Bind(ctx, BindParams{DeviceID: deviceID, PatientID: itPatient, OperatorID: itTech})
	require.NoError(t, err)

	installID, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: deviceID, PatientID: itPatient, TechID: itTech, CalibrateTime: time.Now(),
	})
	require.NoError(t, err)
	assert.Greater(t, installID, int64(0))

	offsets := make([]float32, model.PointCount)
	for i := range offsets {
		offsets[i] = float32(i) * 0.1
	}

	// DB CHECK 兜底：19 点直插必须被拒（服务层已先行校验，此处验证库层防线）
	_, err = itPool.Exec(ctx,
		`INSERT INTO baselines (install_id, device_id, offset_values, calibrator_id) VALUES ($1, $2, $3, $4)`,
		installID, deviceID, offsets[:19], itTech)
	require.Error(t, err, "chk_baselines_offset_len 必须拒绝长度=19")

	// 事务落库：baselines + install_records.baseline_id 回填
	baselineID, err := store.SaveBaseline(ctx, installID, offsets, itTech)
	require.NoError(t, err)
	assert.Greater(t, baselineID, int64(0))

	rec, err := store.GetInstall(ctx, installID)
	require.NoError(t, err)
	require.NotNil(t, rec.BaselineID)
	assert.Equal(t, baselineID, *rec.BaselineID)

	// 基线落库校验：读回 offset_values 完整
	var got []float32
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT offset_values FROM baselines WHERE baseline_id = $1`, baselineID).Scan(&got))
	assert.Len(t, got, model.PointCount)

	// 一次安装至多一条基线（uk_install_baseline + 事务校验）
	_, err = store.SaveBaseline(ctx, installID, offsets, itTech)
	assert.ErrorIs(t, err, ErrConflict)

	// notes/signature_url 回填
	notes, sig := "matrix 完成", "cos://sig/x.png"
	require.NoError(t, store.UpdateInstallMeta(ctx, installID, &notes, &sig))
	rec, _ = store.GetInstall(ctx, installID)
	require.NotNil(t, rec.Notes)
	assert.Equal(t, notes, *rec.Notes)
	require.NotNil(t, rec.SignatureURL)
	assert.Equal(t, sig, *rec.SignatureURL)
}

// ─────────────────────────────────────────────────────────────
// WiFi 配置状态
// ─────────────────────────────────────────────────────────────

func TestIT_SetWifiSSID(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	const deviceID = "DEV-IT-WIFI-001"
	itRegister(ctx, t, store, deviceID)

	require.NoError(t, store.SetWifiSSID(ctx, deviceID, "BraceHome-5G"))
	dev, err := store.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	require.NotNil(t, dev.WifiSSID)
	assert.Equal(t, "BraceHome-5G", *dev.WifiSSID)

	// 有安装记录时 wifi_status 置 connected
	_, err = store.Bind(ctx, BindParams{DeviceID: deviceID, PatientID: itPatient, OperatorID: itTech})
	require.NoError(t, err)
	installID, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: deviceID, PatientID: itPatient, TechID: itTech, CalibrateTime: time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, store.SetWifiSSID(ctx, deviceID, "NewSSID"))
	rec, err := store.GetInstall(ctx, installID)
	require.NoError(t, err)
	assert.Equal(t, "connected", rec.WifiStatus)

	// 未注册设备
	err = store.SetWifiSSID(ctx, "DEV-IT-NO-SUCH", "x")
	assert.ErrorIs(t, err, ErrNotFound)
}
