// T508 实时快照的 kPa 展示档：三字段（contactAreaCm2 / heatmapMaxKpa / 每点 pressureKpa）
// 必须同源于该患者当前绑定设备那一行的面积，且两条读路（DB 优先 / Redis 回退）取同一个值。
//
// 本文件守四件事：
//  1. 换算只发生一次：两条读路共用同一个 areaCm2，逐点 kPa 必须逐格相等
//     （分叉的后果是患者刷新一次页面数字就跳）；
//  2. 面积跟着「解析出的那台设备」走，不是全局键：同型号/不同型号两台设备各自生效；
//  3. 未配置面积 → 三处全部 null（前端 --），N 档读数一律不受影响（§五.3）；
//  4. 未绑定设备 → 不报错、三处 null，与 T200 的「空 deviceId」口径同形。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// t508Area 面积指针（夹具里 nil 表示未配置，不能靠 0 造）
func t508Area(v float64) *float64 { return &v }

// t508FrameN 五条可读值：0 / 1 / 2 / 4 / 6 N（PRD 参考换算表的四档分界）+ 其余 0。
// 取整数量级是为了让两条读路的 float32 往返完全一致：×1000 上报、÷1000 入库都不丢位。
func t508Frame() []float64 { return []float64{0, 1, 2, 4, 6} }

// t508KpaByPoint 把热力图收成 pointId -> kPa（nil 用 -1 表示，便于两条读路逐格对拍）
func t508KpaByPoint(hm []model.HeatmapPoint) map[string]int {
	out := make(map[string]int, len(hm))
	for _, p := range hm {
		if p.PressureKpa == nil {
			out[p.PointID] = -1
			continue
		}
		out[p.PointID] = *p.PressureKpa
	}
	return out
}

// t508Record DB 优先路径的最新行：与 t508Frame 同一组 N 值
func t508Record(t *testing.T, patientID string, ts time.Time) model.PressureRecord {
	t.Helper()
	var pts [model.PointCount]float32
	for i, v := range t508Frame() {
		pts[i] = float32(v)
	}
	return model.PressureRecord{
		RecordID: 1, DeviceID: "DEV-T508-DB", PatientID: patientID,
		Ts: ts, Points: pts, MaxPressure: 6.0, UploadTime: ts,
	}
}

// t508SingleReq 同 singleReq，但设备号可指定（UploadSingle 会校验头与体一致，夹具那台不够用）
func t508SingleReq(deviceID string, ts time.Time, values ...float64) *model.SingleFrameRequest {
	return &model.SingleFrameRequest{
		DeviceID: deviceID, Timestamp: ts.Unix(),
		Points: pts(values...), Battery: 87, Firmware: "v1.2.0",
	}
}

// Redis 回退路（newTestEnv 的 fakeRecords 不实现 GetLatestRecord ⇒ svc.latest 为 nil）
func TestT508_Realtime_RedisPathCarriesKpa(t *testing.T) {
	env := newTestEnv()
	env.devices.setArea(testDevice, 0.64)

	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(t508Frame()...)))
	require.Nil(t, appErr)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	require.NotNil(t, snap.ContactAreaCm2, "已配置面积必须透出，前端不得自持常量")
	assert.InDelta(t, 0.64, *snap.ContactAreaCm2, 1e-9)

	// 色阶上界同源于同一个 area：HeatmapMaxN 默认 6.0 N（T203）→ 94 kPa
	require.NotNil(t, snap.HeatmapMaxKpa)
	assert.Equal(t, 94, *snap.HeatmapMaxKpa)
	assert.Equal(t, model.KpaFromN(snap.HeatmapMaxN, snap.ContactAreaCm2), snap.HeatmapMaxKpa,
		"heatmapMaxKpa 必须是 heatmapMaxN 与同一面积的换算结果")

	require.Len(t, snap.PressureHeatmap, model.PointCount)
	want := []int{0, 16, 31, 63, 94} // PRD §7A.2.1 三 参考换算表（整数显示值按稿面 kpaOf）
	for i, w := range want {
		require.NotNil(t, snap.PressureHeatmap[i].PressureKpa)
		assert.Equal(t, w, *snap.PressureHeatmap[i].PressureKpa, "%s（%v N）", snap.PressureHeatmap[i].PointID, t508Frame()[i])
	}
	// 无压力点也要出 0，不是 null：0 是读到的真值，null 才是「不可换算」
	require.NotNil(t, snap.PressureHeatmap[0].PressureKpa)
	assert.Equal(t, 0, *snap.PressureHeatmap[0].PressureKpa)
	assert.Equal(t, 0, *snap.PressureHeatmap[10].PressureKpa, "帧里的真实零点在 kPa 档也是 0")
}

