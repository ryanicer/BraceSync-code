// Package service T021：健康报告生成单元测试（实现侧）
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

func newReportTestEnv() (*ReportService, *fakeHealthReportStore, *fakeDailyWearStatsStore) {
	reports := newFakeHealthReportStore()
	stats := newFakeDailyWearStats()
	svc := NewReportService(reports, stats)
	svc.now = func() time.Time { return time.Date(2026, 8, 11, 16, 30, 0, 0, time.UTC) } // CST 08-12 00:30
	return svc, reports, stats
}

func TestReportService_WeeklyReport_Success(t *testing.T) {
	svc, reports, stats := newReportTestEnv()

	// 设置 2 个患者有数据
	stats.patients = []string{"P1", "P2"}
	// P1 上周 7 天数据：5 天达标（>=1320min）
	p1Stats := make([]model.DailyWearStats, 7)
	for i := 0; i < 7; i++ {
		wm := 1400 // 达标
		if i >= 5 {
			wm = 600 // 不达标
		}
		p1Stats[i] = model.DailyWearStats{
			PatientID:   "P1",
			WearMinutes: wm,
			AvgPressure: 20.0,
			FrameCount:  48,
		}
	}
	stats.patientRange["P1"] = p1Stats

	// P2 上周 3 天数据
	stats.patientRange["P2"] = []model.DailyWearStats{
		{PatientID: "P2", WearMinutes: 1320, AvgPressure: 25.0, FrameCount: 44},
		{PatientID: "P2", WearMinutes: 1320, AvgPressure: 25.0, FrameCount: 44},
		{PatientID: "P2", WearMinutes: 100, AvgPressure: 10.0, FrameCount: 3},
	}

	reports.suggestions["P1"] = "坚持佩戴，注意姿势"
	reports.suggestions["P2"] = ""

	svc.RunWeeklyReport(context.Background())

	require.Len(t, reports.inserted, 2)

	// P1 报告检查
	r1 := reports.inserted[0]
	assert.Equal(t, "P1", r1.PatientID)
	assert.Equal(t, "weekly", r1.ReportType)
	assert.InDelta(t, 71.43, r1.WearComplianceRate, 0.01) // 5/7 ≈ 71.43%
	assert.Equal(t, "坚持佩戴，注意姿势", r1.Suggestion)

	// P2 报告检查
	r2 := reports.inserted[1]
	assert.Equal(t, "P2", r2.PatientID)
	assert.InDelta(t, 28.57, r2.WearComplianceRate, 0.01) // 2/7 ≈ 28.57%
	assert.Equal(t, "暂无医生建议", r2.Suggestion)
}

func TestReportService_MonthlyReport_Success(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	svc.now = func() time.Time { return time.Date(2026, 8, 31, 16, 30, 0, 0, time.UTC) } // CST 09-01 00:30

	stats.patients = []string{"P1"}
	// 上月 31 天（7月）：全部达标
	p1Stats := make([]model.DailyWearStats, 31)
	for i := range p1Stats {
		p1Stats[i] = model.DailyWearStats{
			PatientID: "P1", WearMinutes: 1320, AvgPressure: 18.0, FrameCount: 44,
		}
	}
	stats.patientRange["P1"] = p1Stats

	svc.RunMonthlyReport(context.Background())

	require.Len(t, reports.inserted, 1)
	r := reports.inserted[0]
	assert.Equal(t, "monthly", r.ReportType)
	assert.InDelta(t, 100.0, r.WearComplianceRate, 0.01)
}

func TestReportService_IdempotentNoDuplicate(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	stats.patients = []string{"P1"}
	stats.patientRange["P1"] = []model.DailyWearStats{
		{PatientID: "P1", WearMinutes: 1320, AvgPressure: 20, FrameCount: 44},
	}

	svc.RunWeeklyReport(context.Background())
	require.Len(t, reports.inserted, 1)

	// 第二次执行：幂等不重复
	svc.RunWeeklyReport(context.Background())
	assert.Len(t, reports.inserted, 1, "幂等：同周期不产生重复报告")
}

func TestReportService_TrendJudgment(t *testing.T) {
	tests := []struct {
		name          string
		curRate       float64
		curPressure   float64
		prevRate      float64
		prevPressure  float32
		expectedTrend string
	}{
		{"both up", 80, 25, 60, 20, "up"},
		{"both down", 40, 15, 70, 25, "down"},
		{"rate up pressure down", 80, 15, 60, 25, "flat"},
		{"rate down pressure up", 40, 25, 70, 15, "flat"},
		{"both flat", 70, 20, 70, 20, "flat"},
		{"no previous", 80, 25, 0, 0, "up"}, // 从 0 变到有值视为上升
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := judgeTrend(tc.curRate, tc.curPressure, tc.prevRate, tc.prevPressure)
			assert.Equal(t, tc.expectedTrend, result)
		})
	}
}

func TestReportService_TrendWithPreviousReport(t *testing.T) {
	svc, reports, stats := newReportTestEnv()

	stats.patients = []string{"P1"}
	stats.patientRange["P1"] = []model.DailyWearStats{
		{PatientID: "P1", WearMinutes: 1400, AvgPressure: 30, FrameCount: 48},
		{PatientID: "P1", WearMinutes: 1400, AvgPressure: 30, FrameCount: 48},
		{PatientID: "P1", WearMinutes: 1400, AvgPressure: 30, FrameCount: 48},
		{PatientID: "P1", WearMinutes: 1400, AvgPressure: 30, FrameCount: 48},
		{PatientID: "P1", WearMinutes: 1400, AvgPressure: 30, FrameCount: 48},
		{PatientID: "P1", WearMinutes: 1400, AvgPressure: 30, FrameCount: 48},
		{PatientID: "P1", WearMinutes: 1400, AvgPressure: 30, FrameCount: 48},
	}

	// 上一周期周报：达标率 57%（4/7），平均压力 20
	reports.prev["P1|weekly"] = &model.HealthReport{
		WearComplianceRate: 57.14,
		AvgPressure:        20,
	}

	svc.RunWeeklyReport(context.Background())

	require.Len(t, reports.inserted, 1)
	assert.Equal(t, "up", reports.inserted[0].TrendJudgment) // 100% > 57%, 30 > 20 → up
}

