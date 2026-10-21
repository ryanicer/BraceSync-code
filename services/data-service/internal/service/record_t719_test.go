// T719 数据管线单位链：固件 1.4 起设备上报 points 单位 = 牛顿 N，入口不得再 ÷1000。
// 历史 bug：入库把 N 当 mN 除 1000 → 库里 16.58N 存成 0.01658 → KpaFromN 恒 1、Apply 减基线后跳负。
// 本文件锁死三件事（正是验收判据）：
//  1. 新帧「存储值 ≈ body 值」：上传 N 值，热力图 pressureValue 原样是那枚 N，不缩水千倍；
//  2. 患者端 kPa 随施力单调变化：施力↑ → kPa↑，量级回到约 259±（16.58N / 0.64cm² × 10）；
//  3. 两条读路（DB 优先 / Redis 回退）同源同值，杜绝「刷新一次数字就跳」。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// t719Area 参考面积 0.64cm²（与 T508 同口径，KpaFromN(n, 0.64)=n/0.64×10）
func t719Frame(idx0N float64) []float64 { return pts(idx0N) }

// 上传一枚已知 N 帧，断言落库读数原样等于该 N（不是 N/1000）。
func TestT719_StoredValueEqualsReportedNewton_DBPath(t *testing.T) {
	env := newTestEnv()
	env.useDBPath()
	env.devices.setArea(testDevice, 0.64)

	const reported = 16.58 // 固件 1.4：body points[0] 就是 16.58 牛
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), t719Frame(reported)))
	require.Nil(t, appErr)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	require.Len(t, snap.PressureHeatmap, model.PointCount)

	p0 := snap.PressureHeatmap[0]
	assert.InDelta(t, reported, p0.PressureValue, 1e-4, "★ 存储值必须 ≈ body 上报的 N（历史 bug 会存成 %.4f）", reported/1000)
	require.NotNil(t, p0.PressureKpa, "已配置面积必须给出 kPa")
	assert.Equal(t, 259, *p0.PressureKpa, "16.58N / 0.64cm² → 约 259 kPa，不得恒 1")
}

// kPa 随施力单调：连续三帧压力递增 → 每帧读数与 kPa 都递增。
func TestT719_KpaMonotonicWithForce(t *testing.T) {
	env := newTestEnv()
	env.useDBPath()
	env.devices.setArea(testDevice, 0.64)

	force := []float64{16.58, 17.02, 17.48}
	var lastKpa int = -1
	for i, n := range force {
		_, appErr := env.svc.UploadSingle(context.Background(), testDevice,
			singleReq(fixedNow.Add(-time.Duration(len(force)-i)*time.Minute), t719Frame(n)))
		require.Nil(t, appErr)

		snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
		require.Nil(t, appErr)
		p0 := snap.PressureHeatmap[0]
		assert.InDelta(t, n, p0.PressureValue, 1e-4, "第%d帧存储值应随施力原样入库", i)
		require.NotNil(t, p0.PressureKpa)
		assert.Greater(t, *p0.PressureKpa, lastKpa, "施力 %v N 的 kPa 必须严格高于上一帧", n)
		lastKpa = *p0.PressureKpa
	}
}

// 两条读路对同一 N 帧必须给同一个存储值与同一个 kPa。
func TestT719_BothReadPaths_Agree(t *testing.T) {
	const reported = 16.58
	dbEnv, redisEnv := newTestEnv(), newTestEnv()
	dbEnv.useDBPath()
	for _, env := range []*testEnv{dbEnv, redisEnv} {
		env.devices.setArea(testDevice, 0.64)
		_, appErr := env.svc.UploadSingle(context.Background(), testDevice,
			singleReq(fixedNow.Add(-time.Minute), t719Frame(reported)))
		require.Nil(t, appErr)
	}

	dbSnap, e1 := dbEnv.svc.GetRealtime(context.Background(), testPatient)
	redisSnap, e2 := redisEnv.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, e1)
	require.Nil(t, e2)

	dbP0, redisP0 := dbSnap.PressureHeatmap[0], redisSnap.PressureHeatmap[0]
	assert.InDelta(t, dbP0.PressureValue, redisP0.PressureValue, 1e-6, "两分支存储值必须同源")
	require.NotNil(t, dbP0.PressureKpa)
	require.NotNil(t, redisP0.PressureKpa)
	assert.Equal(t, *dbP0.PressureKpa, *redisP0.PressureKpa, "两分支 kPa 必须一致，否则取决于 Redis 命中")
	assert.Equal(t, 259, *dbP0.PressureKpa)
}