// DB 优先路（mockRecordStore 实现 GetLatestRecord）
func TestT508_Realtime_DBPathCarriesKpa(t *testing.T) {
	now := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)
	recTs := now.Add(-30 * time.Minute)
	svc := newTestService(
		&mockRecordStore{latestRec: t508Record(t, "P-T508-DB", recTs), latestExist: true},
		&mockDeviceStore{deviceID: "DEV-T508-DB", status: "online", exist: true, areaCm2: t508Area(0.64)},
		&mockCacheStore{},
		now,
	)

	snap, appErr := svc.GetRealtime(context.Background(), "P-T508-DB")
	require.Nil(t, appErr)
	require.NotNil(t, snap.ContactAreaCm2)
	assert.InDelta(t, 0.64, *snap.ContactAreaCm2, 1e-9)
	require.NotNil(t, snap.HeatmapMaxKpa)
	assert.Equal(t, 94, *snap.HeatmapMaxKpa)

	want := []int{0, 16, 31, 63, 94}
	for i, w := range want {
		require.NotNil(t, snap.PressureHeatmap[i].PressureKpa)
		assert.Equal(t, w, *snap.PressureHeatmap[i].PressureKpa)
	}
}

// 两条读路同源对拍：同一组帧值、同一台设备面积 ⇒ 逐点 kPa 与色阶上界完全一致。
// 少贯一个 areaCm2 参数（只改一条路）就是这条判红。
func TestT508_Realtime_TwoReadPathsAgree(t *testing.T) {
	now := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)

	env := newTestEnv()
	env.svc.now = func() time.Time { return now }
	env.devices.setArea(testDevice, 0.64)
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(now.Add(-time.Minute), pts(t508Frame()...)))
	require.Nil(t, appErr)
	redisSnap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)

	dbSvc := newTestService(
		&mockRecordStore{latestRec: t508Record(t, "P-T508-CROSS", now.Add(-time.Minute)), latestExist: true},
		&mockDeviceStore{deviceID: testDevice, status: "online", exist: true, areaCm2: t508Area(0.64)},
		&mockCacheStore{},
		now,
	)
	dbSnap, appErr := dbSvc.GetRealtime(context.Background(), "P-T508-CROSS")
	require.Nil(t, appErr)

	require.Len(t, redisSnap.PressureHeatmap, model.PointCount)
	require.Len(t, dbSnap.PressureHeatmap, model.PointCount)

	// 正对照：两条路的 N 值本身必须一致，否则下面的 kPa 相等只能证明「两边都算错」
	for i := range dbSnap.PressureHeatmap {
		assert.InDelta(t, dbSnap.PressureHeatmap[i].PressureValue, redisSnap.PressureHeatmap[i].PressureValue, 1e-6,
			"第 %d 点 N 值应一致", i)
	}

	assert.Equal(t, t508KpaByPoint(dbSnap.PressureHeatmap), t508KpaByPoint(redisSnap.PressureHeatmap),
		"同一设备同一帧，两条读路的 kPa 必须逐点相等")
	assert.Equal(t, dbSnap.HeatmapMaxKpa, redisSnap.HeatmapMaxKpa)
	assert.Equal(t, dbSnap.ContactAreaCm2, redisSnap.ContactAreaCm2)
}

