package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AlertEvaluator 内联告警评估契约（架构 §1.3 / §3.4）：
// data-service 上报后同步调用 alert-service /internal/evaluate，
// 100ms 超时熔断；超时/不可用由调用方降级写 alert:pending。
type AlertEvaluator interface {
	Evaluate(ctx context.Context, req *AlertEvalRequest) (*AlertEvalResult, error)
}

// AlertEvalRequest 内联评估请求（帧引用 + 必要上下文）
type AlertEvalRequest struct {
	DeviceID   string    `json:"device_id"`
	PatientID  string    `json:"patient_id"`
	Timestamp  time.Time `json:"timestamp"` // 帧采集时刻（ISO 8601 UTC）
	Points     []float64 `json:"points"`    // 20 点压力值
	UploadTime time.Time `json:"upload_time"`
	IsBackfill bool      `json:"is_backfill"`
}

// AlertEvalResult 内联评估响应 data
type AlertEvalResult struct {
	ShouldAlert bool   `json:"shouldAlert"`
	AlertType   string `json:"alertType"`
	SensorPoint string `json:"sensorPoint"`
}

// HTTPAlertClient AlertEvaluator 的 HTTP 实现
type HTTPAlertClient struct {
	baseURL string
	client  *http.Client
}

// NewHTTPAlertClient 创建客户端；timeout 即熔断超时（架构约定 100ms）
func NewHTTPAlertClient(baseURL string, timeout time.Duration) *HTTPAlertClient {
	return &HTTPAlertClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

// evaluateResponse alert-service 统一响应体包裹
type evaluateResponse struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    *AlertEvalResult `json:"data"`
}

// Evaluate POST {baseURL}/internal/evaluate；非 200 / code!=0 / 网络错误均视为不可用
func (c *HTTPAlertClient) Evaluate(ctx context.Context, req *AlertEvalRequest) (*AlertEvalResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/evaluate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err // 超时/连接失败 → 调用方降级
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alert-service returned HTTP %d", resp.StatusCode)
	}
	var parsed evaluateResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("alert-service invalid response: %w", err)
	}
	if parsed.Code != 0 {
		return nil, fmt.Errorf("alert-service code=%d: %s", parsed.Code, parsed.Message)
	}
	if parsed.Data == nil {
		parsed.Data = &AlertEvalResult{}
	}
	return parsed.Data, nil
}

// NoopAlertClient 始终不可用的占位实现（alert-service 地址未配置时使用，
// 全量走 alert:pending 降级队列，由 alert-service 常驻消费者补偿）
type NoopAlertClient struct{}

// Evaluate 返回固定错误触发降级路径
func (NoopAlertClient) Evaluate(context.Context, *AlertEvalRequest) (*AlertEvalResult, error) {
	return nil, fmt.Errorf("alert-service not configured")
}
