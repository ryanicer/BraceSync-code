// Package handler — T030：健康报告查询端点（契约 getHealthReports 落地）
//
// 路由：GET /api/v1/patients/:patientId/health-reports（data-service，gateway JWT 鉴权）。
// ReportLister 未注入时返回 500（生产由 main 注入 ReportRepo，同 SetPublicStore 模式）。
package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// ReportLister 健康报告查询契约（repo.ReportRepo 实现）
type ReportLister interface {
	ListReports(ctx context.Context, patientID string) ([]model.HealthReport, error)
}

// SetReportLister 注入健康报告数据源（生产由 main 注入）
func (h *Handler) SetReportLister(l ReportLister) { h.reports = l }

// healthReportDTO 对齐 shared-types HealthReport（周期为 DATE → YYYY-MM-DD；生成时刻 RFC3339）
type healthReportDTO struct {
	ReportID           string  `json:"reportId"`
	PatientID          string  `json:"patientId"`
	ReportType         string  `json:"reportType"`
	PeriodStart        string  `json:"periodStart"`
	PeriodEnd          string  `json:"periodEnd"`
	WearComplianceRate float64 `json:"wearComplianceRate"`
	AvgPressure        float32 `json:"avgPressure"`
	TrendJudgment      string  `json:"trendJudgment"`
	Suggestion         string  `json:"suggestion"`
	GenerateTime       string  `json:"generateTime"`
}

// getHealthReports GET /api/v1/patients/:patientId/health-reports —— 按周期倒序
func (h *Handler) getHealthReports(c *gin.Context) {
	if h.reports == nil {
		fail(c, model.ErrInternal("report lister not configured"))
		return
	}
	rows, err := h.reports.ListReports(c.Request.Context(), c.Param("patientId"))
	if err != nil {
		fail(c, model.ErrInternal("list health reports failed"))
		return
	}
	list := make([]healthReportDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, healthReportDTO{
			ReportID:           strconv.FormatInt(r.ReportID, 10),
			PatientID:          r.PatientID,
			ReportType:         r.ReportType,
			PeriodStart:        r.PeriodStart.Format("2006-01-02"),
			PeriodEnd:          r.PeriodEnd.Format("2006-01-02"),
			WearComplianceRate: r.WearComplianceRate,
			AvgPressure:        r.AvgPressure,
			TrendJudgment:      r.TrendJudgment,
			Suggestion:         r.Suggestion,
			GenerateTime:       r.GenerateTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	c.JSON(http.StatusOK, apiResponse{Code: model.CodeOK, Message: "success", Data: list})
}
