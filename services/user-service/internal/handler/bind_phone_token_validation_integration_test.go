// Package handler T085 phoneToken 校验契约 KNOWN_RED 测试
//
// 覆盖 §5.2 phoneToken 验证逻辑：
//   - signature tampered → 10605
//   - purpose≠"phone_token" → 10605
//   - openid mismatch → 10605
//   - expired >7d → 10605
//   - valid retry → 零微信调用
package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
	"github.com/bracesync/bracesync/services/testhelper"
)

const testPhoneTokenSecret = "T085-test-secret-for-phonetoken-validation-only"

// ─────────────────────────────────────────────────────────────
// Fixtures: PhoneToken Factories
// ─────────────────────────────────────────────────────────────

func newPhoneTokenTestEnv(t *testing.T) *phoneTokenTestEnv {
	t.Helper()
	
	signer, _ := token.NewSigner(testJWTSecret, time.Hour)
	bindSigner, _ := token.NewSigner(testJWTSecret, 30*time.Minute)
	fc := testhelper.NewFixedClock(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	
	store := &fakeStore{}
	h := New(store, signer, nil)
	wechatClient := testhelper.NewMockWechatClient("13800138000")
	h.SetWXClient(wechatClient)
	
	return &phoneTokenTestEnv{
		t:              t,
		signer:         signer,
		bindSigner:     bindSigner,
		fixedClock:     fc,
		store:          store,
		h:              h,
		wechatClient:   wechatClient,
		phoneTokenSigner: testPhoneTokenSecret, // TODO: Winner 实现后使用 jwt.Signer
	}
}

type phoneTokenTestEnv struct {
	t                  *testing.T
	signer             *token.Signer
	bindSigner         *token.Signer
	fixedClock         *testhelper.FixedClock
	store              *fakeStore
	h                  *Handler
	wechatClient       *testhelper.MockWechatClient
	phoneTokenSigner   string // TODO: Winner 实现后替换为 jwt.Signer
}

// createValidPhoneToken 生成有效的 phoneToken (purpose="phone_token", exp=7d)
func (e *phoneTokenTestEnv) createValidPhoneToken(phoneHash, openID string) string {
	// TODO: Winner 实现后使用 HS256 JWT 格式
	// payload := map[string]interface{}{
	//     "purpose": "phone_token",
	//     "phone_hash": phoneHash,
	//     "openid": openID,
	//     "iat": e.fixedClock.Now().Unix(),
	//     "exp": e.fixedClock.Now().Add(7*24*time.Hour).Unix(),
	// }
	// return jwt.Sign(payload, []byte(e.phoneTokenSigner))
	
	return "valid_phone_token_placeholder_" + openID[len(openID)-6:]
}

// createInvalidPhoneTokenSignatureTampered 生成签名被篡改的 phoneToken
func (e *phoneTokenTestEnv) createInvalidPhoneTokenSignatureTampered(phoneHash, openID string) string {
	// 模拟篡改后的 token（实际应基于有效 token 修改 payload）
	return "tampered_phone_token_" + openID[len(openID)-6:]
}

// createPhoneTokenWrongPurpose 生成 purpose 错误的 phoneToken
func (e *phoneTokenTestEnv) createPhoneTokenWrongPurpose(phoneHash, openID string) string {
	// purpose="wrong_purpose" ≠ "phone_token"
	return "phone_token_with_wrong_purpose_" + openID[len(openID)-6:]
}

// createPhoneTokenOpenidMismatch 生成 openid 不匹配的 phoneToken
func (e *phoneTokenTestEnv) createPhoneTokenOpenidMismatch(phoneHash, openID string) string {
	// phoneToken 含 openid=mismatched_openid
	return "phone_token_mismatch_openid_" + openID[len(openID)-6:]
}

// createPhoneTokenExpired 生成过期 (>7d) 的 phoneToken
func (e *phoneTokenTestEnv) createPhoneTokenExpired(phoneHash, openID string) string {
	// iat=time.Now()-8days (超过 7 天有效期)
	return "expired_phone_token_" + openID[len(openID)-6:]
}

// doBindPhoneWithPhoneToken 用 phoneToken 发起 bind-phone 请求
func (e *phoneTokenTestEnv) doBindPhoneWithPhoneToken(authOpenID, phoneToken string) (*httptest.ResponseRecorder, *model.BaseResponse) {
	e.t.Helper()
	
	body := map[string]string{"phone_token": phoneToken}
	bodyBytes, _ := json.Marshal(body)
	
	w := httptest.NewRequest(http.MethodPost, "/api/v1/patient/bind-phone", strings.NewReader(string(bodyBytes)))
	w.Header.Set("Content-Type", "application/json")
	w.Header.Set("Authorization", "Bearer "+authOpenID)
	
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, w)
	
	resp := &model.BaseResponse{}
	json.Unmarshal(rec.Body.Bytes(), resp)
	
	return rec, resp
}

