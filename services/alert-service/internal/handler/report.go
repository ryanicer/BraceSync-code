// Package handler — T300 患者异常报告汇总/导出端点（合同 §二 患者管理「异常报告汇总与导出」）。
//
// 路由：
//
//	GET /api/v1/admin/abnormal-reports        按患者 + 日期范围汇总（JSON）
//	GET /api/v1/admin/abnormal-reports/export 同口径明细导出（CSV，附件下载）
//
// 口径（最小可用，卡内边界）：
//   - 参数只有 patientId / start / end（北京日历日 YYYY-MM-DD，含端点），不做多维度筛选、不出图表；
//   - 汇总与导出走同一 WHERE（repo.buildAlertWhere），两者数字必然同源；
//   - staff 专属：跨患者聚合端点，患者 token 一律 403（网关 RBAC + 本层双闸，与 T264 同型）。
package handler

import (
	"encoding/csv"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
	"github.com/bracesync/bracesync/services/alert-service/internal/scheduler"
)

const (
	// reportDayLayout 入参日期格式（北京日历日）
	reportDayLayout = "2006-01-02"
	// reportTimeLayout CSV 明细的采集/处理时刻（北京时间，导给医院侧可直接读）
	reportTimeLayout = "2006-01-02 15:04:05"
	// csvBOM Excel 打开 UTF-8 CSV 需要 BOM，否则中文列名乱码
	csvBOM = "\ufeff"
)

// reportAlertTypeLabels 告警类型的中文口径（仅展示层；未知值原样输出）
var reportAlertTypeLabels = map[string]string{
	"pressure_high":        "压力超阈值",
	"pressure_fluctuation": "压力波动",
	"wear_interrupt":       "佩戴中断",
	"sensor_drift":         "传感器漂移",
	"wear_duration_short":  "佩戴时长不足",
}

var reportProcessStatusLabels = map[string]string{
	"pending":    "待处理",
	"processing": "处理中",
	"processed":  "已处理",
}

var reportReadStatusLabels = map[string]string{
	"unread": "未读",
	"read":   "已读",
}

func labelOf(labels map[string]string, key string) string {
	if v, ok := labels[key]; ok {
		return v
	}
	return key
}

// reportQuery 校验通过的查询条件（start/end 为回显用的原字符串）
type reportQuery struct {
	filter    repo.AlertQueryFilter
	patientID string
	start     string
	end       string
}

// parseReportQuery 解析并校验 T300 两个端点的公共查询参数。
// 失败时已写好响应并返回 ok=false（不再触达存储）。
//
// 日期换算：start 日 00:00（含）→ end 日次日 00:00（不含），
// 用半开区间表达「末日全天」，不依赖 ts 的小数秒精度。
func (h *Handler) parseReportQuery(w http.ResponseWriter, r *http.Request) (reportQuery, bool) {
	if h.public == nil {
		h.reject(w, codeInternalError, "public store not configured")
		return reportQuery{}, false
	}
	// T300 水平鉴权：跨患者聚合，患者 token 不得读（X-Role 缺失同样拒，fail-closed）
	if !isStaffRole(r.Header.Get(headerRole)) {
		h.reject(w, codeForbidden, "abnormal report is staff-only")
		return reportQuery{}, false
	}
	q := r.URL.Query()
	patientID := q.Get("patientId")
	if patientID == "" {
		h.reject(w, codeInvalidParam, "patientId is required")
		return reportQuery{}, false
	}
	loc := scheduler.CSTZone()
	startStr, endStr := q.Get("start"), q.Get("end")
	startDay, err := time.ParseInLocation(reportDayLayout, startStr, loc)
	if err != nil {
		h.reject(w, codeInvalidParam, "invalid start: "+startStr)
		return reportQuery{}, false
	}
	endDay, err := time.ParseInLocation(reportDayLayout, endStr, loc)
	if err != nil {
		h.reject(w, codeInvalidParam, "invalid end: "+endStr)
		return reportQuery{}, false
	}
	endTs := endDay.AddDate(0, 0, 1)
	if !endTs.After(startDay) {
		h.reject(w, codeInvalidParam, "end must be >= start")
		return reportQuery{}, false
	}
	return reportQuery{
		filter:    repo.AlertQueryFilter{PatientID: patientID, StartTs: &startDay, EndTs: &endTs},
		patientID: patientID,
		start:     startStr,
		end:       endStr,
	}, true
}

// reportCount 汇总视角的一项（key = 日期 / 类型 / 处理状态）
type reportCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// abnormalReportData GET /admin/abnormal-reports 的 data 字段
type abnormalReportData struct {
	PatientID string        `json:"patientId"`
	Start     string        `json:"start"`
	End       string        `json:"end"`
	Total     int64         `json:"total"`
	ByStatus  []reportCount `json:"byStatus"`
	ByType    []reportCount `json:"byType"`
	ByDay     []reportCount `json:"byDay"`
}

