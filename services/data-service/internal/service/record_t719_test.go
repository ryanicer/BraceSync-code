// T733 方案A（Boss 2026-10-22 拍板）：设备上报 points 单位 = 毫牛 mN，入口原样入库零加工，
// ÷model.MnPerN 只允许发生在 API 出口（FrameToN / ToDTO / GetRealtime 组装）。
//
// 本文件原名 record_t719_test.go，锁的是已作废的 T719 口径——"固件 1.4 发牛顿、入口不得 ÷1000、
// 存储值 ≈ body 原值即正确"。那条口径恰恰是 bug 本身：真机发 346（mN），旧断言要求展示也是 346，
// App 遂显示 "346 N / 5401 kPa"（差 1000 倍）。T733 复原为：346 是 mN，存储仍是 346（mN），
// 展示必须再 ÷1000 = 0.346 N / ≈5 kPa（0.346 / 0.64cm² × 10）。
// 故本文件保留「存储值 == body 上报原值」——但量纲改判为 mN，并新增「展示值 == 存储 ÷1000 = N」这一端。
//
// 本文件锁死三件事（正是验收判据）：
//  1. 新帧「存储值 == body 上报的 mN」且「展示值 == 存储 ÷1000 = N」，两端都不能差千倍；
//  2. 患者端 kPa 随施力单调变化：施力↑ → kPa↑，量级回到 ≈5 kPa（0.346N / 0.64cm² × 10）；
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

// t719Frame 造帧：参考面积 0.64cm²（与 T508 同口径，KpaFromN(n, 0.64)=n/0.64×10）。
// 🔴 T733 方案A 书写口径：values 按 **N** 书写（用例可读），此处 ×1000 换成设备载荷 mN，
// 入库/Redis/告警入参都是那个 mN 原值；展示层出口 ÷1000 后应回到书写的 N。
func t719Frame(idx0N float64) []float64 { return pts(idx0N * model.MnPerN) }

// 上传一枚已知帧，断言「载荷 mN 原样入库 ⇒ 出口展示 N」：既不差 1000 倍，也不在中间多转一道。
func TestT719_StoredValueEqualsReportedNewton_DBPath(t *testing.T) {
	env := newTestEnv()
	env.useDBPath()
	env.devices.setArea(testDevice, 0.64)

	const reported = 16.58 // 固件 1.9 载荷 points[0] = 16580 mN（= 16.58 N）
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), t719Frame(reported)))
	require.Nil(t, appErr)

	// 存储空间：入口零加工，库里必须仍是上报的 mN 原值（16580），不是 16.58 也不是 0.0166。
	require.NotEmpty(t, env.records.rows, "帧必须原样落库")
	stored := env.records.rows[len(env.records.rows)-1].Points[0]
	assert.InDelta(t, reported*model.MnPerN, stored, 1e-4, "★ 存储值必须 == 上报的 mN（入口不得 ÷1000）")

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	require.Len(t, snap.PressureHeatmap, model.PointCount)

	p0 := snap.PressureHeatmap[0]
	assert.InDelta(t, reported, p0.PressureValue, 1e-4, "★ 展示值必须 = 载荷 mN ÷ 1000（库里存的是 16580，不是 16.58 也不是 0.0166）", reported/1000)
	require.NotNil(t, p0.PressureKpa, "已配置面积必须给出 kPa")
	assert.Equal(t, 259, *p0.PressureKpa, "16.58N / 0.64cm² → 约 259 kPa，不得恒 1")

	// 真机症状锚点（T733 卡面数字）：设备发 346 mN → 存储 346 → 展示 0.346 N / ≈5 kPa。
	env2 := newTestEnv()
	env2.useDBPath()
	env2.devices.setArea(testDevice, 0.64)
	_, appErr = env2.svc.UploadSingle(context.Background(), testDevice,
		singleReq(fixedNow.Add(-time.Minute), pts(346))) // pts 即设备原值口径：直接写 mN
	require.Nil(t, appErr)
	assert.InDelta(t, 346, env2.records.rows[0].Points[0], 1e-4, "T733 症状：346 mN 必须原样入库")
	snap2, appErr := env2.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	assert.InDelta(t, 0.346, snap2.PressureHeatmap[0].PressureValue, 1e-6, "T733 症状：展示必须 0.346 N")
	require.NotNil(t, snap2.PressureHeatmap[0].PressureKpa)
	assert.Equal(t, 5, *snap2.PressureHeatmap[0].PressureKpa, "T733 症状：0.346N/0.64cm² → ≈5 kPa，不得是旧 bug 的 5401")
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
		assert.InDelta(t, n, p0.PressureValue, 1e-4, "第%d帧展示值应回到书写的 N（载荷 %v mN 原样入库）", i, n*model.MnPerN)
		require.NotNil(t, p0.PressureKpa)
		assert.Greater(t, *p0.PressureKpa, lastKpa, "施力 %v N 的 kPa 必须严格高于上一帧", n)
		lastKpa = *p0.PressureKpa
	}
}

// 两条读路对同一帧必须给同一个展示值与同一个 kPa（DB 路与 Redis 路不得各转各的）。
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
