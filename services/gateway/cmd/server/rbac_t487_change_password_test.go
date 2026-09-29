// Package main — T487 POST /api/v1/auth/change-password 的网关侧授权登记。
//
// 这条端点「改的是调用者自己的口令」，所以矩阵归属和同族的三条代改通道（T314 医护 /
// T480 技师 / T477 患者，全部 admin-only）相反：它必须是 staff 可用、患者不可用。
//
// 🔴 两处漏登记都会静默坏掉，且坏法不同，故分别钉：
//   - userServiceRoutes 漏 ⇒ 网关 404「改不了密码」，而 RBAC 测试全绿；
//   - staffOnlyPatterns 漏 ⇒ T260 默认拒绝，连 admin 都 403（路由比对脚本查不到这个）。
//
// 技师 token 在网关是放行的（staffRoles 含 technician），到服务层因 admins 无技师行回 404 ——
// 那是 user-service 的职责（见 change_password_t487_unit_test.go 的同名格子），本文件只证网关这一跳。
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t487ChangePwdPath = "/api/v1/auth/change-password"

// TestRBAC_T487_ChangePasswordInStaffMatrix 矩阵归属对账：命中 staff-only，且不落进另外三张。
func TestRBAC_T487_ChangePasswordInStaffMatrix(t *testing.T) {
	require.True(t, matchStaffOnlyPattern(http.MethodPost, t487ChangePwdPath),
		"自助改密必须登记进 staffOnlyPatterns，否则 roleAuthz 默认 403（连 admin 都改不了）")
	assert.False(t, matchRBACPattern(http.MethodPost, t487ChangePwdPath),
		"不得是 admin-only：医护/客服同样要能改自己口令（本卡判据「管理员与医护均可自助改密」）")
	assert.False(t, matchDoctorAdminPattern(http.MethodPost, t487ChangePwdPath), "不得进 doctor+admin 矩阵")
	assert.False(t, matchTechAdminPattern(http.MethodPost, t487ChangePwdPath), "不得进 tech+admin 矩阵")
	assert.False(t, matchPublicPattern(http.MethodPost, t487ChangePwdPath),
		"不得进 public 矩阵：public 会放行患者 token，而患者域改密不走这条端点")

	// 免 JWT 白名单不许被扩到这条：否则未登录也能改密（凭 X-User-Id 伪造身份）
	require.False(t, authWhitelisted(http.MethodPost, "/api/v1"+t487ChangePwdPath),
		"自助改密必须带 JWT（白名单只留四条登录入口）")
	assert.True(t, authWhitelisted(http.MethodPost, "/api/v1/auth/login"), "正对照：登录入口仍在白名单内")

	// 同路径其他方法不被连带放行
	assert.False(t, matchStaffOnlyPattern(http.MethodGet, t487ChangePwdPath), "GET 同路径不得命中")
	assert.False(t, matchStaffOnlyPattern(http.MethodPut, t487ChangePwdPath), "PUT 同路径不得命中")
}

// TestRBAC_T487_ChangePasswordRouteIsProxied 反代表登记：RBAC 全绿但网关没这条路由 ⇒ 前端 404。
// 注意 proxy_admin.go 的路由表按「/api/v1 之后」的相对路径登记，与 RBAC 矩阵的全路径不同形。
func TestRBAC_T487_ChangePasswordRouteIsProxied(t *testing.T) {
	var found bool
	for _, rt := range userServiceRoutes {
		if rt.method == http.MethodPost && rt.path == "/auth/change-password" {
			found = true
		}
	}
	assert.True(t, found, "proxy_admin.go 必须登记 POST /auth/change-password（RBAC 全路径 %s）", t487ChangePwdPath)
}

// TestRBAC_T487_StaffForwardedPatientDenied 逐角色实测：四种后台角色转发，患者与未知角色 403。
func TestRBAC_T487_StaffForwardedPatientDenied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleAdmin, roleDoctor, roleCS, roleTech} {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+t487ChangePwdPath, `{}`, rbacToken(t, role))
		assert.NotEqual(t, http.StatusForbidden, code, "role=%q 自助改密不该被网关挡，body=%s", role, body)
		assert.NotEqual(t, http.StatusNotFound, code, "role=%q 不该 404（路由未登记的特征），body=%s", role, body)
	}
	assert.Len(t, *received, 4, "四种后台角色都应转发到后端")

	for _, role := range []string{rolePatient, "", "ROLE_GHOST"} {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+t487ChangePwdPath, `{}`, rbacToken(t, role))
		assert.Equal(t, http.StatusForbidden, code, "role=%q 自助改密应 403", role)
		assert.Contains(t, body, `"code":403`)
	}
	assert.Len(t, *received, 4, "被拒角色不得触达后端")
}

// TestRBAC_T487_ForgedIdentityHeaderRejected 无 token / 伪造 X-User-Id：
// 改密只认网关注入的身份，外部同名头必须被剥掉（否则拿别人 ID 改别人密码）。
func TestRBAC_T487_ForgedIdentityHeaderRejected(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	// ① 完全没有 Authorization
	code, _ := httpDoFull(t, http.MethodPost, gw.URL+t487ChangePwdPath, `{}`,
		map[string]string{"X-User-Id": "A0007", "X-Role": roleAdmin})
	assert.Equal(t, http.StatusUnauthorized, code, "匿名带伪造身份头应 401")

	// ② 患者 token 伪造 X-User-Id 指向管理员
	tok := signTestJWT(t, testJWTSecretMain, "P20260002", rolePatient, time.Now().Add(time.Hour).Unix())
	code, _ = httpDoFull(t, http.MethodPost, gw.URL+t487ChangePwdPath, `{}`, map[string]string{
		"Authorization": "Bearer " + tok,
		"X-User-Id":     "A0007",
		"X-Role":        roleAdmin,
	})
	assert.Equal(t, http.StatusForbidden, code, "患者伪造身份头改密应 403")

	// ③ 医护 token 正常请求：后端收到的 X-User-Id 必须是 JWT 里那个，不是请求头里塞的
	adminTok := signTestJWT(t, testJWTSecretMain, "A0007", roleDoctor, time.Now().Add(time.Hour).Unix())
	code, body := httpDoFull(t, http.MethodPost, gw.URL+t487ChangePwdPath, `{}`, map[string]string{
		"Authorization": "Bearer " + adminTok,
		"X-User-Id":     "A9999", // 试图冒充别的账号
	})
	require.Equal(t, http.StatusOK, code, "body=%s", body)
	require.Len(t, *received, 1, "前两发（匿名/患者伪造）不得触达后端")
	assert.Contains(t, (*received)[0], "uid=A0007", "🔴 落到后端的身份必须是 JWT 主语，不是请求头自报值")
	assert.NotContains(t, (*received)[0], "uid=A9999", "自报的 A9999 必须被网关剥掉")
}
