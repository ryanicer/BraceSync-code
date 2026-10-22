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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bracesync/bracesync/services/alert-service/internal/config"
	"github.com/bracesync/bracesync/services/alert-service/internal/flowstarter"
	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

// 越权与资源不存在两格按 T402 甲-1 收为「域号 4 + 0 + HTTP 状态三位」（旧值 403/404 是裸
// HTTP 状态数字，与 reject 的 HTTP 状态共用一个参数，改值会连状态一起改）。
// 域号取 4 的理由：架构 §3.5 分段表里 alert 域占 4xxxx（user 1、device 2、data 3、alert 4、msg 5、file 6）。
// codeConflict 与 handler.go 的 codeInvalidParam/codeInternalError 仍留裸数字：Boss 22:07 勾的是丁-2
// （只收 403 与 404），其余裸码留给告警域自己的卡，这是有意的中间态而不是漏项。
const (
	codeNotFound  = 40404 // HTTP 404 资源不存在
	codeForbidden = 40403 // HTTP 403 角色无权 / 越界读
	codeConflict  = 409   // T257 2.7：已处理的告警不允许重新进入「处理中」
	// codeFlowRouteConflict 677：有绑定流程类型的告警，快处直改被拒、强制转流程（HTTP 409）。
	// 与 codeConflict（409 已办结）分码：用户面文案不同，前端可按码区分提示。
	codeFlowRouteConflict = 40409
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
	ProcessAlert(ctx context.Context, alertID int64, operatorID, note string) (exists bool, err error)
	StartProcessing(ctx context.Context, alertID int64) (repo.ProcessState, error)
	// T300 异常报告汇总/导出（GET /admin/abnormal-reports[/export]）
	SummarizeAlerts(ctx context.Context, f repo.AlertQueryFilter) ([]repo.AlertSummaryRow, error)
	ListAlertsForExport(ctx context.Context, f repo.AlertQueryFilter, limit int) ([]repo.AlertRow, bool, error)
	// DoctorTeamByAdmin T350：admin_id → 医护所属团队（数据范围推导，doctors 表只读）
	DoctorTeamByAdmin(ctx context.Context, adminID string) (teamID string, ok bool, err error)
}

