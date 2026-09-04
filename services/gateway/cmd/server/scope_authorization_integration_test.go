// Package main T085 Gateway Scope 鉴权中间件契约 KNOWN_RED 测试
//
// 覆盖 §5.4 Gateway scope 鉴权：
//   - X-Scope header injection from JWT claims
//   - scope=bind JWT → only /patient/bind-phone allowed
//   - scope=full JWT → all patient endpoints allowed except bind-phone forbidden
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testGatewaySecret = "T085-test-secret-for-gateway-scope-only"

// ─────────────────────────────────────────────────────────────
// Fixtures & Helpers
// ─────────────────────────────────────────────────────────────

// signScopeJWT 按 token.Signer 同构方式签发带 scope 字段的 JWT
func signScopeJWT(t *testing.T, secret string, sub, role, scope string, exp int64) string {
	t.Helper()
	
	header := `{"alg":"HS256","typ":"JWT"}`
	payload, err := json.Marshal(map[string]any{
		"sub":    sub,
		"username": "", // patient 无 username
		"name":     "患者小明",
		"role":     role,
		"scope":    scope,
		"iat":      exp - 3600,
		"exp":      exp,
	})
	require.NoError(t, err)
	
	signingInput := base64URLEncode([]byte(header)) + "." + base64URLEncode(payload)
	mac := hmacSHA256([]byte(testGatewaySecret), []byte(signingInput))
	return signingInput + "." + base64URLEncode(mac)
}

func base64URLEncode(data []byte) string {
	return "" // TODO: Winner 实现后使用 standard base64.RawURLEncoding.EncodeToString
}

func hmacSHA256(key, data []byte) []byte {
	return nil // TODO: Winner 实现后使用 crypto/hmac.New
}

