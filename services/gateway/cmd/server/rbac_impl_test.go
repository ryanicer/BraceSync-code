// Package main — T039-H2 端点级 RBAC 实现侧测试（不与测试专家 security_audit_impl_test.go 重叠）
//
// 覆盖：admin 专属端点矩阵匹配（单测）+ 双层真实 HTTP 授权链（ROLE_CS/ROLE_DOCTOR
// 打 admin 专属端点 403 不触达后端；ROLE_ADMIN 全放行；低角色对非 admin 端点不误伤；
// DELETE 技师防绕过路由低角色 403 / ROLE_ADMIN 404 行为不变）。
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRBAC_MatchAdminOnlyPatterns 矩阵匹配：admin 专属端点命中，业务端点不误伤
func TestRBAC_MatchAdminOnlyPatterns(t *testing.T) {
	hit := []struct{ method, path string }{
		{http.MethodPut, "/api/v1/admin/settings"},
		{http.MethodGet, "/api/v1/admin/settings"},
		{http.MethodPut, "/api/v1/admin/roles/ROLE_ADMIN/permissions"},
		{http.MethodGet, "/api/v1/admin/roles"},
		{http.MethodPut, "/api/v1/admin/notify-rules/alert"},
		{http.MethodGet, "/api/v1/admin/notification-logs"},
		{http.MethodGet, "/api/v1/technicians"},
		{http.MethodPost, "/api/v1/admin/technicians"},
		{http.MethodPut, "/api/v1/admin/technicians/TECH001"},
		{http.MethodPost, "/api/v1/technicians/TECH001/toggle"},
		{http.MethodGet, "/api/v1/teams"},
		{http.MethodGet, "/api/v1/teams/T001/members"},
		{http.MethodGet, "/api/v1/doctors"},
	}
	for _, c := range hit {
		assert.True(t, matchRBACPattern(c.method, c.path), "应命中：%s %s", c.method, c.path)
	}

	miss := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/patients"},              // 患者列表（doctor/cs 页面共用）
		{http.MethodGet, "/api/v1/admin/patients/P001"},         // 患者详情
		{http.MethodGet, "/api/v1/patients/P001/realtime"},      // 实时监控
		{http.MethodGet, "/api/v1/patients/P001/records"},       // 矫形日志
		{http.MethodGet, "/api/v1/alerts"},                      // 告警
		{http.MethodPost, "/api/v1/alerts/A001/process"},        // 告警处理
		{http.MethodGet, "/api/v1/feedbacks"},                   // 患者沟通
		{http.MethodPost, "/api/v1/feedbacks/F001/process"},     // 反馈处理
		{http.MethodGet, "/api/v1/admin/dashboard/kpi"},         // Dashboard
		{http.MethodPost, "/api/v1/devices"},                    // 设备注册（technician 域）
		{http.MethodPost, "/api/v1/devices/D001/bind"},          // 设备绑定（technician 域）
		{http.MethodGet, "/api/v1/devices"},                     // 设备列表
		{http.MethodPost, "/api/v1/technicians/TECH001"},        // 未注册方法，不属矩阵
		{http.MethodDelete, "/api/v1/admin/roles/ROLE_X/extra"}, // 段数不匹配
	}
	for _, c := range miss {
		assert.False(t, matchRBACPattern(c.method, c.path), "不得误伤：%s %s", c.method, c.path)
	}
}

// rbacToken 按角色签发测试 token
func rbacToken(t *testing.T, role string) map[string]string {
	t.Helper()
	tok := signTestJWT(t, testJWTSecretMain, "U-"+role, role, time.Now().Add(time.Hour).Unix())
	return map[string]string{"Authorization": "Bearer " + tok}
}