// AlertItem 公开查询返回的告警记录（字段名对齐 shared-types Alert）
type AlertItem struct {
	AlertID     string `json:"alertId"`
	PatientID   string `json:"patientId"`
	PatientName string `json:"patientName"`
	DeviceID    string `json:"deviceId"`
	Type        string `json:"type"`
	// Detail 引擎生成的告警文案（alerts.detail 原值）：压力类文案里的数字按 T733 方案A
	// 以 **mN** 自标量纲（"…压力 6000.0mN 超阈值 5000.0mN"），本层不改写字符串；
	// 若产品侧要 N 口径文案，属展示层模板改造（data/前端域），不在本卡。
	Detail      string `json:"detail"`
	SensorPoint string `json:"sensorPoint"`
	// ThresholdValue / ActualValue 为**展示层 N 口径**（T733 方案A 出口换算）：
	// 库里 pressure_high / sensor_drift 两类的值按 mN 存，响应前 ÷config.MnPerN；
	// 分钟类（wear_interrupt / wear_duration_short）与百分比类（pressure_fluctuation）原样透出。
	// 前端 packages/shared-utils formatAlertValue 对这两类拼「N」后缀，量纲靠本函数对齐。
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

// pressureDisplayAlertTypes 值为**压力量纲**（库内 mN）的告警类型集合：
// 只有这两类需要出口换算 mN→N（与前端 formatAlertValue 的 N 分支逐键同形）。
var pressureDisplayAlertTypes = map[string]bool{
	"pressure_high": true,
	"sensor_drift":  true,
}

// displayValueN 存储值 → 展示值：压力量纲按 mN 存、按 N 展示（÷config.MnPerN，T733 方案A
// 唯一的展示层换算点）；分钟/百分比等其余类型原样透出，不做换算。
func displayValueN(alertType string, stored float64) float64 {
	if pressureDisplayAlertTypes[alertType] {
		return stored / config.MnPerN
	}
	return stored
}

// toAlertItem repo 投影 → 契约 DTO（时间 RFC3339；alertId 字符串化；
// 压力量纲阈值/实际值经 displayValueN 转 N）。
func toAlertItem(r repo.AlertRow) AlertItem {
	item := AlertItem{
		AlertID:        strconv.FormatInt(r.AlertID, 10),
		PatientID:      r.PatientID,
		PatientName:    r.PatientName,
		DeviceID:       r.DeviceID,
		Type:           r.Type,
		Detail:         r.Detail,
		SensorPoint:    r.SensorPoint,
		ThresholdValue: displayValueN(r.Type, r.ThresholdValue),
		ActualValue:    displayValueN(r.Type, r.ActualValue),
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
		h.reject(w, r, http.StatusInternalServerError, codeInternalError, "public store not configured")
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
			h.reject(w, r, http.StatusForbidden, codeForbidden, "missing user identity")
			return
		}
		filter.PatientID = userID // 强制覆盖，不接受调用方传入的 patientId
	} else if !h.applyDoctorTeamScope(w, r, &filter) {
		// T350：医护按所属团队过滤（运营 / 客服不收紧，与改前逐字一致）
		return
	}
	if filter.Type != "" {
		if _, ok := validAlertTypes[filter.Type]; !ok {
			h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "invalid type: "+filter.Type)
			return
		}
	}
	if filter.Status != "" {
		if _, ok := validProcessStatus[filter.Status]; !ok {
			h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "invalid status: "+filter.Status)
			return
		}
	}

	filter.Page = 1
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "invalid page: "+v)
			return
		}
		filter.Page = n
	}
	filter.PageSize = 20
	if v := q.Get("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageSize {
			h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "invalid pageSize: "+v)
			return
		}
		filter.PageSize = n
	}

	rows, total, err := h.public.ListAlerts(r.Context(), filter)
	if err != nil {
		h.log.Error().Err(err).Msg("list alerts failed")
		h.reject(w, r, http.StatusInternalServerError, codeInternalError, "list alerts failed")
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
// T278-①：请求体 {note} 落 process_note（Iris T269 D4 实测「状态落了、备注没了」）；
// 无 body / note 为空 ⇒ 传空串给 repo，行为与本条改动前完全一致。
func (h *Handler) processAlert(w http.ResponseWriter, r *http.Request) {
	if h.public == nil {
		h.reject(w, r, http.StatusInternalServerError, codeInternalError, "public store not configured")
		return
	}
	alertID, ok := h.parseAlertID(w, r)
	if !ok {
		return
	}
	// 677 快处路由门先于节点执法：有绑定类型一律转流程（409），不给旧快处直改开口子；
	// direct / 已 processed 才继续走 T668 执法与落库（无绑定保持现状）。
	if !h.enforceQuickRoute(w, r, alertID) {
		return
	}
	if !h.enforceProcessGuard(w, r, alertID) {
		return
	}
	note, ok := h.parseProcessNote(w, r)
	if !ok {
		return
	}
	exists, err := h.public.ProcessAlert(r.Context(), alertID, r.Header.Get(headerUserID), note)
	if err != nil {
		h.log.Error().Err(err).Int64("alert_id", alertID).Msg("process alert failed")
		h.reject(w, r, http.StatusInternalServerError, codeInternalError, "process alert failed")
		return
	}
	if !exists {
		h.reject(w, r, http.StatusNotFound, codeNotFound, "alert not found: "+r.PathValue("alertId"))
		return
	}
	writeJSON(w, http.StatusOK, envelope{Code: codeSuccess, Message: "success"})
}

// maxProcessNote alerts.process_note 为 VARCHAR(512)，超长 PG 直接报 22001 →
// 与其回 500，不如在入口按字符数拒 400（中文按 rune 计，与 PG 的字符口径一致）。
const maxProcessNote = 512

// processNoteRequest 处理端点的可选请求体（note 缺省 = 无备注）
type processNoteRequest struct {
	Note string `json:"note"`
}

// parseProcessNote 解析可选的 {note} 请求体。
// 空 body（老调用方 / 前端无备注时不带 body）→ 返回空串且放行；
// body 非 JSON 或 note 超长 → 400 并返回 false。
func (h *Handler) parseProcessNote(w http.ResponseWriter, r *http.Request) (string, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "read body: "+err.Error())
		return "", false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return "", true
	}
	var req processNoteRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "invalid json: "+err.Error())
		return "", false
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > maxProcessNote {
		h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "note too long: max 512 characters")
		return "", false
	}
	return note, true
}

