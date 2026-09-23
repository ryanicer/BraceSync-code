package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/calibration"
	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// ─────────────────────────────────────────────────────────────
// T296 真机 payload 网格对齐：设备 points[] 下标 → P01–P20 网格点位一一对应
// 载荷取自 Boss 2026-09-21 真机日志（device PRS-ML05-RC-20260701001, record 7620）
// 覆盖两条读路径：生产 DB 优先分支（getRealtimeFromDB）与 Redis 回退分支
// ─────────────────────────────────────────────────────────────

// t296RealPayload 设备原始 points（单位 mN）：非零项 idx0=599 / idx7=38 / idx14=14 / idx16=14
func t296RealPayload() []float64 {
	return []float64{599, 0, 0, 0, 0, 0, 0, 38, 0, 0, 0, 0, 0, 0, 14, 0, 14, 0, 0, 0}
}

// t296DBRecords 让 fakeRecords 参与 DB 优先路径（生产用 repo.RecordRepo.GetLatestRecord）
type t296DBRecords struct{ *fakeRecords }

func (f *t296DBRecords) GetLatestRecord(_ context.Context, patientID string) (model.PressureRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best model.PressureRecord
	found := false
	for _, r := range f.rows {
		if r.PatientID == patientID && (!found || r.Ts.After(best.Ts)) {
			best, found = r, true
		}
	}
	return best, found, nil
}

// useDBPath 切到 getRealtimeFromDB 分支
func (env *testEnv) useDBPath() { env.svc.latest = &t296DBRecords{env.records} }

func uploadT296(t *testing.T, env *testEnv) {
	t.Helper()
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, &model.SingleFrameRequest{
		DeviceID:  testDevice,
		Timestamp: fixedNow.Add(-time.Minute).Unix(),
		Points:    t296RealPayload(),
		Battery:   100,
		Firmware:  "bracesync-prod-1.1",
	})
	require.Nil(t, appErr)
}

// wantT296Grid 期望网格：÷1000 后 mN→N，下标不位移
func wantT296Grid(t *testing.T, hm []model.HeatmapPoint) {
	t.Helper()
	require.Len(t, hm, model.PointCount)
	want := map[string]float64{"P01": 0.599, "P08": 0.038, "P15": 0.014, "P17": 0.014}
	for _, hp := range hm {
		v, named := want[hp.PointID]
		if !named {
			v = 0
		}
		assert.InDelta(t, v, hp.PressureValue, 1e-6, "%s(%s) 应等于设备同下标值 ÷1000", hp.PointID, hp.Label)
	}
	assert.Equal(t, "P01", maxPointID(t, hm), "★ 只能标在设备最大值点 idx0")
}

func TestT296_RealPayload_GridMatchesUpload_DBPath(t *testing.T) {
	env := newTestEnv()
	env.useDBPath()
	uploadT296(t, env)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	wantT296Grid(t, snap.PressureHeatmap)

	require.Len(t, snap.PressureRecords, 1)
	rec := snap.PressureRecords[0]
	require.Len(t, rec.Points, model.PointCount)
	assert.InDelta(t, 0.599, rec.Points[0].PressureValue, 1e-6, "曲线/明细与热力图同源")
	assert.False(t, rec.Calibrated, "缺基线时显式未校准，不得静默当 0 偏移")
}

func TestT296_RealPayload_GridMatchesUpload_RedisPath(t *testing.T) {
	env := newTestEnv()
	uploadT296(t, env) // 不设 latest → getRealtimeFromRedis

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	wantT296Grid(t, snap.PressureHeatmap)

	// T366：Redis 回退分支也要给 maxPressure（无库侧生成列 ⇒ 按 greatest(p01..p20) 现算），
	// 否则同一字段在两分支一侧有值一侧恒 0，消费方无法一视同仁。
	require.Len(t, snap.PressureRecords, 1)
	assert.InDelta(t, 0.599, snap.PressureRecords[0].MaxPressure, 1e-6, "raw 峰值与设备最大点同值")
}