// 面积跟着设备走：两台设备各配各的面积，两个患者分别取自己那台的。
// 挂成全局键（sys_configs 单键）时这条会两格读成同一个值。
func TestT508_Realtime_AreaFollowsDevice(t *testing.T) {
	env := newTestEnv()
	const devB, patB = "PRS-ML05-RC-20260808002", "P20260002"
	env.devices.bind(devB, patB, "online")
	env.devices.setArea(testDevice, 0.64) // 1 N -> 16 kPa
	env.devices.setArea(devB, 1.0)        // 1 N -> 10 kPa（同值不同面积）

	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, t508SingleReq(testDevice, fixedNow.Add(-time.Minute), t508Frame()...))
	require.Nil(t, appErr)
	_, appErr = env.svc.UploadSingle(context.Background(), devB, t508SingleReq(devB, fixedNow.Add(-time.Minute), t508Frame()...))
	require.Nil(t, appErr)

	snapA, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	snapB, appErr := env.svc.GetRealtime(context.Background(), patB)
	require.Nil(t, appErr)

	require.NotNil(t, snapA.ContactAreaCm2)
	require.NotNil(t, snapB.ContactAreaCm2)
	assert.InDelta(t, 0.64, *snapA.ContactAreaCm2, 1e-9)
	assert.InDelta(t, 1.0, *snapB.ContactAreaCm2, 1e-9)

	require.NotNil(t, snapA.PressureHeatmap[1].PressureKpa)
	require.NotNil(t, snapB.PressureHeatmap[1].PressureKpa)
	assert.Equal(t, 16, *snapA.PressureHeatmap[1].PressureKpa, "P02 = 1 N / 0.64 × 10")
	assert.Equal(t, 10, *snapB.PressureHeatmap[1].PressureKpa, "同一压力值在另一台设备上是另一个 kPa")
	assert.Equal(t, 94, *snapA.HeatmapMaxKpa)
	assert.Equal(t, 60, *snapB.HeatmapMaxKpa, "色阶上界也跟着自己那台的面积")
}

// 未配置面积：三处全 null（--），N 档读数一律不动。
// 退化成 0 就是把「配置缺失」写成「读到零压力」，稿面明令禁止。
func TestT508_Realtime_UnconfiguredAreaFailsClosed(t *testing.T) {
	env := newTestEnv() // 夹具设备未登记面积 = 列 NULL
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(t508Frame()...)))
	require.Nil(t, appErr)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	assert.Nil(t, snap.ContactAreaCm2)
	assert.Nil(t, snap.HeatmapMaxKpa)
	require.Len(t, snap.PressureHeatmap, model.PointCount)
	for _, p := range snap.PressureHeatmap {
		assert.Nil(t, p.PressureKpa, "%s 未配置面积不得造出 kPa", p.PointID)
	}

	// N 档不受影响（§五.3 末句）：色阶上界与逐点数值仍是真读数
	assert.InDelta(t, 6.0, snap.HeatmapMaxN, 1e-9)
	assert.InDelta(t, 6.0, snap.PressureHeatmap[4].PressureValue, 1e-6)
	assert.Equal(t, "P05", snap.PressureHeatmap[4].PointID)
}

// 非法面积（0 / 负 / NaN）在库里是「配坏了的值」，同样 fail-closed；
// 这一格也是「读侧不补默认 0.64」的反证：补了就会算出 94 而不是 null。
func TestT508_Realtime_InvalidAreaFailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)
	for _, bad := range []float64{0, -0.64} {
		svc := newTestService(
			&mockRecordStore{latestRec: t508Record(t, "P-T508-BAD", now.Add(-time.Minute)), latestExist: true},
			&mockDeviceStore{deviceID: "DEV-T508-DB", status: "online", exist: true, areaCm2: t508Area(bad)},
			&mockCacheStore{},
			now,
		)
		snap, appErr := svc.GetRealtime(context.Background(), "P-T508-BAD")
		require.Nil(t, appErr, "面积非法不是接口错误，不得 500/20500")
		assert.Nil(t, snap.HeatmapMaxKpa, "area=%v 不得换算出色阶上界", bad)
		for _, p := range snap.PressureHeatmap {
			assert.Nil(t, p.PressureKpa, "area=%v / %s", bad, p.PointID)
		}
	}
}

// 未绑定设备：不报错、三处 null（与 T200「空 deviceId」同形）
func TestT508_Realtime_UnboundPatientHasNoKpa(t *testing.T) {
	env := newTestEnv()
	snap, appErr := env.svc.GetRealtime(context.Background(), "P-T508-NO-DEVICE")
	require.Nil(t, appErr)
	assert.Empty(t, snap.DeviceID)
	assert.Nil(t, snap.ContactAreaCm2)
	assert.Nil(t, snap.HeatmapMaxKpa)
	assert.Empty(t, snap.PressureHeatmap, "T325：无设备无真帧仍不下发热力图")
}
