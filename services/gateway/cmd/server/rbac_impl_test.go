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

// TestRBAC_TechAdminOnlyPatterns T122：PUT /install-records/:id 收紧为 tech+admin
// 覆盖：technician / ROLE_ADMIN → 放行；ROLE_DOCTOR / ROLE_CS → 403 不触达后端
// （provision-key 原属本矩阵，T193 迁出，见 TestRBAC_T193_ProvisionKeyRoles）
func TestRBAC_TechAdminOnlyPatterns(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	techAdmin := []struct{ method, path string }{
		{http.MethodPut, "/api/v1/install-records/17"}, // T122 新增
	}

	// doctor / cs → 403
	for _, role := range []string{"ROLE_CS", "ROLE_DOCTOR"} {
		for _, c := range techAdmin {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%s %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}
	assert.Empty(t, *received, "越权请求不得触达后端")

	// technician / ROLE_ADMIN → 放行
	for _, role := range []string{"technician", "ROLE_ADMIN"} {
		for _, c := range techAdmin {
			code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			require.Equal(t, http.StatusOK, code, "role=%s %s %s 应放行", role, c.method, c.path)
		}
	}
	assert.Len(t, *received, len(techAdmin)*2, "technician + ROLE_ADMIN 请求全部转发后端")
}

// TestRBAC_T193_ProvisionKeyRoles T193：配网密钥领卡端点角色白名单 = patient + technician +
// ROLE_ADMIN（Boss 裁决 D4 放开患者，支撑 PRD §7A.9 患者自助配网）。
// 医生/客服维持 T091 的 403 口径；本层不校验设备归属（gateway 无 device→patient 视图），
// 患者「只能领自己已绑定设备」由 device-service 强制。
func TestRBAC_T193_ProvisionKeyRoles(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	path := "/api/v1/devices/D001/provision-key"

	for _, role := range []string{"ROLE_CS", "ROLE_DOCTOR"} {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+path, `{}`, rbacToken(t, role))
		assert.Equal(t, http.StatusForbidden, code, "role=%s 领配网密钥应 403", role)
		assert.Contains(t, body, `"code":403`)
	}
	assert.Empty(t, *received, "医生/客服领卡请求不得触达后端")

	for _, role := range []string{"patient", "technician", "ROLE_ADMIN"} {
		code, _ := httpDoFull(t, http.MethodPost, gw.URL+path, `{}`, rbacToken(t, role))
		assert.Equal(t, http.StatusOK, code, "role=%s 领配网密钥应放行", role)
	}
	assert.Len(t, *received, 3, "patient + technician + ROLE_ADMIN 全部转发后端")

	// 矩阵迁移自检：provision-key 不再落在 tech+admin 专属矩阵（否则 patient 会被上层拦掉）
	assert.False(t, matchTechAdminPattern(http.MethodPost, path), "provision-key 不得再属 tech+admin 矩阵")
	assert.True(t, matchProvisionKeyPattern(http.MethodPost, path), "provision-key 须命中新矩阵")
}

// TestRBAC_T130_DoctorAdminOnly T130：复查记录创建端点仅 doctor/admin 可访问
func TestRBAC_T130_DoctorAdminOnly(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	createReviewPath := "/api/v1/admin/review-records"

	// 非 doctor/admin 角色 → 403
	for _, role := range []string{"ROLE_CS", "technician", "patient"} {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+createReviewPath, `{}`, rbacToken(t, role))
		assert.Equal(t, http.StatusForbidden, code, "role=%s POST review-records 应 403", role)
		assert.Contains(t, body, `"code":403`)
	}

	// doctor/admin → 放行（转发后端）
	for _, role := range []string{"ROLE_DOCTOR", "ROLE_ADMIN"} {
		code, _ := httpDoFull(t, http.MethodPost, gw.URL+createReviewPath, `{}`, rbacToken(t, role))
		assert.Equal(t, http.StatusOK, code, "role=%s POST review-records 应放行", role)
	}

	// 患者复查记录列表端点：全角色放行（水平鉴权在 user-service handler 层）
	for _, role := range []string{"patient", "ROLE_DOCTOR", "ROLE_ADMIN", "ROLE_CS"} {
		code, _ := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/patients/P001/review-records", "", rbacToken(t, role))
		assert.Equal(t, http.StatusOK, code, "role=%s GET review-records 不应被网关 RBAC 拦截", role)
	}

	assert.Len(t, *received, 6, "doctor/admin create(2) + 全角色 list(4) 应全部转发后端")
}

