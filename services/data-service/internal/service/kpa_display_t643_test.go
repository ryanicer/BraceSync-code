// T643 A 路：records 桶与 daily-wear 两条响应里的 kPa 展示档必须与热力图同源派生
// （同一个 KpaFromN、同一枚 devices.contact_area_cm2），不落库、不改判档、面积缺失时回 null。
// 反面对照（B 路）在前端另建一套换算 —— 本文件钉的是后端侧「只此一处换算」。
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// T643 用面积：与 T508 夹具同一档（0.64 cm²），换算结果可整数化，避免 float32 往返位差
const t643AreaCm2 = 0.64

func TestT643HistoryFramesCarryKpaSameSource(t *testing.T) {
	env := newTestEnv()
	env.devices.setArea(testDevice, t643AreaCm2)
	uploadFrame(t, env, cst(2026, time.August, 8, 9, 0), 10, 20)

	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", "", 1, 10)
	require.Nil(t, appErr)
	require.Len(t, page.List, 1)
	require.Len(t, page.List[0].Points, model.PointCount)

	area := t508Area(t643AreaCm2)
	for _, p := range page.List[0].Points {
		require.NotNil(t, p.PressureKpa, "面积已配置 ⇒ 逐点 kPa 必须在场")
		assert.Equal(t, model.KpaFromN(p.PressureValue, area), p.PressureKpa,
			"逐点 kPa 只能是 KpaFromN 的现算结果（点 %s）", p.PointID)
	}
	// 0.64 cm² 下 10 N 与 20 N 的可分辨档：156.25 → 156、312.5 → 313（half up，写死数字，改口径即判红）
	assert.Equal(t, 156, *page.List[0].Points[0].PressureKpa)
	assert.Equal(t, 313, *page.List[0].Points[1].PressureKpa)
}

func TestT643HistoryBucketsCarryKpaSameSource(t *testing.T) {
	env := newTestEnv()
	env.devices.setArea(testDevice, t643AreaCm2)
	uploadFrame(t, env, cst(2026, time.August, 8, 0, 10), 10)
	uploadFrame(t, env, cst(2026, time.August, 8, 6, 20), 20)

	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", "6h", 1, 100)
	require.Nil(t, appErr)
	require.Len(t, page.List, 2)

	area := t508Area(t643AreaCm2)
	for _, rec := range page.List {
		for _, p := range rec.Points {
			require.NotNil(t, p.PressureKpa, "桶行同样要带 kPa（前端趋势图打的正是这条读路）")
			assert.Equal(t, model.KpaFromN(p.PressureValue, area), p.PressureKpa)
		}
	}
}

