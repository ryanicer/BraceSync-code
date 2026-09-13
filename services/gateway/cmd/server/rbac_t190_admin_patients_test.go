// Package main — T190 表 B 收口验证：后台患者管理域端点的垂直鉴权。
//
// 全部经 startFullGateway（真实 setupRouter 链：jwtAuth → roleAuthz → 反代），
// 不手工挂单个中间件——避免「测试抄实现」导致的恒绿盲区。
// 断言方向：低角色 403 且请求不得触达后端；staff/ admin 不被误伤。
package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t190AdminWrites 表 B 写端点：仅 ROLE_ADMIN
var t190AdminWrites = []struct{ method, path string }{
	{http.MethodPost, "/api/v1/admin/patients"},
	{http.MethodPost, "/api/v1/admin/patients/batch-bind"},
	{http.MethodPut, "/api/v1/admin/patients/P20260001/team"},
	{http.MethodPost, "/api/v1/admin/patients/P20260001/unbind-wechat"},
	{http.MethodPut, "/api/v1/admin/patients/P20260001/phone"},
}

// t190AdminReads 表 B 读端点：staff 可用、患者 403
// （医生 monitor/orthosis-log/review-records 页共用列表；技师 bind 步骤用详情；
// 医生 dashboard 页用聚合指标）
var t190AdminReads = []struct{ method, path string }{
	{http.MethodGet, "/api/v1/admin/patients"},
	{http.MethodGet, "/api/v1/admin/patients/P20260001"},
	{http.MethodGet, "/api/v1/admin/dashboard/kpi"},
	{http.MethodGet, "/api/v1/admin/dashboard/wear-trend"},
	{http.MethodGet, "/api/v1/admin/dashboard/wear-distribution"},
	{http.MethodGet, "/api/v1/admin/dashboard/alert-trend"},
	{http.MethodGet, "/api/v1/admin/dashboard/team-ranking"},
	{http.MethodGet, "/api/v1/admin/dashboard/doctor-ranking"},
}

// TestRBAC_T190_MatchAdminWrites 矩阵匹配单测：写端点入 admin 矩阵、读端点入 staff 矩阵，
// 且患者域自查端点不得被 staff 矩阵误伤（患者必须能打本人 /patients/:id/*）。
func TestRBAC_T190_MatchAdminWrites(t *testing.T) {
	for _, c := range t190AdminWrites {
		assert.True(t, matchRBACPattern(c.method, c.path), "写端点应命中 admin-only 矩阵：%s %s", c.method, c.path)
	}
	for _, c := range t190AdminReads {
		assert.True(t, matchStaffOnlyPattern(c.method, c.path), "读端点应命中 staff-only 矩阵：%s %s", c.method, c.path)
	}

	// 不得误伤：患者域端点（水平鉴权在 msg/data/user-service handler 层 self-scope）
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/patients/P20260001/daily-wear"},
		{http.MethodGet, "/api/v1/patients/P20260001/feeling-logs"},
		{http.MethodGet, "/api/v1/patients/P20260001/wear-reminder"},
		{http.MethodPut, "/api/v1/patients/P20260001/wear-reminder"},
		{http.MethodGet, "/api/v1/patient/profile"},
	} {
		assert.False(t, matchStaffOnlyPattern(c.method, c.path), "患者域端点不得进 staff 矩阵：%s %s", c.method, c.path)
		assert.False(t, matchRBACPattern(c.method, c.path), "患者域端点不得进 admin 矩阵：%s %s", c.method, c.path)
	}
}

// TestRBAC_T190_PatientDenied 患者 token 打后台管理域 13 个端点 → 403，且不得触达后端。
// 表 B 的账号接管路径（PUT /admin/patients/:id/phone）在此明确断言。
func TestRBAC_T190_PatientDenied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, c := range append(t190AdminWrites, t190AdminReads...) {
		code, body := httpDoFull(t, c.method, gw.URL+c.path, `{"phone":"13800138000"}`,
			rbacToken(t, rolePatient))
		assert.Equal(t, http.StatusForbidden, code, "patient %s %s 应 403", c.method, c.path)
		assert.Contains(t, body, `"code":403`)
	}
	assert.Empty(t, *received, "患者 token 的后台管理域请求不得触达后端")
}

// TestRBAC_T190_PatientCannotForgeRoleHeader 患者 token 伪造 X-Role: ROLE_ADMIN 仍 403
// （身份头由 jwtAuth 剥离后从 JWT 重注入，不可由客户端提权）。
func TestRBAC_T190_PatientCannotForgeRoleHeader(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	tok := signTestJWT(t, testJWTSecretMain, "P20260002", rolePatient, time.Now().Add(time.Hour).Unix())
	for _, c := range t190AdminWrites {
		h := map[string]string{
			"Authorization": "Bearer " + tok,
			"X-Role":        roleAdmin,
			"X-User-Id":     "P20260001",
		}
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, h)
		assert.Equal(t, http.StatusForbidden, code, "伪造 X-Role 后 %s %s 仍应 403", c.method, c.path)
	}
	assert.Empty(t, *received, "伪造身份的请求不得触达后端")
}

