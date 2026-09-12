// Package wechat T085 微信 SDK 集成契约测试
//
// 覆盖 §5.3 GetPhoneNumber + AccessTokenManager 设计契约：
//   - biz error → *WechatError（handler 侧映射 10604 invalid_phone_code）
//   - network / 非 2xx → 普通 error（handler 侧映射 10502 wechat_service_unavailable）
//   - singleflight: concurrent access_token fetch = 1 actual API call
//   - force refresh on errcode 40001/42001 (access_token expired)
//
// T166：本文件的 mock 曾把上游 path 注册成 "/phonenumber/getPhoneNumber"——那是微信文档里
// 该 API 的**短名**、不是 URL path，与实现里的同一个错误字面量互相印证，导致 CI 恒绿而线上恒 502。
// 因此 mock 现按**真实 path 字面量**（ground truth，来自微信开放文档，非引用实现的常量）路由，
// 并按真实请求形状（POST + JSON body {"code":...}）取参；另有 TestGetPhoneNumber_WireContract
// 直接断言出站请求。改错 path 会立刻红，而不是再次与 mock 错得一致。
package wechat

import (
	"context"
	"encoding/json"
	"errors"
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

	// realPhonePath 微信开放文档给定的真实上游 path（ground truth）。
	// 刻意写成字面量、且不用 phonePath 常量：测试若引用实现的常量，就退化成同义反复。
	realPhonePath = "/wxa/business/getuserphonenumber"
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

		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/cgi-bin/token" {
			env.tokenCallCount++
			t.Logf("✓ WeChat access_token API call #%d", env.tokenCallCount)
			resp := map[string]interface{}{
				"access_token": env.accessTokenValue,
				"expires_in":   7200,
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.URL.Path == realPhonePath {
			// 验证 access_token（真实接口经 query 传递）
			accessToken := r.FormValue("access_token")

			if accessToken != env.accessTokenValue || env.errorMode {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"errcode": 40001,
					"errmsg":  "invalid access_token",
				})
				return
			}

			// 真实接口经 POST JSON body 传 code（不是 form 字段 phone_code）
			var reqBody struct {
				Code string `json:"code"`
			}
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			code := reqBody.Code

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
			return
		}

		// 未注册 path → 与真实微信一致：HTTP 404 + 空 body（T166 线上正是这个形状）
		http.NotFound(w, r)
	}))

	t.Cleanup(mockServer.Close)

	env.client = NewClientWithBaseURL(testAppID, testAppSecret, mockServer.URL)
	env.mockServer = mockServer

	return env
}

// ─────────────────────────────────────────────────────────────
// Test Cases: 出站请求线格式（T166 防回归主闸）
// ─────────────────────────────────────────────────────────────

// TestGetPhoneNumber_WireContract 断言 GetPhoneNumber 真实发出的请求形状。
// 这是 T166 的直接回归闸：path 写错（如用文档短名 phonenumber.getPhoneNumber）
// 或 method / 参数位置写错，本测试立即红。
func TestGetPhoneNumber_WireContract(t *testing.T) {
	var gotMethod, gotPath, gotQueryToken, gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cgi-bin/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "wire_contract_token",
				"expires_in":   7200,
			})
		default:
			b, _ := io.ReadAll(r.Body)
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotQueryToken = r.URL.Query().Get("access_token")
			gotBody = string(b)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0,
				"errmsg":  "ok",
				"phone_info": map[string]string{
					"phoneNumber":     "18607101885",
					"purePhoneNumber": "18607101885",
					"countryCode":     "86",
				},
			})
		}
	}))
	defer srv.Close()

	cli := NewClientWithBaseURL(testAppID, testAppSecret, srv.URL)

	pure, countryCode, err := cli.GetPhoneNumber(context.Background(), "phone_code_abc")
	require.NoError(t, err, "线格式正确时不应报错")
	assert.Equal(t, "18607101885", pure)
	assert.Equal(t, "86", countryCode)

	assert.Equal(t, http.MethodPost, gotMethod, "获取手机号必须是 POST")
	assert.Equal(t, realPhonePath, gotPath,
		"上游 path 必须是微信真实 path（T166：文档短名 phonenumber.getPhoneNumber 不是 path）")
	assert.Equal(t, "wire_contract_token", gotQueryToken, "access_token 必须经 query 传递")
	assert.JSONEq(t, `{"code":"phone_code_abc"}`, gotBody, "code 必须在 JSON body 里")
}

