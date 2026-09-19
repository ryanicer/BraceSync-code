// Package main — T252 新增后台端点的垂直鉴权登记验证。
//
// 覆盖 T252 在 user-service 侧新开的一组端点：11.2 角色增删改、2.2 告警规则配置、12.3 操作日志查询。
// 全部经 startFullGateway（真实 setupRouter 链：jwtAuth → roleAuthz → 反代），
// 断言方向：非 admin 角色 403 且请求不得触达后端；admin 不被误伤；伪造 X-Role 不提权。
//
// roleAuthz 是「默认放行」，新端点忘记登记 adminOnlyPatterns = 医生/客服/患者可直接改角色、改告警阈值，
// 本文件即为该缺口的回归门禁（同时依赖 TestRBAC_T190_AllAdminRoutesAreGated 做全表兜底）。
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// t252AdminRoutes T252 三组端点：全部仅 ROLE_ADMIN（含读端点，设计稿只在 admin 菜单出现）
var t252AdminRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/v1/admin/role-templates"},
	{http.MethodPost, "/api/v1/admin/roles"},
	{http.MethodPut, "/api/v1/admin/roles/ROLE_C0A1B2C3D4"},
	{http.MethodDelete, "/api/v1/admin/roles/ROLE_C0A1B2C3D4"},
	{http.MethodGet, "/api/v1/admin/alert-rules"},
	{http.MethodPut, "/api/v1/admin/alert-rules/points"},
	{http.MethodPost, "/api/v1/admin/alert-rules/points/reset"},
	{http.MethodPut, "/api/v1/admin/alert-rules/global"},
	{http.MethodGet, "/api/v1/admin/audit-logs"},
}

func TestRBAC_T252_NewRoutesAreAdminOnlyMatrixMatch(t *testing.T) {
	for _, c := range t252AdminRoutes {
		assert.True(t, matchRBACPattern(c.method, c.path), "应命中 admin-only 矩阵：%s %s", c.method, c.path)
	}
}

func TestRBAC_T252_NonAdminRolesDenied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleDoctor, roleCS, roleTech, rolePatient, "", "ROLE_GHOST"} {
		for _, c := range t252AdminRoutes {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%q %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}
	assert.Empty(t, *received, "非 admin 的后台角色/告警规则/审计请求不得触达后端")
}

func TestRBAC_T252_AdminAllowedAndForgedRoleCannotEscalate(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, c := range t252AdminRoutes {
		code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, roleAdmin))
		assert.Equal(t, http.StatusOK, code, "admin %s %s 不应被误伤，body=%s", c.method, c.path, body)
	}
	assert.Len(t, *received, len(t252AdminRoutes))

	// 患者 token 伪造 X-Role: ROLE_ADMIN 仍 403（身份头由 jwtAuth 从 JWT 重注入）
	tok := signTestJWT(t, testJWTSecretMain, "P20260002", rolePatient, time.Now().Add(time.Hour).Unix())
	before := len(*received)
	for _, c := range t252AdminRoutes {
		h := map[string]string{
			"Authorization": "Bearer " + tok,
			"X-Role":        roleAdmin,
			"X-User-Id":     "P20260001",
		}
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, h)
		assert.Equal(t, http.StatusForbidden, code, "伪造 X-Role 后 %s %s 仍应 403", c.method, c.path)
	}
	assert.Len(t, *received, before, "伪造身份的请求不得触达后端")
}
