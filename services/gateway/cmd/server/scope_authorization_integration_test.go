// Package main T085 Gateway Scope 鉴权中间件契约 KNOWN_RED 测试
//
// 覆盖 §5.4 Gateway scope 鉴权：
//   - JWT 经 jwtAuth 解析后注入 X-User-Id / X-Role 头（中间件行为，已实现）
//   - 绑定态 (sub 形如 openid_*) 仅允许 POST /api/v1/patient/bind-phone
//   - 全态 (sub 形如 P\d{8}) 不可调 bind-phone，其余 patient 端点放行
//
// 说明：T088-V2 §5.4 的 scope=bind / scope=full 语义在 gateway 层由「JWT Subject 的格式」
// 区分（绑定态 sub 是 openid，全态 sub 是 PatientID）。现存 middleware 只注入
// X-User-Id / X-Role，不注入 X-Scope，本测试围绕真实中间件行为断言。
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testGatewaySecret = "T085-test-secret-for-gateway-scope-only"

// ─────────────────────────────────────────────────────────────
// Fixtures & Helpers
// ─────────────────────────────────────────────────────────────

// signScopeJWT 按 auth.Claims 同构方式签发 HS256 token。
// scope 语义编码在 Subject：绑定态 sub="openid_xxx"；全态 sub="P\d{8}"。
func signScopeJWT(t *testing.T, secret, sub, role string, exp int64) string {
	t.Helper()
	header := `{"alg":"HS256","typ":"JWT"}`
	payload, err := json.Marshal(map[string]any{
		"sub":      sub,
		"username": "",
		"name":     "患者小明",
		"role":     role,
		"iat":      exp - 3600,
		"exp":      exp,
	})
	require.NoError(t, err)
	signingInput := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// t085CapturedRequest 记录 mock backend 收到的请求关键字段
type t085CapturedRequest struct {
	Method string
	URI    string
	UserID string
	Role   string
	Body   string
}

// t085CaptureBackend 启动一个统一响应 200 的 mock 后端，并记录所有收到的请求。
// 多个 gateway 服务 URL（user/device/data/msg/alert）全指向这一个后端。
func t085CaptureBackend(t *testing.T) (*httptest.Server, *[]t085CapturedRequest) {
	t.Helper()
	var mu sync.Mutex
	received := make([]t085CapturedRequest, 0, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, t085CapturedRequest{
			Method: r.Method,
			URI:    r.URL.RequestURI(),
			UserID: r.Header.Get("X-User-Id"),
			Role:   r.Header.Get("X-Role"),
			Body:   string(body),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":null}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &received
}

// startTestGateway 启动真实 setupRouter，6 个服务全部指向同一 t085CaptureBackend。
func startTestGateway(t *testing.T, jwtSecret string) (*httptest.Server, *[]t085CapturedRequest) {
	t.Helper()
	backend, received := t085CaptureBackend(t)
	t.Setenv("USER_SERVICE_URL", backend.URL)
	t.Setenv("DEVICE_SERVICE_URL", backend.URL)
	t.Setenv("DATA_SERVICE_URL", backend.URL)
	t.Setenv("MSG_SERVICE_URL", backend.URL)
	t.Setenv("ALERT_SERVICE_URL", backend.URL)
	t.Setenv("JWT_SECRET", jwtSecret)
	srv := httptest.NewServer(setupRouter())
	t.Cleanup(srv.Close)
	return srv, received
}

type testRequest struct {
	Method       string
	Path         string
	Body         string
	Headers      map[string]string
	ExpectedCode int
}

// doTestRequest 走真实 HTTP 通道发起请求，返回 (statusCode, body, 后端收到的请求数)
func doTestRequest(t *testing.T, server *httptest.Server, req testRequest) (int, string) {
	t.Helper()
	url := server.URL + req.Path
	httpReq, err := http.NewRequest(req.Method, url, bytes.NewReader([]byte(req.Body)))
	require.NoError(t, err)
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	require.NoError(t, err, "doTestRequest 真实 HTTP 调用不得 connection refused")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	t.Logf("%s %s -> %d", req.Method, req.Path, resp.StatusCode)
	return resp.StatusCode, string(body)
}

// findBackendRequest 在 t085CaptureBackend 记录里找首个匹配 method+path 的请求
func findBackendRequest(received *[]t085CapturedRequest, method, pathContains string) *t085CapturedRequest {
	for i := range *received {
		r := &(*received)[i]
		if r.Method == method && bytes.Contains([]byte(r.URI), []byte(pathContains)) {
			return r
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// Test Cases: X-User-Id Header Injection
// ─────────────────────────────────────────────────────────────

// TestScopeBind_MiddlewareInjectsXScopeHeader
// 绑定态 JWT 经过 gateway → mock backend 应收到 X-User-Id=openid + X-Role=patient。
func TestScopeBind_MiddlewareInjectsXScopeHeader(t *testing.T) {
	gw, received := startTestGateway(t, testGatewaySecret)

	bindToken := signScopeJWT(t, testGatewaySecret, "openid_bind_xxx", "patient", time.Now().Add(30*time.Minute).Unix())

	req := testRequest{
		Method:       http.MethodPost,
		Path:         "/api/v1/patient/bind-phone",
		Body:         `{"phone_code":"wechat_code_xyz"}`,
		Headers:      map[string]string{"Authorization": "Bearer " + bindToken},
		ExpectedCode: http.StatusOK,
	}

	code, _ := doTestRequest(t, gw, req)
	assert.Equal(t, req.ExpectedCode, code, "绑定态可访问 bind-phone 端点")

	got := findBackendRequest(received, http.MethodPost, "/api/v1/patient/bind-phone")
	require.NotNil(t, got, "请求必须触达 user-service 后端")
	assert.Equal(t, "openid_bind_xxx", got.UserID, "X-User-Id 应等于 JWT sub（绑定态 openid）")
	assert.Equal(t, "patient", got.Role, "X-Role 应注入为 patient")
}

// ─────────────────────────────────────────────────────────────
// Test Cases: Scope=Bind Authorization Matrix
// ─────────────────────────────────────────────────────────────

// TestScopeBind_BindPhoneEndpoint_Allowed
func TestScopeBind_BindPhoneEndpoint_Allowed(t *testing.T) {
	gw, _ := startTestGateway(t, testGatewaySecret)
	bindToken := signScopeJWT(t, testGatewaySecret, "openid_allow_xxx", "patient", time.Now().Add(30*time.Minute).Unix())

	req := testRequest{
		Method:       http.MethodPost,
		Path:         "/api/v1/patient/bind-phone",
		Body:         `{"phone_code":"wechat_code_ok"}`,
		Headers:      map[string]string{"Authorization": "Bearer " + bindToken},
		ExpectedCode: http.StatusOK,
	}

	code, _ := doTestRequest(t, gw, req)
	assert.Equal(t, req.ExpectedCode, code, "scope=bind 应允许访问 bind-phone")
}

// TestScopeBind_OtherPatientEndpoints_Forbidden
// 绑定态 JWT 调其他 patient 端点 → 不应触达后端（403 或 404 但不可 200）
func TestScopeBind_OtherPatientEndpoints_Forbidden(t *testing.T) {
	gw, received := startTestGateway(t, testGatewaySecret)
	bindToken := signScopeJWT(t, testGatewaySecret, "openid_forbidden_xxx", "patient", time.Now().Add(30*time.Minute).Unix())

	req := testRequest{
		Method:       http.MethodGet,
		Path:         "/api/v1/patients/P20269999/orthosis-plans",
		Headers:      map[string]string{"Authorization": "Bearer " + bindToken},
		ExpectedCode: http.StatusForbidden,
	}

	code, _ := doTestRequest(t, gw, req)
	assert.NotEqual(t, http.StatusOK, code, "绑定态不可访问其他 patient 端点（不允许 200）")

	got := findBackendRequest(received, http.MethodGet, "/api/v1/patients/P20269999/orthosis-plans")
	assert.Nil(t, got, "绑定态调用其他 patient 端点不应触达后端")
}

// ─────────────────────────────────────────────────────────────
// Test Cases: Scope=Full Authorization Matrix
// ─────────────────────────────────────────────────────────────

// TestScopeFull_BindPhoneEndpoint_Forbidden
// 全态 (PatientID) JWT 调 bind-phone → 应被拒绝（已绑定用户不能再绑定）
func TestScopeFull_BindPhoneEndpoint_Forbidden(t *testing.T) {
	gw, _ := startTestGateway(t, testGatewaySecret)
	fullToken := signScopeJWT(t, testGatewaySecret, "P20260012", "patient", time.Now().Add(8*time.Hour).Unix())

	req := testRequest{
		Method:       http.MethodPost,
		Path:         "/api/v1/patient/bind-phone",
		Body:         `{"phone_code":"wechat_code_full_user"}`,
		Headers:      map[string]string{"Authorization": "Bearer " + fullToken},
		ExpectedCode: http.StatusForbidden,
	}

	code, _ := doTestRequest(t, gw, req)
	assert.Equal(t, req.ExpectedCode, code, "全态 JWT 不可调用 bind-phone")
}

// TestScopeFull_DefaultEndpoints_Allowed
// 全态 JWT 访问默认 patient 端点 → 200 且后端收到 X-User-Id=PatientID
func TestScopeFull_DefaultEndpoints_Allowed(t *testing.T) {
	gw, received := startTestGateway(t, testGatewaySecret)
	fullToken := signScopeJWT(t, testGatewaySecret, "P20260013", "patient", time.Now().Add(8*time.Hour).Unix())

	req := testRequest{
		Method:       http.MethodGet,
		Path:         "/api/v1/patients/P20260013/orthosis-plans",
		Headers:      map[string]string{"Authorization": "Bearer " + fullToken},
		ExpectedCode: http.StatusOK,
	}

	code, _ := doTestRequest(t, gw, req)
	assert.Equal(t, req.ExpectedCode, code, "全态 JWT 可访问默认 patient 端点")

	got := findBackendRequest(received, http.MethodGet, "/api/v1/patients/P20260013/orthosis-plans")
	require.NotNil(t, got, "请求必须触达后端")
	assert.Equal(t, "P20260013", got.UserID, "X-User-Id 应等于 PatientID")
	assert.Equal(t, "patient", got.Role, "X-Role 应为 patient")
}
