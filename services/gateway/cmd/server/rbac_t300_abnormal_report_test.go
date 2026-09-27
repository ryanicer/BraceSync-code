// Package main — T300 新增读端点 GET /api/v1/admin/abnormal-reports[/export] 的网关侧验证。
//
// 为什么需要本文件：check-routes.sh 只比对 user-service 的路由表，alert-service 的新路由
// 没有 CI 门禁会发现「服务已实现、网关漏转发」或「网关已转发、RBAC 漏登记（T260 默认 403）」。
// 汇总/导出都是**跨患者聚合 + 全字段明细**，一旦漏登记就是患者 token 能拉别人家报告，
// 所以这里既断言放行侧（admin/doctor/技师 + 参数原样透传），也断言拒绝侧（患者、客服 403 且不触达后端）。
//
// T425 口径变更（Boss 09-27 拍 A，PRD §7D.11 权限矩阵第 4 行「异常报告」客服列为 —）：
// 这两条端点从 staffOnlyPatterns 拆出，改由 abnormalReportPatterns + abnormalReportRoles
// （admin/doctor/technician）单独收口。客服此前是**经 staffOnlyPatterns 放行**的（T300 当轮
// 的 4 角色放行循环就在这里），本文件按新口径改写；不改一刀切摘 roleCS —— 那张集合还被
// 20+ 条 staff 端点共用（T421 工程线实测连坐 13 条用例）。
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

// ① RBAC 登记：命中 abnormalReportPatterns 专用白名单 ⇒ 不走 T260 默认拒绝；
// 且不得回落到 staffOnlyPatterns（那等于把客服重新放行）、不得是 admin-only、不得对患者开放。
func TestRBAC_T300_AbnormalReportPatternsAreRegistered(t *testing.T) {
	for _, path := range []string{t300ReportPath, t300ExportPath} {
		assert.True(t, matchAbnormalReportPattern(http.MethodGet, path),
			"GET %s 必须登记进 abnormalReportPatterns，否则 roleAuthz 默认 403", path)
		assert.False(t, matchStaffOnlyPattern(http.MethodGet, path),
			"T425：%s 不得留在 staffOnlyPatterns（该矩阵对 ROLE_CS 放行，客服收口会失效）", path)
		assert.False(t, matchRBACPattern(http.MethodGet, path), "%s 不得是 admin-only（医生/康复师也要出报告）", path)
		assert.False(t, matchPublicPattern(http.MethodGet, path), "%s 不得对患者开放", path)
	}
}

// ② 反代登记 + 角色判定：admin/doctor/技师放行且打到 alert-service（查询参数原样保留），
// 客服与患者 403 且不触达后端。
func TestRBAC_T300_AbnormalReportStaffPassPatientDenied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, path := range []string{t300ReportPath, t300ExportPath} {
		for _, role := range []string{roleAdmin, roleDoctor, roleTech} {
			code, body := httpDoFull(t, http.MethodGet, gw.URL+path+t300Query, "", rbacToken(t, role))
			require.Equal(t, http.StatusOK, code, "role=%s path=%s 应放行，body=%s", role, path, body)
		}
	}
	require.Len(t, *received, 6, "2 个端点 × 3 个放行角色均须真正打到 alert-service（代理未注册则这里为 0）")
	for _, got := range *received {
		// 路径与查询参数都必须原样保留：patientId/start/end 任一被吞掉，服务侧就是另一个口径的报告
		assert.Contains(t, got, "?patientId=P001&start=2026-09-01&end=2026-09-03", "查询参数须透传，实收 %q", got)
		assert.Contains(t, got, "GET /api/v1/admin/abnormal-reports", "转发路径必须原样保留，实收 %q", got)
	}

	// T425 拒绝腿：客服 + 患者 + 角色缺失（X-Role 空串）三种，两条端点各打一遍
	before := len(*received)
	for _, path := range []string{t300ReportPath, t300ExportPath} {
		for _, role := range []string{roleCS, rolePatient, ""} {
			code, body := httpDoFull(t, http.MethodGet, gw.URL+path+t300Query, "", rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code, "role=%q 不得读异常报告，path=%s body=%s", role, path, body)
			assert.Contains(t, body, `"code":403`)
		}
	}
	assert.Len(t, *received, before, "被拒请求不得触达后端（实收 %d 条，起于 %d 条）", len(*received), before)
}

// ③ 收口不外溢：客服在其余 staff 端点上照旧放行。
// 本用例是 T425「按端点收口」与「一刀切摘 roleCS」的分界证据——后者会让这里红。
func TestRBAC_T425_CSStillAllowedOnOtherStaffEndpoints(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/feedbacks"},
		{http.MethodGet, "/api/v1/admin/patients"},
		{http.MethodGet, "/api/v1/alerts"},
	}
	for _, c := range cases {
		code, body := httpDoFull(t, c.method, gw.URL+c.path, "", rbacToken(t, roleCS))
		assert.NotEqual(t, http.StatusForbidden, code, "客服的沟通/患者域端点不得被本次收口连坐：%s %s body=%s", c.method, c.path, body)
	}
	require.Len(t, *received, len(cases), "三条客服放行请求都要真的打到后端")
}
