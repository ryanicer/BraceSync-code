// Package repo — T030：健康报告查询（管理端/医生工作台展示）
//
// 只扩展 ReportRepo 具体方法，不改 HealthReportStore 接口（避免影响既有测试基座）。
package repo

import (
	"context"
	"fmt"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// ListReports 患者健康报告列表（period_start 倒序，同周期 monthly 在前）。
// 数据来源 health_reports（data-service owner，周报/月报定时任务生成）。
func (r *ReportRepo) ListReports(ctx context.Context, patientID string) ([]model.HealthReport, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT report_id, patient_id, report_type, period_start, period_end,
		        COALESCE(wear_compliance_rate, 0), COALESCE(avg_pressure, 0),
		        COALESCE(trend_judgment, 'flat'), COALESCE(suggestion, ''), generate_time
		 FROM health_reports
		 WHERE patient_id = $1
		 ORDER BY period_start DESC, report_type, report_id DESC`, patientID)
	if err != nil {
		return nil, fmt.Errorf("list health reports: %w", err)
	}
	defer rows.Close()

	var list []model.HealthReport
	for rows.Next() {
		var hr model.HealthReport
		if scanErr := rows.Scan(&hr.ReportID, &hr.PatientID, &hr.ReportType, &hr.PeriodStart, &hr.PeriodEnd,
			&hr.WearComplianceRate, &hr.AvgPressure, &hr.TrendJudgment, &hr.Suggestion, &hr.GenerateTime); scanErr != nil {
			return nil, fmt.Errorf("scan health report: %w", scanErr)
		}
		list = append(list, hr)
	}
	return list, rows.Err()
}
