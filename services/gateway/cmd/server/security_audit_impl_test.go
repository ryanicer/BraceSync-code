// Package main — T023 安全审计 Part A · JWT/RBAC/跨域安全测试（不与既有测试文件重叠）
//
// 对齐：docs/ 鉴权面 + RBAC 越权面）
// 审计报告：docs/ T023-Hx 在此引用）
//
// 覆盖：alg=none 降级攻击 / alg 混淆（RS256）/ payload 篡改提权 / exp 边界 /
// 三种 role（admin/doctor/cs）载荷语义转发 / 跨域互斥（JWT≠设备签名）/
// RBAC 垂直越权 KNOWN_RED 预置（当前实现未强制角色，修复后取消 Skip 即转绿）。
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// secSignRawJWT 手工组装 JWT（header 任意指定，用于构造攻击载荷）
func secSignRawJWT(t *testing.T, secret, headerJSON string, payload map[string]any) string {
	t.Helper()
	payloadRaw, err := json.Marshal(payload)
	require.NoError(t, err)
	signingInput := base64.RawURLEncoding.EncodeToString([]byte(headerJSON)) + "." +
		base64.RawURLEncoding.EncodeToString(payloadRaw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func secPayload(sub, role string, exp int64) map[string]any {
	return map[string]any{
		"sub": sub, "username": "admin", "name": "管理员", "role": role,
		"iat": exp - 3600, "exp": exp,
	}
}

// TestSecJWT_AlgNone_DowngradeAttack_401 alg=none 降级攻击必须被拒绝
func TestSecJWT_AlgNone_DowngradeAttack_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	// alg=none + 空签名段：经典降级攻击载荷（攻击者赌服务端跳过验签）
	payloadRaw, err := json.Marshal(secPayload("ADM001", "ROLE_ADMIN", time.Now().Add(time.Hour).Unix()))
	require.NoError(t, err)
	token := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(payloadRaw) + "."

	code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "",
		map[string]string{"Authorization": "Bearer " + token})
	assert.Equal(t, http.StatusUnauthorized, code, "alg=none 必须拒绝")
	assert.Contains(t, body, `"code":401`)
	assert.Empty(t, *received, "降级攻击不得触达后端")
}

// TestSecJWT_AlgConfusion_RS256Header_401 非 HS256 算法声明一律拒绝（防算法混淆）
func TestSecJWT_AlgConfusion_RS256Header_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, alg := range []string{"RS256", "HS512", "HS1", "rs256"} {
		token := secSignRawJWT(t, testJWTSecretMain, `{"alg":"`+alg+`","typ":"JWT"}`,
			secPayload("ADM001", "ROLE_ADMIN", time.Now().Add(time.Hour).Unix()))
		code, _ := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "",
			map[string]string{"Authorization": "Bearer " + token})
		assert.Equal(t, http.StatusUnauthorized, code, "alg=%s 必须拒绝（仅允许 HS256）", alg)
	}
	assert.Empty(t, *received)
}

// TestSecJWT_PayloadTampering_PrivilegeEscalation_401 篡改载荷提权（role 改写）必须被拒绝
func TestSecJWT_PayloadTampering_PrivilegeEscalation_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	// 合法签发 ROLE_CS token，然后把 payload 的 role 改成 ROLE_ADMIN（保留原签名）
	token := signTestJWT(t, testJWTSecretMain, "CS001", "ROLE_CS", time.Now().Add(time.Hour).Unix())
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	tampered := base64.RawURLEncoding.EncodeToString([]byte(
		`{"sub":"CS001","username":"admin","name":"管理员","role":"ROLE_ADMIN",` +
			`"iat":` + secJSONInt(time.Now().Unix()) + `,"exp":` + secJSONInt(time.Now().Add(time.Hour).Unix()) + `}`))
	forged := parts[0] + "." + tampered + "." + parts[2]

	code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/settings", "",
		map[string]string{"Authorization": "Bearer " + forged})
	assert.Equal(t, http.StatusUnauthorized, code, "篡改 payload 提权必须验签失败")
	assert.Contains(t, body, `"code":401`)
	assert.Empty(t, *received)
}

// secJSONInt int64 → JSON 数字文本（避免 strconv 依赖的显式转换）
func secJSONInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestSecJWT_ExpBoundary_ExpiredAndZero 过期（exp<now）与零值 exp 必须拒绝
func TestSecJWT_ExpBoundary_ExpiredAndZero(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	cases := map[string]map[string]any{
		"过期1秒":  secPayload("A", "ROLE_ADMIN", time.Now().Add(-time.Second).Unix()),
		"exp=0": secPayload("A", "ROLE_ADMIN", 0),
	}
	for name, payload := range cases {
		token := secSignRawJWT(t, testJWTSecretMain, `{"alg":"HS256","typ":"JWT"}`, payload)
		code, _ := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "",
			map[string]string{"Authorization": "Bearer " + token})
		assert.Equal(t, http.StatusUnauthorized, code, name)
	}
}