func TestT296_BothReadPaths_AgreeOnGrid(t *testing.T) {
	dbEnv, redisEnv := newTestEnv(), newTestEnv()
	dbEnv.useDBPath()
	uploadT296(t, dbEnv)
	uploadT296(t, redisEnv)

	dbSnap, e1 := dbEnv.svc.GetRealtime(context.Background(), testPatient)
	redisSnap, e2 := redisEnv.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, e1)
	require.Nil(t, e2)
	require.Len(t, dbSnap.PressureHeatmap, model.PointCount)
	require.Len(t, redisSnap.PressureHeatmap, model.PointCount)
	for i := range dbSnap.PressureHeatmap {
		assert.Equal(t, dbSnap.PressureHeatmap[i].PointID, redisSnap.PressureHeatmap[i].PointID)
		assert.InDelta(t, dbSnap.PressureHeatmap[i].PressureValue, redisSnap.PressureHeatmap[i].PressureValue, 1e-6,
			"idx%d 两分支必须同源，否则前端拿到哪一份取决于 Redis 命中", i)
	}
}

func TestT296_PointIDGridMapping_RowMajor(t *testing.T) {
	env := newTestEnv()
	env.useDBPath()
	uploadT296(t, env)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	// PRD §8.3：Pn = RrCc，4 行 5 列行优先；前端标签口径
	assert.Equal(t, "P04", snap.PressureHeatmap[3].PointID)
	assert.Equal(t, "R1C4", snap.PressureHeatmap[3].Label)
	assert.Equal(t, []int{1, 1, 2, 4}, []int{
		snap.PressureHeatmap[0].Row, snap.PressureHeatmap[4].Row,
		snap.PressureHeatmap[5].Row, snap.PressureHeatmap[19].Row,
	})
	assert.Equal(t, "R4C5", snap.PressureHeatmap[19].Label)
}

func TestT296_WithBaseline_OnlyShiftsValuesNotPositions(t *testing.T) {
	env := newTestEnv()
	env.useDBPath()
	// idx0 偏移 0.1N、idx3 偏移 0.2N（其余 0）
	env.withBaselines(map[string]calibration.Baseline{
		testDevice: {BaselineID: 16, Offsets: mustOffsets(0.1, 0, 0, 0.2)},
	})
	uploadT296(t, env)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	hm := snap.PressureHeatmap
	require.Len(t, hm, model.PointCount)
	assert.InDelta(t, 0.499, hm[0].PressureValue, 1e-6, "P01 = 0.599 - 0.1")
	assert.InDelta(t, 0.038, hm[7].PressureValue, 1e-6, "偏移为 0 的点不受影响")
	assert.InDelta(t, -0.2, hm[3].PressureValue, 1e-6, "设备报 0 的点减偏移后可为负，但位置不得移动")
	assert.Equal(t, "P01", maxPointID(t, hm))
}

// ─────────────────────────────────────────────────────────────
// T296 展示口径随快照下发：前端色阶/分级不得再写死常量（T203 后 60/45 滞后一个量级）
// ─────────────────────────────────────────────────────────────

// t296ThresholdConfigs 让 fakeConfigs 兼任 repo.ThresholdStore（pressureThresholds 走类型断言）
type t296ThresholdConfigs struct {
	*fakeConfigs
	th model.PressureThresholds
}

func (c *t296ThresholdConfigs) GetPressureThresholds(_ context.Context) (model.PressureThresholds, error) {
	return c.th, nil
}

func TestT296_RealtimeSnapshot_CarriesDisplayScale(t *testing.T) {
	env := newTestEnv()
	env.useDBPath()
	env.svc.configs = &t296ThresholdConfigs{
		fakeConfigs: env.configs,
		th:          model.PressureThresholds{HeatmapMaxN: 8, PressureHighN: 4},
	}
	uploadT296(t, env)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	assert.InDelta(t, 8.0, snap.HeatmapMaxN, 1e-9, "色阶上界取 sys_configs 当前值")
	assert.InDelta(t, 4.0, snap.PressureHighN, 1e-9, "偏高分界与告警引擎同源")

	// 未绑定设备分支同样要带口径（前端首屏即渲染色阶）
	orphan := newTestEnv()
	orphan.svc.configs = &t296ThresholdConfigs{fakeConfigs: orphan.configs, th: model.DefaultPressureThresholds()}
	none, appErr := orphan.svc.GetRealtime(context.Background(), "P20269999")
	require.Nil(t, appErr)
	assert.InDelta(t, model.HeatmapMaxN, none.HeatmapMaxN, 1e-9)
	assert.InDelta(t, 5.0, none.PressureHighN, 1e-9)
}

func maxPointID(t *testing.T, hm []model.HeatmapPoint) string {
	t.Helper()
	idx := -1
	for i, hp := range hm {
		if hp.IsMax {
			require.Equal(t, -1, idx, "IsMax 只能有一个")
			idx = i
		}
	}
	require.NotEqual(t, -1, idx, "热力图必须标出最大点")
	return hm[idx].PointID
}
