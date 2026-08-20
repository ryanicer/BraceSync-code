// Package metrics — alert-service Prometheus 指标（T010）
//
// 对齐：架构 §6.1 可观测性（alert:pending 长度指标）/ §3.4 降级策略。
// 运维告警规则"alert:pending 长度 >0 持续 5min"直接消费 alert_pending_queue_length。
package metrics

import "github.com/prometheus/client_golang/prometheus"

// 降级队列处理结果（alert_pending_processed_total 的 outcome 标签）
const (
	OutcomeAlerted   = "alerted"    // 补偿评估命中并落库 + 推送
	OutcomeStale     = "stale"      // >1h 积压帧：仅补告警记录，不推送
	OutcomeClean     = "clean"      // 评估无命中
	OutcomeDeduped   = "deduped"    // 幂等键命中（重复入队）
	OutcomeDropped   = "dropped"    // 负载非法（反序列化/字段校验失败）
	OutcomeError     = "error"      // 处理失败（Redis/PG 异常）
	OutcomeEvalError = "eval_error" // 内联评估落库失败（调用方降级入队兜底）
)

// 通知推送结果（alert_notify_total 的 outcome 标签，T019）
const (
	OutcomeSent        = "sent"         // 推送成功（msg-service accepted=true）
	OutcomeFailed      = "failed"       // 推送失败（已入重试队列）
	OutcomeRetryQueued = "retry_queued" // 已进入重试队列
)

var (
	// PendingQueueLength alert:pending 当前长度（常驻消费者每轮刷新）
	PendingQueueLength = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "alert_pending_queue_length",
		Help: "Current length of the alert:pending degradation queue",
	})

	// PendingProcessedTotal 消费者处理帧计数（按结果分类）
	PendingProcessedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "alert_pending_processed_total",
		Help: "Frames processed by the alert:pending consumer, by outcome",
	}, []string{"outcome"})

	// InlineEvaluatedTotal /internal/evaluate 内联评估请求计数（按结果分类：
	// alerted=命中落库 / clean=无命中 / invalid=请求非法 / eval_error=落库失败）
	InlineEvaluatedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "alert_inline_evaluated_total",
		Help: "Inline /internal/evaluate requests, by outcome",
	}, []string{"outcome"})

	// NotifyTotal 告警通知推送计数（T019；按结果分类：
	// sent=成功 / failed=失败入重试 / retry_queued=已入重试队列 / dropped=丢弃）
	NotifyTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "alert_notify_total",
		Help: "Alert notifications sent to msg-service, by outcome",
	}, []string{"outcome"})
)

func init() {
	prometheus.MustRegister(PendingQueueLength, PendingProcessedTotal, InlineEvaluatedTotal, NotifyTotal)
}
