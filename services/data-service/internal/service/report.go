// Package service T021：健康报告生成服务（周报 / 月报）
//
// 对齐：PRD §7A.11 生成规则
//   - 周报：周一 00:30 生成上一自然周（周一至周日）
//   - 月报：每月 1 日 00:30 生成上一自然月
//   - 佩戴达标率 = 达标天数（wear_minutes >= WearTargetMinutes）/ 总天数 × 100
//   - 趋势判断：对比上一周期，达标率+平均压力两维度变化方向
//   - 建议：orthosis_plans 最新 content；无方案 → "暂无医生建议"
package service

import (
	"context"
	"math"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/data-service/internal/metrics"
	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// ReportService 健康报告生成编排
type ReportService struct {
	reports repo.HealthReportStore
	stats   repo.DailyWearStatsStore
	now     func() time.Time
}

// NewReportService 组装 ReportService
func NewReportService(reports repo.HealthReportStore, stats repo.DailyWearStatsStore) *ReportService {
	return &ReportService{
		reports: reports,
		stats:   stats,
		now:     time.Now,
	}
}

// RunWeeklyReport 生成上一自然周的周报
func (s *ReportService) RunWeeklyReport(ctx context.Context) {
	now := s.now().In(model.CSTZone())
	// 上一自然周：上周一至上周日
	thisMonday := time.Date(now.Year(), now.Month(), now.Day()-int((now.Weekday()+6)%7), 0, 0, 0, 0, model.CSTZone())
	lastMonday := thisMonday.AddDate(0, 0, -7)
	lastSunday := thisMonday.AddDate(0, 0, -1) // 上周日（含）

	log.Info().
		Str("from", lastMonday.Format("2006-01-02")).
		Str("to", lastSunday.Format("2006-01-02")).
		Msg("weekly report started")

	if err := s.generateReports(ctx, "weekly", lastMonday, thisMonday); err != nil {
		metrics.ReportJobTotal.WithLabelValues("weekly", "error").Inc()
		log.Error().Err(err).Msg("weekly report generation failed")
		return
	}
	metrics.ReportJobTotal.WithLabelValues("weekly", "ok").Inc()
	log.Info().Msg("weekly report completed")
}

// RunMonthlyReport 生成上一自然月的月报
func (s *ReportService) RunMonthlyReport(ctx context.Context) {
	now := s.now().In(model.CSTZone())
	// 上一自然月
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, model.CSTZone())
	lastMonth := thisMonth.AddDate(0, -1, 0)

	log.Info().
		Str("from", lastMonth.Format("2006-01-02")).
		Str("to", thisMonth.AddDate(0, 0, -1).Format("2006-01-02")).
		Msg("monthly report started")

	if err := s.generateReports(ctx, "monthly", lastMonth, thisMonth); err != nil {
		metrics.ReportJobTotal.WithLabelValues("monthly", "error").Inc()
		log.Error().Err(err).Msg("monthly report generation failed")
		return
	}
	metrics.ReportJobTotal.WithLabelValues("monthly", "ok").Inc()
	log.Info().Msg("monthly report completed")
}

// generateReports 为指定周期内所有有数据的患者生成报告
// periodStart/periodEnd 为 CST 日期，periodEnd 是排他上界
func (s *ReportService) generateReports(ctx context.Context, reportType string, periodStart, periodEnd time.Time) error {
	// periodEnd 是下一天 00:00，实际最后一天 = periodEnd - 1day
	lastDay := periodEnd.AddDate(0, 0, -1)
	totalDays := int(lastDay.Sub(periodStart).Hours()/24) + 1
	if totalDays <= 0 {
		totalDays = 1
	}

	// 查所有有数据的患者
	patients, err := s.stats.ListPatientsWithStats(ctx, periodStart, periodEnd)
	if err != nil {
		return err
	}

	generated := 0
	for _, pid := range patients {
		rpt, genErr := s.buildReport(ctx, pid, reportType, periodStart, periodEnd, totalDays)
		if genErr != nil {
			log.Error().Err(genErr).Str("patient_id", pid).Str("type", reportType).Msg("build report failed, skip patient")
			continue
		}
		inserted, insErr := s.reports.InsertReport(ctx, rpt)
		if insErr != nil {
			log.Error().Err(insErr).Str("patient_id", pid).Msg("insert report failed")
			continue
		}
		if inserted {
			generated++
		}
	}

	log.Info().Str("type", reportType).Int("generated", generated).Int("total_patients", len(patients)).Msg("reports generated")
	return nil
}

// buildReport 为单个患者构建报告
func (s *ReportService) buildReport(ctx context.Context, patientID, reportType string, periodStart, periodEnd time.Time, totalDays int) (*model.HealthReport, error) {
	dailyStats, err := s.stats.QueryRange(ctx, patientID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	// 达标天数 & 平均压力
	compliantDays := 0
	var pressureSum float64
	var pressureCount int
	for _, d := range dailyStats {
		if d.WearMinutes >= model.WearTargetMinutes {
			compliantDays++
		}
		if d.FrameCount > 0 {
			pressureSum += float64(d.AvgPressure) * float64(d.FrameCount)
			pressureCount += d.FrameCount
		}
	}

	complianceRate := 0.0
	if totalDays > 0 {
		complianceRate = float64(compliantDays) / float64(totalDays) * 100
	}
	avgPressure := float32(0)
	if pressureCount > 0 {
		avgPressure = float32(pressureSum / float64(pressureCount))
	}

	// 趋势判断：对比上一周期
	trend := "flat"
	prev, prevErr := s.reports.QueryPreviousReport(ctx, patientID, reportType, periodStart)
	if prevErr != nil {
		log.Warn().Err(prevErr).Str("patient_id", patientID).Msg("query previous report failed, default flat")
	} else if prev != nil {
		trend = judgeTrend(complianceRate, float64(avgPressure), prev.WearComplianceRate, prev.AvgPressure)
	}

	// 建议
	suggestion, sugErr := s.reports.LatestSuggestion(ctx, patientID)
	if sugErr != nil {
		log.Warn().Err(sugErr).Str("patient_id", patientID).Msg("query suggestion failed")
	}
	if suggestion == "" {
		suggestion = "暂无医生建议"
	}

	return &model.HealthReport{
		PatientID:          patientID,
		ReportType:         reportType,
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd.AddDate(0, 0, -1), // 含末日
		WearComplianceRate: math.Round(complianceRate*100) / 100,
		AvgPressure:        avgPressure,
		TrendJudgment:      trend,
		Suggestion:         suggestion,
		GenerateTime:       s.now().UTC(),
	}, nil
}

// judgeTrend 对比当前与上一周期的达标率+平均压力，判断趋势
// 两维度同向则取该方向，否则 flat
func judgeTrend(curRate, curPressure float64, prevRate float64, prevPressure float32) string {
	rateDir := compareValues(curRate, prevRate)
	pressureDir := compareValues(curPressure, float64(prevPressure))

	if rateDir == pressureDir {
		return rateDir
	}
	return "flat"
}

// compareValues 两值比较：差异 < 1% 视为 flat
func compareValues(cur, prev float64) string {
	const threshold = 0.01
	if prev == 0 && cur == 0 {
		return "flat"
	}
	var diff float64
	if prev == 0 {
		diff = 1.0 // 从 0 变到有值视为上升
	} else {
		diff = (cur - prev) / prev
	}
	switch {
	case diff > threshold:
		return "up"
	case diff < -threshold:
		return "down"
	default:
		return "flat"
	}
}
