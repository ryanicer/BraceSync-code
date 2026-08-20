// Package consumer — alert:pending 降级队列常驻消费者（T010）
//
// 对齐：架构 §3.4 同步调用降级策略 / §4.7 Redis Key 设计 / test-plan §3.1 A10。
//
// 链路：data-service 内联评估（/internal/evaluate，100ms 熔断）失败时将帧引用
// LPUSH alert:pending；本消费者常驻轮询 RPOP 补偿评估（FIFO），服务可用即排空
// 积压，不依赖重启触发。
//
// 语义：
//   - 幂等：补偿前 SET NX (device_id, timestamp) 幂等键，重复入队不重复告警；
//     alerts 表 uk_alerts_natural 唯一约束为 DB 层保底。
//   - 积压策略：入队超过 1h 的帧仅补告警记录不再推送（避免过时骚扰）。
//   - 补偿评估无前一帧上下文（rt:frame 已被后续帧覆盖），prevFrame 传 nil，
//     依赖前一帧的规则（压力波动/相邻帧中断）在补偿路径不生效——已知取舍。
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/rs/zerolog"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/metrics"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

// 默认参数
const (
	// DefaultPollInterval 常驻轮询间隔（队列空时空转成本 ≈ 1 次 RPOP）
	DefaultPollInterval = 500 * time.Millisecond
	// DefaultStaleThreshold 积压超过该时长的帧仅补告警记录不推送（架构 §3.4：1h）
	DefaultStaleThreshold = time.Hour
	// DefaultMaxBatch 单轮排空上限，防止异常积压下单轮阻塞过久
	DefaultMaxBatch = 200
)

// PointCount 压力帧点数（与 data-service model.PointCount 契约一致）
const PointCount = 20

// wearingThresholdN 佩戴判定阈值（PRD §8.1：帧 max_pressure > 0.5N 视为佩戴帧）。
// 与 data-service model.WearingThresholdN 保持同值；跨服务不引包，契约见架构 §4.7。
const wearingThresholdN = 0.5

// FrameRef 帧引用（跨服务契约：与 data-service AlertEvalRequest / 队列负载 frame 字段一致）。
// 体积小：device_id + timestamp + 20 点值，不含全量记录。
type FrameRef struct {
	DeviceID   string    `json:"device_id"`
	PatientID  string    `json:"patient_id"`
	Timestamp  time.Time `json:"timestamp"`
	Points     []float64 `json:"points"`
	UploadTime time.Time `json:"upload_time"`
	IsBackfill bool      `json:"is_backfill"`
}

// PendingItem alert:pending 队列元素（与 data-service pendingAlertItem 契约一致）
type PendingItem struct {
	QueuedAt time.Time `json:"queued_at"`
	Frame    FrameRef  `json:"frame"`
}

// ToPressureFrame 帧引用 → 引擎输入。Wearing 按 PRD §8.1 由最大压力推导。
func (f FrameRef) ToPressureFrame() (engine.PressureFrame, error) {
	frame := engine.PressureFrame{
		DeviceID:   f.DeviceID,
		PatientID:  f.PatientID,
		Timestamp:  f.Timestamp,
		UploadTime: f.UploadTime,
		IsBackfill: f.IsBackfill,
	}
	if len(f.Points) != PointCount {
		return frame, errors.New("points length != 20")
	}
	maxP := 0.0
	for i, p := range f.Points {
		frame.Pressures[i] = p
		if p > maxP {
			maxP = p
		}
	}
	frame.Wearing = maxP > wearingThresholdN
	return frame, nil
}

// Queue alert:pending 队列契约（repo 层实现）
type Queue interface {
	// Pop 弹出一条负载（FIFO）；队列空返回 ok=false
	Pop(ctx context.Context) (payload string, ok bool, err error)
	// Len 当前队列长度（Prometheus 指标采集）
	Len(ctx context.Context) (int64, error)
}

// EvalDeduper 补偿评估幂等键契约（repo 层实现）
type EvalDeduper interface {
	// MarkEvaluated 置 (device_id, timestamp) 幂等键；first=false 表示该帧已评估过
	MarkEvaluated(ctx context.Context, deviceID string, ts time.Time) (first bool, err error)
}

// AlertCreator 告警落地库契约（repo.PGAlertRepo 实现）
type AlertCreator interface {
	// CreateAlert 落库告警；唯一约束冲突返回 created=false（DB 层保底去重）。
	// alertID 为落库后主键（BIGINT 字符串化），去重时为空。
	CreateAlert(ctx context.Context, alert scanner.NewAlert) (alertID string, created bool, err error)
}

// Notifier 告警推送契约（msg-service 链路）。
// 一期未接 msg-service 使用 NoopNotifier；>1h 积压帧跳过 Notify 的语义不变。
type Notifier interface {
	Notify(ctx context.Context, alert scanner.NewAlert)
}

// NoopNotifier 占位推送器（一期告警推送未接入 msg-service）
type NoopNotifier struct{}

// Notify 空实现
func (NoopNotifier) Notify(context.Context, scanner.NewAlert) {}

// Consumer alert:pending 常驻消费者
type Consumer struct {
	queue          Queue
	dedup          EvalDeduper
	alerts         AlertCreator
	eval           *engine.RuleEvaluator
	notifier       Notifier
	staleThreshold time.Duration
	maxBatch       int
	now            func() time.Time
	log            zerolog.Logger
}

