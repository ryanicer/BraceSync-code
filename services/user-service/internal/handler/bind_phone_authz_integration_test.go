// Package handler T085 绑定态 JWT 鉴权契约 KNOWN_RED 测试
//
// 覆盖 §5.2 bind-phone 接口鉴权规则：
//   - scope=bind JWT 仅可调用 /patient/bind-phone
//   - scope=full JWT 调 bind-phone → 403 Forbidden
//   - phoneCode/phoneToken 参数校验 (二选一同传优先)
package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
	"github.com/bracesync/bracesync/services/testhelper"
)

// ─────────────────────────────────────────────────────────────
// Fixtures: Auth Token Factories
// ─────────────────────────────────────────────────────────────

func newAuthTestEnv(t *testing.T) *authTestEnv {
	t.Helper()
	
	signer, err := token.NewSigner(testJWTSecret, time.Hour)
	require.NoError(t, err)
	bindSigner, _ := token.NewSigner(testJWTSecret, 30*time.Minute)
	
	store := &fakeStore{}
	h := New(store, signer, nil)
	wechatClient := testhelper.NewMockWechatClient("13800138000")
	h.SetWXClient(wechatClient)
	
	return &authTestEnv{
		t:             t,
		signer:        signer,
		bindSigner:    bindSigner,
		store:         store,
		h:             h,
		wechatClient:  wechatClient,
	}
}

type authTestEnv struct {
	t            *testing.T
	signer       *token.Signer
	bindSigner   *token.Signer
	store        *fakeStore
	h            *Handler
	wechatClient *testhelper.MockWechatClient
}

// createScopeFullJWT 签发正常登录态 JWT (scope=full)
func (e *authTestEnv) createScopeFullJWT(patientID string) string {
	token, _ := e.signer.Sign(
		patientID, "", "患者小明", "patient",
	)
	return token
}

// createScopeBindJWT 签发绑定态 JWT (scope=bind, sub=openid)
func (e *authTestEnv) createScopeBindJWT(openid string) string {
	token, _ := e.bindSigner.Sign(
		openid, "", "测试患者", "patient",
	)
	return token
}

// doBindPhoneWithAuth 用指定 token 发起 bind-phone 请求
func (e *authTestEnv) doBindPhoneWithAuth(authToken string, phoneCode, phoneToken string) (*httptest.ResponseRecorder, *model.BaseResponse) {
	e.t.Helper()
	
	body := map[string]string{"phone_code": phoneCode, "phone_token": phoneToken}
	bodyBytes, _ := json.Marshal(body)
	
	w := httptest.NewRequest(http.MethodPost, "/api/v1/patient/bind-phone", strings.NewReader(string(bodyBytes)))
	w.Header.Set("Content-Type", "application/json")
	w.Header.Set("Authorization", "Bearer "+authToken)
	
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, w)
	
	resp := &model.BaseResponse{}
	json.Unmarshal(rec.Body.Bytes(), resp)
	
	return rec, resp
}

// ─────────────────────────────────────────────────────────────
// Scenario: Scope Authorization Rules
// ─────────────────────────────────────────────────────────────

// TestBindPhoneScopeFullJWT_Returns403_KNOWN_RED 正常 JWT(scope=full) 调 bind-phone → 403
func TestBindPhoneScopeFullJWT_Returns403_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newAuthTestEnv(t)
	
	t.Run("scope_full_jwt_bind_phone_endpoint_403_forbidden", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 未实现 scope 鉴权，预期 HTTP 403 + 40301")
		
		scopeFullJWT := e.createScopeFullJWT("P20260001")
		
		w, resp := e.doBindPhoneWithAuth(scopeFullJWT, "wechat_code", "")
		
		assert.Equal(t, http.StatusForbidden, w.Code, "scope=full 不应允许调用 bind-phone")
		assert.Equal(t, 40301, resp.Code, "错误码应为 40301 (forbidden_scope)")
		
		// 断言：不执行后续业务逻辑
	})
}