// startProcessingAlert POST /api/v1/alerts/{alertId}/processing —— 开始处理（T257 2.7 新增）。
// pending → processing 并记 in_progress_at；幂等（已 processing 原样返回、**不刷新**耗时起点）；
// 已 processed → 409（处理完的记录不允许重新打开）；不存在 404；alertId 非法 400。
func (h *Handler) startProcessingAlert(w http.ResponseWriter, r *http.Request) {
	if h.public == nil {
		h.reject(w, r, http.StatusInternalServerError, codeInternalError, "public store not configured")
		return
	}
	alertID, ok := h.parseAlertID(w, r)
	if !ok {
		return
	}
	if !h.enforceProcessGuard(w, r, alertID) {
		return
	}
	st, err := h.public.StartProcessing(r.Context(), alertID)
	if err != nil {
		h.log.Error().Err(err).Int64("alert_id", alertID).Msg("start processing alert failed")
		h.reject(w, r, http.StatusInternalServerError, codeInternalError, "start processing alert failed")
		return
	}
	if !st.Exists {
		h.reject(w, r, http.StatusNotFound, codeNotFound, "alert not found: "+r.PathValue("alertId"))
		return
	}
	if st.Status == "processed" {
		h.reject(w, r, http.StatusConflict, codeConflict, "alert already processed: "+r.PathValue("alertId"))
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

// enforceProcessGuard T668 卡面口径 3：告警 process/processing 落库前问 user-service
// 流程节点执法。绑定流程且实例 running 时，操作者不是当前节点合法处理人 → 403；
// user-service 不可达等依赖故障 → 503（权限判定 fail-closed，不旁路成 staff 放行）。
// 返回 false 时响应已写出，调用方直接 return。
func (h *Handler) enforceProcessGuard(w http.ResponseWriter, r *http.Request, alertID int64) bool {
	d, err := h.processGuard.CheckProcess(r.Context(), alertID,
		r.Header.Get(headerUserID), r.Header.Get(headerRole))
	if err != nil {
		h.log.Error().Err(err).Int64("alert_id", alertID).Msg("flow process-guard check failed")
		h.reject(w, r, http.StatusServiceUnavailable, codeInternalError,
			"流程权限校验暂不可用，请稍后重试")
		return false
	}
	if d.Enforced && !d.Allowed {
		reason := d.Reason
		if reason == "" {
			reason = "流程进行中，当前节点未授权该账号处理"
		}
		h.reject(w, r, http.StatusForbidden, codeForbidden, reason)
		return false
	}
	return true
}

// enforceQuickRoute 677：快处（process）落库前的路由一问。
//   - route=flow 且未 processed → 409 强制转流程（有绑定类型不再允许快处直改）；
//   - route=direct → 放行（无绑定/不可绑定类型保持现状，完全收口留 T672）；
//   - alreadyProcessed → 放行，走原幂等直通（T028 幂等契约不破）；
//   - 依赖故障 → 503 fail-closed（与 enforceProcessGuard 同口径，不许旁路回旧快处）。
//
// 返回 false 时响应已写出，调用方直接 return。
func (h *Handler) enforceQuickRoute(w http.ResponseWriter, r *http.Request, alertID int64) bool {
	d, err := h.quickRouter.QuickRoute(r.Context(), alertID)
	if err != nil {
		h.log.Error().Err(err).Int64("alert_id", alertID).Msg("flow quick-route check failed")
		h.reject(w, r, http.StatusServiceUnavailable, codeInternalError,
			"流程路由校验暂不可用，请稍后重试")
		return false
	}
	if d.Route == flowstarter.RouteFlow && !d.AlreadyProcessed {
		h.reject(w, r, http.StatusConflict, codeFlowRouteConflict,
			fmt.Sprintf("alert %d routed to flow instance %s; quick process refused", alertID, d.InstanceID))
		return false
	}
	return true
}

// parseAlertID 路径参数 alertId → int64（非数字或 <1 → 400，两处理端点同口径）
func (h *Handler) parseAlertID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idStr := r.PathValue("alertId")
	alertID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || alertID < 1 {
		h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "invalid alertId: "+idStr)
		return 0, false
	}
	return alertID, true
}
