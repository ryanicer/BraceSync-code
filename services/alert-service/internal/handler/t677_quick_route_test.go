// 677 快处路由门测试（POST /alerts/:id/process）：
//   - route=flow 未办结 → 409 codeFlowRouteConflict，不落 ProcessAlert（强制转流程）
//   - route=flow 但 alreadyProcessed → 放行幂等直通（T028 契约不破）
//   - route=direct → 保持旧快处直改现状（无绑定残留）
//   - 依赖故障 → 503 fail-closed；默认 NoopRouter = 不注入时旧行为不变
//   - 路由门先于 T668 节点执法（flow 拒绝时守卫不被打到）
package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/flowstarter"
	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

// t677FakeRouter 快处路由 fake（模拟 user-service 三态 + 依赖故障）。
type t677FakeRouter struct {
	d   flowstarter.RouteDecision
	err error
	// 记录调用与次序（验「路由门先于节点执法」）
	hits       int
	lastAlert  int64
	guardAfter bool // 路由返回后守卫是否被打到（由测试在守卫里置位）
}

func (r *t677FakeRouter) QuickRoute(_ context.Context, alertID int64) (flowstarter.RouteDecision, error) {
	r.hits++
	r.lastAlert = alertID
	return r.d, r.err
}

// t677Do 与 t668Do 同形（无 body 快处请求）。
func t677Do(h *Handler, path, role string) *httptest.ResponseRecorder {
	return t668Do(h, path, role)
}

// 有绑定类型（route=flow）→ 409 转流程，且不落 ProcessAlert、不打节点执法。
func TestT677_QuickRoute_FlowRefuses409(t *testing.T) {
	store := &fakePublicStore{exists: true}
	r := &t677FakeRouter{d: flowstarter.RouteDecision{Route: flowstarter.RouteFlow, InstanceID: "FLOW_I1"}}
	g := &t668FakeGuard{d: flowstarter.Decision{Enforced: false, Allowed: true}}
	h := newPublicHandler(store)
	h.SetQuickRouter(r)
	h.SetProcessGuard(g)

	rec := t677Do(h, "/api/v1/alerts/42/process", "ROLE_CS")
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, codeFlowRouteConflict, mustCode(t, rec), "必须是转流程专用码，不是「已办结」409")
	assert.Zero(t, store.procHits, "转流程拒绝不得落 ProcessAlert（旧快处直改被堵）")
	assert.Equal(t, 1, r.hits)
	assert.EqualValues(t, 42, r.lastAlert)
}

// 路由门先于节点执法：flow 拒绝时守卫一次都没被调用。
func TestT677_QuickRoute_FlowRefused_GuardNotConsulted(t *testing.T) {
	store := &fakePublicStore{exists: true}
	r := &t677FakeRouter{d: flowstarter.RouteDecision{Route: flowstarter.RouteFlow, InstanceID: "FLOW_I1"}}
	g := &t668FakeGuard{d: flowstarter.Decision{Enforced: true, Allowed: false, Reason: "节点无权"}}
	h := newPublicHandler(store)
	h.SetQuickRouter(r)
	h.SetProcessGuard(g)

	rec := t677Do(h, "/api/v1/alerts/42/process", "ROLE_CS")
	require.Equal(t, http.StatusConflict, rec.Code, "409 转流程优先于 403 节点执法")
	assert.EqualValues(t, int64(0), g.lastAlertID, "守卫不得被打到")
	assert.Zero(t, store.procHits)
}

// route=flow 但告警已 processed → 幂等直通放行（T028 契约不破）。
func TestT677_QuickRoute_FlowButAlreadyProcessed_Proceeds(t *testing.T) {
	store := &fakePublicStore{exists: true}
	r := &t677FakeRouter{d: flowstarter.RouteDecision{
		Route: flowstarter.RouteFlow, InstanceID: "FLOW_I1", AlreadyProcessed: true,
	}}
	h := newPublicHandler(store)
	h.SetQuickRouter(r)

	rec := t677Do(h, "/api/v1/alerts/42/process", "ROLE_CS")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, store.procHits, "已 processed 快处重放保持幂等 200")
}

// route=direct（无绑定/不可绑定类型残留）→ 旧快处直改保持现状。
func TestT677_QuickRoute_Direct_KeepsLegacyBehavior(t *testing.T) {
	store := &fakePublicStore{exists: true}
	r := &t677FakeRouter{d: flowstarter.RouteDecision{Route: flowstarter.RouteDirect}}
	h := newPublicHandler(store)
	h.SetQuickRouter(r)

	rec := t677Do(h, "/api/v1/alerts/42/process", "ROLE_CS")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, store.procHits)
}

// 路由依赖故障 → 503 fail-closed，不落处理（不许旁路回旧快处）。
func TestT677_QuickRoute_Error503(t *testing.T) {
	store := &fakePublicStore{exists: true}
	r := &t677FakeRouter{err: errors.New("user-service down")}
	h := newPublicHandler(store)
	h.SetQuickRouter(r)

	rec := t677Do(h, "/api/v1/alerts/42/process", "ROLE_CS")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assert.Zero(t, store.procHits, "依赖故障 fail-closed")
}

// 未知 route 值由客户端层拒收（此处模拟 handler 收到合法值之外的兜底不可达，略）——
// 默认不注入 = NoopRouter → direct，存量行为不变（保护既有测试基线）。
func TestT677_DefaultNoopRouter_LegacyStillProcesses(t *testing.T) {
	store := &fakePublicStore{exists: true}
	h := newPublicHandler(store) // 不 SetQuickRouter
	rec := t677Do(h, "/api/v1/alerts/42/process", "ROLE_CS")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, store.procHits)
}

// processing（开始处理）端点不走快处路由门（677 最小闭环只收 process；T668 守卫仍在）。
func TestT677_StartProcessing_NotGatedByQuickRoute(t *testing.T) {
	store := &fakePublicStore{exists: true, state: repo.ProcessState{Status: "processing", Exists: true}}
	r := &t677FakeRouter{d: flowstarter.RouteDecision{Route: flowstarter.RouteFlow, InstanceID: "FLOW_I1"}}
	h := newPublicHandler(store)
	h.SetQuickRouter(r)

	rec := t677Do(h, "/api/v1/alerts/42/processing", "ROLE_CS")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Zero(t, r.hits, "processing 不打路由门（scope=旧快处 process）")
	assert.Equal(t, 1, store.startHits)
}