// ─────────────────────────────────────────────────────────────
// Scenario: Invalid PhoneToken Cases
// ─────────────────────────────────────────────────────────────

// TestBindPhoneInvalidPhoneToken_SignatureTampered_KNOWN_RED signature 被篡改 → 10605
func TestBindPhoneInvalidPhoneToken_SignatureTampered_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newPhoneTokenTestEnv(t)
	
	t.Run("signature_tampered_returns_10605", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未实现 phoneToken 签名校验，预期 10605 invalid_phone_token")
		
		scopeBindJWT := e.createScopeBindJWT("openid_tampered")
		tamperedToken := e.createInvalidPhoneTokenSignatureTampered("hash_xyz", "openid_tampered")
		
		w, resp := e.doBindPhoneWithPhoneToken(scopeBindJWT, tamperedToken)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10605, resp.Code, "错误码应为 10605 (invalid_phone_token)")
	})
}

// TestBindPhoneInvalidPhoneToken_WrongPurpose_KNOWN_Red purpose≠"phone_token" → 10605
func TestBindPhoneInvalidPhoneToken_WrongPurpose_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newPhoneTokenTestEnv(t)
	
	t.Run("purpose_not_phone_token_returns_10605", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未检查 purpose，预期 10605 (purpose 必须等于 phone_token)")
		
		scopeBindJWT := e.createScopeBindJWT("openid_wrong_purpose")
		wrongPurposeToken := e.createPhoneTokenWrongPurpose("hash_xyz", "openid_wrong_purpose")
		
		w, resp := e.doBindPhoneWithPhoneToken(scopeBindJWT, wrongPurposeToken)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10605, resp.Code, "错误码应为 10605")
	})
}

// TestBindPhoneInvalidPhoneToken_OpenidMismatch_KNOWN_RED openid 与绑定态 sub 不一致 → 10605
func TestBindPhoneInvalidPhoneToken_OpenidMismatch_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newPhoneTokenTestEnv(t)
	
	t.Run("openid_mismatch_with_bind_token_sub_returns_10605", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未校验 openid 一致性，预期 10605 (phoneToken.openid 应等于绑定态 sub)")
		
		scopeBindJWT := e.createScopeBindJWT("openid_auth_subject")
		mismatchToken := e.createPhoneTokenOpenidMismatch("hash_xyz", "openid_different_from_auth")
		
		w, resp := e.doBindPhoneWithPhoneToken(scopeBindJWT, mismatchToken)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10605, resp.Code, "错误码应为 10605")
	})
}

// TestBindPhoneInvalidPhoneToken_Expired_KNOWN_RED expired >7d → 10605
func TestBindPhoneInvalidPhoneToken_Expired_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newPhoneTokenTestEnv(t)
	
	t.Run("expired_phone_token_returns_10605", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未检查过期时间，预期 10605 (phoneToken exp 应≤7d)")
		
		scopeBindJWT := e.createScopeBindJWT("openid_expired")
		expiredToken := e.createPhoneTokenExpired("hash_xyz", "openid_expired")
		
		w, resp := e.doBindPhoneWithPhoneToken(scopeBindJWT, expiredToken)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10605, resp.Code, "错误码应为 10605")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario: Valid Retry → Zero WeChat Call
// ─────────────────────────────────────────────────────────────

// TestBindPhoneValidRetry_NoWechatCall_KNOWN_RED 有效 phoneToken 重试 → mock 断言零调微信接口
func TestBindPhoneValidRetry_NoWechatCall_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newPhoneTokenTestEnv(t)
	
	// Fixture: phone_hash 匹配档案但 wx_openid=NULL (等待绑定)
	patient := repo.PatientRow{
		PatientID: "P20260006", Name: "患者小红", 
		PhoneHash: "hash_retry_0_wechat_call", WxOpenid: "", Status: "active",
	}
	e.store.patients = append(e.store.patients, &patient)
	e.store.findPatientByPhoneHash = &patient
	
	t.Run("valid_phone_token_retry_zero_wechat_api_call", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 可能调用微信 API，预期 phoneToken 模式不调用微信接口")
		
		scopeBindJWT := e.createScopeBindJWT("openid_retry_no_wechat")
		validToken := e.createValidPhoneToken("hash_retry_0_wechat_call", "openid_retry_no_wechat")
		
		// 重置调用计数
		e.wechatClient.ResetCount()
		
		w, resp := e.doBindPhoneWithPhoneToken(scopeBindJWT, validToken)
		
		assert.Equal(t, http.StatusOK, w.Code, "有效 phoneToken 应返回 200 success")
		
		// **关键断言**: phoneToken 模式不应调用微信 GetPhoneNumber
		assert.Equal(t, 0, e.wechatClient.CallCount(), "phoneToken 重试模式应零微信调用")
		
		// 预期成功响应
		if w.Code == http.StatusOK && resp.Code == model.CodeOK {
			t.Log("✓ 有效 phoneToken 重试成功且零微信调用")
		}
	})
}
