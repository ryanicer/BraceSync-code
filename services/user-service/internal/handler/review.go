// Package handler — T130 复查记录端点（合同患者端「复查管理」）
//
// 路由：
//
//	POST /api/v1/admin/review-records              创建复查记录（doctor/admin，网关 RBAC 拦截）
//	GET  /api/v1/patients/:patientId/review-records  患者复查记录列表（患者自查 + admin 任意）
//
// 水平鉴权：listReviewRecords 在 handler 层校验 X-User-Id == patientId（仿 data-service getDailyWear）。
// 报告文件元数据/下载 URL 由 file-service 提供（fileSvc 客户端，nil 时降级不返回文件信息）。
package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// createReviewRecord POST /api/v1/admin/review-records —— 医生/管理员创建复查记录
func (h *Handler) createReviewRecord(c *gin.Context) {
	// 网关 RBAC 已拦截非 doctor/admin，此处兜底校验
	role := c.GetHeader(headerRole)
	if role != roleAdmin && role != "ROLE_DOCTOR" {
		fail(c, model.ErrForbidden("only admin or doctor can create review records"))
		return
	}

	var req model.CreateReviewRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}

	// 校验 review_date 格式（YYYY-MM-DD）
	reviewDate, err := time.Parse("2006-01-02", req.ReviewDate)
	if err != nil {
		fail(c, model.ErrInvalidParam("invalid reviewDate format (expected YYYY-MM-DD)"))
		return
	}

	// 校验 next_review_date 格式（可空）
	var nextReviewDate *time.Time
	if req.NextReviewDate != nil && *req.NextReviewDate != "" {
		t, err := time.Parse("2006-01-02", *req.NextReviewDate)
		if err != nil {
			fail(c, model.ErrInvalidParam("invalid nextReviewDate format (expected YYYY-MM-DD)"))
			return
		}
		nextReviewDate = &t
	}

	// 生成 review_id（RV + 时间戳 + 随机 8 位）
	reviewID := generateReviewID()

	row := repo.ReviewRecordRow{
		ReviewID:       reviewID,
		PatientID:      req.PatientID,
		ReviewDate:     reviewDate,
		ReviewType:     req.ReviewType,
		Findings:       req.Findings,
		NextReviewDate: nextReviewDate,
		DoctorID:       req.DoctorID,
		ReportFileID:   req.ReportFileID,
	}

	created, err := h.store.CreateReviewRecord(c.Request.Context(), row)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrReviewPatientNotFound):
			fail(c, model.ErrNotFound("patient not found"))
		default:
			fail(c, model.ErrInternal("create review record failed: %v", err))
		}
		return
	}

	dto := reviewRowToDTO(created)
	ok(c, dto)
}

// listReviewRecords GET /api/v1/patients/:patientId/review-records —— 复查记录列表
// 水平鉴权：ROLE_ADMIN 可查任意患者；其他角色仅 X-User-Id == patientId 可查。
func (h *Handler) listReviewRecords(c *gin.Context) {
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}

	// 水平鉴权（fail-closed：缺失头视为无权限）
	role := c.GetHeader(headerRole)
	userID := c.GetHeader(headerUserID)
	if role != roleAdmin {
		if userID == "" || userID != patientID {
			fail(c, model.ErrForbidden("may only query your own review records"))
			return
		}
	}

	rows, err := h.store.ListReviewRecordsByPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("list review records failed: %v", err))
		return
	}

	list := make([]model.ReviewRecordDTO, 0, len(rows))
	for _, row := range rows {
		dto := reviewRowToDTO(&row)
		// 填充报告文件元数据 + 下载 URL（fileSvc 不可用时降级为空）
		if row.ReportFileID != nil && *row.ReportFileID != "" && h.fileSvc != nil {
			h.fillReportFileInfo(c.Request.Context(), &dto, *row.ReportFileID)
		}
		list = append(list, dto)
	}
	ok(c, list)
}

// fillReportFileInfo 从 file-service 拉取文件元数据 + 下载预签名 URL，失败时静默降级
func (h *Handler) fillReportFileInfo(ctx context.Context, dto *model.ReviewRecordDTO, fileID string) {
	fm, err := h.fileSvc.GetFileByID(ctx, fileID)
	if err != nil || fm == nil {
		return
	}
	dto.ReportContentType = &fm.ContentType
	dto.ReportSize = &fm.Size
	if !fm.UploadedAt.IsZero() {
		s := fm.UploadedAt.UTC().Format(time.RFC3339)
		dto.ReportUploadedAt = &s
	}
	// 从 object_key 提取文件名作为展示名
	dto.ReportFileName = &fm.ObjectKey
	// 获取下载预签名 URL
	if dlURL, err := h.fileSvc.GetDownloadURL(ctx, fileID); err == nil && dlURL != "" {
		dto.ReportDownloadURL = &dlURL
	}
}

// reviewRowToDTO 将 repo 行转为 DTO（不含文件元数据）
func reviewRowToDTO(row *repo.ReviewRecordRow) model.ReviewRecordDTO {
	dto := model.ReviewRecordDTO{
		ReviewID:     row.ReviewID,
		PatientID:    row.PatientID,
		ReviewDate:   row.ReviewDate.Format("2006-01-02"),
		ReviewType:   row.ReviewType,
		Findings:     row.Findings,
		DoctorID:     row.DoctorID,
		ReportFileID: row.ReportFileID,
		CreatedAt:    row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.NextReviewDate != nil {
		s := row.NextReviewDate.Format("2006-01-02")
		dto.NextReviewDate = &s
	}
	return dto
}

// generateReviewID 生成复查记录 ID：RV_ + unixnano + 随机 8hex
func generateReviewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "RV_" + time.Now().Format("20060102150405") + "_" + hex.EncodeToString(b)
}
