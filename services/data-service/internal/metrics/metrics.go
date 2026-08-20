// Package metrics — data-service Prometheus 指标（T010）
//
// 对齐：架构 §3.4 降级策略 / §6.1 可观测性。
// 连续降级 >5min 的运维信号由 record.go 结构化日志 + 本包指标共同承载：
// data_alert_degraded_seconds >0 持续 5min 即可配 Prometheus 告警规则。
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	// AlertDegradeTotal 内联评估失败降级入 alert:pending 的帧计数
	AlertDegradeTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "data_alert_degrade_total",
		Help: "Frames degraded to alert:pending because alert-service evaluate failed",
	})

	// AlertDegradedSeconds 当前连续降级窗口时长（秒）；正常态为 0。
	// 每次降级帧刷新为已持续时长，恢复评估成功时归零。
	AlertDegradedSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "data_alert_degraded_seconds",
		Help: "Duration of the ongoing alert-service degradation window (0 = healthy)",
	})
)

func init() {
	prometheus.MustRegister(AlertDegradeTotal, AlertDegradedSeconds)
	prometheus.MustRegister(RollupJobTotal, ReportJobTotal, PartitionJobTotal, ArchiveJobTotal)
}

// ─────────────────────────────────────────────────────────────
// T021：定时任务指标
// ─────────────────────────────────────────────────────────────

var (
	// RollupJobTotal daily rollup / 补传重算执行计数
	RollupJobTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "data_rollup_job_total",
		Help: "Daily rollup job executions by status (ok/error)",
	}, []string{"status"})

	// ReportJobTotal 周报/月报生成执行计数
	ReportJobTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "data_report_job_total",
		Help: "Health report generation by type and status",
	}, []string{"type", "status"})

	// PartitionJobTotal 分区预建执行计数
	PartitionJobTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "data_partition_job_total",
		Help: "Partition pre-creation job executions by status",
	}, []string{"status"})

	// ArchiveJobTotal 冷归档各步骤执行计数
	ArchiveJobTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "data_archive_job_total",
		Help: "Cold archive job steps by step and status",
	}, []string{"step", "status"})
)