// startTestGateway 启动 gateway with fake backends
func startTestGateway(t *testing.T) *httptest.Server {
	userURL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":null}`))
	}))
	defer userURL.Close()
	
	// TODO: Winner 实现后调用 setupRouter() 并注入真实路由配置
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stub: mock gateway response for now
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"message":"ok"}`))
	}))
}

type testRequest struct {
	Method       string
	Path         string
	Body         string
	Headers      map[string]string
	ExpectedCode int
	ExpectedXScope string // 预期 X-Scope 值（若需要验证）
}

// doTestRequest 发送测试请求
func doTestRequest(t *testing.T, server *httptest.Server, req testRequest) (*httptest.ResponseRecorder, []string) {
	t.Helper()
	
	url := server.URL + req.Path
	
	httpReq, err := http.NewRequest(req.Method, url, bytes.NewReader([]byte(req.Body)))
	require.NoError(t, err)
	
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	
	t.Logf("%s %s → %d", req.Method, req.Path, resp.StatusCode)
	
	return nil, []string{string(body)}
}

// ─────────────────────────────────────────────────────────────
// Test Cases: X-Scope Header Injection
// ─────────────────────────────────────────────────────────────

// TestScopeBind_MiddlewareInjectsXScopeHeader_KNOWN_RED middleware 解析 JWT scope→X-Scope header
func TestScopeBind_MiddlewareInjectsXScopeHeader_KNOWN_RED(t *testing.T) {
	t.Skip("KNOWN_RED: await Winner's implementation")
	t.Parallel()
	
	gw := startTestGateway(t)
	
	// Fixture: 签发 scope=bind 绑定态 JWT
	bindToken := signScopeJWT(t, testGatewaySecret, "openid_bind_xxx", "patient", "bind", time.Now().Add(30*time.Minute).Unix())
	
	req := testRequest{
		Method:  "POST",
		Path:    "/api/v1/patient/bind-phone",
		Body:    `{"phone_code":"wechat_code_xyz"}`,
		Headers: map[string]string{"Authorization": "Bearer " + bindToken},
		ExpectedCode: http.StatusOK,
		ExpectedXScope: "bind",
	}
	
	t.Run("bind_scope_injected_to_x_scope_header", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未实现 scope 解析，预期 X-Scope=bind header 注入")
		
		rec, bodies := doTestRequest(t, gw, req)
		
		assert.Equal(t, http.StatusOK, rec.Code, "成功应返回 200")
		
		// TODO: Winner 实现后断言 backend 收到 X-Scope header
		_ = bodies
	})
}

// ─────────────────────────────────────────────────────────────
// Test Cases: Scope=Bind Authorization Matrix
// ─────────────────────────────────────────────────────────────

// TestScopeBind_BindPhoneEndpoint_Allowed_KNOWN_RED scope=bind → POST /patient/bind-phone 允许
func TestScopeBind_BindPhoneEndpoint_Allowed_KNOWN_RED(t *testing.T) {
	t.Skip("KNOWN_RED: await Winner's implementation")
	t.Parallel()
	
	gw := startTestGateway(t)
	
	bindToken := signScopeJWT(t, testGatewaySecret, "openid_allow_xxx", "patient", "bind", time.Now().Add(30*time.Minute).Unix())
	
	req := testRequest{
		Method:  "POST",
		Path:    "/api/v1/patient/bind-phone",
		Body:    `{"phone_code":"wechat_code_ok"}`,
		Headers: map[string]string{"Authorization": "Bearer " + bindToken},
		ExpectedCode: http.StatusOK,
	}
	
	t.Run("bind_scope_allowed_on_bind_phone_endpoint", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未实现 scope 校验，预期 bind 仅可访问 bind-phone 端点")
		
		rec, _ := doTestRequest(t, gw, req)
		
		// 断言：成功放行
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// TestScopeBind_OtherPatientEndpoints_Forbidden_KNOWN_Red scope=bind → GET /patient/dashboard → 403
func TestScopeBind_OtherPatientEndpoints_Forbidden_KNOWN_RED(t *testing.T) {
	t.Skip("KNOWN_RED: await Winner's implementation")
	t.Parallel()
	
	gw := startTestGateway(t)
	
	bindToken := signScopeJWT(t, testGatewaySecret, "openid_forbidden_xxx", "patient", "bind", time.Now().Add(30*time.Minute).Unix())
	
	req := testRequest{
		Method:  "GET",
		Path:    "/api/v1/patient/dashboard",
		Body:    "",
		Headers: map[string]string{"Authorization": "Bearer " + bindToken},
		ExpectedCode: http.StatusForbidden,
	}
	
	t.Run("bind_scope_denied_on_other_patient_endpoints", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未拒绝 scope=bind 对其他端点的访问，预期 HTTP 403")
		
		rec, _ := doTestRequest(t, gw, req)
		
		assert.Equal(t, http.StatusForbidden, rec.Code, "绑定态不可访问其他 patient 端点")
	})
}

// ─────────────────────────────────────────────────────────────
// Test Cases: Scope=Full Authorization Matrix
// ─────────────────────────────────────────────────────────────

// TestScopeFull_BindPhoneEndpoint_Forbidden_KNOWN_Red scope=full → POST /patient/bind-phone → 403
func TestScopeFull_BindPhoneEndpoint_Forbidden_KNOWN_RED(t *testing.T) {
	t.Skip("KNOWN_RED: await Winner's implementation")
	t.Parallel()
	
	gw := startTestGateway(t)
	
	fullToken := signScopeJWT(t, testGatewaySecret, "P20260012", "patient", "full", time.Now().Add(8*time.Hour).Unix())
	
	req := testRequest{
		Method:  "POST",
		Path:    "/api/v1/patient/bind-phone",
		Body:    `{"phone_code":"wechat_code_full_user"}`,
		Headers: map[string]string{"Authorization": "Bearer " + fullToken},
		ExpectedCode: http.StatusForbidden,
	}
	
	t.Run("full_scope_denied_on_bind_phone_endpoint", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未限制正常 JWT 调用 bind-phone，预期 403 (需绑定态)")
		
		rec, _ := doTestRequest(t, gw, req)
		
		assert.Equal(t, http.StatusForbidden, rec.Code, "正常登录态不可调用绑定点")
		
		// TODO: Winner 实现后断言错误码 40301
	})
}

// TestScopeFull_DefaultEndpoints_Allowed_KNOWN_RED scope=full → GET /patient/dashboard → 200 OK
func TestScopeFull_DefaultEndpoints_Allowed_KNOWN_RED(t *testing.T) {
	t.Skip("KNOWN_RED: await Winner's implementation")
	t.Parallel()
	
	gw := startTestGateway(t)
	
	fullToken := signScopeJWT(t, testGatewaySecret, "P20260013", "patient", "full", time.Now().Add(8*time.Hour).Unix())
	
	req := testRequest{
		Method:  "GET",
		Path:    "/api/v1/patient/dashboard",
		Body:    "",
		Headers: map[string]string{"Authorization": "Bearer " + fullToken},
		ExpectedCode: http.StatusOK,
	}
	
	t.Run("full_scope_allowed_on_default_patient_endpoints", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未实现 scope=full 校验，预期正常登录态可访问所有 patient 端点")
		
		rec, _ := doTestRequest(t, gw, req)
		
		assert.Equal(t, http.StatusOK, rec.Code, "正常 JWT 应允许访问默认端点")
		
		// TODO: Winner 实现后断言 backend 收到 X-User-Id=X-Scope headers
	})
}
