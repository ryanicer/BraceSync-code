// Package consumer — HTTPNotifier 告警推送实现（T019）
//
// 替换 NoopNotifier，打通 alert→msg 推送链路端到端。
// 调用 msg-service POST /internal/msg/send（X-Internal-Service 头 + Compose 内网隔离）。
//
// 降级策略（对齐 T010 降级队列模式 / 架构 §3.4）：
//   - HTTP 调用超时（默认 1s）或失败不阻塞告警落库（落库在 Notify 之前已完成）；
//   - 失败进 Redis 本地重试队列 alert:notify_pending（LPUSH 非阻塞）；
//   - 常驻 goroutine 轮询 RPOP 重试（最多 maxRetries 次，间隔由消费端控制）；
//   - Redis 不可用时仅日志告警，不抛错不影响主链路。
//
// Prometheus 指标：alert_notify_total（outcome: sent / failed / retry_queued / dropped）。
package consumer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/bracesync/bracesync/services/alert-service/internal/metrics"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

// notifyRequest msg-service sendAlertNotification 请求体（契约 docs/
type notifyRequest struct {
	AlertID        string   `json:"alertId"`
	Type           string   `json:"type"`
	PatientID      string   `json:"patientId"`
	DeviceID       string   `json:"deviceId"`
	Detail         string   `json:"detail"`
	SensorPoint    string   `json:"sensorPoint,omitempty"`
	ThresholdValue *float64 `json:"thresholdValue,omitempty"`
	ActualValue    *float64 `json:"actualValue,omitempty"`
	Timestamp      string   `json:"timestamp"` // ISO 8601
}

// notifyResponse msg-service sendAlertNotification 响应 data（仅解析 accepted 字段）
type notifyResponse struct {
	Accepted bool `json:"accepted"`
}

// envelope 统一响应体（msg-service 返回格式）
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    *notifyResponse `json:"data"`
}

// RetryQueue 通知重试队列契约（Redis LPUSH/RPOP）
type RetryQueue interface {
	// Push 非阻塞推入重试队列；队列不可用时返回 error（调用方仅日志不阻塞）
	Push(ctx context.Context, payload string) error
	// Pop 弹出一条重试负载；队列空返回 ok=false
	Pop(ctx context.Context) (payload string, ok bool, err error)
	// Run 常驻消费重试，直到 ctx 取消
	Run(ctx context.Context, interval time.Duration)
}

// HTTPNotifier Notifier HTTP 实现：调用 msg-service 推送告警通知。
// 超时/失败不阻塞主链路，失败进本地重试队列。
type HTTPNotifier struct {
	url        string       // msg-service 地址（如 http://msg-service:8081）
	client     *http.Client // 带超时的 HTTP 客户端
	retryQueue RetryQueue   // 失败重试队列（Redis）；nil 时仅日志
	maxRetries int          // 最大重试次数（含首次调用 = maxRetries+1 次机会）
	log        zerolog.Logger
}

// HTTPNotifierConfig HTTPNotifier 配置
type HTTPNotifierConfig struct {
	MsgServiceURL string        // msg-service 地址（必填）
	Timeout       time.Duration // HTTP 调用超时，默认 1s
	MaxRetries    int           // 最大重试次数，默认 3
	RetryQueue    RetryQueue    // 重试队列（可选；nil 时仅日志不重试）
	Logger        zerolog.Logger
}

// NewHTTPNotifier 创建 HTTPNotifier
func NewHTTPNotifier(cfg HTTPNotifierConfig) *HTTPNotifier {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = time.Second
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &HTTPNotifier{
		url:        cfg.MsgServiceURL,
		client:     &http.Client{Timeout: timeout},
		retryQueue: cfg.RetryQueue,
		maxRetries: maxRetries,
		log:        cfg.Logger,
	}
}

// Notify 推送告警通知到 msg-service（非阻塞：失败不 panic、不阻塞调用方）。
// 链路：构造请求 → HTTP POST → 解析响应 → 失败进重试队列。
func (n *HTTPNotifier) Notify(ctx context.Context, alert scanner.NewAlert) {
	req := n.buildRequest(alert)
	body, err := json.Marshal(req)
	if err != nil {
		metrics.NotifyTotal.WithLabelValues(metrics.OutcomeDropped).Inc()
		n.log.Error().Err(err).Str("alert_id", alert.AlertID).Msg("notify: marshal request failed")
		return
	}

	if err := n.doSend(ctx, body); err != nil {
		metrics.NotifyTotal.WithLabelValues(metrics.OutcomeFailed).Inc()
		n.log.Warn().Err(err).Str("alert_id", alert.AlertID).Msg("notify: send to msg-service failed")
		n.enqueueRetry(ctx, body, 0)
		return
	}

	metrics.NotifyTotal.WithLabelValues(metrics.OutcomeSent).Inc()
	n.log.Debug().Str("alert_id", alert.AlertID).Msg("notify: alert notification accepted")
}

