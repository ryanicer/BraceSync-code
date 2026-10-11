// Package handler — alert-service 北向 HTTP（T010）
//
// 路由（Go 1.22 ServeMux 方法路由）：
//
//	POST /internal/evaluate  上报内联规则评估（data-service 服务间直连白名单，不经网关，架构 §3.3/§3.4）
//	GET  /api/v1/alerts      公开分页查询（T028，经 gateway 代理 + 统一鉴权）
//	POST /api/v1/alerts/{alertId}/process  标记已处理（T028，幂等）
//	POST /api/v1/alerts/{alertId}/processing  开始处理（T257 2.7，幂等；已处理 409）
//	GET  /api/v1/admin/abnormal-reports       患者异常报告汇总（T300，staff-only）
//	GET  /api/v1/admin/abnormal-reports/export 同上口径 CSV 导出（T300）
//	GET  /metrics            Prometheus 采集端点（架构 §6.1）
//	GET  /healthz            存活探针
//
// 统一响应体（架构 §3.5）：{ "code": 0, "message": "success", "data": {...} }
// data-service 侧契约：HTTPAlertClient 解析 code/data，非 200 或 code!=0 均触发降级入队。
package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"

	"github.com/bracesync/bracesync/services/alert-service/internal/consumer"
	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/flowstarter"
	"github.com/bracesync/bracesync/services/alert-service/internal/metrics"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

// 业务错误码（统一响应体 code 字段）。
// T402 丁-2：本轮只把越权与不存在两格收进「域号 4 + 0 + HTTP 三位」（见 public.go），
// 下面两格仍是裸 HTTP 数字，属有意保留的中间态，收口留给告警域自己的卡。
const (
	codeSuccess       = 0
	codeInvalidParam  = 400
	codeInternalError = 500
)

// EvalRequest /internal/evaluate 请求体（与 data-service AlertEvalRequest 字段一致）
type EvalRequest = consumer.FrameRef

// evalResultData 响应 data 字段（与 data-service AlertEvalResult 字段一致）
type evalResultData struct {
	ShouldAlert bool   `json:"shouldAlert"`
	AlertType   string `json:"alertType"`
	SensorPoint string `json:"sensorPoint"`
}

// envelope 统一响应体
// T464：错误响应的 Message 为用户面中文（技术文本只进日志），Trace 回传错误码 + 请求关联号。
type envelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    any         `json:"data,omitempty"`
	Trace   *errorTrace `json:"trace,omitempty"`
}

// Handler /internal/evaluate 处理器
type Handler struct {
	eval     *engine.RuleEvaluator
	alerts   consumer.AlertCreator
	notifier consumer.Notifier
	flow     flowstarter.Starter // T653：告警入库后按绑定自动建实例（SetFlowStarter 注入，默认 Noop）
	public   PublicAlertStore    // T028 公开端点数据源（SetPublicStore 注入，可空）
	log      zerolog.Logger
}

// New 组装 Handler；notifier 传 nil 使用 NoopNotifier（一期未接 msg-service）
func New(eval *engine.RuleEvaluator, alerts consumer.AlertCreator, notifier consumer.Notifier) *Handler {
	if notifier == nil {
		notifier = consumer.NoopNotifier{}
	}
	return &Handler{eval: eval, alerts: alerts, notifier: notifier,
		flow: flowstarter.Noop{}, log: zerolog.Nop()}
}

// SetLogger 注入日志器（生产使用；默认 Nop）
func (h *Handler) SetLogger(l zerolog.Logger) { h.log = l }

// SetFlowStarter T653：注入 user-service 自动建实例客户端（不注入 = 不自动建，告警与通知不受影响）。
func (h *Handler) SetFlowStarter(s flowstarter.Starter) {
	if s != nil {
		h.flow = s
	}
}

// Router 组装路由（可测试）
func (h *Handler) Router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/evaluate", h.evaluate)
	mux.HandleFunc("GET /api/v1/alerts", h.listAlerts)                                  // T028
	mux.HandleFunc("POST /api/v1/alerts/{alertId}/process", h.processAlert)             // T028
	mux.HandleFunc("POST /api/v1/alerts/{alertId}/processing", h.startProcessingAlert)  // T257 2.7
	mux.HandleFunc("GET /api/v1/admin/abnormal-reports", h.abnormalReport)              // T300 汇总
	mux.HandleFunc("GET /api/v1/admin/abnormal-reports/export", h.exportAbnormalReport) // T300 CSV 导出
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, envelope{Code: codeSuccess, Message: "success", Data: map[string]string{"status": "ok"}})
	})
	return mux
}

