// Package handler — T030：管理端只读列表端点（设备 + 安装记录）
//
// 路由：
//
//	GET /api/v1/devices          设备分页列表（patientName join，契约 getDevices 扩展，T030 #3）
//	GET /api/v1/install-records  安装记录分页列表（patientName/techName join，契约 getInstallRecords）
//
// 分页/筛选对齐 T028 public.go 风格：page 1 起、pageSize 默认 20 上限 100、非法 400。
// ListStore 未注入时返回 500（生产由 main 注入 PGStore，同 alert-service SetPublicStore 模式）。
package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

// 分页口径（架构 §3.5）
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// SetListStore 注入管理端查询数据源（生产由 main 注入；未注入时列表端点返回 500）
func (h *Handler) SetListStore(s repo.ListStore) { h.list = s }

// deviceListDTO Device + patientName（契约 getDevices 扩展字段，T030 #3）
type deviceListDTO struct {
	DeviceID        string  `json:"deviceId"`
	Model           string  `json:"model"`
	FirmwareVersion string  `json:"firmwareVersion"`
	PatientID       *string `json:"patientId"`
	PatientName     *string `json:"patientName"`
	WifiSsid        *string `json:"wifiSsid"`
	BindTime        *string `json:"bindTime"`
	Status          string  `json:"status"`
	LastReportAt    *string `json:"lastReportAt"`
}

// installListDTO InstallRecord + patientName/techName（契约 getInstallRecords 扩展）
type installListDTO struct {
	InstallID     string  `json:"installId"`
	DeviceID      string  `json:"deviceId"`
	PatientID     string  `json:"patientId"`
	PatientName   *string `json:"patientName"`
	TechID        string  `json:"techId"`
	TechName      *string `json:"techName"`
	CalibrateTime string  `json:"calibrateTime"`
	BaselineID    *string `json:"baselineId"`
	Notes         string  `json:"notes"`
	SignatureUrl  string  `json:"signatureUrl"`
	WifiStatus    string  `json:"wifiStatus"`
}

// pageData PaginatedResponse<T>（契约统一分页响应）
type pageData struct {
	List     any   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// parseListPaging 分页参数校验（非法 400，对齐 T028）
func parseListPaging(c *gin.Context) (int, int, *model.AppError) {
	page, pageSize := 1, defaultPageSize
	if v := c.Query("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return 0, 0, model.ErrInvalidParam("invalid page %q", v)
		}
		page = n
	}
	if v := c.Query("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageSize {
			return 0, 0, model.ErrInvalidParam("invalid pageSize %q", v)
		}
		pageSize = n
	}
	return page, pageSize, nil
}

// listDevices GET /api/v1/devices —— keyword=设备ID/患者ID/患者姓名（ILIKE）+ 分页
func (h *Handler) listDevices(c *gin.Context) {
	if h.list == nil {
		fail(c, model.ErrInternal("list store not configured"))
		return
	}
	page, pageSize, appErr := parseListPaging(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	rows, total, err := h.list.ListDevices(c.Request.Context(), strings.TrimSpace(c.Query("keyword")), page, pageSize)
	if err != nil {
		fail(c, model.ErrInternal("list devices failed"))
		return
	}
	list := make([]deviceListDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, deviceListDTO{
			DeviceID:        r.DeviceID,
			Model:           r.Model,
			FirmwareVersion: r.FirmwareVersion,
			PatientID:       r.PatientID,
			PatientName:     r.PatientName,
			WifiSsid:        r.WifiSSID,
			BindTime:        fmtTsPtr(r.BindTime),
			Status:          r.Status,
			LastReportAt:    fmtTsPtr(r.LastReportAt),
		})
	}
	ok(c, pageData{List: list, Total: total, Page: page, PageSize: pageSize})
}

// listInstallRecords GET /api/v1/install-records —— keyword 多字段 ILIKE + 分页
func (h *Handler) listInstallRecords(c *gin.Context) {
	if h.list == nil {
		fail(c, model.ErrInternal("list store not configured"))
		return
	}
	page, pageSize, appErr := parseListPaging(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	rows, total, err := h.list.ListInstallRecords(c.Request.Context(), strings.TrimSpace(c.Query("keyword")), page, pageSize)
	if err != nil {
		fail(c, model.ErrInternal("list install records failed"))
		return
	}
	list := make([]installListDTO, 0, len(rows))
	for _, r := range rows {
		item := installListDTO{
			InstallID:     strconv.FormatInt(r.InstallID, 10),
			DeviceID:      r.DeviceID,
			PatientID:     r.PatientID,
			PatientName:   r.PatientName,
			TechID:        r.TechID,
			TechName:      r.TechName,
			CalibrateTime: r.CalibrateTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Notes:         strOrEmpty(r.Notes),
			SignatureUrl:  strOrEmpty(r.SignatureURL),
			WifiStatus:    r.WifiStatus,
		}
		if r.BaselineID != nil {
			s := strconv.FormatInt(*r.BaselineID, 10)
			item.BaselineID = &s
		}
		list = append(list, item)
	}
	ok(c, pageData{List: list, Total: total, Page: page, PageSize: pageSize})
}

// fmtTsPtr 时间 → RFC3339 UTC 指针（nil 透传 null）
func fmtTsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format("2006-01-02T15:04:05Z07:00")
	return &s
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// registerListRoutes 挂载列表路由到既有 v1 组（Router 装配调用）
func (h *Handler) registerListRoutes(v1 *gin.RouterGroup) {
	v1.GET("/devices", h.listDevices)
	v1.GET("/install-records", h.listInstallRecords)
}
