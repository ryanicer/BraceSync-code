package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/flowstarter"
)

// t668FakeGuard 节点执法守卫 fake（模拟 user-service 三态 + 依赖故障）。
type t668FakeGuard struct {
	d   flowstarter.Decision
	err error
	// 记录最后一问（验透传 alertId/operator/role）
	lastAlertID int64
	lastOp      string
	lastRole    string
}

func (g *t668FakeGuard) CheckProcess(_ context.Context, alertID int64, operatorID, role string) (flowstarter.Decision, error) {
	g.lastAlertID, g.lastOp, g.lastRole = alertID, operatorID, role
	return g.d, g.err
}

func t668GuardedHandler(g flowstarter.Guard) *Handler {
	h := newPublicHandler(&fakePublicStore{exists: true})
	h.SetProcessGuard(g)
	return h
}

func t668Do(h *Handler, path, role string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set(headerUserID, "D0001")
	if role != "" {
		req.Header.Set(headerRole, role)
	}
	h.Router().ServeHTTP(rec, req)
	return rec
}

// 绑定流程但当前节点无权 → 403，且不落 ProcessAlert（旁路被堵）。
func TestT668_ProcessAlert_NodeGuardDenied403(t *testing.T) {
	store := &fakePublicStore{exists: true}
	g := &t668FakeGuard{d: flowstarter.Decision{Enforced: true, Allowed: false, Reason: "无权"}}
	h := newPublicHandler(store)
	h.SetProcessGuard(g)

	rec := t668Do(h, "/api/v1/alerts/42/process", "ROLE_CS")
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Zero(t, store.procHits, "执法拒绝不得落 ProcessAlert")
	assert.Equal(t, codeForbidden, mustCode(t, rec))
	assert.EqualValues(t, 42, g.lastAlertID)
	assert.Equal(t, "D0001", g.lastOp)
	assert.Equal(t, "ROLE_CS", g.lastRole, "角色头透传给守卫")
}

// 绑定流程且放行 → 正常处理。
func TestT668_ProcessAlert_NodeGuardAllowed(t *testing.T) {
	store := &fakePublicStore{exists: true}
	g := &t668FakeGuard{d: flowstarter.Decision{Enforced: true, Allowed: true}}
	h := newPublicHandler(store)
	h.SetProcessGuard(g)

	rec := t668Do(h, "/api/v1/alerts/42/process", "ROLE_DOCTOR")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, store.procHits)
}

// 无流程实例（不执法）→ staff 现状放行。
func TestT668_ProcessAlert_NotEnforcedAllowed(t *testing.T) {
	store := &fakePublicStore{exists: true}
	g := &t668FakeGuard{d: flowstarter.Decision{Enforced: false, Allowed: true}}
	h := newPublicHandler(store)
	h.SetProcessGuard(g)

	rec := t668Do(h, "/api/v1/alerts/42/process", "technician")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// 守卫依赖故障 → 503 fail-closed，不落处理。
func TestT668_ProcessAlert_GuardError503(t *testing.T) {
	store := &fakePublicStore{exists: true}
	g := &t668FakeGuard{err: errors.New("user-service down")}
	h := newPublicHandler(store)
	h.SetProcessGuard(g)

	rec := t668Do(h, "/api/v1/alerts/42/process", "ROLE_DOCTOR")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assert.Zero(t, store.procHits, "依赖故障 fail-closed，不得旁路落处理")
}

// startProcessing 同口径收执法：无权 403 且不推进状态。
func TestT668_StartProcessing_NodeGuardDenied403(t *testing.T) {
	store := &fakePublicStore{exists: true}
	g := &t668FakeGuard{d: flowstarter.Decision{Enforced: true, Allowed: false}}
	h := newPublicHandler(store)
	h.SetProcessGuard(g)

	rec := t668Do(h, "/api/v1/alerts/42/processing", "ROLE_CS")
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, 0, store.startHits)
}

func TestT668_StartProcessing_GuardAllowed(t *testing.T) {
	store := &fakePublicStore{exists: true}
	g := &t668FakeGuard{d: flowstarter.Decision{Enforced: true, Allowed: true}}
	h := newPublicHandler(store)
	h.SetProcessGuard(g)

	rec := t668Do(h, "/api/v1/alerts/42/processing", "ROLE_DOCTOR")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, store.startHits)
}

// 默认（不注入守卫）= NoopGuard，旧行为不变（保护存量调用方与既有测试基线）。
func TestT668_DefaultNoopGuard_StaffStillProcesses(t *testing.T) {
	store := &fakePublicStore{exists: true}
	h := newPublicHandler(store) // 不 SetProcessGuard
	rec := t668Do(h, "/api/v1/alerts/42/process", "ROLE_CS")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, store.procHits)
}

func mustCode(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return env.Code
}
