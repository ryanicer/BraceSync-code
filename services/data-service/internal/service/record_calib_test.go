package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/calibration"
	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// ─────────────────────────────────────────────────────────────
// T173 校准读侧闭环：realtime / records / heatmap / 佩戴判定同源
// ─────────────────────────────────────────────────────────────

type fakeBaselines struct {
	mu    sync.Mutex
	rows  map[string]calibration.Baseline
	err   error
	calls int
}

func (f *fakeBaselines) GetLatestBaseline(_ context.Context, deviceID string) (calibration.Baseline, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return calibration.Baseline{}, false, f.err
	}
	bl, ok := f.rows[deviceID]
	return bl, ok, nil
}

// withBaselines 给 testEnv 装配校准器并返回可断言的假基线仓
func (env *testEnv) withBaselines(rows map[string]calibration.Baseline) *fakeBaselines {
	fb := &fakeBaselines{rows: rows}
	env.svc.SetCalibrator(calibration.NewCalibrator(fb))
	return fb
}

func TestCalibration_UploadSingle_WearingJudgedOnCalibrated(t *testing.T) {
	env := newTestEnv()
	// P01 偏移 0.3N：raw 0.34N > 0.05N，减偏移后 0.04N ≤ 0.05N → 非佩戴帧（T203 ÷10）
	env.withBaselines(map[string]calibration.Baseline{
		testDevice: {BaselineID: 7, Offsets: mustOffsets(0.3)},
	})

	resp, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(0.34)))
	require.Nil(t, appErr)
	assert.False(t, resp.Duplicated)

	st := env.cache.stat[testPatient]
	require.NotNil(t, st)
	assert.Equal(t, 0, st.wear, "佩戴判定用减偏移后值：0.04N ≤ 0.05N 不计佩戴")
	assert.InDelta(t, 0.04, st.max, 0.001, "stat:today max 记校准后值")

	// rt:frame 存入口 ÷1000 后的 raw（N），偏移减法只发生在读侧
	var frame realtimeFrame
	require.NoError(t, json.Unmarshal([]byte(env.cache.rtFrame[testDevice]), &frame))
	require.Len(t, frame.Points, model.PointCount)
	assert.InDelta(t, 0.34, frame.Points[0], 0.0001, "rt:frame 保留 raw 值（原始真值）")

	// 告警入参同样用校准后帧
	require.Equal(t, 1, env.alerts.calls)
	assert.InDelta(t, 0.04, env.alerts.reqs[0].Points[0], 0.0001)
}

func TestCalibration_GetRealtime_FullCalibratedSnapshot(t *testing.T) {
	env := newTestEnv()
	env.withBaselines(map[string]calibration.Baseline{
		testDevice: {BaselineID: 7, Offsets: mustOffsets(0.5, 1.0)},
	})

	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(10, 25.5)))
	require.Nil(t, appErr)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	require.Len(t, snap.PressureRecords, 1)
	rec := snap.PressureRecords[0]
	require.True(t, rec.Calibrated, "命中基线 → calibrated=true")
	assert.InDelta(t, 9.5, rec.Points[0].PressureValue, 0.001)
	assert.InDelta(t, 24.5, rec.Points[1].PressureValue, 0.001)

	// 快照 max/maxPoint 用校准后值
	assert.InDelta(t, 24.5, snap.MaxPressure, 0.001)
	assert.Equal(t, "P02", snap.MaxPoint)

	// heatmap 同源校准
	require.Len(t, snap.PressureHeatmap, model.PointCount)
	assert.InDelta(t, 9.5, snap.PressureHeatmap[0].PressureValue, 0.001)
	assert.InDelta(t, 24.5, snap.PressureHeatmap[1].PressureValue, 0.001)
}

func TestCalibration_GetRealtime_NoBaseline_CalibratedFalse(t *testing.T) {
	env := newTestEnv()
	fb := env.withBaselines(map[string]calibration.Baseline{})

	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(10)))
	require.Nil(t, appErr)

	snap, appErr := env.svc.GetRealtime(context.Background(), testPatient)
	require.Nil(t, appErr)
	require.Len(t, snap.PressureRecords, 1)
	assert.False(t, snap.PressureRecords[0].Calibrated, "缺基线显式未校准")
	assert.InDelta(t, 10.0, snap.PressureRecords[0].Points[0].PressureValue, 0.001)
	assert.Greater(t, fb.calls, 0, "确实查询过基线（非未装配校准器）")
}

func TestCalibration_GetHistory_CalibratedDTO(t *testing.T) {
	env := newTestEnv()
	env.withBaselines(map[string]calibration.Baseline{
		testDevice: {BaselineID: 7, Offsets: mustOffsets(1.0)},
	})

	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(12.3)))
	require.Nil(t, appErr)

	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", 1, 20)
	require.Nil(t, appErr)
	require.Len(t, page.List, 1)
	assert.True(t, page.List[0].Calibrated)
	assert.InDelta(t, 11.3, page.List[0].Points[0].PressureValue, 0.001)
	assert.InDelta(t, 0, page.List[0].Points[1].PressureValue, 0.001, "无上报点且无偏移 → 0")
}

func TestCalibration_BaselineLookupFail_ServesRaw(t *testing.T) {
	env := newTestEnv()
	fb := env.withBaselines(nil)
	fb.err = errors.New("db down")

	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow.Add(-time.Minute), pts(0.6)))
	require.Nil(t, appErr, "基线查询失败 fail-open 为 raw，不阻塞上报")

	// raw 0.6 > 0.5 → 佩戴帧（fail-open 语义：宁可多计不误判为未佩戴）
	st := env.cache.stat[testPatient]
	require.NotNil(t, st)
	assert.Equal(t, 30, st.wear)
	assert.InDelta(t, 0.6, st.max, 0.001)
}

// mustOffsets 构造偏移矩阵：前 n 个取 values（单位 N），其余 0
func mustOffsets(values ...float32) [model.PointCount]float32 {
	var out [model.PointCount]float32
	copy(out[:], values)
	return out
}