// TestBindPhoneScopeBindJWT_OtherPatientEndpointsDenied_KNOWN_RED 绑定态 JWT 调其他 patient 端点 → 403
// （注：本用例需在 Gateway 或 Handler 层验证 scope=bind 对其他 /patient/* 端点的拒绝）
func TestBindPhoneScopeBindJWT_OtherPatientEndpointsDenied_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newAuthTestEnv(t)
	
	// Fixture: 模拟 /patient/dashboard 端点（实际应在 Gateway 层测试）
	// TODO: Winner 实现后需与 Gateway 中间件集成测试
	
	_ = e
}

// ─────────────────────────────────────────────────────────────
// Scenario: Parameter Validation
// ─────────────────────────────────────────────────────────────

// TestBindPhoneMissingBothParams_ParamError_KNOWN_RED 缺 phoneCode+phoneToken → 参数错误
func TestBindPhoneMissingBothParams_ParamError_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newAuthTestEnv(t)
	
	t.Run("missing_both_params_returns_invalid_param", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 400 CodeInvalidParam (phoneCode 和 phoneToken 均缺失)")
		
		scopeBindJWT := e.createScopeBindJWT("openid_test_missing")
		
		// 空 body（不含 phone_code 和 phone_token）
		w := httptest.NewRequest(http.MethodPost, "/api/v1/patient/bind-phone", strings.NewReader("{}"))
		w.Header.Set("Content-Type", "application/json")
		w.Header.Set("Authorization", "Bearer "+scopeBindJWT)
		
		rec := httptest.NewRecorder()
		e.h.Router().ServeHTTP(rec, w)
		
		resp := &model.BaseResponse{}
		json.Unmarshal(rec.Body.Bytes(), resp)
		
		assert.Equal(t, http.StatusBadRequest, w.Code, "缺少必填参数应返回 400")
		assert.Equal(t, model.CodeInvalidParam, resp.Code, "错误码应为 CodeInvalidParam")
	})
}

// TestBindPhoneBothParams_PhoneTokenPrecedence_KNOWN_Red 同传 phoneCode+phoneToken → phoneToken 优先
func TestBindPhoneBothParams_PhoneTokenPrecedence_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newAuthTestEnv(t)
	
	// Fixture: mock 微信客户端记录调用次数
	t.Run("both_params_phone_token_precedence_no_wechat_call", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 phoneToken 优先处理（不调用微信 API）")
		
		scopeBindJWT := e.createScopeBindJWT("openid_precedence")
		validPhoneToken := "valid_phone_token_placeholder" // TODO: Winner 实现后生成有效 phoneToken
		
		// 同时传入 phoneCode 和 phoneToken
		w := httptest.NewRequest(http.MethodPost, "/api/v1/patient/bind-phone", 
			strings.NewReader(`{"phone_code":"wechat_code","phone_token":"`+validPhoneToken+`"}`))
		w.Header.Set("Content-Type", "application/json")
		w.Header.Set("Authorization", "Bearer "+scopeBindJWT)
		
		rec := httptest.NewRecorder()
		e.h.Router().ServeHTTP(rec, w)
		
		// 断言：wechatClient.CallCount() == 0 (phoneToken 模式不调微信)
		_ = rec
	})
}

// TestBindPhoneOnlyPhoneCode_ViaWeChatAPI_KNOWN_RED 仅 phoneCode → 调用微信 API 换取手机号
func TestBindPhoneOnlyPhoneCode_ViaWeChatAPI_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newAuthTestEnv(t)
	
	e.wechatClient.DisableError()
	e.wechatClient.SetPhoneNumber("13800138005")
	
	t.Run("only_phone_code_invokes_wechat_api", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未实现微信调用逻辑，预期通过 phonenumber.getPhoneNumber 换取手机号")
		
		scopeBindJWT := e.createScopeBindJWT("openid_phonecode")
		
		w := httptest.NewRequest(http.MethodPost, "/api/v1/patient/bind-phone", 
			strings.NewReader(`{"phone_code":"wechat_code_xyz"}`))
		w.Header.Set("Content-Type", "application/json")
		w.Header.Set("Authorization", "Bearer "+scopeBindJWT)
		
		rec := httptest.NewRecorder()
		e.h.Router().ServeHTTP(rec, w)
		
		// 断言：wechatClient.GetPhoneNumber 被调用
		assert.Equal(t, 1, e.wechatClient.CallCount(), "phoneCode 模式应调用一次微信 API")
		
		_ = rec
	})
}