// TestRBAC_LowRoleDenied_AdminPasses 低角色 admin 专属端点 403 不触达后端；ROLE_ADMIN 放行
func TestRBAC_LowRoleDenied_AdminPasses(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	adminOnly := []struct{ method, path string }{
		{http.MethodPut, "/api/v1/admin/settings"},
		{http.MethodGet, "/api/v1/admin/settings"},
		{http.MethodPut, "/api/v1/admin/roles/ROLE_DOCTOR/permissions"},
		{http.MethodGet, "/api/v1/admin/roles"},
		{http.MethodGet, "/api/v1/technicians"},
		{http.MethodPost, "/api/v1/admin/technicians"},
		{http.MethodPut, "/api/v1/admin/technicians/TECH001"},
		{http.MethodPost, "/api/v1/technicians/TECH001/toggle"},
		{http.MethodGet, "/api/v1/teams"},
		{http.MethodGet, "/api/v1/doctors"},
	}
	for _, role := range []string{"ROLE_CS", "ROLE_DOCTOR"} {
		for _, c := range adminOnly {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%s %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}
	assert.Empty(t, *received, "越权请求不得触达后端")

	// ROLE_ADMIN 全放行（以 settings/roles/permissions 为例，转发后端）
	for _, c := range adminOnly[:3] {
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, "ROLE_ADMIN"))
		require.Equal(t, http.StatusOK, code, "ROLE_ADMIN %s %s 应放行", c.method, c.path)
	}
	assert.Len(t, *received, 3, "ROLE_ADMIN 请求转发后端")
}

// TestRBAC_LowRoleNonAdminEndpointsAllowed 低角色访问非 admin 专属端点不误伤
func TestRBAC_LowRoleNonAdminEndpointsAllowed(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	cases := []struct {
		role, method, path string
	}{
		{"ROLE_DOCTOR", http.MethodGet, "/api/v1/admin/patients"},
		{"ROLE_DOCTOR", http.MethodGet, "/api/v1/patients/P001/records"},
		{"ROLE_DOCTOR", http.MethodGet, "/api/v1/alerts"},
		{"ROLE_DOCTOR", http.MethodGet, "/api/v1/admin/dashboard/kpi"},
		{"ROLE_CS", http.MethodGet, "/api/v1/admin/patients"},
		{"ROLE_CS", http.MethodGet, "/api/v1/feedbacks"},
		{"ROLE_CS", http.MethodPost, "/api/v1/feedbacks/F001/process"},
		{"technician", http.MethodPost, "/api/v1/devices"},
		{"technician", http.MethodPost, "/api/v1/devices/D001/bind"},
		{"patient", http.MethodGet, "/api/v1/alerts"},
	}
	for _, c := range cases {
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, c.role))
		assert.Equal(t, http.StatusOK, code, "role=%s %s %s 不应被 RBAC 误伤", c.role, c.method, c.path)
	}
	assert.Len(t, *received, len(cases), "非 admin 专属端点全部转发后端")
}

// TestRBAC_DeleteTechnician_BypassClosed DELETE 技师防绕过路由：低角色 403，ROLE_ADMIN 404（原行为）
func TestRBAC_DeleteTechnician_BypassClosed(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{"ROLE_CS", "ROLE_DOCTOR"} {
		code, body := httpDoFull(t, http.MethodDelete, gw.URL+"/api/v1/admin/technicians/TECH001", "",
			rbacToken(t, role))
		assert.Equal(t, http.StatusForbidden, code, "role=%s DELETE 技师应 403", role)
		assert.Contains(t, body, `"code":403`)
	}

	// ROLE_ADMIN：路由存在但契约无此端点 → 404 统一响应体（与修复前网关 404 语义一致）
	code, body := httpDoFull(t, http.MethodDelete, gw.URL+"/api/v1/admin/technicians/TECH001", "",
		rbacToken(t, "ROLE_ADMIN"))
	assert.Equal(t, http.StatusNotFound, code)
	assert.Contains(t, body, `"code":404`)

	assert.Empty(t, *received, "DELETE 技师请求不得触达后端")
}
