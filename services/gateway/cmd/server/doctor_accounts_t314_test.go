// Package main — T314 医护账号写端点的网关侧登记验证。
//
// 派发单口径同 T274：网关是「默认拒绝」，新端点不登记进 adminOnlyPatterns 就 403。
// 本页四个写端点（创建/编辑/重置密码/启停）都是**凭据级**操作（发初始密码、禁登录），
// 一档同 sys_config / flow 模板 ⇒ 只放 ROLE_ADMIN；医生/客服/技师/患者一律拒。
// 🔴 特别注意 ROLE_DOCTOR：被管理的就是医生本人，绝不能让自己改自己的密码或状态。
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var t314DoctorAccountRoutes = []struct{ method, path string }{
	{http.MethodPost, "/api/v1/admin/doctors"},
	{http.MethodPut, "/api/v1/admin/doctors/DOC0A1B2C3D4E5"},
	{http.MethodPost, "/api/v1/admin/doctors/DOC0A1B2C3D4E5/reset-password"},
	{http.MethodPost, "/api/v1/admin/doctors/DOC0A1B2C3D4E5/status"},
}

// TestRBAC_T314_DoctorAccountRoutesAreAdminOnly 防回归：四条路由确实落在 admin-only 一档，
// 且没被顺手挪进 staffOnlyPatterns（那等于把「给同事发初始密码」开放给全体医护/客服）。
func TestRBAC_T314_DoctorAccountRoutesAreAdminOnly(t *testing.T) {
	for _, c := range t314DoctorAccountRoutes {
		assert.True(t, matchRBACPattern(c.method, c.path), "应命中 admin-only：%s %s", c.method, c.path)
		assert.False(t, matchStaffOnlyPattern(c.method, c.path),
			"凭据级写端点不得命中 staff-only：%s %s", c.method, c.path)
		assert.False(t, matchPublicPattern(c.method, c.path),
			"凭据级写端点不得命中 public：%s %s", c.method, c.path)
	}
}

// TestRBAC_T314_NonAdminRolesDenied 四个角色 + 空 + 伪造角色全部 403，且不得触达后端。
func TestRBAC_T314_NonAdminRolesDenied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleDoctor, roleCS, roleTech, rolePatient, "", "ROLE_GHOST"} {
		for _, c := range t314DoctorAccountRoutes {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%q %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}
	assert.Empty(t, *received, "非 admin 的医护账号写操作不得触达 user-service")
}

// TestRBAC_T314_AdminAllowed admin 四条全放行并转发。
func TestRBAC_T314_AdminAllowed(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, c := range t314DoctorAccountRoutes {
		code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, roleAdmin))
		assert.Equal(t, http.StatusOK, code, "admin %s %s 不应被误伤，body=%s", c.method, c.path, body)
	}
	assert.Len(t, *received, len(t314DoctorAccountRoutes))
}

// TestRBAC_T314_DoctorCannotEscalateViaForgedHeaders 🔴 被管理对象=self：医生拿自己的
// 患者/医生 token 伪造 X-Role=admin 也改不动自己的密码与启停状态。
func TestRBAC_T314_DoctorCannotEscalateViaForgedHeaders(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	tok := signTestJWT(t, testJWTSecretMain, "D0001", roleDoctor, time.Now().Add(time.Hour).Unix())
	for _, c := range t314DoctorAccountRoutes {
		h := map[string]string{
			"Authorization": "Bearer " + tok,
			"X-Role":        roleAdmin,
			"X-User-Id":     "D0001",
		}
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, h)
		assert.Equal(t, http.StatusForbidden, code, "伪造 X-Role 后 %s %s 仍应 403", c.method, c.path)
	}
	assert.Empty(t, *received, "伪造身份的医护账号写操作不得触达后端")
}