// 反证一：设备未配置面积 ⇒ kPa 全 null，且 N 值与判档一个字都不许变（fail-closed 不是 0）
func TestT643HistoryKpaIsNullWhenAreaMissing(t *testing.T) {
	env := newTestEnv() // bind 了设备但没 setArea
	uploadFrame(t, env, cst(2026, time.August, 8, 9, 0), 10, 20)

	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", "", 1, 10)
	require.Nil(t, appErr)
	require.Len(t, page.List, 1)
	pts := page.List[0].Points
	require.Len(t, pts, model.PointCount)
	assert.Nil(t, pts[0].PressureKpa, "未配置面积 ⇒ null，不用默认 0.64 补位")
	assert.InDelta(t, 10.0, pts[0].PressureValue, 1e-9, "N 值不受面积缺失影响")
	// 默认阈值（PressureHighN=5.0 / HeatmapMaxN=6.0）下 10 N 实测即 critical：判档认的是 N，面积缺失不把它抹平
	assert.Equal(t, "critical", pts[0].Status, "判档只认 N，与面积在场与否无关")

	raw, err := json.Marshal(page.List[0])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"pressureKpa":null`, "键恒在（无 omitempty），缺失只以 null 表达")
}

// 反证二：设备查询挂掉也不能把整条历史读路判失败
func TestT643HistoryDegradesWhenDeviceLookupFails(t *testing.T) {
	env := newTestEnv()
	uploadFrame(t, env, cst(2026, time.August, 8, 9, 0), 10)
	env.devices.err = context.DeadlineExceeded // 上传成功后再断设备源，只影响读侧换算

	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", "", 1, 10)
	require.Nil(t, appErr, "面积读不到只是展示档降级，历史主链路不受影响")
	require.Len(t, page.List, 1)
	assert.Nil(t, page.List[0].Points[0].PressureKpa)
}

func TestT643DailyWearRowsCarryKpaSameSource(t *testing.T) {
	svc := NewDailyWearService(&fakeDailyWearStore{rows: []model.DailyWearStats{{
		PatientID: "P1", StatDate: t366Day(t, "2026-09-22"),
		WearMinutes: 480, AvgPressure: 12.8, MaxPressure: 51.2, MaxPoint: "P03", FrameCount: 6,
	}}}, nil, nil, nil)
	svc.SetDeviceStore(&mockDeviceStore{deviceID: testDevice, status: "online", exist: true, areaCm2: t508Area(t643AreaCm2)})
	svc.now = func() time.Time { return time.Date(2026, 9, 22, 10, 0, 0, 0, model.CSTZone()) }

	list, appErr := svc.GetDailyWear(context.Background(), "P1", "2026-09-22", "2026-09-22")
	require.Nil(t, appErr)
	require.Len(t, list, 1)

	area := t508Area(t643AreaCm2)
	row := list[0]
	assert.Equal(t, model.KpaFromN(float64(row.AvgPressure), area), row.AvgPressureKpa)
	assert.Equal(t, model.KpaFromN(float64(row.MaxPressure), area), row.MaxPressureKpa)
	require.NotNil(t, row.AvgPressureKpa)
	require.NotNil(t, row.MaxPressureKpa)
	// 51.2 N / 0.64 cm² × 10 = 800 kPa；12.8 N 同式 = 200 kPa
	assert.Equal(t, 800, *row.MaxPressureKpa)
	assert.Equal(t, 200, *row.AvgPressureKpa)
}

// 未注入 devices（测试/降级形）⇒ 逐行 kPa 为 null，N 两列照常下发
func TestT643DailyWearKpaNullWithoutDeviceStore(t *testing.T) {
	svc := NewDailyWearService(&fakeDailyWearStore{rows: []model.DailyWearStats{{
		PatientID: "P1", StatDate: t366Day(t, "2026-09-22"),
		AvgPressure: 12.8, MaxPressure: 51.2, FrameCount: 6,
	}}}, nil, nil, nil)
	svc.now = func() time.Time { return time.Date(2026, 9, 22, 10, 0, 0, 0, model.CSTZone()) }

	list, appErr := svc.GetDailyWear(context.Background(), "P1", "2026-09-22", "2026-09-22")
	require.Nil(t, appErr)
	require.Len(t, list, 1)
	assert.Nil(t, list[0].AvgPressureKpa)
	assert.Nil(t, list[0].MaxPressureKpa)
	assert.InDelta(t, 12.8, list[0].AvgPressure, 1e-6, "N 值照旧")

	raw, err := json.Marshal(list)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"avgPressureKpa":null`)
	assert.Contains(t, string(raw), `"maxPressureKpa":null`)
}

// T599 补出来的零帧行同样要同源：行上两枚 N 是 0 ⇒ 面积在场给 0（不给 0 N 配一个「--」），
// 面积读不到给 null（fail-closed，不退成 0 说成「已知的零」）。
func TestT643BackfilledZeroRowCarriesKpa(t *testing.T) {
	store := &fakeDailyWearStore{}
	abn := &fakeAbnormalSource{counts: map[string]int{"2026-09-01": 3}}

	svc := NewDailyWearService(store, nil, nil, abn)
	svc.SetDeviceStore(&mockDeviceStore{deviceID: testDevice, status: "online", exist: true, areaCm2: t508Area(t643AreaCm2)})
	svc.now = func() time.Time { return fakeNowT599 }
	list, appErr := svc.GetDailyWear(context.Background(), "P1", "2026-08-27", "2026-09-01")
	require.Nil(t, appErr)
	row := findT599Day(list, "2026-09-01")
	require.NotNil(t, row, "甲形：已过日无聚合行但有告警要补出行")
	require.NotNil(t, row.AvgPressureKpa, "面积在场 ⇒ 补行的 kPa 是数值，不是「--」")
	assert.Zero(t, *row.AvgPressureKpa, "0 N 同源得 0 kPa")
	assert.Zero(t, *row.MaxPressureKpa)

	// 同一份数据、不注入面积源 ⇒ 两枚回 null
	svc2 := NewDailyWearService(store, nil, nil, abn)
	svc2.now = func() time.Time { return fakeNowT599 }
	list2, appErr := svc2.GetDailyWear(context.Background(), "P1", "2026-08-27", "2026-09-01")
	require.Nil(t, appErr)
	row2 := findT599Day(list2, "2026-09-01")
	require.NotNil(t, row2)
	assert.Nil(t, row2.AvgPressureKpa, "未注入面积源 ⇒ null，不用 0 补位")
	assert.Nil(t, row2.MaxPressureKpa)
}
