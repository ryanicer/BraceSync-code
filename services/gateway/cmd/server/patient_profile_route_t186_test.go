// Package main — T186 gateway 侧取证：GET /api/v1/patient/profile 挂载 + 身份注入
//
// 双层真实 HTTP（httptest.Server：gateway 全量路由 + 模拟 user-service），覆盖：
//   - 新路由确实被代理到 user-service（路径原样透传）；
//   - 患者 JWT 的 sub 覆盖外部伪造的 X-User-Id/X-Role —— 服务侧 self-scope 的查询键
//     因此只能是本人在 JWT 里的 patientId（T186 越权不可能的结构性根据）；
//   - 无 token → 401，且后端零调用。
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t186ProfilePath = "/api/v1/patient/profile"

func TestT186_PatientProfile_ProxiedWithJWTIdentity(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	patientTok := signTestJWT(t, testJWTSecretMain, "P20260001", "patient", time.Now().Add(time.Hour).Unix())
	hdrs := map[string]string{
		"Authorization": "Bearer " + patientTok,
		"X-User-Id":     "P20269999", // 外部伪造：必须被 gateway 剥离后按 JWT sub 重写
		"X-Role":        "ROLE_ADMIN",
	}
	code, body := httpDoFull(t, "GET", gw.URL+t186ProfilePath, "", hdrs)

	t.Logf("脱敏 curl:\n  curl -s -H 'Authorization: Bearer <jwt %s...>' -H 'X-User-Id: P20269999' '%s%s'",
		patientTok[:12], gw.URL, t186ProfilePath)
	t.Logf("gateway → 后端实收: %v\n后端响应原文 (HTTP %d): %s", *received, code, body)

	require.Equal(t, 200, code)
	require.Len(t, *received, 1, "只应命中 user-service 一次")
	assert.Contains(t, (*received)[0], "GET "+t186ProfilePath, "路径透传不重写")
	assert.Contains(t, (*received)[0], "uid=P20260001", "X-User-Id 取 JWT sub，伪造值失效")
	assert.Contains(t, (*received)[0], "role=patient", "X-Role 取 JWT role")
}

func TestT186_PatientProfile_NoToken_401_BackendUntouched(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, body := httpDoFull(t, "GET", gw.URL+t186ProfilePath, "", nil)
	t.Logf("无 token 响应原文 (HTTP %d): %s", code, body)

	assert.Equal(t, 401, code)
	assert.Empty(t, *received, "鉴权失败不得触达 user-service")
}