// TestGetPhoneNumber_UnknownPathMapsToPlainError 复现 T166 线上故障形状：
// 未知 path → 上游 HTTP 404 + 空 body → 返回**普通 error（非 *WechatError）**，
// 因而 handler 侧落到 10502 / HTTP 502。锁死「502 ⟺ 非业务错误」这条映射契约。
func TestGetPhoneNumber_UnknownPathMapsToPlainError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/cgi-bin/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "tok",
				"expires_in":   7200,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound) // 真实微信对未知 path 的行为：404 + 空 body
	}))
	defer srv.Close()

	cli := NewClientWithBaseURL(testAppID, testAppSecret, srv.URL)

	_, _, err := cli.GetPhoneNumber(context.Background(), "any")
	require.Error(t, err)
	var we *WechatError
	assert.False(t, errors.As(err, &we),
		"HTTP 404 不应被当作业务错误（否则会被误映射为 10604 而非 502）")
	assert.Contains(t, err.Error(), "phone http status 404")
}

// ─────────────────────────────────────────────────────────────
// Test Cases: Mock Server Behavior Verification
// ─────────────────────────────────────────────────────────────

// TestWechatMockServer_BizError 业务错误经真实客户端换取 → *WechatError
func TestWechatMockServer_BizError(t *testing.T) {
	t.Parallel()

	env := setupTestEnv(t)

	t.Run("client_surfaces_41401_and_41208_as_wechat_error", func(t *testing.T) {
		for _, tc := range []struct {
			code    string
			wantErr int
		}{
			{"invalid_code", 41401},
			{"used_code", 41208},
		} {
			_, _, err := env.client.GetPhoneNumber(context.Background(), tc.code)
			require.Error(t, err, "code=%s 应返回错误", tc.code)
			var we *WechatError
			require.True(t, errors.As(err, &we), "code=%s 应为 *WechatError, got %T", tc.code, err)
			assert.Equal(t, tc.wantErr, we.ErrCode)
		}
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

		req, _ := http.NewRequest("GET", env.mockServer.URL+realPhonePath+"?phone_code=test", nil)
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

		// 先拿一次有效 token 入缓存
		_, err := env.client.GetAccessToken(context.Background())
		require.NoError(t, err)

		// 切换到错误模式：phonenumber 接口恒返回 errcode=40001
		env.mu.Lock()
		env.errorMode = true
		env.mu.Unlock()

		// 调用 GetPhoneNumber：缓存 token → 40001 → 强制刷新 → 重试仍 40001
		_, _, callErr := env.client.GetPhoneNumber(context.Background(), "test")

		// 强制刷新契约：收到 40001/42001 后应重新请求 access_token，
		// 因此 /cgi-bin/token 调用次数应 >= 2（首次获取 + 强制刷新）。
		env.mu.Lock()
		tokenCalls := env.tokenCallCount
		env.mu.Unlock()
		assert.GreaterOrEqual(t, tokenCalls, 2, "errcode=40001 应触发 access_token 强制刷新")

		// 重试仍失败时必须把微信业务错误原样上抛（*WechatError），
		// 否则 handler 无法区分「code 失效」与「上游不可用」。
		var we *WechatError
		require.True(t, errors.As(callErr, &we), "应返回 *WechatError, got %T (%v)", callErr, we)
		assert.Equal(t, 40001, we.ErrCode)
	})
}