// buildRequest scanner.NewAlert → notifyRequest（对齐 msg-service sendAlertRequest DTO）
func (n *HTTPNotifier) buildRequest(alert scanner.NewAlert) notifyRequest {
	req := notifyRequest{
		AlertID:   alert.AlertID,
		Type:      string(alert.Type),
		PatientID: alert.PatientID,
		DeviceID:  alert.DeviceID,
		Detail:    alert.Detail,
		Timestamp: alert.Ts.UTC().Format(time.RFC3339),
	}
	if alert.SensorPoint != "" {
		req.SensorPoint = alert.SensorPoint
	}
	if alert.ThresholdValue != 0 {
		v := alert.ThresholdValue
		req.ThresholdValue = &v
	}
	if alert.ActualValue != 0 {
		v := alert.ActualValue
		req.ActualValue = &v
	}
	return req
}

// doSend 执行 HTTP POST 并解析响应；非 2xx 或 accepted=false 返回 error
func (n *HTTPNotifier) doSend(ctx context.Context, body []byte) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		n.url+"/internal/msg/send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Service", "alert-service")

	resp, err := n.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("msg-service returned %d: %s", resp.StatusCode, respBody)
	}

	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("msg-service error code=%d: %s", env.Code, env.Message)
	}
	if env.Data != nil && !env.Data.Accepted {
		return fmt.Errorf("msg-service rejected notification")
	}
	return nil
}

// enqueueRetry 推入重试队列（失败仅日志，不影响主链路）
func (n *HTTPNotifier) enqueueRetry(ctx context.Context, body []byte, attempt int) {
	if n.retryQueue == nil {
		n.log.Warn().Int("attempt", attempt).Msg("notify: no retry queue configured, notification lost")
		return
	}
	item := retryItem{
		Payload:  string(body),
		Attempt:  attempt,
		MaxRetry: n.maxRetries,
	}
	data, err := json.Marshal(item)
	if err != nil {
		metrics.NotifyTotal.WithLabelValues(metrics.OutcomeDropped).Inc()
		n.log.Error().Err(err).Msg("notify: marshal retry item failed")
		return
	}
	if err := n.retryQueue.Push(ctx, string(data)); err != nil {
		metrics.NotifyTotal.WithLabelValues(metrics.OutcomeDropped).Inc()
		n.log.Error().Err(err).Msg("notify: push to retry queue failed, notification lost")
		return
	}
	metrics.NotifyTotal.WithLabelValues(metrics.OutcomeRetryQueued).Inc()
	n.log.Info().Int("attempt", attempt).Msg("notify: enqueued for retry")
}

// retryItem 重试队列元素（JSON 序列化存入 Redis）
type retryItem struct {
	Payload  string `json:"payload"`
	Attempt  int    `json:"attempt"`
	MaxRetry int    `json:"max_retry"`
}

// DrainRetryOnce 排空一轮重试队列（由外部 goroutine 定期调用）。
// 每条：doSend → 失败且 attempt < maxRetry → 重新入队；超过重试上限则丢弃。
func (n *HTTPNotifier) DrainRetryOnce(ctx context.Context) (int, error) {
	if n.retryQueue == nil {
		return 0, nil
	}
	processed := 0
	for processed < 50 { // 单轮上限
		if ctx.Err() != nil {
			break
		}
		payload, ok, err := n.retryQueue.Pop(ctx)
		if err != nil {
			return processed, err
		}
		if !ok {
			break
		}
		processed++

		var item retryItem
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			n.log.Warn().Err(err).Msg("notify retry: invalid payload, dropped")
			continue
		}

		if err := n.doSend(ctx, []byte(item.Payload)); err != nil {
			if item.Attempt+1 < item.MaxRetry {
				n.enqueueRetry(ctx, []byte(item.Payload), item.Attempt+1)
			} else {
				metrics.NotifyTotal.WithLabelValues(metrics.OutcomeDropped).Inc()
				n.log.Error().Int("attempts", item.Attempt+1).Err(err).
					Msg("notify retry: max retries exhausted, notification dropped")
			}
		} else {
			metrics.NotifyTotal.WithLabelValues(metrics.OutcomeSent).Inc()
		}
	}
	return processed, nil
}

// RunRetry 常驻重试消费 goroutine（轮询 DrainRetryOnce 直至 ctx 取消）
func (n *HTTPNotifier) RunRetry(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	n.log.Info().Dur("interval", interval).Msg("notify retry consumer started")
	for {
		if _, err := n.DrainRetryOnce(ctx); err != nil && ctx.Err() == nil {
			n.log.Warn().Err(err).Msg("notify retry drain failed")
		}
		select {
		case <-ctx.Done():
			n.log.Info().Msg("notify retry consumer stopped")
			return
		case <-ticker.C:
		}
	}
}
