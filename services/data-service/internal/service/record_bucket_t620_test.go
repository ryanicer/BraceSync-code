package service

// T620：趋势降采样读路的单测（interval 桶查询）。
// 夹具是合成帧，证的是「取数面能不能覆盖整窗」这一维；
// SQL 的取余语义（AT TIME ZONE 折墙钟）不在本文件覆盖范围，归 integration_test 那格。

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// cst 把东八区墙钟拼成瞬时（与 periodRange 的 CSTZone 口径同一套写法）
func cst(y int, mon time.Month, d, hh, mm int) time.Time {
	return time.Date(y, mon, d, hh, mm, 0, 0, model.CSTZone())
}

// uploadFrame 按 N 语义写值上传一帧（pts 负责 mN 换算）
func uploadFrame(t *testing.T, env *testEnv, ts time.Time, values ...float64) {
	t.Helper()
	_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(ts, pts(values...)))
	require.Nil(t, appErr)
}

func TestGetHistoryBuckets_DayCoversWholeWindow(t *testing.T) {
	env := newTestEnv()
	// 四帧铺在东八区 2026-08-08 的 0/6/12/18 点四个 6h 桶里（08-08 是周六，与老用例同窗）
	uploadFrame(t, env, cst(2026, time.August, 8, 0, 10), 10)
	uploadFrame(t, env, cst(2026, time.August, 8, 6, 20), 20)
	uploadFrame(t, env, cst(2026, time.August, 8, 12, 30), 30)
	uploadFrame(t, env, cst(2026, time.August, 8, 18, 40), 40)

	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", "6h", 1, 100)
	require.Nil(t, appErr)

	// 整窗四个桶：明细分页那一发只能拿到 pageSize 上限内的最新几条，取不到这个形状
	require.Len(t, page.List, 4)
	assert.Equal(t, int64(4), page.Total)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 4, page.PageSize)

	// 桶界 = 东八区墙钟 0/6/12/18 点，DTO 出参是 UTC ISO：00:00 CST 即前一日 16:00Z
	assert.Equal(t, "2026-08-07T16:00:00Z", page.List[0].Timestamp)
	assert.Equal(t, "2026-08-07T22:00:00Z", page.List[1].Timestamp)
	assert.Equal(t, "2026-08-08T04:00:00Z", page.List[2].Timestamp)
	assert.Equal(t, "2026-08-08T10:00:00Z", page.List[3].Timestamp)

	// 逐点均值：每桶一帧，P01 就是该帧值，其余点为 0
	assert.InDelta(t, 10.0, page.List[0].Points[0].PressureValue, 1e-6)
	assert.InDelta(t, 40.0, page.List[3].Points[0].PressureValue, 1e-6)
	assert.Equal(t, "P01", page.List[0].Points[0].PointID)
	require.Len(t, page.List[0].Points, model.PointCount)
}

func TestGetHistoryBuckets_KeepsZeroValueBuckets(t *testing.T) {
	env := newTestEnv()
	// 同一 6h 桶两帧（10N 与 0N）→ 均值 5N；相邻桶整桶 0N（患者静息，0 是真实读数不是缺帧）
	uploadFrame(t, env, cst(2026, time.August, 8, 1, 0), 10)
	uploadFrame(t, env, cst(2026, time.August, 8, 2, 0), 0)
	uploadFrame(t, env, cst(2026, time.August, 8, 7, 0), 0)

	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", "6h", 1, 100)
	require.Nil(t, appErr)

	require.Len(t, page.List, 2, "全零的桶必须留在结果里，否则趋势又回到孤点形状")
	assert.InDelta(t, 5.0, page.List[0].Points[0].PressureValue, 1e-6)
	assert.InDelta(t, 0.0, page.List[1].Points[0].PressureValue, 1e-6)
	// 桶行的 maxPressure 是桶内均值（口径见 model.PressureRecordDTO 的 T620 例外注释）
	assert.InDelta(t, 5.0, page.List[0].MaxPressure, 1e-6)
	assert.InDelta(t, 0.0, page.List[1].MaxPressure, 1e-6)
}

func TestGetHistoryBuckets_RejectsUnknownAndTooFineInterval(t *testing.T) {
	env := newTestEnv()
	uploadFrame(t, env, cst(2026, time.August, 8, 1, 0), 10)

	// 未知档
	_, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", "15m", 1, 100)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeQueryParam, appErr.Code)

	// 合法档但相对窗口太细：月窗 + 30m ≈ 1489 个桶，超过 maxHistoryBuckets
	_, appErr = env.svc.GetHistory(context.Background(), testPatient, "month", "2026-08-08", "30m", 1, 100)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeQueryParam, appErr.Code)

	// 同窗换到 1d 档就放行（证明上一条错是档位判据，不是月窗本身）
	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "month", "2026-08-08", "1d", 1, 100)
	require.Nil(t, appErr)
	assert.NotEmpty(t, page.List)
}

func TestGetHistoryBuckets_EmptyWindowReturnsEmptyList(t *testing.T) {
	env := newTestEnv()
	page, appErr := env.svc.GetHistory(context.Background(), testPatient, "day", "2026-08-08", "30m", 1, 100)
	require.Nil(t, appErr)
	assert.Empty(t, page.List)
	assert.Equal(t, int64(0), page.Total)
	require.NotNil(t, page.List)
}