// TestRBAC_T135_DoctorAdminOnly T135：复查报告模板管理端点仅 doctor/admin 可访问。
// 合同运营后台「复查报告模板管理」：admin 后台上传/替换/列表/下载，doctor 列表/下载空白模板。
func TestRBAC_T135_DoctorAdminOnly(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	templateRoutes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/admin/review-templates"},               // 上传/创建模板
		{http.MethodPost, "/api/v1/admin/review-templates/GRP_1/replace"}, // 版本替换
		{http.MethodGet, "/api/v1/admin/review-templates"},                // 模板列表
		{http.MethodGet, "/api/v1/admin/review-templates/GRP_1/download"}, // 模板下载
	}

	// 非 doctor/admin 角色 → 403（不触达后端）
	for _, role := range []string{"ROLE_CS", "technician", "patient"} {
		for _, c := range templateRoutes {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%s %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}

	// doctor/admin → 放行（转发后端）
	for _, role := range []string{"ROLE_DOCTOR", "ROLE_ADMIN"} {
		for _, c := range templateRoutes {
			code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusOK, code, "role=%s %s %s 应放行", role, c.method, c.path)
		}
	}

	// 3 低角色 × 4 端点 全被拦（0 触达） + 2 高角色 × 4 端点 全转发 = 8
	assert.Len(t, *received, 8, "high-role template 请求应转发后端，low-role 应被网关 RBAC 拦截")
}

// TestRBAC_T260_DefaultDeny_UnregisteredPattern 未登记进任一矩阵的 method+path 在
// 所有 match* 函数中均返回 false（fail-closed 设计的单元验证）。
// HTTP 层：gin 对未注册路由直接返 404（路由层即拒），中间件不执行；
// 已注册但漏登记矩阵的路由才会走 roleAuthz 末尾 default-deny → 403。
func TestRBAC_T260_DefaultDeny_UnregisteredPattern(t *testing.T) {
	// 单元层：所有 match* 对未登记模式返回 false
	unregistered := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/nonexistent-xyz"},
		{http.MethodPost, "/api/v1/not-registered-abc"},
		{http.MethodGet, "/api/v1/unknown-path-123"},
	}
	for _, c := range unregistered {
		assert.False(t, matchProvisionKeyPattern(c.method, c.path), "%s %s 不应命中 provisionKey", c.method, c.path)
		assert.False(t, matchTechAdminPattern(c.method, c.path), "%s %s 不应命中 techAdmin", c.method, c.path)
		assert.False(t, matchDoctorAdminPattern(c.method, c.path), "%s %s 不应命中 doctorAdmin", c.method, c.path)
		assert.False(t, matchStaffOnlyPattern(c.method, c.path), "%s %s 不应命中 staffOnly", c.method, c.path)
		assert.False(t, matchRBACPattern(c.method, c.path), "%s %s 不应命中 adminOnly", c.method, c.path)
		assert.False(t, matchPublicPattern(c.method, c.path), "%s %s 不应命中 public", c.method, c.path)
	}

	// HTTP 层：未注册路由 → 404（路由层拒绝，非 200 泄漏）
	backend, _ := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)
	for _, role := range []string{"ROLE_ADMIN", "ROLE_DOCTOR", "patient"} {
		code, _ := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/nonexistent-xyz", `{}`, rbacToken(t, role))
		assert.Equal(t, http.StatusNotFound, code, "role=%s 未注册路由应 404（路由层拒绝）", role)
	}
}

