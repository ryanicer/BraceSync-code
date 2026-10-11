// Package flowstarter — T653 告警入库后按「类型↔模板绑定」自动建流程实例的服务间客户端。
//
// 链路：alert-service 三条告警创建路径（handler.evaluate 内联 / consumer.processItem 补偿 /
// scanner 定时扫描）在 CreateAlert 成功且 created=true 后调用 AutoStart；
// user-service POST /internal/flow/auto-start 查绑定建实例。
//
// 与通知链解耦（PRD V3.44 R8 甲）：
//   - 无绑定 → user-service 回 started=false,reason=unbound，正常路径；
//   - 已有实例 → started=false,reason=exists（一告警一实例，409 语义幂等）；
//   - 网络错误/非 200/超时 → 仅记日志，不阻塞告警落库与通知（本侧无重试，
//     绑定本就是「显式无兜底」，错过自动建实例不影响告警与通知；如需补实例可走人工启动端点）。
package flowstarter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// DefaultTimeout 服务间调用熔断超时（建实例含一次 DB 事务，正常 <100ms；1s 为抖动兜底）。
const DefaultTimeout = time.Second

// Starter 自动建实例契约（handler/consumer/scanner 三处共同注入点；测试可换 fake）。
type Starter interface {
	AutoStart(ctx context.Context, alertID, alertType string)
}

// Noop 未配置 user-service 地址时的占位（与 consumer.NoopNotifier 同思路）。
type Noop struct{}

// AutoStart 空实现。
func (Noop) AutoStart(context.Context, string, string) {}

// autoStartRequest 请求体（与 user-service model.AutoStartFlowRequest 同形）。
type autoStartRequest struct {
	AlertID   string `json:"alertId"`
	AlertType string `json:"alertType"`
}

// autoStartResponse 统一响应体（code!=0 视为失败）。
type autoStartResponse struct {
	Code int `json:"code"`
	Data struct {
		Started    bool    `json:"started"`
		Reason     string  `json:"reason"`
		InstanceID *string `json:"instanceId"`
	} `json:"data"`
}

// HTTPClient Starter 的 HTTP 实现（范式对齐 data-service HTTPAlertClient）。
type HTTPClient struct {
	baseURL string
	client  *http.Client
	log     zerolog.Logger
}

// New 创建客户端；timeout<=0 用 DefaultTimeout；log 零值可用（内部再降级 Nop）。
func New(baseURL string, timeout time.Duration, log zerolog.Logger) *HTTPClient {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &HTTPClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
		log:     log,
	}
}

// AutoStart POST {baseURL}/internal/flow/auto-start。
// 任何失败只打日志不外抛：调用方在告警热路径上，流程链不得反噬告警链。
func (c *HTTPClient) AutoStart(ctx context.Context, alertID, alertType string) {
	if c == nil || c.baseURL == "" || alertID == "" || alertType == "" {
		return
	}
	body, err := json.Marshal(autoStartRequest{AlertID: alertID, AlertType: alertType})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/internal/flow/auto-start", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		c.log.Warn().Err(err).Str("alert_type", alertType).Str("alert_id", alertID).
			Msg("flow auto-start call failed (non-blocking)")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	if err != nil {
		c.log.Warn().Err(err).Str("alert_type", alertType).Msg("flow auto-start read response failed (non-blocking)")
		return
	}
	if resp.StatusCode != http.StatusOK {
		c.log.Warn().Int("http_status", resp.StatusCode).Str("alert_type", alertType).
			Str("alert_id", alertID).Msg("flow auto-start non-200 (non-blocking)")
		return
	}
	var parsed autoStartResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		c.log.Warn().Err(err).Str("alert_type", alertType).Msg("flow auto-start bad response json (non-blocking)")
		return
	}
	if parsed.Code != 0 {
		c.log.Warn().Int("code", parsed.Code).Str("alert_type", alertType).
			Msg("flow auto-start business code non-zero (non-blocking)")
		return
	}
	if parsed.Data.Started {
		c.log.Info().Str("alert_type", alertType).Str("alert_id", alertID).
			Str("instance_id", deref(parsed.Data.InstanceID)).Msg("flow auto-started by binding")
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