// evaluate 内联评估：命中即落库 + 推送；落库失败返回非 0 码，
// 由 data-service 降级入 alert:pending 兜底（告警不丢，幂等补偿不重复）
func (h *Handler) evaluate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "read body: "+err.Error())
		return
	}
	var req EvalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "invalid json: "+err.Error())
		return
	}
	frame, err := req.ToPressureFrame()
	if err != nil || frame.PatientID == "" || frame.DeviceID == "" {
		metrics.InlineEvaluatedTotal.WithLabelValues(metrics.OutcomeDropped).Inc()
		h.reject(w, r, http.StatusBadRequest, codeInvalidParam, "invalid frame ref (need device_id/patient_id/20 points)")
		return
	}

	result := h.eval.Evaluate(frame, nil) // 内联评估无前一帧上下文（与补偿路径一致，见 consumer 包注释）
	if result == nil {
		metrics.InlineEvaluatedTotal.WithLabelValues(metrics.OutcomeClean).Inc()
		writeJSON(w, http.StatusOK, envelope{Code: codeSuccess, Message: "success", Data: evalResultData{ShouldAlert: false}})
		return
	}

	alert := scanner.NewAlert{
		PatientID:      frame.PatientID,
		DeviceID:       frame.DeviceID,
		Type:           result.AlertType,
		SensorPoint:    result.SensorPoint,
		Detail:         result.Message,
		ThresholdValue: result.ThresholdValue,
		ActualValue:    result.ActualValue,
		Ts:             frame.Timestamp,
		IngestSource:   req.IngestSource, // T498 来源印章，空 → 落 NULL（见迁移 000033）
	}
	alertID, created, err := h.alerts.CreateAlert(r.Context(), alert)
	if err != nil {
		// 落库失败 → 非 0 码触发调用方降级入队，补偿评估兜底（不丢告警）
		metrics.InlineEvaluatedTotal.WithLabelValues(metrics.OutcomeEvalError).Inc()
		h.log.Error().Err(err).Str("device_id", frame.DeviceID).Msg("inline alert persist failed, caller will degrade")
		h.reject(w, r, http.StatusInternalServerError, codeInternalError, "persist alert failed")
		return
	}
	alert.AlertID = alertID // 落库后回填，Notify 时使用

	metrics.InlineEvaluatedTotal.WithLabelValues(metrics.OutcomeAlerted).Inc()
	h.notifier.Notify(r.Context(), alert)
	// T653：新建告警入库后按绑定自动建流程实例（与通知链解耦，失败仅日志；去重命中不重复触发）。
	if created && alertID != "" {
		h.flow.AutoStart(r.Context(), alertID, string(result.AlertType))
	}
	h.log.Info().Str("device_id", frame.DeviceID).
		Str("alert_type", string(result.AlertType)).
		Str("sensor_point", result.SensorPoint).
		Msg("inline alert hit")

	writeJSON(w, http.StatusOK, envelope{Code: codeSuccess, Message: "success", Data: evalResultData{
		ShouldAlert: true,
		AlertType:   string(result.AlertType),
		SensorPoint: result.SensorPoint,
	}})
}

// reject 非 0 码响应。
// T402 甲-1 前置：本函数原来只收一个 code，函数体里把同一个入参既当 HTTP 状态又当业务码
// （writeJSON(w, code, envelope{Code: code, ...})），于是「改业务码」必然连带改 HTTP 状态。
// 现拆成两个参数，32 个调用点各自显式给出 HTTP 状态；HTTP 状态取值与拆分前逐格相同。
// 调用方 data-service 的 HTTPAlertClient 仍按「非 2xx 视为不可用」处理，判定依据是 HTTP 状态，未受影响。
//
// T464：message 入参是技术文本（含 err.Error() 拼接与 alertId 回显），一律只写日志；
// 响应体给用户的是按码映射的中文短句 + trace（错误码 + 请求关联号）。
func (h *Handler) reject(w http.ResponseWriter, r *http.Request, httpStatus, code int, message string) {
	requestID := requestIDOf(r)
	w.Header().Set(HeaderRequestID, requestID)
	h.log.Warn().Int("http_status", httpStatus).Int("code", code).
		Str("request_id", requestID).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg(message)
	writeJSON(w, httpStatus, envelope{
		Code:    code,
		Message: userText(code),
		Trace:   &errorTrace{ErrorCode: code, RequestID: requestID},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
