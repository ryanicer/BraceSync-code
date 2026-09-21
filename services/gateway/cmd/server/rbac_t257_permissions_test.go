// Package main — T257 11.5 两个新读端点的网关侧验证。
//
//	GET /api/v1/admin/permissions/catalog  子权限目录 → admin 专属（只有权限分配页要渲染这棵树）
//	GET /api/v1/admin/me/permissions       当前用户有效权限 → 全 staff（每个角色都要据此藏菜单），患者 403
//
// 为什么需要本文件：T260 之后 roleAuthz 是**默认拒绝**——路由只加反代不登记矩阵就是 403，
// 而 check-routes.sh 只比对「服务有、网关没有」的方向，管不到矩阵漏登记。
package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t257MePermsPath     = "/api/v1/admin/me/permissions"
	t257PermCatalogPath = "/api/v1/admin/permissions/catalog"
)

// ① 矩阵登记：两个端点各归其位，且互不串台
func TestRBAC_T257_PermissionRoutesRegistered(t *testing.T) {
	assert.True(t, matchStaffOnlyPattern(http.MethodGet, t257MePermsPath),
		"GET %s 必须登记进 staffOnlyPatterns，否则 roleAuthz 默认 403", t257MePermsPath)
	assert.False(t, matchRBACPattern(http.MethodGet, t257MePermsPath),
		"医生/客服/技师都要读自己的权限，不得收进 admin-only")
	assert.False(t, matchPublicPattern(http.MethodGet, t257MePermsPath), "患者无后台菜单，不得放行")

	assert.True(t, matchRBACPattern(http.MethodGet, t257PermCatalogPath),
		"GET %s 必须是 admin-only（权限分配页专属）", t257PermCatalogPath)
	assert.False(t, matchStaffOnlyPattern(http.MethodGet, t257PermCatalogPath))
	assert.False(t, matchPublicPattern(http.MethodGet, t257PermCatalogPath))
}

// ② /me/permissions：4 个 staff 角色放行并真正打到 user-service，患者 403 且不触达后端
func TestRBAC_T257_MePermissionsStaffPassPatientDenied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleAdmin, roleDoctor, roleCS, roleTech} {
		code, body := httpDoFull(t, http.MethodGet, gw.URL+t257MePermsPath, "", rbacToken(t, role))
		require.Equal(t, http.StatusOK, code, "role=%s 应放行，body=%s", role, body)
	}
	require.Len(t, *received, 4, "4 个 staff 角色均须真正打到 user-service（代理未注册则这里为 0）")
	for _, got := range *received {
		assert.Contains(t, got, "GET /api/v1/admin/me/permissions", "转发路径必须原样保留，实收 %q", got)
	}

	before := len(*received)
	code, body := httpDoFull(t, http.MethodGet, gw.URL+t257MePermsPath, "", rbacToken(t, rolePatient))
	assert.Equal(t, http.StatusForbidden, code, "患者不得读后台权限，body=%s", body)
	assert.Len(t, *received, before, "患者请求不得触达后端")
}

// ③ /permissions/catalog：仅 admin 放行；其余 staff 与患者一律 403 且不触达后端
func TestRBAC_T257_PermissionCatalogAdminOnly(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodGet, gw.URL+t257PermCatalogPath, "", rbacToken(t, roleAdmin))
	require.Equal(t, http.StatusOK, code, "admin 应放行，body=%s", body)
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "GET /api/v1/admin/permissions/catalog")

	for _, role := range []string{roleDoctor, roleCS, roleTech, rolePatient} {
		code, body := httpDoFull(t, http.MethodGet, gw.URL+t257PermCatalogPath, "", rbacToken(t, role))
		assert.Equal(t, http.StatusForbidden, code, "role=%s 不得读权限目录，body=%s", role, body)
	}
	assert.Len(t, *received, 1, "被拒请求不得触达后端")
}
