package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DeviceReporter 上报/补传事件回写契约（架构 §4.6 / T015 状态机落库）：
// data-service 落库成功后调 device-service POST /internal/devices/:deviceId/report，
// 由 device-service 单调推进 devices.last_report_at 与 status。
//
// 分工依据：devices 表对 data-service 只读（repo.DeviceStore 契约注释「写归 device-service」），
// 故回填必须经服务间调用，不能由 data-service 直接 UPDATE。
type DeviceReporter interface {
	Report(ctx context.Context, deviceID string, ts time.Time, faultCode int) error
}

// DefaultDeviceReportTimeout 状态回写熔断超时。
// 取值为"不影响上报成功率"优先：设备采集间隔 30min，一次回写的往返耗时可忽略。
const DefaultDeviceReportTimeout = 300 * time.Millisecond

// deviceReportRequest device-service reportRequest 契约：timestamp 为 Unix 秒（0=服务器当前时刻）
type deviceReportRequest struct {
	Timestamp int64 `json:"timestamp"`
	FaultCode int   `json:"fault_code"`
}

// HTTPDeviceClient DeviceReporter 的 HTTP 实现
type HTTPDeviceClient struct {
	baseURL string
	client  *http.Client
}

// NewHTTPDeviceClient 创建客户端；timeout 即熔断超时
func NewHTTPDeviceClient(baseURL string, timeout time.Duration) *HTTPDeviceClient {
	return &HTTPDeviceClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

// deviceReportResponse device-service 统一响应体包裹
type deviceReportResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Report POST {baseURL}/internal/devices/{deviceID}/report；非 200 / code!=0 / 网络错误均返回 error
func (c *HTTPDeviceClient) Report(ctx context.Context, deviceID string, ts time.Time, faultCode int) error {
	body, err := json.Marshal(&deviceReportRequest{Timestamp: ts.Unix(), FaultCode: faultCode})
	if err != nil {
		return err
	}
	endpoint := c.baseURL + "/internal/devices/" + url.PathEscape(deviceID) + "/report"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("device-service returned HTTP %d", resp.StatusCode)
	}
	var parsed deviceReportResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("device-service invalid response: %w", err)
	}
	if parsed.Code != 0 {
		return fmt.Errorf("device-service code=%d: %s", parsed.Code, parsed.Message)
	}
	return nil
}
