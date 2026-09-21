//go:build integration
// +build integration

// Package repo 集成测试 — T300 异常报告汇总/导出（真实 PG15）
//
// 为什么要真库：汇总口径的两个关键决定都发生在 SQL 里，fake 层测不出来 ——
//  1. 按天分组必须走北京时间（ts AT TIME ZONE 'Asia/Shanghai'），
//     否则 UTC 会把晚上 8 点后的告警算到前一天；
//  2. 日期范围半开区间 [start, end) 的边界行必须落在正确的桶里；
//  3. 汇总与明细两条 SQL 共享 buildAlertWhere，参数位序错了会在真库上直接报错。
//
// 运行：make test-integration（需 Docker）
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

// t300CST 北京时间构造器（用例里的日期一律按「北京日历日」书写）
func t300CST(y int, mo time.Month, d, hh, mm int) time.Time {
	return time.Date(y, mo, d, hh, mm, 0, 0, time.FixedZone("CST", 8*3600))
}

// seedT300Alerts 造跨 4 个北京日的告警（本患者 6 条 + 他人 1 条）：
//
//	9-01 23:30 pressure_high   pending     ← 区间外（起点前）
//	9-02 00:05 pressure_high   pending     ← 起点边界（含）
//	9-02 12:00 wear_interrupt  processing
//	9-03 08:00 sensor_drift    processed
//	9-03 23:59 pressure_high   pending     ← 末日深夜（UTC 会掉到 9-03 之外）
//	9-04 00:05 pressure_high   pending     ← 终点边界（不含）
//	另 P-NOT-MINE 一条，不得混入
func seedT300Alerts(ctx context.Context, t *testing.T) *PGAlertRepo {
	t.Helper()
	_, err := itPool.Exec(ctx, `TRUNCATE TABLE alerts RESTART IDENTITY`)
	require.NoError(t, err)
	r := NewAlertRepo(itPool)
	type seed struct {
		patient, device, typ, status string
		ts                           time.Time
	}
	for _, s := range []seed{
		{itPatient, itDevice, string(engine.TypePressureHigh), "pending", t300CST(2026, 9, 1, 23, 30)},
		{itPatient, itDevice, string(engine.TypePressureHigh), "pending", t300CST(2026, 9, 2, 0, 5)},
		{itPatient, itDevice, string(engine.TypeWearInterrupt), "processing", t300CST(2026, 9, 2, 12, 0)},
		{itPatient, itDevice, string(engine.TypeSensorDrift), "processed", t300CST(2026, 9, 3, 8, 0)},
		{itPatient, itDevice, string(engine.TypePressureHigh), "pending", t300CST(2026, 9, 3, 23, 59)},
		{itPatient, itDevice, string(engine.TypePressureHigh), "pending", t300CST(2026, 9, 4, 0, 5)},
		{itPatient2, itDevice2, string(engine.TypePressureHigh), "pending", t300CST(2026, 9, 2, 9, 0)},
	} {
		_, created, cErr := r.CreateAlert(ctx, scanner.NewAlert{
			PatientID: s.patient, DeviceID: s.device, Type: engine.AlertType(s.typ),
			Detail: "T300 IT", Ts: s.ts,
		})
		require.NoError(t, cErr)
		require.True(t, created)
		if s.status != "pending" {
			_, err = itPool.Exec(ctx,
				`UPDATE alerts SET process_status = $2 WHERE patient_id = $1 AND type = $3`,
				s.patient, s.status, s.typ)
			require.NoError(t, err)
		}
	}
	return r
}

