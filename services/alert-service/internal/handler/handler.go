// Package handler — alert-service 北向 HTTP（T010）
//
// 路由（Go 1.22 ServeMux 方法路由）：
//
//	POST /internal/evaluate  上报内联规则评估（data-service 服务间直连白名单，不经网关，架构 §3.3/§3.4）
//	GET  /api/v1/alerts      公开分页查询（T028，经 gateway 代理 + 统一鉴权）
//	POST /api/v1/alerts/{alertId}/process  标记已处理（T028，幂等）
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
	"github.com/bracesync/bracesync/services/alert-service/internal/metrics"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

// 业务错误码（统一响应体 code 字段；HTTP 状态码同步映射）
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
type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Handler /internal/evaluate 处理器
type Handler struct {
	eval     *engine.RuleEvaluator
	alerts   consumer.AlertCreator
	notifier consumer.Notifier
	public   PublicAlertStore // T028 公开端点数据源（SetPublicStore 注入，可空）
	log      zerolog.Logger
}

// New 组装 Handler；notifier 传 nil 使用 NoopNotifier（一期未接 msg-service）
func New(eval *engine.RuleEvaluator, alerts consumer.AlertCreator, notifier consumer.Notifier) *Handler {
	if notifier == nil {
		notifier = consumer.NoopNotifier{}
	}
	return &Handler{eval: eval, alerts: alerts, notifier: notifier, log: zerolog.Nop()}
}

// SetLogger 注入日志器（生产使用；默认 Nop）
func (h *Handler) SetLogger(l zerolog.Logger) { h.log = l }

// Router 组装路由（可测试）
func (h *Handler) Router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/evaluate", h.evaluate)
	mux.HandleFunc("GET /api/v1/alerts", h.listAlerts)                      // T028
	mux.HandleFunc("POST /api/v1/alerts/{alertId}/process", h.processAlert) // T028
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
		h.reject(w, codeInvalidParam, "read body: "+err.Error())
		return
	}
	var req EvalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.reject(w, codeInvalidParam, "invalid json: "+err.Error())
		return
	}
	frame, err := req.ToPressureFrame()
	if err != nil || frame.PatientID == "" || frame.DeviceID == "" {
		metrics.InlineEvaluatedTotal.WithLabelValues(metrics.OutcomeDropped).Inc()
		h.reject(w, codeInvalidParam, "invalid frame ref (need device_id/patient_id/20 points)")
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
	}
	alertID, _, err := h.alerts.CreateAlert(r.Context(), alert)
	if err != nil {
		// 落库失败 → 非 0 码触发调用方降级入队，补偿评估兜底（不丢告警）
		metrics.InlineEvaluatedTotal.WithLabelValues(metrics.OutcomeEvalError).Inc()
		h.log.Error().Err(err).Str("device_id", frame.DeviceID).Msg("inline alert persist failed, caller will degrade")
		h.reject(w, codeInternalError, "persist alert failed")
		return
	}
	alert.AlertID = alertID // 落库后回填，Notify 时使用

	metrics.InlineEvaluatedTotal.WithLabelValues(metrics.OutcomeAlerted).Inc()
	h.notifier.Notify(r.Context(), alert)
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

// reject 非 0 码响应（HTTP 状态码与 code 一致，调用方 HTTPAlertClient 视为不可用）
func (h *Handler) reject(w http.ResponseWriter, code int, message string) {
	h.log.Warn().Int("code", code).Msg(message)
	writeJSON(w, code, envelope{Code: code, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
