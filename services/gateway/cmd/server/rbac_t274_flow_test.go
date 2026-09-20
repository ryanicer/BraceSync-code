// Package main — T274 流程画布端点的网关侧登记验证。
//
// 派发单口径：「网关是默认拒绝策略，新端点不登记就 403」。本文件把 10 条 flow 路由拆成
// 两组断言方向：
//   - 模板 5 条（配置级，同 sys_config / alert_rules 一档）→ 仅 ROLE_ADMIN；
//   - 实例 5 条（运行态处置，同 POST /alerts/:id/process 一档）→ staff 放行、patient 拒。
//
// 走真实 setupRouter 链（jwtAuth → roleAuthz → 反代），并核对「未登记差集」由
// TestRBAC_T190_AllAdminRoutesAreGated 全表兜底。
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var t274FlowAdminRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/v1/admin/flow/templates"},
	{http.MethodPost, "/api/v1/admin/flow/templates"},
	{http.MethodGet, "/api/v1/admin/flow/templates/FLOW_T0A1B2C3D4"},
	{http.MethodPut, "/api/v1/admin/flow/templates/FLOW_T0A1B2C3D4"},
	{http.MethodDelete, "/api/v1/admin/flow/templates/FLOW_T0A1B2C3D4"},
}

var t274FlowStaffRoutes = []struct{ method, path string }{
	{http.MethodPost, "/api/v1/admin/flow/instances"},
	{http.MethodGet, "/api/v1/admin/flow/instances"},
	{http.MethodGet, "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/nodes"},
	{http.MethodPost, "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/nodes/N1/actions"},
	{http.MethodGet, "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/actions"},
}

func TestRBAC_T274_FlowRoutesLandInIntendedMatrix(t *testing.T) {
	for _, c := range t274FlowAdminRoutes {
		assert.True(t, matchRBACPattern(c.method, c.path), "模板路由应命中 admin-only：%s %s", c.method, c.path)
		assert.False(t, matchStaffOnlyPattern(c.method, c.path),
			"模板路由不得同时命中 staff-only（会放开给患者以外的全部 staff）：%s %s", c.method, c.path)
	}
	for _, c := range t274FlowStaffRoutes {
		assert.True(t, matchStaffOnlyPattern(c.method, c.path), "实例路由应命中 staff-only：%s %s", c.method, c.path)
		assert.False(t, matchRBACPattern(c.method, c.path),
			"实例路由不该锁死在 admin（医生要在 2.3 上点确认处理）：%s %s", c.method, c.path)
	}
}

func TestRBAC_T274_FlowTemplatesAdminOnly(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleDoctor, roleCS, roleTech, rolePatient, "", "ROLE_GHOST"} {
		for _, c := range t274FlowAdminRoutes {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%q %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}
	assert.Empty(t, *received, "非 admin 的模板读写不得触达后端")

	for _, c := range t274FlowAdminRoutes {
		code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, roleAdmin))
		assert.Equal(t, http.StatusOK, code, "admin %s %s 不应被误伤，body=%s", c.method, c.path, body)
	}
	assert.Len(t, *received, len(t274FlowAdminRoutes), "admin 请求应全部转发后端")
}

func TestRBAC_T274_FlowInstancesStaffAllowedPatientDenied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleAdmin, roleDoctor, roleCS, roleTech} {
		for _, c := range t274FlowStaffRoutes {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusOK, code, "role=%s %s %s 应放行，body=%s", role, c.method, c.path, body)
		}
	}
	assert.Len(t, *received, len(t274FlowStaffRoutes)*4, "staff 4 角色 × 5 条实例路由应全部转发后端")

	for _, role := range []string{rolePatient, "", "ROLE_GHOST"} {
		for _, c := range t274FlowStaffRoutes {
			code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%q %s %s 应 403（fail-closed）", role, c.method, c.path)
		}
	}
	assert.Len(t, *received, len(t274FlowStaffRoutes)*4, "患者/未知角色的实例操作不得新增触达后端")
}

func TestRBAC_T274_ForgedRoleCannotEscalateFlowTemplates(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	tok := signTestJWT(t, testJWTSecretMain, "P20260002", rolePatient, time.Now().Add(time.Hour).Unix())
	for _, c := range t274FlowAdminRoutes {
		h := map[string]string{
			"Authorization": "Bearer " + tok,
			"X-Role":        roleAdmin,
			"X-User-Id":     "P20260001",
		}
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, h)
		assert.Equal(t, http.StatusForbidden, code, "患者 token 伪造 X-Role 后 %s %s 仍应 403", c.method, c.path)
	}
	assert.Empty(t, *received, "伪造身份的 flow 请求不得触达后端")
}
