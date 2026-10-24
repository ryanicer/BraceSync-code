// T706 厂商凭据面 · 网关侧矩阵（设计稿 §4 的 G 层：M06-M10、M12、M13 + 三条鉴权失败形状 + P-2）。
//
// 真链路：setupRouter（含全局 vendorScopeGate、vendorKeyAuth、JWT 组）→ httptest 假后端。
// handler 层的业务分支（三态批量、跨厂商 404 等）在 device-service 侧另测，这里只判网关裁决。
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const vendorTestPepper = "t706-gateway-test-pepper"

var (
	vendorKeyA       = "bsk_VND000001_" + strings.Repeat("a1", 32)
	vendorKeyB       = "bsk_VND000002_" + strings.Repeat("b2", 32)
	vendorKeyRevoked = "bsk_VND000001_" + strings.Repeat("c3", 32)
	vendorKeyExpired = "bsk_VND000001_" + strings.Repeat("d4", 32)
)

func vendorHash(key string) string {
	mac := hmac.New(sha256.New, []byte(vendorTestPepper))
	mac.Write([]byte(key))
	return hex.EncodeToString(mac.Sum(nil))
}

// vendorBackend 同时扮演 device-service（候选行 / 越权回报）与被代理的业务后端。
type vendorBackend struct {
	mu      sync.Mutex
	proxied []string
	denied  []string
	keys    []map[string]any
}

func newVendorBackend(t *testing.T) (*httptest.Server, *vendorBackend) {
	t.Helper()
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	b := &vendorBackend{keys: []map[string]any{
		{"keyId": "VKAAAA0001", "vendorId": "VND000001", "keyHash": vendorHash(vendorKeyA),
			"status": "active", "expiresAt": future, "vendorStatus": "active"},
		{"keyId": "VKBBBB0002", "vendorId": "VND000002", "keyHash": vendorHash(vendorKeyB),
			"status": "active", "expiresAt": future, "vendorStatus": "active"},
		{"keyId": "VKCCCC0003", "vendorId": "VND000001", "keyHash": vendorHash(vendorKeyRevoked),
			"status": "revoked", "expiresAt": future, "vendorStatus": "active"},
		{"keyId": "VKDDDD0004", "vendorId": "VND000001", "keyHash": vendorHash(vendorKeyExpired),
			"status": "active", "expiresAt": past, "vendorStatus": "active"},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/internal/vendor-keys":
			b.mu.Lock()
			keys := b.keys
			b.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
		case r.URL.Path == "/internal/vendor-denied":
			b.mu.Lock()
			b.denied = append(b.denied, r.Method+" "+r.URL.Path)
			b.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			b.mu.Lock()
			b.proxied = append(b.proxied, fmt.Sprintf("%s %s vendor=%s key=%s",
				r.Method, r.URL.RequestURI(), r.Header.Get("X-Vendor-Id"), r.Header.Get("X-Vendor-Key-Id")))
			b.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, b
}

func startVendorGateway(t *testing.T, backendURL string) *httptest.Server {
	t.Helper()
	t.Setenv("VENDOR_KEY_PEPPER", vendorTestPepper)
	return startFullGateway(t, backendURL, backendURL, backendURL, backendURL, backendURL, testJWTSecretMain)
}

// M06-M09 + M12：厂商 key 打域外端点一律 403 vendor_scope_denied，且不进后端。
func TestVendor_T706_M06_M09_M12_ScopeDenied(t *testing.T) {
	backend, b := newVendorBackend(t)
	gw := startVendorGateway(t, backend.URL)

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/patients"},                                // M06
		{http.MethodGet, "/api/v1/patients/PAT-X/realtime"},                 // M06 压力数据
		{http.MethodGet, "/api/v1/patients/PAT-X/health-reports"},           // M06 异常报告
		{http.MethodGet, "/api/v1/alerts"},                                  // M07
		{http.MethodPost, "/api/v1/alerts/AL-1/process"},                    // M07 处理
		{http.MethodGet, "/api/v1/teams"},                                   // M08
		{http.MethodGet, "/api/v1/admin/users"},                             // M08（未注册也要 403）
		{http.MethodGet, "/api/v1/admin/roles"},                             // M08
		{http.MethodGet, "/api/v1/admin/configs"},                           // M08（未注册也要 403）
		{http.MethodPost, "/api/v1/devices/PRS-ML05-RC-20261024001/bind"},   // M09
		{http.MethodPost, "/api/v1/devices/PRS-ML05-RC-20261024001/rebind"}, // M09
		{http.MethodPost, "/api/v1/devices/PRS-ML05-RC-20261024001/unbind"}, // M09
		{http.MethodPost, "/api/v1/auth/login"},                             // M12
		{http.MethodPost, "/api/v1/tech/login"},                             // M12
		{http.MethodPost, "/api/v1/patient/login"},                          // M12
	}
	for _, c := range cases {
		code, body := httpDoFull(t, c.method, gw.URL+c.path, `{"username":"x","password":"y"}`,
			map[string]string{"Authorization": "Bearer " + vendorKeyA})
		assert.Equal(t, http.StatusForbidden, code, "%s %s", c.method, c.path)
		assert.Contains(t, body, "vendor_scope_denied", "%s %s", c.method, c.path)
		assert.NotContains(t, body, "token", "登录面不得签出 JWT：%s", c.path)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	assert.Empty(t, b.proxied, "域外请求不得触达业务后端")
}

// M10：设备上报族只认 HMAC 四头；厂商 key 不构成设备身份 → 401。
func TestVendor_T706_M10_DeviceReport_401(t *testing.T) {
	backend, _ := newVendorBackend(t)
	gw := startVendorGateway(t, backend.URL)

	for _, path := range []string{"/api/v1/device/records", "/api/v1/device/records/batch"} {
		code, body := httpDoFull(t, http.MethodPost, gw.URL+path, `{}`,
			map[string]string{"Authorization": "Bearer " + vendorKeyA})
		assert.Equal(t, http.StatusUnauthorized, code, path)
		assert.Contains(t, body, `"code":20401`, path) // 设备验签统一业务码，HTTP 层是 401
	}
}

// 三条鉴权失败形状：状态各自成句，且不泄露 key 归属。
func TestVendor_T706_KeyFailureShapes(t *testing.T) {
	backend, _ := newVendorBackend(t)
	gw := startVendorGateway(t, backend.URL)

	cases := map[string]struct {
		key  string
		slug string
	}{
		"revoked":   {vendorKeyRevoked, "key_revoked"},
		"expired":   {vendorKeyExpired, "key_expired"},
		"unknown":   {"bsk_VND000001_" + strings.Repeat("ee", 32), "invalid_key"},
		"malformed": {"bsk_bad_key", "invalid_key"},
	}
	for name, c := range cases {
		code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/vendor/devices", "",
			map[string]string{"Authorization": "Bearer " + c.key})
		assert.Equal(t, http.StatusUnauthorized, code, name)
		assert.Contains(t, body, c.slug, name)
		assert.NotContains(t, body, "VND000001", "错误报文不得泄露厂商号（%s）", name)
	}

	code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/vendor/devices", "", nil)
	assert.Equal(t, http.StatusUnauthorized, code, "无 Authorization")
	assert.Contains(t, body, "invalid_key")
}

// P-2 + 允许面：vendor 组独立验签、注入 X-Vendor-* 三头并剥离外部伪造值。
func TestVendor_T706_AllowedRoutes_Proxy(t *testing.T) {
	backend, b := newVendorBackend(t)
	gw := startVendorGateway(t, backend.URL)

	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/vendor/devices", `{"model":"PRS-ML05-RC","devices":[]}`},
		{http.MethodGet, "/api/v1/vendor/devices", ""},
		{http.MethodGet, "/api/v1/vendor/devices/PRS-ML05-RC-20261024001", ""},
		{http.MethodGet, "/api/v1/vendor/devices/PRS-ML05-RC-20261024001/secret?reason=reflash", ""},
	}
	for _, c := range cases {
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, c.body, map[string]string{
			"Authorization":   "Bearer " + vendorKeyA,
			"X-Vendor-Id":     "VND000002", // 外部伪造：必须被剥离后按 key 重写
			"X-Vendor-Key-Id": "VK-FORGED",
		})
		assert.Equal(t, http.StatusOK, code, "%s %s", c.method, c.path)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	require.Len(t, b.proxied, len(cases))
	for _, line := range b.proxied {
		assert.Contains(t, line, "vendor=VND000001")
		assert.Contains(t, line, "key=VKAAAA0001")
		assert.NotContains(t, line, "VND000002")
		assert.NotContains(t, line, "VK-FORGED")
	}
}

