// Package handler — alert-service 公开查询/处理端点（T028，T019B 契约偏差补齐）
//
// 路由（Go 1.22 ServeMux 方法路由）：
//
//	GET  /api/v1/alerts                      分页查询（patientId/type/status 筛选）
//	POST /api/v1/alerts/{alertId}/processing 开始处理（T257 2.7 新增，幂等）
//	POST /api/v1/alerts/{alertId}/process    标记已处理（幂等）
//
// 契约：docs/ getAlerts / processAlert；
// 字段对齐 shared-types Alert；响应体沿用统一 envelope（架构 §3.5）。
// 对外接口经 gateway 代理转发（统一鉴权在 gateway 侧，/internal/* 才是服务间直连）。
package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

// codeNotFound 资源不存在（HTTP 状态码同步映射）
const (
	codeNotFound  = 404
	codeForbidden = 403
	codeConflict  = 409 // T257 2.7：已处理的告警不允许重新进入「处理中」
)

// 身份头（gateway JWT 鉴权后注入）
const (
	headerUserID = "X-User-Id"
	headerRole   = "X-Role"
	roleAdmin    = "ROLE_ADMIN"
)

// staffRoles 内部 staff 角色集合（与 gateway / 其他服务对齐；不含 patient）
var staffRoles = map[string]bool{
	"admin":       true,
	"ROLE_ADMIN":  true,
	"ROLE_DOCTOR": true,
	"ROLE_CS":     true,
	"technician":  true,
}

func isStaffRole(role string) bool { return staffRoles[role] }

// maxPageSize 分页上限（防大页扫描；与前端契约 page 从 1 起配套）
const maxPageSize = 100

// 枚举白名单（对齐 shared-types Alert；DB CHECK 约束兜底）
//
// T257 2.6：新增 wear_duration_short（佩戴时长不足）；pressure_fluctuation（压力波动）自本卡起
// 引擎不再产生，但白名单**保留**——历史告警行仍需按类型筛出（000001:191-193 起即有存量行）。
// T257 2.7：process_status 白名单由两态扩为三态（pending / processing / processed）。
var (
	validAlertTypes = map[string]struct{}{
		"pressure_high":        {},
		"pressure_fluctuation": {},
		"wear_interrupt":       {},
		"sensor_drift":         {},
		"wear_duration_short":  {},
	}
	validProcessStatus = map[string]struct{}{
		"pending":    {},
		"processing": {},
		"processed":  {},
	}
)

// PublicAlertStore 公开端点数据访问接口（repo.PGAlertRepo 实现）
type PublicAlertStore interface {
	ListAlerts(ctx context.Context, f repo.AlertQueryFilter) ([]repo.AlertRow, int64, error)
	ProcessAlert(ctx context.Context, alertID int64, operatorID string) (exists bool, err error)
	StartProcessing(ctx context.Context, alertID int64) (repo.ProcessState, error)
}

// AlertItem 公开查询返回的告警记录（字段名对齐 shared-types Alert）
type AlertItem struct {
	AlertID        string  `json:"alertId"`
	PatientID      string  `json:"patientId"`
	PatientName    string  `json:"patientName"`
	DeviceID       string  `json:"deviceId"`
	Type           string  `json:"type"`
	Detail         string  `json:"detail"`
	SensorPoint    string  `json:"sensorPoint"`
	ThresholdValue float64 `json:"thresholdValue"`
	ActualValue    float64 `json:"actualValue"`
	Timestamp      string  `json:"timestamp"`
	ReadStatus     string  `json:"readStatus"`
	ProcessStatus  string  `json:"processStatus"`
	ResolvedStatus string  `json:"resolvedStatus"`
	ResolvedAt     *string `json:"resolvedAt"`
	InProgressAt   *string `json:"inProgressAt"` // T257 2.7：null = 从未进入过处理中（含历史行）
	ProcessedBy    *string `json:"processedBy"`
	ProcessedAt    *string `json:"processedAt"`
	ProcessNote    *string `json:"processNote"`
}

// toAlertItem repo 投影 → 契约 DTO（时间 RFC3339；alertId 字符串化）
func toAlertItem(r repo.AlertRow) AlertItem {
	item := AlertItem{
		AlertID:        strconv.FormatInt(r.AlertID, 10),
		PatientID:      r.PatientID,
		PatientName:    r.PatientName,
		DeviceID:       r.DeviceID,
		Type:           r.Type,
		Detail:         r.Detail,
		SensorPoint:    r.SensorPoint,
		ThresholdValue: r.ThresholdValue,
		ActualValue:    r.ActualValue,
		Timestamp:      r.Ts.Format(time.RFC3339),
		ReadStatus:     r.ReadStatus,
		ProcessStatus:  r.ProcessStatus,
		ResolvedStatus: r.ResolvedStatus,
	}
	if r.ResolvedAt != nil {
		s := r.ResolvedAt.Format(time.RFC3339)
		item.ResolvedAt = &s
	}
	if r.InProgressAt != nil {
		s := r.InProgressAt.Format(time.RFC3339)
		item.InProgressAt = &s
	}
	if r.ProcessedBy != nil {
		item.ProcessedBy = r.ProcessedBy
	}
	if r.ProcessedAt != nil {
		s := r.ProcessedAt.Format(time.RFC3339)
		item.ProcessedAt = &s
	}
	if r.ProcessNote != nil {
		item.ProcessNote = r.ProcessNote
	}
	return item
}

