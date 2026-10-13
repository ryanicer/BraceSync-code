// 677 快处路由一问客户端（POST {user-service}/internal/flow/quick-route）。
//
// 与 T668 ProcessGuard 同在**写操作**路径：processAlert 落库前问「该告警直改还是转流程」。
// 判定权威在 user-service（绑定表归属地），本侧只采信结论：
//   - Route=flow 且未 processed → handler 回 409 强制转流程；
//   - Route=direct → 维持旧快处直改现状（677 最小闭环，完全收口留 T672）；
//   - alreadyProcessed → 已办结告警走原幂等直通，不回 409。
//
// 失败口径与 ProcessGuard 一致：非 200 / 坏响应 / 不可达一律外抛 error，
// handler 回 503 fail-closed——依赖故障不得把有绑定的告警放行回旧快处直改。
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

// 快处路由结论的 route 取值（与 user-service quickRouteResponse.route 同词表）。
const (
	RouteDirect = "direct"
	RouteFlow   = "flow"
)

// RouteDecision 快处路由结论。
type RouteDecision struct {
	Route            string // RouteDirect / RouteFlow
	InstanceID       string // Route=flow 时的流程实例 id（转流程跳转用）
	AlreadyProcessed bool   // 告警已 processed（调用方走幂等直通）
}

// Router 快处路由契约（alert handler 注入点；测试可换 fake）。
type Router interface {
	QuickRoute(ctx context.Context, alertID int64) (RouteDecision, error)
}

// NoopRouter 未配置 user-service 地址时的占位：一律 direct（= 快处保持现状，
// 与 NoopGuard「无流程服务按 staff 现状放行」同口径）。
type NoopRouter struct{}

// QuickRoute 空实现 = 不转流程。
func (NoopRouter) QuickRoute(context.Context, int64) (RouteDecision, error) {
	return RouteDecision{Route: RouteDirect}, nil
}

type quickRouteRequest struct {
	AlertID int64 `json:"alertId"`
}

type quickRouteResponse struct {
	Code int `json:"code"`
	Data struct {
		Route            string `json:"route"`
		InstanceID       string `json:"instanceId"`
		AlreadyProcessed bool   `json:"alreadyProcessed"`
	} `json:"data"`
}

// RouteHTTPClient Router 的 HTTP 实现（与 ProcessGuardHTTPClient 同范式）。
type RouteHTTPClient struct {
	baseURL string
	client  *http.Client
}

// NewRouteClient 创建路由客户端；timeout<=0 用 DefaultTimeout。
func NewRouteClient(baseURL string, timeout time.Duration) *RouteHTTPClient {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &RouteHTTPClient{baseURL: baseURL, client: &http.Client{Timeout: timeout}}
}

// QuickRoute POST {baseURL}/internal/flow/quick-route。任何依赖侧异常都返回 error
// （fail-closed，调用方回 503）；HTTP 200 + code=0 才采信结论。
func (c *RouteHTTPClient) QuickRoute(ctx context.Context, alertID int64) (RouteDecision, error) {
	if c == nil || c.baseURL == "" {
		return RouteDecision{Route: RouteDirect}, nil
	}
	body, err := json.Marshal(quickRouteRequest{AlertID: alertID})
	if err != nil {
		return RouteDecision{}, fmt.Errorf("marshal quick-route request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/internal/flow/quick-route", bytes.NewReader(body))
	if err != nil {
		return RouteDecision{}, fmt.Errorf("build quick-route request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return RouteDecision{}, fmt.Errorf("call quick-route: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	if err != nil {
		return RouteDecision{}, fmt.Errorf("read quick-route response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return RouteDecision{}, fmt.Errorf("quick-route non-200: status=%d", resp.StatusCode)
	}
	var parsed quickRouteResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return RouteDecision{}, fmt.Errorf("parse quick-route response: %w", err)
	}
	if parsed.Code != 0 {
		return RouteDecision{}, fmt.Errorf("quick-route business code=%d", parsed.Code)
	}
	switch parsed.Data.Route {
	case RouteDirect, RouteFlow:
	default:
		return RouteDecision{}, fmt.Errorf("quick-route unknown route=%q", parsed.Data.Route)
	}
	return RouteDecision{
		Route:            parsed.Data.Route,
		InstanceID:       parsed.Data.InstanceID,
		AlreadyProcessed: parsed.Data.AlreadyProcessed,
	}, nil
}