// New 组装消费者；staleThreshold/maxBatch 传零值使用默认（1h / 200）
func New(queue Queue, dedup EvalDeduper, alerts AlertCreator, eval *engine.RuleEvaluator, notifier Notifier) *Consumer {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	return &Consumer{
		queue:          queue,
		dedup:          dedup,
		alerts:         alerts,
		eval:           eval,
		notifier:       notifier,
		staleThreshold: DefaultStaleThreshold,
		maxBatch:       DefaultMaxBatch,
		now:            time.Now,
		log:            zerolog.Nop(),
	}
}

// SetLogger 注入日志器（生产使用；默认 Nop）
func (c *Consumer) SetLogger(l zerolog.Logger) { c.log = l }

// SetStaleThreshold 覆盖积压阈值（测试用）
func (c *Consumer) SetStaleThreshold(d time.Duration) { c.staleThreshold = d }

// SetNow 注入时钟（测试用）
func (c *Consumer) SetNow(now func() time.Time) { c.now = now }

// Run 常驻轮询直至 ctx 取消（服务可用即排空积压，不依赖重启）
func (c *Consumer) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	c.log.Info().Dur("interval", interval).Msg("alert:pending consumer started")
	for {
		if _, err := c.DrainOnce(ctx); err != nil && ctx.Err() == nil {
			c.log.Warn().Err(err).Msg("drain alert:pending failed")
		}
		select {
		case <-ctx.Done():
			c.log.Info().Msg("alert:pending consumer stopped")
			return
		case <-ticker.C:
		}
	}
}

// DrainOnce 排空一轮（上限 maxBatch），返回处理条数。
// 每轮结束刷新 alert_pending_queue_length 指标（空队列也刷 0）。
func (c *Consumer) DrainOnce(ctx context.Context) (int, error) {
	processed := 0
	for processed < c.maxBatch {
		if ctx.Err() != nil {
			break
		}
		payload, ok, err := c.queue.Pop(ctx)
		if err != nil {
			c.refreshQueueLength(ctx)
			return processed, err
		}
		if !ok {
			break
		}
		c.processItem(ctx, payload)
		processed++
	}
	c.refreshQueueLength(ctx)
	return processed, nil
}

// refreshQueueLength 刷新队列长度指标（采集失败仅日志，不影响消费）
func (c *Consumer) refreshQueueLength(ctx context.Context) {
	n, err := c.queue.Len(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.log.Warn().Err(err).Msg("llen alert:pending failed")
		}
		return
	}
	metrics.PendingQueueLength.Set(float64(n))
}

// processItem 单条补偿评估：反序列化 → 幂等 → 评估 → 落库 → （非积压）推送
func (c *Consumer) processItem(ctx context.Context, payload string) {
	var item PendingItem
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeDropped).Inc()
		c.log.Warn().Err(err).Msg("invalid alert:pending payload, dropped")
		return
	}

	frame, err := item.Frame.ToPressureFrame()
	if err != nil {
		metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeDropped).Inc()
		c.log.Warn().Err(err).Str("device_id", item.Frame.DeviceID).Msg("invalid frame ref, dropped")
		return
	}
	if frame.PatientID == "" {
		metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeDropped).Inc()
		c.log.Warn().Str("device_id", frame.DeviceID).Msg("frame ref missing patient_id, dropped")
		return
	}

	// 幂等：重复入队（data-service 重试/队列重放）不重复评估
	first, err := c.dedup.MarkEvaluated(ctx, frame.DeviceID, frame.Timestamp)
	if err != nil {
		metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeError).Inc()
		c.log.Error().Err(err).Str("device_id", frame.DeviceID).Msg("mark evaluated failed")
		return
	}
	if !first {
		metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeDeduped).Inc()
		c.log.Debug().Str("device_id", frame.DeviceID).Time("ts", frame.Timestamp).Msg("frame already evaluated, skip")
		return
	}

	stale := c.now().Sub(item.QueuedAt) > c.staleThreshold
	result := c.eval.Evaluate(frame, nil) // 补偿无前一帧上下文（包注释说明）
	if result == nil {
		metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeClean).Inc()
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
		Ts:             frame.Timestamp, // 告警时刻 = 帧采集时刻（uk_alerts_natural 组成部分）
	}
	alertID, created, err := c.alerts.CreateAlert(ctx, alert)
	if err != nil {
		metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeError).Inc()
		c.log.Error().Err(err).Str("device_id", frame.DeviceID).Msg("create alert failed (compensation)")
		return
	}
	if !created {
		metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeDeduped).Inc() // 唯一约束保底命中（幂等键外的极端并发场景）
		return
	}
	alert.AlertID = alertID // 落库后回填，Notify 时使用

	if stale {
		// >1h 积压帧：仅补告警记录不推送（避免过时骚扰，架构 §3.4）
		metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeStale).Inc()
		c.log.Info().Str("device_id", frame.DeviceID).
			Str("alert_type", string(result.AlertType)).
			Dur("backlog", c.now().Sub(item.QueuedAt)).
			Msg("stale frame compensated: alert recorded, push skipped")
		return
	}

	metrics.PendingProcessedTotal.WithLabelValues(metrics.OutcomeAlerted).Inc()
	c.notifier.Notify(ctx, alert)
	c.log.Info().Str("device_id", frame.DeviceID).
		Str("alert_type", string(result.AlertType)).
		Msg("compensation alert created")
}
