// Package main — T477 POST /admin/patients/:patientId/password 的网关侧授权登记。
//
// 这条端点「能给他人设登录口令」= 能登他人账号，危害与同族的「改手机号」（T190 收口过）
// 同级甚至更高，因此收口 admin-only：非 ROLE_ADMIN 在入口 403，且不触达 user-service。
//
// 与 T467 不同，这里没有「医生的团队谓词跑不到」的顾虑：handler 侧只判 admin，
// 不做团队推导（设密不属于本团队医护的日常操作，PRD 未给这条通道）。
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

const t477PasswordPath = "/api/v1/admin/patients/P20260001/password"

// TestRBAC_T477_SetPasswordLandsInAdminMatrix 矩阵归属对账：命中 admin-only 且不进其余三张。
// 四张矩阵都可能放行 admin，只断「命中 adminOnly」的话，挪去别处也照样绿。
func TestRBAC_T477_SetPasswordLandsInAdminMatrix(t *testing.T) {
	require.True(t, matchRBACPattern(http.MethodPost, t477PasswordPath),
		"设密端点应命中 admin-only 矩阵（不登记则 default-allow 放行任意角色）")
	assert.False(t, matchStaffOnlyPattern(http.MethodPost, t477PasswordPath), "不得进 staff-only 矩阵")
	assert.False(t, matchDoctorAdminPattern(http.MethodPost, t477PasswordPath), "不得进 doctor+admin 矩阵")
	assert.False(t, matchTechAdminPattern(http.MethodPost, t477PasswordPath), "不得进 tech+admin 矩阵")

	// 同路径的其余方法不许被这条登记连带放行；同族的改手机号仍是 admin-only（未被本笔挪动）
	assert.False(t, matchRBACPattern(http.MethodPut, t477PasswordPath), "PUT 同路径不得被误命中")
	assert.False(t, matchRBACPattern(http.MethodGet, t477PasswordPath), "GET 同路径不得被误命中")
	assert.True(t, matchRBACPattern(http.MethodPut, "/api/v1/admin/patients/P20260001/phone"),
		"改手机号仍是 admin-only")
}

// TestRBAC_T477_SetPasswordRouteIsProxied 反代表登记：user-service 侧测试再绿，
// 网关没这条路由也只会 404，CI 拿不到口令。
func TestRBAC_T477_SetPasswordRouteIsProxied(t *testing.T) {
	var found bool
	for _, rt := range userServiceRoutes {
		if rt.method == http.MethodPost && rt.path == "/admin/patients/:patientId/password" {
			found = true
		}
	}
	assert.True(t, found, "proxy_admin.go 患者域必须登记 POST /admin/patients/:patientId/password")
}

// TestRBAC_T477_OnlyAdminForwarded admin 放行到后端；其余角色 403 且零转发。
func TestRBAC_T477_OnlyAdminForwarded(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleDoctor, roleCS, roleTech, rolePatient, "", "ROLE_GHOST"} {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+t477PasswordPath, "", rbacToken(t, role))
		assert.Equal(t, http.StatusForbidden, code, "role=%q 设密应 403", role)
		assert.Contains(t, body, `"code":403`)
	}
	assert.Empty(t, *received, "被拒角色的设密请求不得触达后端")

	code, body := httpDoFull(t, http.MethodPost, gw.URL+t477PasswordPath, "", rbacToken(t, roleAdmin))
	assert.Equal(t, http.StatusOK, code, "admin 设密应放行到后端，body=%s", body)
	assert.Len(t, *received, 1, "admin 那一发应转发后端")
}

// TestRBAC_T477_CannotEscalateViaForgedHeaders 患者 token 伪造 X-Role/X-User-Id 仍 403
// （身份头由 jwtAuth 从 JWT 重注入），也不触达后端。
func TestRBAC_T477_CannotEscalateViaForgedHeaders(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	tok := signTestJWT(t, testJWTSecretMain, "P20260002", rolePatient, time.Now().Add(time.Hour).Unix())
	h := map[string]string{
		"Authorization": "Bearer " + tok,
		"X-Role":        roleAdmin,
		"X-User-Id":     "P20260001",
	}
	code, _ := httpDoFull(t, http.MethodPost, gw.URL+t477PasswordPath, "", h)
	assert.Equal(t, http.StatusForbidden, code, "伪造 X-Role 后设密仍应 403")
	assert.Empty(t, *received, "伪造身份的设密请求不得触达后端")
}
