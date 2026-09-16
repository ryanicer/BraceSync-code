// Package main — T226 gateway 侧取证：PUT /api/v1/patients/:patientId 挂载 + 身份注入
//
// 双层真实 HTTP（httptest.Server：gateway 全量路由 + 模拟 user-service），覆盖：
//   - 新路由确实被代理到 user-service（PUT 方法 + 路径原样透传）；
//   - 患者 JWT 的 sub 覆盖外部伪造的 X-User-Id/X-Role ⇒ user-service 限本人判定的
//     身份头只能是本人在 JWT 里的 patientId（改他人 patientId 的路径参数不变，由 handler 403）；
//   - 无 token → 401，且后端零调用。
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t226PatchPath = "/api/v1/patients/P20260001"

func TestT226_UpdateProfile_ProxiedWithJWTIdentity(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	patientTok := signTestJWT(t, testJWTSecretMain, "P20260001", "patient", time.Now().Add(time.Hour).Unix())
	hdrs := map[string]string{
		"Authorization": "Bearer " + patientTok,
		"X-User-Id":     "P20269999", // 外部伪造：必须被 gateway 剥离后按 JWT sub 重写
		"X-Role":        "ROLE_ADMIN",
	}
	body := `{"name":"患者小明改","heightCm":162}`
	code, respBody := httpDoFull(t, "PUT", gw.URL+t226PatchPath, body, hdrs)

	t.Logf("脱敏 curl:\n  curl -s -X PUT -H 'Authorization: Bearer <jwt %s...>' -H 'X-User-Id: P20269999' -d '%s' '%s%s'",
		patientTok[:12], body, gw.URL, t226PatchPath)
	t.Logf("gateway → 后端实收: %v\n后端响应原文 (HTTP %d): %s", *received, code, respBody)

	require.Equal(t, 200, code)
	require.Len(t, *received, 1, "只应命中 user-service 一次")
	assert.Contains(t, (*received)[0], "PUT "+t226PatchPath, "PUT 方法与路径透传不重写")
	assert.Contains(t, (*received)[0], "uid=P20260001", "X-User-Id 取 JWT sub，伪造值失效")
	assert.Contains(t, (*received)[0], "role=patient", "X-Role 取 JWT role")
	assert.Contains(t, (*received)[0], "患者小明改", "请求体透传")
}

func TestT226_UpdateProfile_NoToken_401_BackendUntouched(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, body := httpDoFull(t, "PUT", gw.URL+t226PatchPath, `{"name":"x"}`, nil)
	t.Logf("无 token 响应原文 (HTTP %d): %s", code, body)

	assert.Equal(t, 401, code)
	assert.Empty(t, *received, "鉴权失败不得触达 user-service")
}
