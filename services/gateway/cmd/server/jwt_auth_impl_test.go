// Package main — T032 JWT 鉴权中间件实现侧测试（不与测试专家/T028/T030 测试文件重叠）
//
// 双层真实 HTTP（httptest.Server：gateway 全量路由 + 模拟后端），覆盖：
// 无 token/非法/过期 → 401 统一响应体；白名单放行；身份头注入与伪造头剥离；
// JWT_SECRET 缺失 fail-closed（T039-H1）；alerts（T028）纳入鉴权。
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testJWTSecretMain = "t032-gateway-test-secret"

// signTestJWT 按 user-service token.Signer 同构方式签发 HS256 token
func signTestJWT(t *testing.T, secret string, sub, role string, exp int64) string {
	t.Helper()
	header := `{"alg":"HS256","typ":"JWT"}`
	payload, err := json.Marshal(map[string]any{
		"sub": sub, "username": "admin", "name": "管理员", "role": role,
		"iat": exp - 3600, "exp": exp,
	})
	require.NoError(t, err)
	signingInput := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// httpDoFull 带请求体与自定义头的请求助手
func httpDoFull(t *testing.T, method, target, body string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

// startFullGateway 全量路由 gateway（setupRouter），四服务 env 指向各自模拟后端
func startFullGateway(t *testing.T, userURL, deviceURL, dataURL, msgURL, alertURL, jwtSecret string) *httptest.Server {
	t.Helper()
	t.Setenv("USER_SERVICE_URL", userURL)
	t.Setenv("DEVICE_SERVICE_URL", deviceURL)
	t.Setenv("DATA_SERVICE_URL", dataURL)
	t.Setenv("MSG_SERVICE_URL", msgURL)
	t.Setenv("ALERT_SERVICE_URL", alertURL)
	if jwtSecret != "" {
		t.Setenv("JWT_SECRET", jwtSecret)
	}
	srv := httptest.NewServer(setupRouter())
	t.Cleanup(srv.Close)
	return srv
}

// captureBackend 记录收到的请求（方法+URI+身份头）并回统一响应体
func captureBackend(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var received []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = append(received, r.Method+" "+r.URL.RequestURI()+
			" uid="+r.Header.Get("X-User-Id")+" role="+r.Header.Get("X-Role")+
			" dev="+r.Header.Get("X-Device-Id")+" body="+string(body))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":null}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &received
}

func validBearer(t *testing.T) map[string]string {
	t.Helper()
	tok := signTestJWT(t, testJWTSecretMain, "ADM001", "ROLE_ADMIN", time.Now().Add(time.Hour).Unix())
	return map[string]string{"Authorization": "Bearer " + tok}
}

func TestJWT_ProtectedEndpoint_NoToken_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	for _, path := range []string{
		"/api/v1/admin/patients", "/api/v1/devices", "/api/v1/alerts",
		"/api/v1/patients/P001/realtime", "/api/v1/admin/notify-rules",
	} {
		code, body := httpDoFull(t, http.MethodGet, gw.URL+path, "", nil)
		assert.Equal(t, http.StatusUnauthorized, code, path)
		assert.Contains(t, body, `"code":401`, path)
	}
	assert.Empty(t, *received, "未鉴权请求不得触达后端")
}

func TestJWT_InvalidAndExpiredToken_401(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	cases := map[string]string{
		"非法token":   "Bearer not.a.jwt",
		"错误secret":  "Bearer " + signTestJWT(t, "wrong-secret", "A", "R", time.Now().Add(time.Hour).Unix()),
		"过期token":   "Bearer " + signTestJWT(t, testJWTSecretMain, "A", "R", time.Now().Add(-time.Hour).Unix()),
		"无Bearer前缀": signTestJWT(t, testJWTSecretMain, "A", "R", time.Now().Add(time.Hour).Unix()),
	}
	for name, auth := range cases {
		code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "",
			map[string]string{"Authorization": auth})
		assert.Equal(t, http.StatusUnauthorized, code, name)
		assert.Contains(t, body, `"code":401`, name)
	}
}

func TestJWT_ValidToken_ForwardsAndInjectsIdentity(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	// 外部伪造 X-User-Id/X-Role 必须被剥离，以网关注入为准
	hdrs := validBearer(t)
	hdrs["X-User-Id"] = "FORGED"
	hdrs["X-Role"] = "FORGED_ROLE"
	code, _ := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients?page=1", "", hdrs)

	require.Equal(t, http.StatusOK, code)
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "GET /api/v1/admin/patients?page=1")
	assert.Contains(t, (*received)[0], "uid=ADM001", "X-User-Id 取 claims.sub")
	assert.Contains(t, (*received)[0], "role=ROLE_ADMIN", "X-Role 取 claims.role")
	assert.NotContains(t, (*received)[0], "FORGED", "伪造身份头必须剥离")
}

func TestJWT_Whitelist_LoginWithoutToken(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, _ := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/auth/login",
		`{"username":"admin","password":"x"}`, nil)
	require.Equal(t, http.StatusOK, code, "登录入口免 JWT")
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "POST /api/v1/auth/login")

	// GET /auth/login 未注册（仅 POST 登录入口）→ 404，且不触达后端
	code, _ = httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/auth/login", "", nil)
	assert.Equal(t, http.StatusNotFound, code)
}

func TestJWT_Whitelist_TechPatientLogin(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	// 技师登录免 JWT
	code, _ := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/tech/login",
		`{"phone":"13800000001","password":"Password1!"}`, nil)
	require.Equal(t, http.StatusOK, code, "技师登录入口免 JWT")
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "POST /api/v1/tech/login")

	// 患者登录免 JWT
	code, _ = httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/patient/login",
		`{"phone":"13800000002","password":"Password1!"}`, nil)
	require.Equal(t, http.StatusOK, code, "患者登录入口免 JWT")
	require.Len(t, *received, 2)
	assert.Contains(t, (*received)[1], "POST /api/v1/patient/login")

	// GET /tech/login 未注册 → 404
	code, _ = httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/tech/login", "", nil)
	assert.Equal(t, http.StatusNotFound, code)
}

func TestJWT_HealthzPublic(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodGet, gw.URL+"/healthz", "", nil)
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `"ok"`)
}

func TestJWT_SecretMissing_FailClosed_401(t *testing.T) {
	// T039-H1（T023-H1 修复）：JWT_SECRET 未配置时 fail-closed——非白名单请求一律 401，
	// 绝不降级放行；登录白名单仍可转发（不依赖密钥）
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, "")

	code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "", nil)
	assert.Equal(t, http.StatusUnauthorized, code, "未配置 JWT_SECRET 时必须拒绝（fail-closed）")
	assert.Contains(t, body, `"code":401`)

	// 白名单登录不受空密钥影响（转发后端，登录自身不依赖网关密钥）
	code, _ = httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/auth/login",
		`{"username":"admin","password":"x"}`, nil)
	assert.Equal(t, http.StatusOK, code, "登录入口免 JWT，空密钥不阻塞")
	require.Len(t, *received, 1, "仅登录请求触达后端")
}
