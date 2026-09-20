// Package main — T257 2.7 新增写端点 POST /api/v1/alerts/:alertId/processing 的网关侧验证。
//
// 为什么需要本文件：check-routes.sh 只比对 user-service 的路由表，alert-service 的新路由
// 没有任何 CI 门禁会发现「服务已实现、网关漏转发」或「网关已转发、RBAC 漏登记（T260 默认 403）」。
// 这里用真实 setupRouter 链（jwtAuth → roleAuthz → 反代）把两条都断言掉。
//
// 期望口径与 /process 完全一致：staff 四角色可用、患者 403（处理告警是内部动作）。
package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t257ProcessingPath = "/api/v1/alerts/42/processing"

// ① RBAC 登记：命中 staffOnly 矩阵 ⇒ 不走 T260 默认拒绝
func TestRBAC_T257_StartProcessingPatternIsRegistered(t *testing.T) {
	assert.True(t, matchStaffOnlyPattern(http.MethodPost, t257ProcessingPath),
		"POST %s 必须登记进 staffOnlyPatterns，否则 roleAuthz 默认 403", t257ProcessingPath)
	// 与 /process 同组：staff 专属，不是 admin 专属（医生/康复师都要能点「开始处理」）
	assert.False(t, matchRBACPattern(http.MethodPost, t257ProcessingPath), "开始处理不得是 admin-only")
	assert.False(t, matchPublicPattern(http.MethodPost, t257ProcessingPath), "开始处理不得对患者开放")
}

// ② 反代登记 + 角色判定：staff 放行且打到 alert-service，患者 403 且不触达后端
func TestRBAC_T257_StartProcessingStaffPassPatientDenied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{roleAdmin, roleDoctor, roleCS, roleTech} {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+t257ProcessingPath, `{}`, rbacToken(t, role))
		require.Equal(t, http.StatusOK, code, "role=%s 应放行，body=%s", role, body)
	}
	require.Len(t, *received, 4, "4 个 staff 角色均须真正打到 alert-service（代理未注册则这里为 0）")
	for _, got := range *received {
		assert.Contains(t, got, "POST /api/v1/alerts/42/processing", "转发路径必须原样保留，实收 %q", got)
	}

	before := len(*received)
	code, body := httpDoFull(t, http.MethodPost, gw.URL+t257ProcessingPath, `{}`, rbacToken(t, rolePatient))
	assert.Equal(t, http.StatusForbidden, code, "患者不得处理告警，body=%s", body)
	assert.Len(t, *received, before, "患者请求不得触达后端")
}
