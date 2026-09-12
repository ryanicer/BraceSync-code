// Package handler T085 患者微信登录绑定契约 KNOWN_RED 测试
//
// 覆盖 §5.1 wx-login 接口改造契约：
//
//	POST /api/v1/patient/wx-login
//
// 预期行为（设计源：docs/tasks/ella/T088-登录绑定设计-V2.md · §5.1）：
//   - openid 已绑定 + active → 200 `{token, patientId, name, role}` (8h normal JWT)
//   - openid 已绑定 + status≠active → HTTP 401 10001 (防枚举)
//   - openid 未绑定 → 不创建患者表行 + 10601 + bindToken (scope=bind, exp=30min)
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
)

const t085TestJWTSecret = "T085-test-secret-for-wxlogin-only-do-not-use-in-prod"

// ─────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────

func newWxLoginEnv(t *testing.T) *wxLoginTestEnv {
	t.Helper()

	// 登录 JWT TTL 对齐 T037 定稿：8h
	signer, err := token.NewSigner(t085TestJWTSecret, 8*time.Hour)
	require.NoError(t, err)

	fc := testhelper.NewFixedClock(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	store := newT085Store()
	h := New(store, signer, nil)
	wechatClient := testhelper.NewMockWechatClient("13800138000")
	h.SetWXClient(&t085WXClient{MockWechatClient: wechatClient})

	return &wxLoginTestEnv{
		t:            t,
		store:        store,
		signer:       signer,
		h:            h,
		fixedClock:   fc,
		wechatClient: testhelper.NewMockWechatClient("13800138000"),
	}
}

type wxLoginTestEnv struct {
	t            *testing.T
	store        *t085Store
	signer       *token.Signer
	h            *Handler
	fixedClock   *testhelper.FixedClock
	wechatClient *testhelper.MockWechatClient
}

// fakeWechatOpenid 模拟微信登录返回的 openid
const fakeWechatOpenid = "openid_ABC123XYZ789"

// do 发起 wx-login HTTP 请求
func (e *wxLoginTestEnv) do(code string) (*httptest.ResponseRecorder, *testResp) {
	e.t.Helper()

	w := httptest.NewRequest(http.MethodPost, "/api/v1/patient/wx-login", strings.NewReader(`{"code":"`+code+`"}`))
	w.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, w)

	resp := &testResp{}
	if err := json.Unmarshal(rec.Body.Bytes(), resp); err != nil {
		e.t.Fatalf("Failed to unmarshal response: %v", err)
	}

	return rec, resp
}

// ─────────────────────────────────────────────────────────────
// Scenario A-1: openid 已绑定 + active → 直接登录
// ─────────────────────────────────────────────────────────────

