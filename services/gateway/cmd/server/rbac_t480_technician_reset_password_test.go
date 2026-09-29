// Package main — T480 POST /admin/technicians/:techId/reset-password 的网关侧授权登记。
//
// 这条端点「能给他人设登录口令」= 能登他人账号，与同族的 doctor reset-password（T314）、
// 患者设密（T477）同级，因此收口 admin-only：非 ROLE_ADMIN 在入口 403，且不触达 user-service。
// 技师自己也不能改自己的口令（PRD 未给这条自助通道）⇒ 同族 admin 写端点同口径。
//
// 全部经 startFullGateway（真实 setupRouter 链：jwtAuth → roleAuthz → 反代）。
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t480ResetPath = "/api/v1/admin/technicians/TECH0001/reset-password"

// TestRBAC_T480_ResetPasswordLandsInAdminMatrix 矩阵归属对账：命中 admin-only 且不进其余三张。
// 四张矩阵都可能放行 admin，只断「命中 adminOnly」的话，挪去别处也照样绿。
func TestRBAC_T480_ResetPasswordLandsInAdminMatrix(t *testing.T) {
	require.True(t, matchRBACPattern(http.MethodPost, t480ResetPath),
		"重置口令端点应命中 admin-only 矩阵（不登记则 default-allow 放行任意角色）")
	assert.False(t, matchStaffOnlyPattern(http.MethodPost, t480ResetPath), "不得进 staff-only 矩阵")
	assert.False(t, matchDoctorAdminPattern(http.MethodPost, t480ResetPath), "不得进 doctor+admin 矩阵")
	assert.False(t, matchTechAdminPattern(http.MethodPost, t480ResetPath), "不得进 tech+admin 矩阵：技师不得自助改口令")

	// 同路径的其余方法不许被这条登记连带放行；同族的启停/编辑仍各在其位
	assert.False(t, matchRBACPattern(http.MethodPut, t480ResetPath), "PUT 同路径不得被误命中")
	assert.False(t, matchRBACPattern(http.MethodGet, t480ResetPath), "GET 同路径不得被误命中")
	assert.True(t, matchRBACPattern(http.MethodPost, "/api/v1/admin/technicians"), "新建技师仍是 admin-only")
	assert.True(t, matchRBACPattern(http.MethodPost, "/api/v1/technicians/TECH0001/toggle"),
		"启停技师仍是 admin-only")
}

// TestRBAC_T480_ResetPasswordRouteIsProxied 反代表登记：user-service 侧测试再绿，
// 网关没这条路由也只会 404，管理员拿不到新口令。
func TestRBAC_T480_ResetPasswordRouteIsProxied(t *testing.T) {
	var found bool
	for _, rt := range userServiceRoutes {
		if rt.method == http.MethodPost && rt.path == "/admin/technicians/:techId/reset-password" {
			found = true
		}
	}
	assert.True(t, found, "proxy_admin.go 技师域必须登记 POST /admin/technicians/:techId/reset-password")
}

// TestRBAC_T480_OnlyAdminForwarded admin 放行到后端；其余角色 403 且零转发。
func TestRBAC_T480_OnlyAdminForwarded(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleDoctor, roleCS, roleTech, rolePatient, "", "ROLE_GHOST"} {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+t480ResetPath, "", rbacToken(t, role))
		assert.Equal(t, http.StatusForbidden, code, "role=%q 重置口令应 403", role)
		assert.Contains(t, body, `"code":403`)
	}
	assert.Empty(t, *received, "被拒角色的重置请求不得触达后端")

	code, body := httpDoFull(t, http.MethodPost, gw.URL+t480ResetPath, "", rbacToken(t, roleAdmin))
	assert.Equal(t, http.StatusOK, code, "admin 重置口令应放行到后端，body=%s", body)
	assert.Len(t, *received, 1, "admin 那一发应转发后端")
}

// TestRBAC_T480_CannotEscalateViaForgedHeaders 技师 token 伪造 X-Role/X-User-Id 仍 403
// （身份头由 jwtAuth 从 JWT 重注入），也不触达后端。
func TestRBAC_T480_CannotEscalateViaForgedHeaders(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	tok := signTestJWT(t, testJWTSecretMain, "TECH0002", roleTech, time.Now().Add(time.Hour).Unix())
	h := map[string]string{
		"Authorization": "Bearer " + tok,
		"X-Role":        roleAdmin,
		"X-User-Id":     "TECH0001",
	}
	code, _ := httpDoFull(t, http.MethodPost, gw.URL+t480ResetPath, "", h)
	assert.Equal(t, http.StatusForbidden, code, "伪造 X-Role 后重置口令仍应 403")
	assert.Empty(t, *received, "伪造身份的重置请求不得触达后端")
}