// TestRBAC_T260_PublicPatterns_AllRoles publicPatterns 端点对所有已认证角色放行
// （patient + staff）。每条均有服务层 self-scope / admin-or-self / owner 校验依据。
// file-service 路由代理到 file-service:8085（测试环境不可达 → 502），
// 只要不是 403 即说明 RBAC 放行；其余路由经测试后端 → 200。
func TestRBAC_T260_PublicPatterns_AllRoles(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	// 非 file-service 的 public 路由 → 200（均有服务层鉴权依据）
	publicOK := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/patient/profile"},
		{http.MethodGet, "/api/v1/patients/P001/wear-reminder"},
		{http.MethodGet, "/api/v1/patients/P001/daily-wear"},
		{http.MethodGet, "/api/v1/patients/P001/feeling-logs"},
		{http.MethodGet, "/api/v1/patients/P001/review-records"},
		// T264 已合：服务层补齐 admin-or-self / 患者身份绑定，患者端恢复可用
		{http.MethodGet, "/api/v1/patients/P001/records"},
		{http.MethodGet, "/api/v1/patients/P001/realtime"},
		{http.MethodGet, "/api/v1/patients/P001/health-reports"},
		{http.MethodGet, "/api/v1/patients/P001/orthosis-plans"},
		{http.MethodGet, "/api/v1/alerts"},
	}
	// file-service 路由 → 502（后端不可达），但非 403 即 RBAC 放行
	publicFile := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/files/presign"},
		{http.MethodGet, "/api/v1/files/query"},
	}
	roles := []string{"ROLE_ADMIN", "ROLE_DOCTOR", "ROLE_CS", "technician", "patient"}
	for _, role := range roles {
		for _, c := range publicOK {
			code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusOK, code, "role=%s public %s %s 应 200", role, c.method, c.path)
		}
		for _, c := range publicFile {
			code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.NotEqual(t, http.StatusForbidden, code, "role=%s public file %s %s 不应被 RBAC 拦截（403）", role, c.method, c.path)
		}
	}
}

// TestRBAC_T260_StaffOnly_DeniedForPatient staff-only 端点对 patient 一律 403，staff 放行。
func TestRBAC_T260_StaffOnly_DeniedForPatient(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	staffOnly := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/feedbacks"},
		{http.MethodPost, "/api/v1/feedbacks/F001/process"},
		{http.MethodPost, "/api/v1/alerts/A001/process"},
		{http.MethodGet, "/api/v1/devices"},
		{http.MethodPost, "/api/v1/devices/D001/bind"},
		{http.MethodPost, "/api/v1/install-records"},
	}
	// patient → 403
	for _, c := range staffOnly {
		code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, "patient"))
		assert.Equal(t, http.StatusForbidden, code, "patient %s %s 应 403", c.method, c.path)
		assert.Contains(t, body, `"code":403`)
	}
	// staff（admin/doctor/cs/tech）→ 放行
	staffCount := 0
	for _, role := range []string{"ROLE_ADMIN", "ROLE_DOCTOR", "ROLE_CS", "technician"} {
		for _, c := range staffOnly {
			code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusOK, code, "role=%s staff-only %s %s 应放行", role, c.method, c.path)
			staffCount++
		}
	}
	assert.Len(t, *received, staffCount, "staff 请求应转发后端，patient 请求被网关拦截")
}

// TestRBAC_T260_DoctorAdminOnly_DeniedForOthers doctor+admin 专属（feeling-logs reply /
// orthosis-plans save）对 cs/tech/patient 403，doctor/admin 放行。
func TestRBAC_T260_DoctorAdminOnly_DeniedForOthers(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/feeling-logs/L001/reply"},
		{http.MethodPost, "/api/v1/patients/P001/orthosis-plans"},
	}
	// low roles → 403
	for _, role := range []string{"ROLE_CS", "technician", "patient"} {
		for _, c := range cases {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%s %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}
	// doctor/admin → 放行
	highCount := 0
	for _, role := range []string{"ROLE_DOCTOR", "ROLE_ADMIN"} {
		for _, c := range cases {
			code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusOK, code, "role=%s %s %s 应放行", role, c.method, c.path)
			highCount++
		}
	}
	assert.Len(t, *received, highCount)
}

// TestRBAC_T260_AdminOnly_TeamsWrite teams 写操作仅 admin，doctor/cs/tech/patient 403。
func TestRBAC_T260_AdminOnly_TeamsWrite(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	teamsWrite := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/teams"},
		{http.MethodPut, "/api/v1/teams/T001"},
		{http.MethodDelete, "/api/v1/teams/T001"},
		{http.MethodPost, "/api/v1/teams/T001/members"},
		{http.MethodPut, "/api/v1/teams/T001/members/M001"},
		{http.MethodDelete, "/api/v1/teams/T001/members/M001"},
	}
	for _, role := range []string{"ROLE_DOCTOR", "ROLE_CS", "technician", "patient"} {
		for _, c := range teamsWrite {
			code, body := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%s teams 写 %s %s 应 403", role, c.method, c.path)
			assert.Contains(t, body, `"code":403`)
		}
	}
	// admin → 放行
	adminCount := 0
	for _, c := range teamsWrite {
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`, rbacToken(t, "ROLE_ADMIN"))
		assert.Equal(t, http.StatusOK, code, "ROLE_ADMIN teams 写 %s %s 应放行", c.method, c.path)
		adminCount++
	}
	assert.Len(t, *received, adminCount)
}