// TestSecJWT_ThreeRoles_ForwardedVerbatim admin/doctor/cs 三种 role 语义：网关只鉴权不改写，
// 身份头按 claims 原样注入后端（后端授权职责见 KNOWN_RED 用例）
func TestSecJWT_ThreeRoles_ForwardedVerbatim(t *testing.T) {
	for _, role := range []string{"ROLE_ADMIN", "ROLE_DOCTOR", "ROLE_CS"} {
		backend, received := captureBackend(t)
		gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

		token := signTestJWT(t, testJWTSecretMain, "U-"+role, role, time.Now().Add(time.Hour).Unix())
		code, _ := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "",
			map[string]string{"Authorization": "Bearer " + token})
		require.Equal(t, http.StatusOK, code, "role=%s 有效 token 应通过鉴权", role)
		require.Len(t, *received, 1)
		assert.Contains(t, (*received)[0], "uid=U-"+role, "X-User-Id 取 claims.sub")
		assert.Contains(t, (*received)[0], "role="+role, "X-Role 按 claims 原样注入")
	}
}

// TestSecJWT_CrossDomain_JWTCannotAccessDeviceRoute JWT（admin 域凭据）不得用于设备域端点
func TestSecJWT_CrossDomain_JWTCannotAccessDeviceRoute(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	token := signTestJWT(t, testJWTSecretMain, "ADM001", "ROLE_ADMIN", time.Now().Add(time.Hour).Unix())
	body := `{"pressures":[1]}`
	for _, path := range []string{"/api/v1/device/records", "/api/v1/device/records/batch"} {
		code, respBody := httpDoFull(t, http.MethodPost, gw.URL+path, body,
			map[string]string{"Authorization": "Bearer " + token})
		assert.Equal(t, http.StatusUnauthorized, code, "JWT 打设备端点 %s 必须拒绝（设备域走 HMAC 验签）", path)
		assert.Contains(t, respBody, `"code":20401`, path)
	}
	assert.Empty(t, *received, "设备域只认设备验签，JWT 不得放行")
}

// TestSecJWT_CrossDomain_DeviceHeadersCannotAccessAdminRoute 设备签名头不得用于 admin 域端点
func TestSecJWT_CrossDomain_DeviceHeadersCannotAccessAdminRoute(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	// 携带完整设备签名头但无 Authorization → JWT 中间件拒绝
	hdrs := deviceHeaders("dev-secret-abc", "GET", "/api/v1/admin/patients", "", time.Now())
	code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "", hdrs)
	assert.Equal(t, http.StatusUnauthorized, code, "设备签名头不得替代 JWT")
	assert.Contains(t, body, `"code":401`)
	assert.Empty(t, *received)
}

// TestSecRBAC_LowRole_AccessAdminOnlyEndpoint KNOWN_RED：低角色访问管理端点应被拒。
//
// KNOWN_RED(T023-H2，垂直越权)：当前全链路无端点级角色强制（gateway 只鉴权不授权，
// user-service 端点不读 X-Role，rbac 包为 T003 桩）——ROLE_CS/ROLE_DOCTOR token
// 可调用任意 admin 域端点（PUT /admin/settings、PUT /admin/roles/:roleId/permissions 等）。
// 修复方实现端点级 RBAC 后删除本 Skip，用例即转绿。详见 T023-安全审计报告.md §高危 H2。
func TestSecRBAC_LowRole_AccessAdminOnlyEndpoint(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	cases := []struct {
		role, method, path string
	}{
		{"ROLE_CS", http.MethodPut, "/api/v1/admin/settings"},
		{"ROLE_CS", http.MethodPut, "/api/v1/admin/roles/ROLE_ADMIN/permissions"},
		{"ROLE_DOCTOR", http.MethodPut, "/api/v1/admin/settings"},
		{"ROLE_DOCTOR", http.MethodDelete, "/api/v1/admin/technicians/TECH001"},
	}
	for _, c := range cases {
		token := signTestJWT(t, testJWTSecretMain, "LOW-"+c.role, c.role, time.Now().Add(time.Hour).Unix())
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`,
			map[string]string{"Authorization": "Bearer " + token})
		assert.Contains(t, []int{http.StatusForbidden, http.StatusUnauthorized}, code,
			"role=%s %s %s 必须拒绝（垂直越权）", c.role, c.method, c.path)
	}
	assert.Empty(t, *received, "越权请求不得触达后端")
}
