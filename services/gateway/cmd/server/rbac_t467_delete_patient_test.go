// Package main — T467 DELETE /admin/patients/:patientId 的网关侧授权登记。
//
// 本卡选型：落 doctorAdminOnlyPatterns（医生+管理员），不放 adminOnlyPatterns、不放 staffOnlyPatterns。
// 理由不是「医生更该能删」，而是判定序：
//   - 收口成 admin-only ⇒ 医生的删除请求在入口就被截断，user-service 里
//     assertPatientInScope 那条「仅本团队可删、跨团队 403」的团队谓词永远不会被执行到，
//     派发单第 2 项「沿用 T350 体系、跨团队 403 与现有一族对齐」无从验收
//     （现有一族 = T373 的两条 doctor+admin 写端点）。
//   - 放进 staff-only ⇒ ROLE_CS / technician 也能删患者；而 T350 里客服不受团队隔离
//     （resolveTeamScope 只对 ROLE_DOCTOR 受限），等于「任意客服删任意患者」。
//
// 全部经 startFullGateway（真实 setupRouter 链：jwtAuth → roleAuthz → 反代），
// 断言方向：doctor/admin 转发后端，cs/tech/patient/空角色/未知角色 403 且不触达后端。
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t467DeletePatientPath = "/api/v1/admin/patients/P20260001"

// TestRBAC_T467_DeletePatientLandsInDoctorAdminMatrix 矩阵归属对账（选型本身的门禁）。
// 四个 match* 都在同一批 pattern 上跑，所以「命中 doctorAdmin」与「不命中其余三张」必须同时断：
// 只断前者则挪去 adminOnly 也照样绿。
func TestRBAC_T467_DeletePatientLandsInDoctorAdminMatrix(t *testing.T) {
	require.True(t, matchDoctorAdminPattern(http.MethodDelete, t467DeletePatientPath),
		"删除端点应命中 doctor+admin 矩阵（否则医生在入口被截断，团队谓词跑不到）")
	assert.False(t, matchRBACPattern(http.MethodDelete, t467DeletePatientPath),
		"不得进 admin-only 矩阵")
	assert.False(t, matchStaffOnlyPattern(http.MethodDelete, t467DeletePatientPath),
		"不得进 staff-only 矩阵（客服/技师可删任意患者）")
	assert.False(t, matchTechAdminPattern(http.MethodDelete, t467DeletePatientPath),
		"不得进 tech+admin 矩阵")

	// 同路径的其余方法不许被我的登记连带改动：PUT 档案编辑仍是 admin-only，GET 详情仍是 staff-only
	assert.False(t, matchDoctorAdminPattern(http.MethodPut, t467DeletePatientPath),
		"PUT 同路径不得被误命中 doctor+admin 矩阵")
	assert.True(t, matchRBACPattern(http.MethodPut, t467DeletePatientPath), "PUT 档案编辑仍是 admin-only")
	assert.True(t, matchStaffOnlyPattern(http.MethodGet, t467DeletePatientPath), "GET 详情仍是 staff-only")
	assert.False(t, matchDoctorAdminPattern(http.MethodGet, t467DeletePatientPath))
}

// TestRBAC_T467_DeletePatientRouteIsProxied 反代表登记：handler 侧测试再绿，
// 网关没这条路由也只会 404（前端点删除按钮打不到 user-service）。
func TestRBAC_T467_DeletePatientRouteIsProxied(t *testing.T) {
	var found bool
	for _, rt := range userServiceRoutes {
		if rt.method == http.MethodDelete && rt.path == "/admin/patients/:patientId" {
			found = true
		}
	}
	assert.True(t, found, "proxy_admin.go 患者域必须登记 DELETE /admin/patients/:patientId")
}

// TestRBAC_T467_DoctorAndAdminForwarded cs/tech/patient/空/未知 5 个角色 403 且零转发，
// doctor/admin 放行并触达后端。
func TestRBAC_T467_DoctorAndAdminForwarded(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleCS, roleTech, rolePatient, "", "ROLE_GHOST"} {
		code, body := httpDoFull(t, http.MethodDelete, gw.URL+t467DeletePatientPath, "", rbacToken(t, role))
		assert.Equal(t, http.StatusForbidden, code, "role=%q DELETE 患者应 403", role)
		assert.Contains(t, body, `"code":403`)
	}
	assert.Empty(t, *received, "被拒角色的删除请求不得触达后端")

	for _, role := range []string{roleDoctor, roleAdmin} {
		code, body := httpDoFull(t, http.MethodDelete, gw.URL+t467DeletePatientPath, "", rbacToken(t, role))
		assert.Equal(t, http.StatusOK, code, "role=%s DELETE 患者应放行到后端，body=%s", role, body)
	}
	assert.Len(t, *received, 2, "doctor/admin 两条放行请求应转发后端")
}

// TestRBAC_T467_PatientCannotEscalateViaForgedHeaders 患者 token 伪造 X-Role/X-User-Id
// 仍 403（身份头由 jwtAuth 剥离后从 JWT 重注入），并且不触达后端。
func TestRBAC_T467_PatientCannotEscalateViaForgedHeaders(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	tok := signTestJWT(t, testJWTSecretMain, "P20260002", rolePatient, time.Now().Add(time.Hour).Unix())
	h := map[string]string{
		"Authorization": "Bearer " + tok,
		"X-Role":        roleAdmin,
		"X-User-Id":     "P20260001",
	}
	code, _ := httpDoFull(t, http.MethodDelete, gw.URL+t467DeletePatientPath, "", h)
	assert.Equal(t, http.StatusForbidden, code, "伪造 X-Role 后 DELETE 患者仍应 403")
	assert.Empty(t, *received, "伪造身份的删除请求不得触达后端")
}