func TestIT_T300_SummarizeAlerts_BeijingDaysAndRange(t *testing.T) {
	ctx := context.Background()
	r := seedT300Alerts(ctx, t)

	start := t300CST(2026, 9, 2, 0, 0) // [9-02 00:00 CST, 9-04 00:00 CST)
	end := t300CST(2026, 9, 4, 0, 0)
	rows, err := r.SummarizeAlerts(ctx, AlertQueryFilter{PatientID: itPatient, StartTs: &start, EndTs: &end})
	require.NoError(t, err)

	var total int64
	dayType := map[string]int64{}
	status := map[string]int64{}
	for _, s := range rows {
		total += s.Count
		dayType[s.Date+"|"+s.Type] += s.Count
		status[s.ProcessStatus] += s.Count
	}
	assert.EqualValues(t, 4, total, "区间内仅本患者 4 条（两端边界外与他人行都不算）")
	assert.EqualValues(t, 1, dayType["2026-09-02|"+string(engine.TypePressureHigh)])
	assert.EqualValues(t, 1, dayType["2026-09-02|"+string(engine.TypeWearInterrupt)])
	assert.EqualValues(t, 1, dayType["2026-09-03|"+string(engine.TypeSensorDrift)])
	assert.EqualValues(t, 1, dayType["2026-09-03|"+string(engine.TypePressureHigh)])
	assert.EqualValues(t, 2, status["pending"])
	assert.EqualValues(t, 1, status["processing"])
	assert.EqualValues(t, 1, status["processed"])

	// 北京日分桶的判据：9-02 00:05 CST = 9-01 16:05 UTC。
	// 若按 UTC 分组（漏掉 AT TIME ZONE），这行会掉进 2026-09-01 桶，
	// 而区间内本没有任何 9-01 的告警 ⇒ 出现 9-01 桶即为口径错。
	for _, s := range rows {
		assert.NotEqual(t, "2026-09-01", s.Date, "9-02 00:05 CST 被按 UTC 算到了前一天")
		assert.NotEqual(t, "2026-09-04", s.Date, "末日 23:59 CST 不得溢出到次日")
	}

	// 两端不限（nil）时不得生成比较条件：本患者 6 条
	all, err := r.SummarizeAlerts(ctx, AlertQueryFilter{PatientID: itPatient})
	require.NoError(t, err)
	var allTotal int64
	for _, s := range all {
		allTotal += s.Count
	}
	assert.EqualValues(t, 6, allTotal)
}

func TestIT_T300_ListAlertsForExport_BoundsAndTruncation(t *testing.T) {
	ctx := context.Background()
	r := seedT300Alerts(ctx, t)
	start := t300CST(2026, 9, 2, 0, 0)
	end := t300CST(2026, 9, 4, 0, 0)
	f := AlertQueryFilter{PatientID: itPatient, StartTs: &start, EndTs: &end}

	rows, truncated, err := r.ListAlertsForExport(ctx, f, 10)
	require.NoError(t, err)
	assert.False(t, truncated)
	require.Len(t, rows, 4)
	for i := 1; i < len(rows); i++ {
		assert.False(t, rows[i].Ts.After(rows[i-1].Ts), "ts DESC（与列表页同口径）")
	}
	for _, row := range rows {
		assert.Equal(t, itPatient, row.PatientID)
		assert.False(t, row.Ts.Before(start), "起点含")
		assert.True(t, row.Ts.Before(end), "终点不含")
	}

	// 上限截断：limit=2 ⇒ 取满 2 条并回报还有剩余
	rows, truncated, err = r.ListAlertsForExport(ctx, f, 2)
	require.NoError(t, err)
	assert.True(t, truncated)
	assert.Len(t, rows, 2)

	// limit 恰好等于范围内条数：不算截断
	rows, truncated, err = r.ListAlertsForExport(ctx, f, 4)
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Len(t, rows, 4)
}

// 汇总与明细共享 WHERE ⇒ 同一筛选下两边计数必须相等（合同「汇总与导出」同源）
func TestIT_T300_SummaryAndExport_Agree(t *testing.T) {
	ctx := context.Background()
	r := seedT300Alerts(ctx, t)
	start := t300CST(2026, 9, 1, 0, 0)
	end := t300CST(2026, 9, 5, 0, 0)
	f := AlertQueryFilter{PatientID: itPatient, StartTs: &start, EndTs: &end}

	summary, err := r.SummarizeAlerts(ctx, f)
	require.NoError(t, err)
	var summed int64
	for _, s := range summary {
		summed += s.Count
	}
	details, truncated, err := r.ListAlertsForExport(ctx, f, MaxExportRows)
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.EqualValues(t, len(details), summed, "汇总总数 == 明细行数")
}
