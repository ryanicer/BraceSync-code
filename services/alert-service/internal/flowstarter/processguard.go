// T668 告警处理（process/processing）的节点执法一问客户端。
//
// 与 AutoStart（告警创建热路径、失败只 warn 不阻塞）相反：本守卫在**写操作**路径上，
// 回答「该操作者能不能处理这条告警」。绑定流程且实例 running 时，user-service 按当前
// 节点的角色/指派人执法；调用错误（user-service 不可达/非 200/坏响应）一律外抛，
// 由 handler 回 503——权限判定 fail-closed，不允许因依赖故障旁路成 staff 放行。
package flowstarter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Decision 节点执法结论。
type Decision struct {
	Enforced bool   // 该告警存在 running 流程实例（true=必须过节点执法）
	Allowed  bool   // 操作者是否被允许处理
	Reason   string // 拒绝时的可读短句（来自 user-service 执法判定）
}

// Guard 告警处理守卫契约（alert handler 注入点；测试可换 fake）。
type Guard interface {
	CheckProcess(ctx context.Context, alertID int64, operatorID, role string) (Decision, error)
}

// NoopGuard 未配置 user-service 地址时的占位：不启用执法（开发/测试环境无流程服务时保持 staff 现状）。
type NoopGuard struct{}

// CheckProcess 空实现 = 不执法直接放行。
func (NoopGuard) CheckProcess(context.Context, int64, string, string) (Decision, error) {
	return Decision{Enforced: false, Allowed: true}, nil
}

type processGuardRequest struct {
	AlertID    int64  `json:"alertId"`
	OperatorID string `json:"operatorId"`
	Role       string `json:"role"`
}

type processGuardResponse struct {
	Code int `json:"code"`
	Data struct {
		Enforced      bool   `json:"enforced"`
		Allowed       bool   `json:"allowed"`
		Reason        string `json:"reason"`
		InstanceID    string `json:"instanceId"`
		CurrentNodeID string `json:"currentNodeId"`
	} `json:"data"`
}

// ProcessGuardHTTPClient Guard 的 HTTP 实现（与 HTTPClient 同 baseURL 范式）。
type ProcessGuardHTTPClient struct {
	baseURL string
	client  *http.Client
}

// NewProcessGuard 创建守卫客户端；timeout<=0 用 DefaultTimeout。
func NewProcessGuard(baseURL string, timeout time.Duration) *ProcessGuardHTTPClient {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &ProcessGuardHTTPClient{baseURL: baseURL, client: &http.Client{Timeout: timeout}}
}

// CheckProcess POST {baseURL}/internal/flow/process-guard。任何依赖侧异常都返回 error
// （fail-closed，调用方回 503）；HTTP 200 + code=0 才采信结论。
func (c *ProcessGuardHTTPClient) CheckProcess(ctx context.Context, alertID int64, operatorID, role string) (Decision, error) {
	if c == nil || c.baseURL == "" {
		return Decision{Enforced: false, Allowed: true}, nil
	}
	body, err := json.Marshal(processGuardRequest{AlertID: alertID, OperatorID: operatorID, Role: role})
	if err != nil {
		return Decision{}, fmt.Errorf("marshal process-guard request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/internal/flow/process-guard", bytes.NewReader(body))
	if err != nil {
		return Decision{}, fmt.Errorf("build process-guard request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("call process-guard: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	if err != nil {
		return Decision{}, fmt.Errorf("read process-guard response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Decision{}, fmt.Errorf("process-guard non-200: status=%d", resp.StatusCode)
	}
	var parsed processGuardResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Decision{}, fmt.Errorf("parse process-guard response: %w", err)
	}
	if parsed.Code != 0 {
		return Decision{}, fmt.Errorf("process-guard business code=%d", parsed.Code)
	}
	return Decision{
		Enforced: parsed.Data.Enforced,
		Allowed:  parsed.Data.Allowed,
		Reason:   parsed.Data.Reason,
	}, nil
}
