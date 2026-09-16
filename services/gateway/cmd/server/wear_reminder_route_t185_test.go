// Package main — T185 msg-service 患者域代理挂载 + RBAC 收敛测试
//
// 覆盖：
//   - 5 条 msg-service 患者域端点经 gateway JWT 组可达（此前只挂 3 条 admin 端点，患者侧全不通）
//   - 网关按 §5.2 注入 X-User-Id / X-Role，供 msg-service 侧水平鉴权使用
//   - POST subscription-quota/grant 为权益写操作 → adminOnly 矩阵命中，低角色 403 且不触达后端
//   - 其余 4 条患者自查端点不得进 adminOnly 矩阵（否则患者无法管理自己的提醒）
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T185 患者域路径常量
const (
	t185Patient = "P20260001"
	t185Other   = "P9999999"
)

// TestT185_RBAC_GrantAdminOnly_OthersPass 矩阵匹配：额度授予命中 adminOnly，
// 患者自查的 4 条端点必须不命中（否则患者侧功能被自己封死）。
func TestT185_RBAC_GrantAdminOnly_OthersPass(t *testing.T) {
	hit := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/patients/" + t185Patient + "/subscription-quota/grant"},
		{http.MethodPost, "/api/v1/patients/" + t185Other + "/subscription-quota/grant"},
	}
	for _, c := range hit {
		assert.True(t, matchRBACPattern(c.method, c.path), "应命中 adminOnly：%s %s", c.method, c.path)
	}

	miss := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/patients/" + t185Patient + "/wear-reminder"},
		{http.MethodPut, "/api/v1/patients/" + t185Patient + "/wear-reminder"},
		{http.MethodGet, "/api/v1/patients/" + t185Patient + "/subscription-quota"},
		{http.MethodGet, "/api/v1/patients/" + t185Patient + "/notifications"},
	}
	for _, c := range miss {
		assert.False(t, matchRBACPattern(c.method, c.path), "不得误伤患者自查：%s %s", c.method, c.path)
	}
}

// TestT185_PatientRoutesProxied 经真实网关 JWT 组打 5 条端点：
// 4 条患者自查端点转发到 msg-service 并带上网关注入的身份头；grant 低角色 403 不触达后端。
func TestT185_PatientRoutesProxied(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, "http://127.0.0.1:1", "http://127.0.0.1:1",
		"http://127.0.0.1:1", backend.URL, "http://127.0.0.1:1", testJWTSecretMain)

	hdr := func(sub, role string) map[string]string {
		return map[string]string{
			"Authorization": "Bearer " + signTestJWT(t, testJWTSecretMain, sub, role, time.Now().Add(time.Hour).Unix()),
		}
	}

	patientHdr := hdr(t185Patient, "patient")

	// 患者本人：wear-reminder 读写 + 额度查 + 通知列表，全部透传到 msg-service
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/patients/" + t185Patient + "/wear-reminder"},
		{http.MethodPut, "/api/v1/patients/" + t185Patient + "/wear-reminder"},
		{http.MethodGet, "/api/v1/patients/" + t185Patient + "/subscription-quota"},
		{http.MethodGet, "/api/v1/patients/" + t185Patient + "/notifications"},
	}
	for _, tc := range cases {
		code, body := httpDoFull(t, tc.method, gw.URL+tc.path, `{}`, patientHdr)
		require.Equal(t, http.StatusOK, code, "%s %s 应经网关打通", tc.method, tc.path)
		assert.Contains(t, body, `"code":0`)
	}

	require.Len(t, *received, len(cases))
	for i, tc := range cases {
		rec := (*received)[i]
		assert.Contains(t, rec, tc.method+" "+tc.path, "路径/方法原样透传")
		assert.Contains(t, rec, "uid="+t185Patient+" role=patient",
			"网关须注入身份头供 msg-service 做水平鉴权")
	}

	// grant：患者 → 网关 RBAC 403，且不得触达后端
	before := len(*received)
	code, body := httpDoFull(t, http.MethodPost,
		gw.URL+"/api/v1/patients/"+t185Patient+"/subscription-quota/grant", `{}`, patientHdr)
	assert.Equal(t, http.StatusForbidden, code, "患者不得自行授予订阅额度")
	assert.Contains(t, body, "forbidden")
	assert.Len(t, *received, before, "403 必须在网关拦下，不转发后端")

	// grant：ROLE_ADMIN → 放行到后端
	code, _ = httpDoFull(t, http.MethodPost,
		gw.URL+"/api/v1/patients/"+t185Patient+"/subscription-quota/grant", `{}`,
		hdr("ADM001", "ROLE_ADMIN"))
	assert.Equal(t, http.StatusOK, code)
	assert.Len(t, *received, before+1)
}

// TestT185_Unauthenticated_401 未带 token 打新挂载端点 → 网关 401（JWT 组覆盖到本批路由）
func TestT185_Unauthenticated_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, "http://127.0.0.1:1", "http://127.0.0.1:1",
		"http://127.0.0.1:1", backend.URL, "http://127.0.0.1:1", testJWTSecretMain)

	code, _ := httpDoFull(t, http.MethodGet,
		gw.URL+"/api/v1/patients/"+t185Patient+"/wear-reminder", "", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Empty(t, *received, "未鉴权请求不得触达后端")
}
