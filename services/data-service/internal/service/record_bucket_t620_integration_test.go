//go:build integration
// +build integration

// T620：records 桶降采样读路的集成用例（真实 PG15 才证得了 historyBucketSQL 本体）。
// 单测里的 Go 桩镜像的是同一条口径，本文件证的是 SQL 本身：
// ① 语句能解析执行；② 桶界按东八区墙钟切（不是 epoch 取余）；③ 全零帧的桶留在结果里。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// itPointsP01 只填 P01 的上报载荷（入参 N，上报口径 mN）
func itPointsP01(n float64) []float64 {
	out := make([]float64, model.PointCount)
	out[0] = n * model.MnPerN
	return out
}

func uploadITFrame(t *testing.T, svc *RecordService, at time.Time, p01 float64) {
	t.Helper()
	ctx := context.Background()
	_, appErr := svc.UploadSingle(ctx, itDevice, &model.SingleFrameRequest{
		DeviceID: itDevice, Timestamp: at.Unix(), Points: itPointsP01(p01), Battery: 85, Firmware: "v1.2.0",
	})
	require.Nil(t, appErr)
}

func TestIT_HistoryBuckets_CSTAlignedAndZeroKept(t *testing.T) {
	ctx := context.Background()
	svc := newITSvc(t, nil)
	require.NoError(t, itRedis.FlushDB(ctx).Err())
	truncateRecords(ctx)

	// 锚点 = 前天 00:00（Asia/Shanghai）。用绝对日期而不是「现在回溯」，
	// 桶界断言才与用例跑在一天中的哪个时刻无关。
	local := time.Now().In(model.CSTZone())
	base := time.Date(local.Year(), local.Month(), local.Day()-2, 0, 0, 0, 0, model.CSTZone())
	dateStr := base.Format("2006-01-02")

	uploadITFrame(t, svc, base.Add(10*time.Minute), 10)
	uploadITFrame(t, svc, base.Add(150*time.Minute), 20) // 同一 6h 桶 → 均值 15
	uploadITFrame(t, svc, base.Add(6*time.Hour+20*time.Minute), 0)
	uploadITFrame(t, svc, base.Add(18*time.Hour+40*time.Minute), 30)

	page, appErr := svc.GetHistory(ctx, itPatient, "day", dateStr, "6h", 1, 100)
	require.Nil(t, appErr)

	require.Len(t, page.List, 3, "四帧落进三个 6h 桶")
	assert.Equal(t, int64(3), page.Total)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 3, page.PageSize)

	// 桶界 = 东八区墙钟 0/6/18 点；按 epoch 取余会给出前一日 16 点/次日 0 点那一形
	assert.Equal(t, base.UTC().Format(time.RFC3339), page.List[0].Timestamp)
	assert.Equal(t, base.Add(6*time.Hour).UTC().Format(time.RFC3339), page.List[1].Timestamp)
	assert.Equal(t, base.Add(18*time.Hour).UTC().Format(time.RFC3339), page.List[2].Timestamp)

	assert.InDelta(t, 15.0, page.List[0].Points[0].PressureValue, 1e-6)
	assert.InDelta(t, 0.0, page.List[1].Points[0].PressureValue, 1e-6, "全零帧的桶必须留在结果里")
	assert.InDelta(t, 30.0, page.List[2].Points[0].PressureValue, 1e-6)
	assert.InDelta(t, 15.0, page.List[0].MaxPressure, 1e-6)
	require.Len(t, page.List[0].Points, model.PointCount)

	// 1d 档同样按 CST 切日：只有一枚桶，且桶起点就是 base 那一刻
	pageDay, appErr := svc.GetHistory(ctx, itPatient, "day", dateStr, "1d", 1, 100)
	require.Nil(t, appErr)
	require.Len(t, pageDay.List, 1)
	assert.Equal(t, base.UTC().Format(time.RFC3339), pageDay.List[0].Timestamp)

	// interval 缺席 → 原明细分页读路不受影响（四帧四行，桶读路一次都没碰）
	pageRaw, appErr := svc.GetHistory(ctx, itPatient, "day", dateStr, "", 1, 100)
	require.Nil(t, appErr)
	assert.Equal(t, int64(4), pageRaw.Total)

	// 直接对着 SQL 复核桶界（不经 service 层），排除「桩与 SQL 同一套自证」的余地
	var firstBucket time.Time
	require.NoError(t, itPool.QueryRow(ctx, `
SELECT MIN((ts AT TIME ZONE 'Asia/Shanghai')::date)
FROM pressure_records
WHERE patient_id = $1`, itPatient).Scan(&firstBucket))
	assert.Equal(t, base.Format("2006-01-02"), firstBucket.UTC().Format("2006-01-02"))
}