// M13：厂商侧没有 key 管理端点，也不存在批量导出全平台设备的口子。
func TestVendor_T706_M13_KeySelfManageAbsent(t *testing.T) {
	backend, b := newVendorBackend(t)
	gw := startVendorGateway(t, backend.URL)

	cases := []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/api/v1/vendor/keys", http.StatusNotFound},
		{http.MethodPost, "/api/v1/vendor/keys", http.StatusNotFound},
		{http.MethodPost, "/api/v1/vendor/keys/VKAAAA0001/revoke", http.StatusNotFound},
		// POST 与已注册的 GET /vendor/devices/:deviceId 同形 → gin 405；判据是「不是可用端点」
		{http.MethodPost, "/api/v1/vendor/devices/export", http.StatusMethodNotAllowed},
		{http.MethodPut, "/api/v1/vendor/devices/PRS-ML05-RC-20261024001/status", http.StatusNotFound},
	}
	for _, c := range cases {
		code, _ := httpDoFull(t, c.method, gw.URL+c.path, `{}`,
			map[string]string{"Authorization": "Bearer " + vendorKeyA})
		assert.Equal(t, c.status, code, "%s %s", c.method, c.path)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	assert.Empty(t, b.proxied, "不存在的厂商端点不得被代理")
}

// P-1 辅证：同一 ID 跨厂商与不存在在网关层都进后端（404 同形由 device-service 保证），
// 但非本域仍 403 —— 码不同是为了让枚举探测拿不到增量信息。
func TestVendor_T706_P1_CrossVendorVsScope(t *testing.T) {
	backend, _ := newVendorBackend(t)
	gw := startVendorGateway(t, backend.URL)

	code, body := httpDoFull(t, http.MethodGet,
		gw.URL+"/api/v1/vendor/devices/PRS-ML05-RB-20261024001/secret?reason=x", "",
		map[string]string{"Authorization": "Bearer " + vendorKeyA})
	assert.Equal(t, http.StatusOK, code, "跨厂商 404 由 handler 判定，网关只负责放行到本域")
	assert.NotContains(t, body, "VND000002")

	code, body = httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/patients", "",
		map[string]string{"Authorization": "Bearer " + vendorKeyA})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, body, "vendor_scope_denied")
}
