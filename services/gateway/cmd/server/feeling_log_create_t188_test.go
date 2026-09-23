// Package main — T188 gateway 侧取证：患者端佩戴感受录入端点的挂载与 RBAC
//
// POST /api/v1/patients/:patientId/feeling-logs 登记进 publicPatterns（患者写本人、
// staff 可代录），水平鉴权在 user-service handler 层 assertAdminOrSelf，不在网关层。
// 同址读端点与后台跨患者流（GET /admin/feeling-logs）的边界不变 —— 本文件把这两条钉住。
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t188CreatePath = "/api/v1/patients/P20260001/feeling-logs"

func t188JWT(t *testing.T, subject, role string) map[string]string {
	t.Helper()
	tok := signTestJWT(t, testJWTSecretMain, subject, role, time.Now().Add(time.Hour).Unix())
	return map[string]string{"Authorization": "Bearer " + tok}
}

const t188Body = `{"logDate":"2026-09-23","feeling":"discomfort","discomfortAreas":["胸椎"],"notes":"胸椎处压得疼"}`

// TestT188_FeelingCreate_PatientProxied 患者 token 命中新路由并透传。
// 网关对 user-service 是 default-deny：漏登记时这里直接 403，比端到端验收先暴露。
func TestT188_FeelingCreate_PatientProxied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, body := httpDoFull(t, "POST", gw.URL+t188CreatePath, t188Body, t188JWT(t, "P20260001", "patient"))
	t.Logf("patient → HTTP %d %s｜后端实收 %v", code, body, *received)

	require.Equal(t, 200, code, "患者录自己的佩戴感受不得被网关拦下")
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "POST "+t188CreatePath)
	assert.Contains(t, (*received)[0], "uid=P20260001", "网关须把 JWT sub 注入 X-User-Id 供后端做 self-scope")
}

// TestT188_FeelingCreate_StaffProxied 医生/管理员代录在网关层放行（后端不限制 staff）
func TestT188_FeelingCreate_StaffProxied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{"ROLE_ADMIN", "ROLE_DOCTOR"} {
		code, body := httpDoFull(t, "POST", gw.URL+t188CreatePath, t188Body, t188JWT(t, "U-"+role, role))
		t.Logf("role=%s → HTTP %d %s", role, code, body)
		assert.Equal(t, 200, code, "staff 角色 %s 应可代录感受", role)
	}
	assert.Len(t, *received, 2)
}

func TestT188_FeelingCreate_NoToken_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, _ := httpDoFull(t, "POST", gw.URL+t188CreatePath, t188Body, nil)
	assert.Equal(t, 401, code)
	assert.Empty(t, *received, "未鉴权请求不得触达 user-service")
}

// TestT188_FeelingCreate_RegisteredInPublicMatrix 防回归：登记位置也要钉住。
// 挪进 doctorAdminOnlyPatterns ⇒ 患者端保存静默 403；挪进 staffOnlyPatterns 同理。
func TestT188_FeelingCreate_RegisteredInPublicMatrix(t *testing.T) {
	assert.True(t, matchPublicPattern("POST", t188CreatePath))
	assert.False(t, matchStaffOnlyPattern("POST", t188CreatePath))
	assert.False(t, matchDoctorAdminPattern("POST", t188CreatePath))
}

// TestT188_AdminRead_StillStaffOnly 跨患者日志流的读边界不随本卡变化
func TestT188_AdminRead_StillStaffOnly(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, _ := httpDoFull(t, "GET", gw.URL+"/api/v1/admin/feeling-logs", "", t188JWT(t, "P20260001", "patient"))
	assert.Equal(t, 403, code, "GET /admin/feeling-logs 仍是 staff 专属，不得随写端点一起放开")
	assert.Empty(t, *received)
}