func TestReportService_EmptyPatientData(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	stats.patients = nil // 无患者数据

	svc.RunWeeklyReport(context.Background())
	assert.Empty(t, reports.inserted)
}

func TestReportService_EmptyDailyStats(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	stats.patients = []string{"P1"}
	stats.patientRange["P1"] = nil // 有患者但无日聚合数据

	svc.RunWeeklyReport(context.Background())
	require.Len(t, reports.inserted, 1)
	assert.InDelta(t, 0.0, reports.inserted[0].WearComplianceRate, 0.01)
	assert.Equal(t, float32(0), reports.inserted[0].AvgPressure)
}

func TestReportService_InsertError(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	stats.patients = []string{"P1"}
	stats.patientRange["P1"] = []model.DailyWearStats{
		{PatientID: "P1", WearMinutes: 1320, FrameCount: 44},
	}
	reports.insertErr = errors.New("db error")

	svc.RunWeeklyReport(context.Background()) // 不应 panic
}

func TestReportService_ListPatientsError(t *testing.T) {
	svc, _, stats := newReportTestEnv()
	stats.listErr = errors.New("db error")

	svc.RunWeeklyReport(context.Background()) // 不应 panic，应记录错误
}

func TestReportService_QueryRangeError(t *testing.T) {
	svc, _, stats := newReportTestEnv()
	stats.patients = []string{"P1"}
	stats.queryErr = errors.New("query failed")

	svc.RunWeeklyReport(context.Background()) // 不应 panic
}

func TestReportService_SuggestionError(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	stats.patients = []string{"P1"}
	stats.patientRange["P1"] = []model.DailyWearStats{
		{PatientID: "P1", WearMinutes: 1320, FrameCount: 44},
	}
	reports.sugErr = errors.New("suggestion error")

	svc.RunWeeklyReport(context.Background())
	require.Len(t, reports.inserted, 1)
	assert.Equal(t, "暂无医生建议", reports.inserted[0].Suggestion)
}

func TestReportService_PrevReportError(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	stats.patients = []string{"P1"}
	stats.patientRange["P1"] = []model.DailyWearStats{
		{PatientID: "P1", WearMinutes: 1320, FrameCount: 44, AvgPressure: 20},
	}
	reports.prevErr = errors.New("prev query failed")

	svc.RunWeeklyReport(context.Background())
	require.Len(t, reports.inserted, 1)
	assert.Equal(t, "flat", reports.inserted[0].TrendJudgment)
}

// compareValues 边界测试
func TestCompareValues(t *testing.T) {
	assert.Equal(t, "flat", compareValues(0, 0))
	assert.Equal(t, "up", compareValues(10, 0))
	assert.Equal(t, "up", compareValues(110, 100))
	assert.Equal(t, "down", compareValues(90, 100))
	assert.Equal(t, "flat", compareValues(100.5, 100))
}

func TestReportService_WearComplianceRate_AllCompliant(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	stats.patients = []string{"P1"}
	dailyStats := make([]model.DailyWearStats, 7)
	for i := range dailyStats {
		dailyStats[i] = model.DailyWearStats{
			PatientID: "P1", WearMinutes: model.WearTargetMinutes, AvgPressure: 20, FrameCount: 44,
		}
	}
	stats.patientRange["P1"] = dailyStats

	svc.RunWeeklyReport(context.Background())
	require.Len(t, reports.inserted, 1)
	assert.InDelta(t, 100.0, reports.inserted[0].WearComplianceRate, 0.01)
}

func TestReportService_AvgPressureWeighted(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	stats.patients = []string{"P1"}
	stats.patientRange["P1"] = []model.DailyWearStats{
		{PatientID: "P1", WearMinutes: 1320, AvgPressure: 20, FrameCount: 100},
		{PatientID: "P1", WearMinutes: 1320, AvgPressure: 30, FrameCount: 100},
	}

	svc.RunWeeklyReport(context.Background())
	require.Len(t, reports.inserted, 1)
	// 加权平均 = (20*100 + 30*100) / (100+100) = 25
	assert.InDelta(t, 25.0, float64(reports.inserted[0].AvgPressure), 0.01)
}

func TestReportService_NoDailyStatsZeroAvgPressure(t *testing.T) {
	svc, reports, stats := newReportTestEnv()
	stats.patients = []string{"P1"}
	stats.patientRange["P1"] = []model.DailyWearStats{
		{PatientID: "P1", WearMinutes: 0, AvgPressure: 0, FrameCount: 0},
	}

	svc.RunWeeklyReport(context.Background())
	require.Len(t, reports.inserted, 1)
	assert.Equal(t, float32(0), reports.inserted[0].AvgPressure)
}

func TestJudgeTrend_EdgeCases(t *testing.T) {
	// Both zero
	assert.Equal(t, "flat", judgeTrend(0, 0, 0, 0))
	// Small differences → flat
	assert.Equal(t, "flat", judgeTrend(50.5, 20.0, 50, 20))
	// Both up
	assert.Equal(t, "up", judgeTrend(80, 30, 50, 20))
	// Both down
	assert.Equal(t, "down", judgeTrend(30, 10, 50, 20))
}
