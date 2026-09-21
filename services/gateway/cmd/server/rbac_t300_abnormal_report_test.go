// Package main — T300 新增读端点 GET /api/v1/admin/abnormal-reports[/export] 的网关侧验证。
//
// 为什么需要本文件：check-routes.sh 只比对 user-service 的路由表，alert-service 的新路由
// 没有 CI 门禁会发现「服务已实现、网关漏转发」或「网关已转发、RBAC 漏登记（T260 默认 403）」。
// 汇总/导出都是**跨患者聚合 + 全字段明细**，一旦漏登记就是患者 token 能拉别人家报告，
// 所以这里既断言放行侧（staff 四角色 + 参数原样透传），也断言拒绝侧（患者 403 且不触达后端）。
package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t300ReportPath = "/api/v1/admin/abnormal-reports"
	t300ExportPath = "/api/v1/admin/abnormal-reports/export"
	t300Query      = "?patientId=P001&start=2026-09-01&end=2026-09-03"
)

// ① RBAC 登记：命中 staffOnly 矩阵 ⇒ 不走 T260 默认拒绝；且不得是 admin-only / 对患者开放
func TestRBAC_T300_AbnormalReportPatternsAreRegistered(t *testing.T) {
	for _, path := range []string{t300ReportPath, t300ExportPath} {
		assert.True(t, matchStaffOnlyPattern(http.MethodGet, path),
			"GET %s 必须登记进 staffOnlyPatterns，否则 roleAuthz 默认 403", path)
		assert.False(t, matchRBACPattern(http.MethodGet, path), "%s 不得是 admin-only（医生/康复师也要出报告）", path)
		assert.False(t, matchPublicPattern(http.MethodGet, path), "%s 不得对患者开放", path)
	}
}

// ② 反代登记 + 角色判定：staff 放行且打到 alert-service（查询参数原样保留），患者 403 不触达后端
func TestRBAC_T300_AbnormalReportStaffPassPatientDenied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, path := range []string{t300ReportPath, t300ExportPath} {
		for _, role := range []string{roleAdmin, roleDoctor, roleCS, roleTech} {
			code, body := httpDoFull(t, http.MethodGet, gw.URL+path+t300Query, "", rbacToken(t, role))
			require.Equal(t, http.StatusOK, code, "role=%s path=%s 应放行，body=%s", role, path, body)
		}
	}
	require.Len(t, *received, 8, "2 个端点 × 4 个 staff 角色均须真正打到 alert-service（代理未注册则这里为 0）")
	for _, got := range *received {
		// 路径与查询参数都必须原样保留：patientId/start/end 任一被吞掉，服务侧就是另一个口径的报告
		assert.Contains(t, got, "?patientId=P001&start=2026-09-01&end=2026-09-03", "查询参数须透传，实收 %q", got)
		assert.Contains(t, got, "GET /api/v1/admin/abnormal-reports", "转发路径必须原样保留，实收 %q", got)
	}

	before := len(*received)
	for _, path := range []string{t300ReportPath, t300ExportPath} {
		code, body := httpDoFull(t, http.MethodGet, gw.URL+path+t300Query, "", rbacToken(t, rolePatient))
		assert.Equal(t, http.StatusForbidden, code, "患者不得读他人异常报告，path=%s body=%s", path, body)
	}
	assert.Len(t, *received, before, "患者请求不得触达后端")
}