// TestRBAC_T190_StaffRolesDeniedWrites 医生/客服/技师打 5 个写端点 → 403（垂直越权面）。
func TestRBAC_T190_StaffRolesDeniedWrites(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleDoctor, roleCS, roleTech} {
		for _, c := range t190AdminWrites {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%s %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}
	assert.Empty(t, *received, "非 admin 的写请求不得触达后端")
}

// TestRBAC_T190_StaffReadsAllowed 读端点保持既有 staff 调用方不被误伤（改前后行为一致）：
// ROLE_ADMIN 全部放行；医生/客服可读名册与聚合；技师可读患者详情（bind 步骤）。
func TestRBAC_T190_StaffReadsAllowed(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	cases := []struct{ role, method, path string }{
		{roleAdmin, http.MethodGet, "/api/v1/admin/patients"},
		{roleAdmin, http.MethodGet, "/api/v1/admin/patients/P20260001"},
		{roleAdmin, http.MethodGet, "/api/v1/admin/dashboard/kpi"},
		{roleAdmin, http.MethodPost, "/api/v1/admin/patients"},
		{roleAdmin, http.MethodPut, "/api/v1/admin/patients/P20260001/phone"},
		{roleAdmin, http.MethodPut, "/api/v1/admin/patients/P20260001/team"},
		{roleAdmin, http.MethodPost, "/api/v1/admin/patients/batch-bind"},
		{roleAdmin, http.MethodPost, "/api/v1/admin/patients/P20260001/unbind-wechat"},
		{roleDoctor, http.MethodGet, "/api/v1/admin/patients"},
		{roleDoctor, http.MethodGet, "/api/v1/admin/dashboard/kpi"},
		{roleCS, http.MethodGet, "/api/v1/admin/patients"},
		{roleTech, http.MethodGet, "/api/v1/admin/patients/P20260001"},
	}
	for _, c := range cases {
		code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, c.role))
		assert.Equal(t, http.StatusOK, code, "role=%s %s %s 不应被误伤，body=%s", c.role, c.method, c.path, body)
	}
	assert.Len(t, *received, len(cases), "staff 合法请求应全部转发后端")
}

// TestRBAC_T190_UnknownRoleFailsClosed 未知角色 / 无角色 → 读端点 403（fail-closed，
// staff 用 allow-list 而非「仅拒 patient」）。
func TestRBAC_T190_UnknownRoleFailsClosed(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{"", "ROLE_GHOST"} {
		for _, c := range t190AdminReads {
			code, _ := httpDoFull(t, c.method, gw.URL+c.path, "", rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%q %s %s 应 403（fail-closed）",
				role, c.method, c.path)
		}
	}
	assert.Empty(t, *received, "未知/空角色的后台请求不得触达后端")
}

// TestRBAC_T190_AllAdminRoutesAreGated 防回归（不读本文件的用例表，避免自指恒绿）：
// 遍历 gateway 真实注册表里所有 /admin/ 前缀路由，逐条断言被某个 RBAC 矩阵覆盖。
// roleAuthz 是「默认放行」，漏登记 = 患者 token 可直接打（T184 表 B 的成因）。
func TestRBAC_T190_AllAdminRoutesAreGated(t *testing.T) {
	tables := map[string][]proxyRoute{
		"user":   userServiceRoutes,
		"data":   dataServiceRoutes,
		"device": deviceServiceRoutes,
		"msg":    msgServiceRoutes,
		"file":   fileServiceRoutes,
	}

	var total, ungated int
	for svc, routes := range tables {
		for _, rt := range routes {
			if !strings.HasPrefix(rt.path, "/admin/") {
				continue
			}
			total++
			path := "/api/v1" + rt.path
			gated := matchRBACPattern(rt.method, path) || matchStaffOnlyPattern(rt.method, path) ||
				matchTechAdminPattern(rt.method, path) || matchDoctorAdminPattern(rt.method, path)
			if !gated {
				ungated++
				t.Errorf("%s-service %s %s 未登记进任何 RBAC 矩阵（默认放行 = 零防护）", svc, rt.method, path)
			}
		}
	}
	require.Greater(t, total, 0, "注册表中应能扫到 /admin/ 路由，扫到 0 条说明本测试已失去意义")
	t.Logf("T190 覆盖核对：/admin/ 注册路由 %d 条，未登记矩阵 %d 条", total, ungated)
}
