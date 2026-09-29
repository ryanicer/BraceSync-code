// Package main — T486 POST /tech/change-password 的网关侧授权登记。
//
// 技师自助改密改的是「自己那一行」（身份只取 jwtAuth 从 JWT 注入的 X-User-Id，
// 请求体里没有 techId），但入口仍要按角色收口：医护/客服/患者/未知角色 token 打进来
// 一律 403 且不触达 user-service。放行面 = tech+admin 矩阵（admin 在 roleAuthz 入口
// 已早退，仍会进后端，由 user-service 的 technician 判定挡回 —— 双层，同 T480 口径）。
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

const t486ChangePath = "/api/v1/tech/change-password"

// TestRBAC_T486_SelfServiceLandsInTechAdminMatrix 矩阵归属对账：命中 tech+admin 且不进其余三张。
// 挪去 admin-only 会让技师自己改不了密码（这条通道就是给技师的），挪去 staff-only 会把
// 医护放进改密入口，都不许绿。
func TestRBAC_T486_SelfServiceLandsInTechAdminMatrix(t *testing.T) {
	require.True(t, matchTechAdminPattern(http.MethodPost, t486ChangePath),
		"自助改密端点应命中 tech+admin 矩阵（不登记则 default-deny 把技师也 403）")
	assert.False(t, matchRBACPattern(http.MethodPost, t486ChangePath), "不得进 admin-only 矩阵")
	assert.False(t, matchStaffOnlyPattern(http.MethodPost, t486ChangePath),
		"不得进 staff-only 矩阵：医护不得有改技师口令的入口")
	assert.False(t, matchDoctorAdminPattern(http.MethodPost, t486ChangePath), "不得进 doctor+admin 矩阵")

	// 同路径其余方法不许被连带放行
	assert.False(t, matchTechAdminPattern(http.MethodPut, t486ChangePath), "PUT 同路径不得被误命中")
	assert.False(t, matchTechAdminPattern(http.MethodGet, t486ChangePath), "GET 同路径不得被误命中")
	// 同族的 admin 重置通道仍在 admin-only，未被这条登记挪动
	assert.True(t, matchRBACPattern(http.MethodPost, "/api/v1/admin/technicians/TECH0001/reset-password"),
		"T480 管理员重置仍是 admin-only")

	// 免鉴权白名单只给登录入口；改密必须带 token，否则等于任人无凭据改口令
	assert.False(t, authWhitelisted(http.MethodPost, t486ChangePath),
		"自助改密不得进 JWT 免鉴权白名单")
	assert.True(t, authWhitelisted(http.MethodPost, "/api/v1/tech/login"), "技师登录仍在白名单（对照组）")
}

// TestRBAC_T486_RouteIsProxied 反代表登记：user-service 侧测试再绿，
// 网关没这条路由也只会 404，技师永远改不了密码。
func TestRBAC_T486_RouteIsProxied(t *testing.T) {
	var found bool
	for _, rt := range userServiceRoutes {
		if rt.method == http.MethodPost && rt.path == "/tech/change-password" {
			found = true
		}
	}
	assert.True(t, found, "proxy_admin.go 技师域必须登记 POST /tech/change-password")
}

// TestRBAC_T486_OnlyTechForwarded 技师放行到后端；其余角色 403 且零转发。
// admin 那一发按上面的双层口径放行到后端（网关不拒），由 user-service 判 403 ——
// 那条断言在 services/user-service 的 T486 handler 测试里。
func TestRBAC_T486_OnlyTechForwarded(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleDoctor, roleCS, rolePatient, "", "ROLE_GHOST"} {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+t486ChangePath, "", rbacToken(t, role))
		assert.Equal(t, http.StatusForbidden, code, "role=%q 自助改密应 403", role)
		assert.Contains(t, body, `"code":403`)
	}
	assert.Empty(t, *received, "被拒角色的改密请求不得触达后端")

	code, body := httpDoFull(t, http.MethodPost, gw.URL+t486ChangePath,
		`{"oldPassword":"oldpw123","newPassword":"newpw456"}`, rbacToken(t, roleTech))
	assert.Equal(t, http.StatusOK, code, "技师自助改密应放行到后端，body=%s", body)
	require.Len(t, *received, 1, "技师那一发应转发后端")
	assert.Contains(t, (*received)[0], "POST /api/v1/tech/change-password")
	assert.Contains(t, (*received)[0], `"newPassword":"newpw456"`, "请求体须原样转发，否则后端收不到新口令")
}

// TestRBAC_T486_TechIdentityComesFromJWT 技师 token 伪造 X-User-Id 也不得改他人口令：
// 身份头由 jwtAuth 剥离后按 claims.sub 重注入（本端点无路径参数，X-User-Id 就是唯一目标）。
func TestRBAC_T486_TechIdentityComesFromJWT(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	tok := signTestJWT(t, testJWTSecretMain, "TECH0002", roleTech, time.Now().Add(time.Hour).Unix())
	h := map[string]string{
		"Authorization": "Bearer " + tok,
		"X-User-Id":     "TECH0001",
		"X-Role":        roleAdmin,
	}
	code, _ := httpDoFull(t, http.MethodPost, gw.URL+t486ChangePath, `{"oldPassword":"a1","newPassword":"b2"}`, h)
	require.Equal(t, http.StatusOK, code, "真技师 token 应放行")
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "uid=TECH0002", "X-User-Id 取 claims.sub，不是外部伪造值")
	assert.Contains(t, (*received)[0], "role=technician")
	assert.NotContains(t, (*received)[0], "TECH0001", "伪造的目标技师号必须被剥离")
	assert.NotContains(t, (*received)[0], "ROLE_ADMIN")
}

// TestRBAC_T486_NoToken401 无 token / 非法 token 一律 401 且零转发
// （派发单「未带 JWT」那一格的网关侧；后端侧的 401 缺身份格在 user-service 测试里）。
func TestRBAC_T486_NoToken401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	cases := map[string]map[string]string{
		"无 Authorization": nil,
		"非法 token":        {"Authorization": "Bearer not.a.jwt"},
		"错误 secret": {"Authorization": "Bearer " + signTestJWT(t, "t486-wrong-secret",
			"TECH0002", roleTech, time.Now().Add(time.Hour).Unix())},
		"过期 token": {"Authorization": "Bearer " + signTestJWT(t, testJWTSecretMain,
			"TECH0002", roleTech, time.Now().Add(-time.Hour).Unix())},
		"有效但无 Bearer 前缀": {"Authorization": signTestJWT(t, testJWTSecretMain,
			"TECH0002", roleTech, time.Now().Add(time.Hour).Unix())},
	}

	for name, hdr := range cases {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+t486ChangePath,
			`{"oldPassword":"a1","newPassword":"b2"}`, hdr)
		assert.Equal(t, http.StatusUnauthorized, code, "%s 应 401", name)
		assert.Contains(t, body, `"code":401`, name)
	}
	assert.Empty(t, *received, "未鉴权的改密请求不得触达后端")
}