// alertPageData PaginatedResponse<Alert>（契约 getAlerts data 字段）
type alertPageData struct {
	List     []AlertItem `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
}

// SetPublicStore 注入公开端点数据源（生产由 main 注入；未注入时公开端点返回 500）
func (h *Handler) SetPublicStore(s PublicAlertStore) { h.public = s }

// listAlerts GET /api/v1/alerts —— 分页 + patientId/type/status 筛选。
// 参数缺省 page=1 / pageSize=20；非法枚举/分页越界返回 400。
//
// T264 水平鉴权：
//   - staff（admin/doctor/cs/technician）：可按 ?patientId= 过滤（跨患者）
//   - 非 staff（patient）：强制 patientId = X-User-Id，忽略调用方传入值（只看本人告警）；
//     缺失 X-User-Id → 403（fail-closed）
func (h *Handler) listAlerts(w http.ResponseWriter, r *http.Request) {
	if h.public == nil {
		h.reject(w, codeInternalError, "public store not configured")
		return
	}
	q := r.URL.Query()

	filter := repo.AlertQueryFilter{
		PatientID: q.Get("patientId"),
		Type:      q.Get("type"),
		Status:    q.Get("status"),
	}

	// T264：患者身份绑定
	if !isStaffRole(r.Header.Get(headerRole)) {
		userID := r.Header.Get(headerUserID)
		if userID == "" {
			h.reject(w, codeForbidden, "missing user identity")
			return
		}
		filter.PatientID = userID // 强制覆盖，不接受调用方传入的 patientId
	}
	if filter.Type != "" {
		if _, ok := validAlertTypes[filter.Type]; !ok {
			h.reject(w, codeInvalidParam, "invalid type: "+filter.Type)
			return
		}
	}
	if filter.Status != "" {
		if _, ok := validProcessStatus[filter.Status]; !ok {
			h.reject(w, codeInvalidParam, "invalid status: "+filter.Status)
			return
		}
	}

	filter.Page = 1
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			h.reject(w, codeInvalidParam, "invalid page: "+v)
			return
		}
		filter.Page = n
	}
	filter.PageSize = 20
	if v := q.Get("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageSize {
			h.reject(w, codeInvalidParam, "invalid pageSize: "+v)
			return
		}
		filter.PageSize = n
	}

	rows, total, err := h.public.ListAlerts(r.Context(), filter)
	if err != nil {
		h.log.Error().Err(err).Msg("list alerts failed")
		h.reject(w, codeInternalError, "list alerts failed")
		return
	}
	items := make([]AlertItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAlertItem(row))
	}
	writeJSON(w, http.StatusOK, envelope{Code: codeSuccess, Message: "success", Data: alertPageData{
		List:     items,
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	}})
}

// processAlert POST /api/v1/alerts/{alertId}/process —— 标记已处理（幂等）。
// T257 2.7：pending → processed（跳级）与 processing → processed 都合法；
// 重复处理不报错（processed_at / processed_by 以首次为准）；不存在返回 404。契约返回 ApiResponse<null>。
// processed_by 取 gateway 注入的 X-User-Id（T257 前该列一直为空，见交件说明）。
func (h *Handler) processAlert(w http.ResponseWriter, r *http.Request) {
	if h.public == nil {
		h.reject(w, codeInternalError, "public store not configured")
		return
	}
	alertID, ok := h.parseAlertID(w, r)
	if !ok {
		return
	}
	exists, err := h.public.ProcessAlert(r.Context(), alertID, r.Header.Get(headerUserID))
	if err != nil {
		h.log.Error().Err(err).Int64("alert_id", alertID).Msg("process alert failed")
		h.reject(w, codeInternalError, "process alert failed")
		return
	}
	if !exists {
		h.reject(w, codeNotFound, "alert not found: "+r.PathValue("alertId"))
		return
	}
	writeJSON(w, http.StatusOK, envelope{Code: codeSuccess, Message: "success"})
}

// startProcessingAlert POST /api/v1/alerts/{alertId}/processing —— 开始处理（T257 2.7 新增）。
// pending → processing 并记 in_progress_at；幂等（已 processing 原样返回、**不刷新**耗时起点）；
// 已 processed → 409（处理完的记录不允许重新打开）；不存在 404；alertId 非法 400。
func (h *Handler) startProcessingAlert(w http.ResponseWriter, r *http.Request) {
	if h.public == nil {
		h.reject(w, codeInternalError, "public store not configured")
		return
	}
	alertID, ok := h.parseAlertID(w, r)
	if !ok {
		return
	}
	st, err := h.public.StartProcessing(r.Context(), alertID)
	if err != nil {
		h.log.Error().Err(err).Int64("alert_id", alertID).Msg("start processing alert failed")
		h.reject(w, codeInternalError, "start processing alert failed")
		return
	}
	if !st.Exists {
		h.reject(w, codeNotFound, "alert not found: "+r.PathValue("alertId"))
		return
	}
	if st.Status == "processed" {
		h.reject(w, codeConflict, "alert already processed: "+r.PathValue("alertId"))
		return
	}
	inProgressAt := ""
	if st.InProgressAt != nil {
		inProgressAt = st.InProgressAt.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, envelope{Code: codeSuccess, Message: "success", Data: startProcessingData{
		AlertID:       strconv.FormatInt(alertID, 10),
		ProcessStatus: st.Status,
		InProgressAt:  inProgressAt,
	}})
}

// startProcessingData POST /alerts/{alertId}/processing 的 data 字段
type startProcessingData struct {
	AlertID       string `json:"alertId"`
	ProcessStatus string `json:"processStatus"`
	InProgressAt  string `json:"inProgressAt"`
}

// parseAlertID 路径参数 alertId → int64（非数字或 <1 → 400，两处理端点同口径）
func (h *Handler) parseAlertID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idStr := r.PathValue("alertId")
	alertID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || alertID < 1 {
		h.reject(w, codeInvalidParam, "invalid alertId: "+idStr)
		return 0, false
	}
	return alertID, true
}