// TestWxLoginBoundActive_DirectLogin openid 已绑定且档案 active 时直接签发正常 JWT
func TestWxLoginBoundActive_DirectLogin(t *testing.T) {
	t.Parallel()

	e := newWxLoginEnv(t)

	// Fixture: openid 已绑定且 status=active
	e.store.wxPatientByOpenID = map[string]*repo.PatientLoginRow{
		fakeWechatOpenid: {PatientID: "P20260001", Name: "患者小明", Status: "active"},
	}

	t.Run("success_200_with_normal_JWT_and_patientDTO", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 + JWT 8h + patientDTO")

		w, resp := e.do(fakeWechatOpenid)

		assert.Equal(t, http.StatusOK, w.Code, "成功应返回 200")
		assert.Equal(t, model.CodeOK, resp.Code)

		// Token 响应结构校验
		var loginResult model.LoginResultDTO
		require.NoError(t, json.Unmarshal(resp.Data, &loginResult))
		require.NotEmpty(t, loginResult.Token, "响应应包含 JWT token")

		// JWT 签发与验证
		claims, err := e.signer.Verify(loginResult.Token)
		require.NoError(t, err, "JWT 签名应合法")

		assert.Equal(t, "P20260001", claims.Subject, "sub 应等于 patientId")
		assert.Equal(t, "patient", claims.RoleID, "role 应为 patient")

		// JWT 有效期：T037 定稿 8h，token.Signer exp-iat 精确等于 ttl
		ttl := claims.ExpireAt - claims.IssuedAt
		assert.Equal(t, int64(8*3600), ttl, "JWT 有效期应精确等于 8h (T037 定稿)")

		assert.Equal(t, "患者小明", loginResult.Name)
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario A-2: openid 已绑定 + status≠active → 401 防枚举
// ─────────────────────────────────────────────────────────────

// TestWxLoginBoundInactive_Return401 openid 已绑定但档案非 active 时返回 401 统一文案
func TestWxLoginBoundInactive_Return401(t *testing.T) {
	t.Parallel()

	e := newWxLoginEnv(t)

	// Fixture: openid 已绑定但 status=pending
	e.store.wxPatientByOpenID = map[string]*repo.PatientLoginRow{
		fakeWechatOpenid: {PatientID: "P20260001", Name: "患者小明", Status: "pending"},
	}

	t.Run("http401_invalid_credentials_status_inactive", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 HTTP 401 + 10001 (status!=active)")

		w, resp := e.do(fakeWechatOpenid)

		assert.Equal(t, http.StatusUnauthorized, w.Code, "status 异常应返回 401")
		assert.Equal(t, 10001, resp.Code, "错误码应为 10001 (invalid_credentials)")
		assert.Equal(t, "invalid_credentials", resp.Message)

		// 无 token 返回
		assert.Nil(t, resp.Data, "失败不应返回 token")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario B entry: openid 未绑定 → 不创建 + 10601 + bindToken
// ─────────────────────────────────────────────────────────────

// TestWxLoginUnbound_OpenidNotCreated openid 未绑定时禁止自动创建患者记录
func TestWxLoginUnbound_OpenidNotCreated(t *testing.T) {
	t.Parallel()

	e := newWxLoginEnv(t)

	// Fixture: openid 不存在于系统中（wxPatientByOpenID 保持 nil/空）

	t.Run("openid_not_created_table_row_count_unchanged", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500 并创建患者，预期 10601 + bindToken (禁止自动创建)")

		initialCreateCalls := e.store.wxCreateCalls

		w, resp := e.do(fakeWechatOpenid)

		// 断言：未创建患者（CreatePatientByWXOpenID 不应被调用）
		assert.Equal(t, initialCreateCalls, e.store.wxCreateCalls, "未绑定场景不应调用 CreatePatientByWXOpenID")

		// 断言：返回业务码 10601
		assert.Equal(t, http.StatusOK, w.Code, "未绑定场景返回 HTTP 200 + business code")
		assert.Equal(t, 10601, resp.Code, "错误码应为 10601 (patient_not_bound)")

		// 断言：返回 bindToken
		require.NotNil(t, resp.Data, "响应应包含 bindToken data")
	})
}

// TestWxLoginUnbound_ReturnsBindTokenClaims 未绑定场景下发 bindToken 的 claims 布局校验
func TestWxLoginUnbound_ReturnsBindTokenClaims(t *testing.T) {
	t.Parallel()

	e := newWxLoginEnv(t)

	// Fixture: openid 不存在

	t.Run("bindToken_claims_scope_bind_exp_30min", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 不发 token，预期 bindToken (scope=bind, exp=30min)")

		// 使用固定时钟：签发时间为 2026-09-04 12:00:00
		e.fixedClock.Set(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))

		w, resp := e.do(fakeWechatOpenid)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10601, resp.Code)

		// 解析 bindToken
		type BindTokenData struct {
			Token string `json:"token"`
		}
		var data BindTokenData
		require.NoError(t, json.Unmarshal(resp.Data, &data))
		require.NotEmpty(t, data.Token, "响应应包含 bindToken")

		// 验证 bindToken claims
		claims, err := e.signer.Verify(data.Token)
		require.NoError(t, err)

		assert.Equal(t, fakeWechatOpenid, claims.Subject, "sub 应等于 openid")
		assert.Equal(t, "patient", claims.RoleID, "role 应为 patient")

		// exp 校验：30min TTL（bindToken signer 用 30min 创建）
		ttl := claims.ExpireAt - claims.IssuedAt
		assert.Equal(t, int64(30*60), ttl, "bindToken 有效期应精确等于 30min")
	})
}
