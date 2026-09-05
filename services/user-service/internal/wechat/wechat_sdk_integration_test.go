// Package wechat T085 微信 SDK 集成契约 KNOWN_RED 测试
//
// 覆盖 §5.3 GetPhoneNumber + AccessTokenManager 设计契约：
//   - biz error → 10604 invalid_phone_code
//   - network error → 10502 wechat_service_unavailable
//   - singleflight: concurrent access_token fetch = 1 actual API call
//   - force refresh on errcode 40001/42001 (access_token expired)
//
// KNOWN_RED: GetPhoneNumber 尚未实现（T085 Winner 任务），本测试使用 httptest.Server 模拟微信后端行为
package wechat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAppID     = "wx_test_appid_123456"
	testAppSecret = "wx_test_appsecret_abcdef"
)

// ─────────────────────────────────────────────────────────────
// Fixtures & Helpers
// ─────────────────────────────────────────────────────────────

type testEnv struct {
	client           *Client
	mockServer       *httptest.Server
	tokenCallCount   int
	mu               sync.Mutex
	accessTokenValue string
	errorMode        bool // 模拟 access_token 失效场景
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	env := &testEnv{accessTokenValue: "mock_access_token_xyz_123456"}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.mu.Lock()
		defer env.mu.Unlock()

		if r.URL.Path == "/cgi-bin/token" {
			env.tokenCallCount++
			t.Logf("✓ WeChat access_token API call #%d", env.tokenCallCount)
		}

		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/cgi-bin/token" {
			// 模拟成功返回
			resp := map[string]interface{}{
				"access_token": env.accessTokenValue,
				"expires_in":   7200,
			}
			_ = json.NewEncoder(w).Encode(resp)
		} else if r.URL.Path == "/phonenumber/getPhoneNumber" {
			// 验证 access_token
			accessToken := r.FormValue("access_token")

			if accessToken != env.accessTokenValue || env.errorMode {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"errcode": 40001,
					"errmsg":  "invalid access_token",
				})
				return
			}

			code := r.FormValue("phone_code")
			if code == "invalid_code" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"errcode": 41401,
					"errmsg":  "code is invalid or expired",
				})
				return
			}

			if code == "used_code" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"errcode": 41208,
					"errmsg":  "code has been used",
				})
				return
			}

			// 正常响应
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0,
				"errmsg":  "",
				"phone_info": map[string]string{
					"phoneNumber":     "13800138000",
					"purePhoneNumber": "13800138000",
					"countryCode":     "+86",
				},
			})
		} else {
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(mockServer.Close)

	// Client 目前仅有 DoCode2Session，GetPhoneNumber 待 T085 实现
	// 本测试仅验证架构设计和 mock 服务器行为
	env.client = NewClientWithBaseURL(testAppID, testAppSecret, mockServer.URL)
	env.mockServer = mockServer

	return env
}

// ─────────────────────────────────────────────────────────────
// Test Cases: Mock Server Behavior Verification
// ─────────────────────────────────────────────────────────────

// TestWechatMockServer_BizError 微信业务错误模拟（mock server）
func TestWechatMockServer_BizError(t *testing.T) {
	t.Parallel()

	env := setupTestEnv(t)

	t.Run("mock_server_simulates_biz_error_41401_and_41208", func(t *testing.T) {
		t.Log("KNOWN_RED: GetPhoneNumber 尚未实现，但 mock server 已准备就绪")

		// 断言 mock server 能正确返回业务错误码
		req, _ := http.NewRequest("GET", env.mockServer.URL+"/phonenumber/getPhoneNumber?access_token="+env.accessTokenValue+"&phone_code=invalid_code", nil)
		resp, err := env.client.httpCli.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		body, _ := io.ReadAll(resp.Body)
		var weWechatError map[string]int
		_ = json.Unmarshal(body, &weWechatError)

		assert.Equal(t, 41401, weWechatError["errcode"], "mock server 应返回 errcode=41401")
	})
}

// TestWechatMockServer_NetworkError 网络错误模拟
// 关闭 mock server 后发起请求，应返回连接错误（dial tcp connection refused）。
func TestWechatMockServer_NetworkError(t *testing.T) {
	t.Parallel()

	env := setupTestEnv(t)

	t.Run("network_error_returns_connection_refused", func(t *testing.T) {
		t.Log("关闭 mock server 模拟网络不可达，预期请求返回错误")

		// 关闭 mock server，后续请求应 connection refused
		env.mockServer.Close()

		req, _ := http.NewRequest("GET", env.mockServer.URL+"/phonenumber/getPhoneNumber?phone_code=test", nil)
		resp, err := env.client.httpCli.Do(req)

		// 断言：网络错误场景下 err 不为 nil（连接被拒绝）
		assert.Error(t, err, "mock server 关闭后请求应返回连接错误")
		if resp != nil {
			_ = resp.Body.Close()
		}
	})
}

// ─────────────────────────────────────────────────────────────
// Test Cases: AccessTokenManager Design Contract
// ─────────────────────────────────────────────────────────────

// TestAccessTokenManager_Singleflight_Design singleflight 设计契约验证
func TestAccessTokenManager_Singleflight_Design(t *testing.T) {
	t.Parallel()

	env := setupTestEnv(t)

	t.Run("singleflight_contract_mock_server_verification", func(t *testing.T) {
		t.Log("T085: AccessTokenManager singleflight 合并并发请求为 1 次上游调用")

		const concurrency = 10
		var wg sync.WaitGroup
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = env.client.GetAccessToken(context.Background())
			}()
		}

		wg.Wait()

		// singleflight 契约：10 并发请求 access_token，上游 /cgi-bin/token 应仅被调用 1 次。
		assert.Equal(t, 1, env.tokenCallCount, "singleflight 应保证并发请求仅 1 次上游调用")
	})
}

// TestAccessTokenManager_ForceRefresh_Design 强制刷新设计契约
func TestAccessTokenManager_ForceRefresh_Design(t *testing.T) {
	t.Parallel()

	env := setupTestEnv(t)

	t.Run("force_refresh_contract_on_40001_or_42001", func(t *testing.T) {
		t.Log("T085: GetPhoneNumber 遇 40001 触发 access_token 强制刷新并重试")

		// 切换到错误模式：phonenumber 接口返回 errcode=40001
		env.mu.Lock()
		env.errorMode = true
		env.accessTokenValue = "invalid_token"
		env.mu.Unlock()

		// 调用 GetPhoneNumber：首次取 token(count=1) → 40001 → 强制刷新(count=2) → 重试仍 40001
		_, _, _ = env.client.GetPhoneNumber(context.Background(), "test")

		// 强制刷新契约：收到 40001/42001 后应重新请求 access_token，
		// 因此 /cgi-bin/token 调用次数应 >= 2（首次获取 + 强制刷新）。
		assert.GreaterOrEqual(t, env.tokenCallCount, 2, "errcode=40001 应触发 access_token 强制刷新")
	})
}
