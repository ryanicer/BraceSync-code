// Package main — T311 gateway 侧取证：患者端反馈创建端点的挂载与 RBAC
//
// POST /api/v1/feedbacks 登记进 publicPatterns（患者 + staff 均可达），
// 水平鉴权（患者仅本人）在 user-service handler 层 assertAdminOrSelf，不在网关层。
// 同域读端点 GET /api/v1/feedbacks 仍留 staffOnlyPatterns —— 本文件把这条边界钉住，
// 防「为了放开写而顺手把读也放开」。
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t311FeedbackPath = "/api/v1/feedbacks"

func t311JWT(t *testing.T, subject, role string) map[string]string {
	t.Helper()
	tok := signTestJWT(t, testJWTSecretMain, subject, role, time.Now().Add(time.Hour).Unix())
	return map[string]string{"Authorization": "Bearer " + tok}
}

const t311CreateBody = `{"patientId":"P20260001","type":"wifi_setup_failure","content":"WiFi 连接失败","status":"pending"}`

// TestT311_FeedbackCreate_PatientProxied 患者 token 命中新路由并透传（未登记矩阵时 default-deny ⇒ 403）
func TestT311_FeedbackCreate_PatientProxied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, body := httpDoFull(t, "POST", gw.URL+t311FeedbackPath, t311CreateBody, t311JWT(t, "P20260001", "patient"))
	t.Logf("patient → HTTP %d %s｜后端实收 %v", code, body, *received)

	require.Equal(t, 200, code, "患者提交配网失败反馈不得被网关拦下")
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "POST "+t311FeedbackPath)
	assert.Contains(t, (*received)[0], "uid=P20260001", "网关须把 JWT sub 注入 X-User-Id 供后端做 self-scope")
	assert.Contains(t, (*received)[0], "role=patient")
}

// TestT311_FeedbackCreate_StaffProxied 客服/管理员代录同样放行（后端按 staff 免 self-scope）
func TestT311_FeedbackCreate_StaffProxied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{"ROLE_ADMIN", "ROLE_CS"} {
		code, body := httpDoFull(t, "POST", gw.URL+t311FeedbackPath, t311CreateBody, t311JWT(t, "U-"+role, role))
		t.Logf("role=%s → HTTP %d %s", role, code, body)
		assert.Equal(t, 200, code, "staff 角色 %s 应可代录反馈", role)
	}
	assert.Len(t, *received, 2)
}

// TestT311_FeedbackCreate_NoToken_401 未鉴权不得触达后端
func TestT311_FeedbackCreate_NoToken_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, _ := httpDoFull(t, "POST", gw.URL+t311FeedbackPath, t311CreateBody, nil)
	assert.Equal(t, 401, code)
	assert.Empty(t, *received)
}

// TestT311_FeedbackRead_StillStaffOnly 读端点边界不变：患者不得读反馈列表
func TestT311_FeedbackRead_StillStaffOnly(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, _ := httpDoFull(t, "GET", gw.URL+t311FeedbackPath, "", t311JWT(t, "P20260001", "patient"))
	assert.Equal(t, 403, code, "GET /feedbacks 仍是 staff 专属，不得随写端点一起放开")
	assert.Empty(t, *received, "RBAC 拒绝不得触达 user-service")
}

// TestT311_FeedbackCreate_RegisteredInPublicMatrix 防回归：路由已登记（网关 default-deny，
// 漏登记 = 患者端提交必挂，而本用例先于端到端验收暴露它）
func TestT311_FeedbackCreate_RegisteredInPublicMatrix(t *testing.T) {
	assert.True(t, matchPublicPattern("POST", t311FeedbackPath))
	assert.False(t, matchStaffOnlyPattern("POST", t311FeedbackPath),
		"创建端点若被挪进 staffOnlyPatterns，患者端提交流会静默 403")
}
