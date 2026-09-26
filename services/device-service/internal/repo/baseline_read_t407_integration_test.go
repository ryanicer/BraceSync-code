//go:build integration
// +build integration

// T407：PGStore.GetLatestBaselineByDevice 在真库上的「规矩 A」语义。
//
// 单测（FakeStore）只证明 handler/service 取的是那条记录，证不了三件只有 PG 能判的事：
//  1. SQL 真的按 baseline_id DESC 取，而不是按 created_at —— 换基线窗口里旧基线的
//     created_at 完全可能晚于新基线（补录/时钟漂移），按时间取会读到被作废的那一条，
//     反推出来的 avg_pressure 就与 data-service 实际减的偏移不同源；
//  2. WHERE device_id = $1 真的收窄：他设备更新的基线不得串到本机；
//  3. 该设备从未校准返回 ErrNotFound（服务层据此回 calibrated:false，不是 500）。
//
// 这条查询与 data-service/internal/repo/baseline.go:27 的 GetLatestBaseline 必须逐字同谓词
// （后者是 calibration.Apply 实际用的那一条）—— 两服务跨仓不共享代码，靠本用例锁语义。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI go-integration job 跑）
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// t407Offsets 长度 20 且各点互不相同的偏移值（用 base 区分不同基线）
func t407Offsets(base float32) []float32 {
	out := make([]float32, model.PointCount)
	for i := range out {
		out[i] = base + float32(i)*0.01
	}
	return out
}

func TestIT_T407_GetLatestBaselineByDevice(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	const dev = "DEV-IT-T407-A"
	const devOther = "DEV-IT-T407-B"
	itRegister(ctx, t, store, dev)
	itRegister(ctx, t, store, devOther)
	t.Cleanup(func() {
		_, _ = itPool.Exec(context.Background(),
			`DELETE FROM baselines WHERE device_id IN ($1, $2)`, dev, devOther)
		_, _ = itPool.Exec(context.Background(),
			`DELETE FROM install_records WHERE device_id IN ($1, $2)`, dev, devOther)
		_, _ = itPool.Exec(context.Background(), `DELETE FROM devices WHERE device_id IN ($1, $2)`, dev, devOther)
	})

	// 无基线：ErrNotFound（服务层映射为 calibrated:false）
	_, err := store.GetLatestBaselineByDevice(ctx, dev)
	require.ErrorIs(t, err, ErrNotFound, "从未校准的设备必须落 ErrNotFound，不是空指针也不是其它错误")

	// 第一次安装 + 基线
	off1 := t407Offsets(0.1)
	i1, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: dev, PatientID: itPatient, TechID: itTech, CalibrateTime: time.Now(),
	})
	require.NoError(t, err)
	b1, err := store.SaveBaseline(ctx, i1, off1, itTech)
	require.NoError(t, err)

	got, err := store.GetLatestBaselineByDevice(ctx, dev)
	require.NoError(t, err)
	assert.Equal(t, b1, got.BaselineID)
	assert.Equal(t, off1, got.OffsetValues)
	assert.Equal(t, i1, got.InstallID, "命中记录要带自己的 install_id，供调用方对账 per-install 详情")

	// 换装再校准：读侧必须跟到新基线（规矩 A）
	off2 := t407Offsets(0.9)
	i2, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: dev, PatientID: itPatient, TechID: itTech, CalibrateTime: time.Now(),
	})
	require.NoError(t, err)
	b2, err := store.SaveBaseline(ctx, i2, off2, itTech)
	require.NoError(t, err)
	require.Greater(t, b2, b1, "IDENTITY 发号：新基线 id 必须更大，否则本用例的判据不成立")

	// 他设备的基线：baseline_id 更大，但不得串到 dev 的读结果里
	offOther := t407Offsets(7.7)
	iOther, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: devOther, PatientID: itPatient, TechID: itTech, CalibrateTime: time.Now(),
	})
	require.NoError(t, err)
	bOther, err := store.SaveBaseline(ctx, iOther, offOther, itTech)
	require.NoError(t, err)
	require.Greater(t, bOther, b2)

	// 反证核心：直插一条 baseline_id 更大、created_at 却倒退 30 天的基线（真实现场是
	// 「新安装 + 补录时间早于旧基线」：换基线一定伴随新安装，见 uk_install_baseline）。
	// 按 id 取 → 命中它；按 created_at 取 → 仍回 b2。两者只可能绿一个。
	off3 := t407Offsets(3.3)
	i3, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: dev, PatientID: itPatient, TechID: itTech, CalibrateTime: time.Now(),
	})
	require.NoError(t, err)
	var b3 int64
	require.NoError(t, itPool.QueryRow(ctx,
		`INSERT INTO baselines (install_id, device_id, offset_values, calibrator_id, created_at)
		 VALUES ($1, $2, $3, $4, now() - interval '30 days') RETURNING baseline_id`,
		i3, dev, off3, itTech).Scan(&b3))
	require.Greater(t, b3, bOther, "直插行必须拿到全表最大 id，否则这条反证没有牙")

	got, err = store.GetLatestBaselineByDevice(ctx, dev)
	require.NoError(t, err)
	assert.Equal(t, b3, got.BaselineID, "判据是 baseline_id 最大，不是 created_at 最新")
	assert.Equal(t, off3, got.OffsetValues)
	assert.Equal(t, i3, got.InstallID)

	// 收窄性：devOther 的读仍指自己的最新，不受 dev 那条直插行影响
	gotOther, err := store.GetLatestBaselineByDevice(ctx, devOther)
	require.NoError(t, err)
	assert.Equal(t, bOther, gotOther.BaselineID)
	assert.Equal(t, offOther, gotOther.OffsetValues)
}
