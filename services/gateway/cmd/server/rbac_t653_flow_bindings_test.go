// Package main — T653 告警类型 ↔ 流程模板绑定的网关侧登记验证。
//
// 口径：GET/PUT /api/v1/admin/flow/type-bindings 决定「未来告警是否自动起流程」，
// 属配置变更面，与 alert-rules 同级，GET/PUT 均收口 admin-only（医护不看绑定区）。
// 服务间端点 POST /internal/flow/auto-start 不挂网关、不进 RBAC、不鉴 JWT。
package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var t653FlowBindingRoutes = []flowRoute{
	{http.MethodGet, "/api/v1/admin/flow/type-bindings"},
	{http.MethodPut, "/api/v1/admin/flow/type-bindings"},
}

func TestRBAC_T653_FlowTypeBindingsLandInAdminOnly(t *testing.T) {
	for _, c := range t653FlowBindingRoutes {
		assert.True(t, matchRBACPattern(c.method, c.path),
			"绑定路由应命中 admin-only：%s %s", c.method, c.path)
		assert.False(t, matchStaffOnlyPattern(c.method, c.path),
			"绑定路由不得落 staff-only（绑定区仅 admin）：%s %s", c.method, c.path)
	}
}

func TestRBAC_T653_FlowTypeBindingsAdminOnlyEndToEnd(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleDoctor, roleCS, roleTech, rolePatient, "", "ROLE_GHOST"} {
		for _, c := range t653FlowBindingRoutes {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%q %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}
	assert.Empty(t, *received, "非 admin 的绑定读/写不得触达后端")

	for _, c := range t653FlowBindingRoutes {
		code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, roleAdmin))
		assert.Equal(t, http.StatusOK, code, "admin %s %s 不应被误伤，body=%s", c.method, c.path, body)
	}
	assert.Len(t, *received, len(t653FlowBindingRoutes), "admin 两条绑定路由应全部转发后端")
}

// TestRBAC_T653_InternalAutoStartNotRegistered 服务间端点不得经网关暴露：
// 反代白名单没有它，RBAC 矩阵也没有它（默认拒绝），只允许 docker 内网直连 user-service。
func TestRBAC_T653_InternalAutoStartNotRegistered(t *testing.T) {
	for _, r := range userServiceRoutes {
		assert.NotEqual(t, "/internal/flow/auto-start", r.path,
			"服务间端点不得登记进 userServiceRoutes：%s %s", r.method, r.path)
	}
	for _, r := range append(append([]rbacPattern{}, adminOnlyPatterns...), staffOnlyPatterns...) {
		assert.NotContains(t, strings.Join(r.segments, "/"), "/internal/flow/auto-start", "内部端点不得进任何 RBAC 矩阵")
	}

	// 端到端：经网关打内部路径必须被默认拒绝（404 无此反代路由），不得触达后端
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)
	code, _ := httpDoFull(t, http.MethodPost, gw.URL+"/internal/flow/auto-start",
		`{"alertId":"1","alertType":"pressure_high"}`, rbacToken(t, roleAdmin))
	assert.NotEqual(t, http.StatusOK, code, "内部端点不得经网关放行")
	assert.Empty(t, *received, "被网关拦下的内部调用不得触达后端")
}