// buildAbnormalReport 由「日×类型×状态」分组行派生三个视角（纯函数，可单测）。
// byStatus 固定按 pending→processing→processed 排（缺项补 0，前端表头稳定）；
// byType 计数降序、同数按类型名升序；byDay 按日期升序（时间轴方向自然）。
func buildAbnormalReport(rq reportQuery, rows []repo.AlertSummaryRow) abnormalReportData {
	var total int64
	byType := map[string]int64{}
	byDay := map[string]int64{}
	byStatus := map[string]int64{}
	for _, row := range rows {
		total += row.Count
		byType[row.Type] += row.Count
		byDay[row.Date] += row.Count
		byStatus[row.ProcessStatus] += row.Count
	}
	data := abnormalReportData{
		PatientID: rq.patientID, Start: rq.start, End: rq.end, Total: total,
		ByStatus: make([]reportCount, 0, len(byStatus)),
		ByType:   make([]reportCount, 0, len(byType)),
		ByDay:    make([]reportCount, 0, len(byDay)),
	}
	ordered := []string{"pending", "processing", "processed"}
	for _, st := range ordered {
		data.ByStatus = append(data.ByStatus, reportCount{Key: st, Count: byStatus[st]})
		delete(byStatus, st)
	}
	for k, v := range byStatus { // 白名单外的历史值：不丢计数
		data.ByStatus = append(data.ByStatus, reportCount{Key: k, Count: v})
	}
	for k, v := range byType {
		data.ByType = append(data.ByType, reportCount{Key: k, Count: v})
	}
	sort.Slice(data.ByType, func(i, j int) bool {
		if data.ByType[i].Count != data.ByType[j].Count {
			return data.ByType[i].Count > data.ByType[j].Count
		}
		return data.ByType[i].Key < data.ByType[j].Key
	})
	for k, v := range byDay {
		data.ByDay = append(data.ByDay, reportCount{Key: k, Count: v})
	}
	sort.Slice(data.ByDay, func(i, j int) bool { return data.ByDay[i].Key < data.ByDay[j].Key })
	return data
}

// abnormalReport GET /api/v1/admin/abnormal-reports —— 按患者 + 日期范围汇总。
func (h *Handler) abnormalReport(w http.ResponseWriter, r *http.Request) {
	rq, ok := h.parseReportQuery(w, r)
	if !ok {
		return
	}
	rows, err := h.public.SummarizeAlerts(r.Context(), rq.filter)
	if err != nil {
		h.log.Error().Err(err).Msg("summarize abnormal report failed")
		h.reject(w, codeInternalError, "summarize abnormal report failed")
		return
	}
	writeJSON(w, http.StatusOK, envelope{
		Code: codeSuccess, Message: "success", Data: buildAbnormalReport(rq, rows),
	})
}

// exportAbnormalReport GET /api/v1/admin/abnormal-reports/export —— CSV 明细下载。
// 与汇总同一 WHERE、同一排序口径（时间升序输出，便于按日翻阅）。
// 超出 repo.MaxExportRows 时只导最近若干条，并在文件末行明示截断（不把局部当全量）。
func (h *Handler) exportAbnormalReport(w http.ResponseWriter, r *http.Request) {
	rq, ok := h.parseReportQuery(w, r)
	if !ok {
		return
	}
	rows, truncated, err := h.public.ListAlertsForExport(r.Context(), rq.filter, repo.MaxExportRows)
	if err != nil {
		h.log.Error().Err(err).Msg("export abnormal report failed")
		h.reject(w, codeInternalError, "export abnormal report failed")
		return
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Ts.Before(rows[j].Ts) })

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+reportFilename(rq))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte(csvBOM))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"告警ID", "患者ID", "患者姓名", "设备ID", "异常类型", "采集点", "详情",
		"阈值(N)", "实际值(N)", "采集时间(北京)", "读取状态", "处理状态",
		"开始处理时间", "处理时间", "处理人", "处理备注",
	})
	for _, row := range rows {
		_ = cw.Write([]string{
			strconv.FormatInt(row.AlertID, 10),
			csvCell(row.PatientID),
			csvCell(row.PatientName),
			csvCell(row.DeviceID),
			csvCell(labelOf(reportAlertTypeLabels, row.Type)),
			csvCell(row.SensorPoint),
			csvCell(row.Detail),
			strconv.FormatFloat(row.ThresholdValue, 'f', 2, 64),
			strconv.FormatFloat(row.ActualValue, 'f', 2, 64),
			row.Ts.In(scheduler.CSTZone()).Format(reportTimeLayout),
			csvCell(labelOf(reportReadStatusLabels, row.ReadStatus)),
			csvCell(labelOf(reportProcessStatusLabels, row.ProcessStatus)),
			formatCST(row.InProgressAt),
			formatCST(row.ProcessedAt),
			csvCell(derefString(row.ProcessedBy)),
			csvCell(derefString(row.ProcessNote)),
		})
	}
	if truncated {
		_ = cw.Write([]string{"注意：范围内数据超过 " + strconv.Itoa(repo.MaxExportRows) + " 条，本文件仅含最近 " + strconv.Itoa(repo.MaxExportRows) + " 条"})
	}
	cw.Flush()
}

// reportFilename 下载文件名。patientId 来自调用方，直接进 Content-Disposition
// 会允许 CR/LF/引号注入响应头，故只保留 [A-Za-z0-9_-] 且限长。
func reportFilename(rq reportQuery) string {
	safe := make([]rune, 0, 32)
	for _, c := range rq.patientID {
		if len(safe) >= 32 {
			break
		}
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			safe = append(safe, c)
		default:
			safe = append(safe, '_')
		}
	}
	return `"abnormal-report-` + string(safe) + `-` + rq.start + `_` + rq.end + `.csv"`
}

// formatCST 可空时刻 → 北京时间字符串（NULL 出空串，Excel 单元格留空）
func formatCST(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(scheduler.CSTZone()).Format(reportTimeLayout)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// csvCell 防电子表格式注入：以 = + - @ 或制表/回车开头的自由文本（备注、详情、姓名）
// 会被 Excel/Sheets 当公式求值，统一加前导单引号使其退化为纯文本。
func csvCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}
