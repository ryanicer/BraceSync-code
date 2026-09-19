// Package main — T248 gateway 侧取证：两条新 user-service 端点的挂载与 RBAC
//
//   - PUT /api/v1/admin/patients/:patientId（4.3 档案编辑）：admin 放行、其余角色入口即 403；
//   - GET /api/v1/feedbacks/stats（7.1 统计栏）：与 GET /feedbacks 同权限面，正常代理。
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t248EditPath  = "/api/v1/admin/patients/P20260001"
	t248StatsPath = "/api/v1/feedbacks/stats"
)

func t248JWT(t *testing.T, subject, role string) map[string]string {
	t.Helper()
	tok := signTestJWT(t, testJWTSecretMain, subject, role, time.Now().Add(time.Hour).Unix())
	return map[string]string{"Authorization": "Bearer " + tok}
}

// TestT248_AdminEditPatient_Proxied admin JWT 命中新路由并透传到 user-service
func TestT248_AdminEditPatient_Proxied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	body := `{"name":"患者小明改","diagnosis":"胸椎右侧凸 28°","cobbAngle":28.5}`
	code, respBody := httpDoFull(t, "PUT", gw.URL+t248EditPath, body, t248JWT(t, "ADM001", "ROLE_ADMIN"))
	t.Logf("gateway → 后端实收: %v\n后端响应原文 (HTTP %d): %s", *received, code, respBody)

	require.Equal(t, 200, code)
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "PUT "+t248EditPath)
	assert.Contains(t, (*received)[0], "role=ROLE_ADMIN")
	assert.Contains(t, (*received)[0], "胸椎右侧凸")
}

// TestT248_AdminEditPatient_NonAdmin403 医生/客服/患者 token 在网关层即拒，后端零调用
func TestT248_AdminEditPatient_NonAdmin403(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, role := range []string{"doctor", "cs", "patient", "technician"} {
		code, body := httpDoFull(t, "PUT", gw.URL+t248EditPath, `{"name":"x"}`, t248JWT(t, "U-"+role, role))
		t.Logf("role=%s → HTTP %d %s", role, code, body)
		assert.Equal(t, 403, code, "角色 %s 不得改他人档案", role)
	}
	assert.Empty(t, *received, "RBAC 拒绝不得触达 user-service")
}

// TestT248_FeedbackStats_Proxied 统计栏端点经 JWT 组正常代理（客服工作台用）
func TestT248_FeedbackStats_Proxied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, respBody := httpDoFull(t, "GET", gw.URL+t248StatsPath, "", t248JWT(t, "CS001", "cs"))
	t.Logf("gateway → 后端实收: %v\n后端响应原文 (HTTP %d): %s", *received, code, respBody)

	require.Equal(t, 200, code)
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "GET "+t248StatsPath)
}

// TestT248_NewRoutes_NoToken_401 未鉴权不得触达后端
func TestT248_NewRoutes_NoToken_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	c, _ := httpDoFull(t, "GET", gw.URL+t248StatsPath, "", nil)
	assert.Equal(t, 401, c)
	c, _ = httpDoFull(t, "PUT", gw.URL+t248EditPath, `{"name":"x"}`, nil)
	assert.Equal(t, 401, c)
	assert.Empty(t, *received)
}
