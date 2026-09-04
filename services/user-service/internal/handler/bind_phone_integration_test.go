// Package handler T085 患者手机号绑定契约 KNOWN_RED 测试
//
// 覆盖 §5.2 bind-phone 接口核心流程：
//
//	POST /api/v1/patient/bind-phone
//
// 预期行为（设计源：docs/tasks/ella/T088-登录绑定设计-V2.md · §5.2）：
//   - 场景 B: phone_hash 匹配唯一 active+wx_openid=NULL → 绑定成功 + JWT 8h
//   - 场景 C: phone_hash 无匹配或 status≠active → 10602 + phoneToken
//   - 场景 D: wx_openid≠当前 openid (已绑定其他微信) → 10603 + phoneToken
//   - 场景 H: 并发两请求 → 恰好 1 成功 1 个 10603 (行锁保证)
//   - 幂等：同一 openid 重复绑定同一手机号 → 200 success
//
// KNOWN_RED 机制：当前 bind-phone handler 尚未实现 → stub 返回 500
// → 本测试全部 FAIL，属预期红态。
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
// Fixtures & Helpers
// ─────────────────────────────────────────────────────────────

func newBindPhoneEnv(t *testing.T) *bindPhoneTestEnv {
	t.Helper()
	
	signer, err := token.NewSigner(testJWTSecret, time.Hour)
	require.NoError(t, err)
	
	fc := testhelper.NewFixedClock(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	store := &fakeStore{}
	h := New(store, signer, nil)
	wechatClient := testhelper.NewMockWechatClient("13800138000")
	h.SetWXClient(wechatClient)
	
	return &bindPhoneTestEnv{
		t:              t,
		store:          store,
		signer:         signer,
		h:              h,
		fixedClock:     fc,
		wechatClient:   wechatClient,
		bindTokenSigner: MustNewBindTokenSigner(),
	}
}

type bindPhoneTestEnv struct {
	t              *testing.T
	store          *fakeStore
	signer         *token.Signer
	h              *Handler
	fixedClock     *testhelper.FixedClock
	wechatClient   *testhelper.MockWechatClient
	bindTokenSigner *token.Signer
}

// MustNewBindTokenSigner 创建绑定态 JWT Signer (30min TTL)
func MustNewBindTokenSigner() *token.Signer {
	s, err := token.NewSigner(testJWTSecret, 30*time.Minute)
	if err != nil {
		panic(err)
	}
	return s
}

// createBindToken 签发绑定态 JWT (sub=openid, scope=bind)
func (e *bindPhoneTestEnv) createBindToken(openid string) string {
	e.fixedClock.Set(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	token, _ := e.bindTokenSigner.Sign(
		openid,        // subject
		"",            // username (patient 无 username)
		"测试患者",     // name
		"patient",     // roleID
	)
	return token
}

// samplePatientRowWithHash 生成带 phone_hash 的患者样本
func samplePatientRowWithHash(id, openID, phone, status string) repo.PatientRow {
	hash := hashPhoneNumber(phone)
	return repo.PatientRow{
		PatientID: id, Name: "患者小明", PhoneEnc: "encrypted", 
		PhoneHash: hash, WxOpenid: openID, Status: status,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

// hashPhoneNumber 模拟手机号 SHA-256 hash (测试用简化版)
func hashPhoneNumber(phone string) string {
	// 实际应使用 crypto/sha256，此处简化为前缀标识
	return "hash_" + phone[len(phone)-6:]
}

// doBindPhone 发起 bind-phone HTTP 请求
func (e *bindPhoneTestEnv) doBindPhone(phoneCode, phoneToken string, openid string) (*httptest.ResponseRecorder, *model.BaseResponse) {
	e.t.Helper()
	
	type ReqBody struct {
		Code      string `json:"phone_code,omitempty"`
		Token     string `json:"phone_token,omitempty"`
	}
	
	body := ReqBody{Code: phoneCode, Token: phoneToken}
	bodyBytes, _ := json.Marshal(body)
	
	w := httptest.NewRequest(http.MethodPost, "/api/v1/patient/bind-phone", strings.NewReader(string(bodyBytes)))
	w.Header.Set("Content-Type", "application/json")
	
	// 注入绑定态 Authorization header
	auth := "Bearer " + openid
	w.Header.Set("Authorization", auth)
	
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, w)
	
	resp := &model.BaseResponse{}
	json.Unmarshal(rec.Body.Bytes(), resp)
	
	return rec, resp
}

// ─────────────────────────────────────────────────────────────
// Scenario B: Happy Path - Successful Binding
// ─────────────────────────────────────────────────────────────

// TestBindPhoneHappyPath_SuccessfulBinding_KNOWN_RED 手机号匹配唯一未绑定档案 → 绑定成功
func TestBindPhoneHappyPath_SuccessfulBinding_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newBindPhoneEnv(t)
	
	// Fixture: phone_hash 匹配 unique active patient with wx_openid=NULL
	phone := "13800138000"
	patient := samplePatientRowWithHash("P20260001", "", phone, "active")
	e.store.patients = append(e.store.patients, &patient)
	e.store.findPatientByPhoneHash = &patient
	
	// Mock 微信返回手机号
	e.wechatClient.DisableError()
	e.wechatClient.SetPhoneNumber(phone)
	
	t.Run("success_200_with_JWT_and_wx_openid_written", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 + JWT 8h + UPDATE wx_openid")
		
		bindToken := e.createBindToken("openid_bind_B")
		w, resp := e.doBindPhone("wechat_code_xyz", "", bindToken)
		
		assert.Equal(t, http.StatusOK, w.Code, "绑定成功应返回 200")
		assert.Equal(t, model.CodeOK, resp.Code, "业务码应为 0")
		
		// Token 校验
		var loginResult model.LoginResultDTO
		require.NoError(t, json.Unmarshal(resp.Data, &loginResult))
		require.NotEmpty(t, loginResult.Token)
		
		claims, err := e.signer.Verify(loginResult.Token)
		require.NoError(t, err)
		assert.Equal(t, "P20260001", claims.Subject)
		assert.Greater(t, claims.ExpireAt.Unix()-claims.IssuedAt.Unix(), int64(7*time.Hour), "JWT 有效期应≈8h")
		
		// 断言：store 被调用写入 wx_openid
		assert.NotNil(t, e.store.lastUpdateWxOpenid, "store 应被调用更新 wx_openid")
		assert.Equal(t, "openid_bind_B", e.store.lastUpdateWxOpenid.Openid)
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario C: No Match → 10602 + phoneToken
// ─────────────────────────────────────────────────────────────

// TestBindPhoneNoMatch_UnregisteredPhone_KNOWN_RED phone_hash 无匹配 → 10602 + phoneToken
func TestBindPhoneNoMatch_UnregisteredPhone_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newBindPhoneEnv(t)
	
	// Fixture: phone_hash 不存在
	e.store.findPatientByPhoneHash = nil
	
	t.Run("not_found_10602_with_phoneToken", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 10602 + phoneToken (purpose=phone_token/exp=7d)")
		
		bindToken := e.createBindToken("openid_bind_C")
		w, resp := e.doBindPhone("wechat_code_unreg", "", bindToken)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10602, resp.Code, "错误码应为 10602 (patient_not_found)")
		
		// 断言：返回 phoneToken
		type PhoneTokenData struct {
			PhoneToken string `json:"phone_token"`
		}
		var data PhoneTokenData
		require.NoError(t, json.Unmarshal(resp.Data, &data))
		require.NotEmpty(t, data.PhoneToken, "响应应包含 phoneToken")
		
		// phoneToken claims 校验（待 Winner 实现后验证 purpose/exp）
		_ = data.PhoneToken
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario C Variant: status!=active → 10602 (防枚举同码)
// ─────────────────────────────────────────────────────────────

// TestBindPhoneInactiveStatus_Returns10602_KNOWN_RED 档案 status≠active → 10602 (与无匹配同码)
func TestBindPhoneInactiveStatus_Returns10602_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newBindPhoneEnv(t)
	
	// Fixture: phone_hash 匹配但 status=pending
	phone := "13800138001"
	patient := samplePatientRowWithHash("P20260002", "", phone, "pending")
	e.store.patients = append(e.store.patients, &patient)
	e.store.findPatientByPhoneHash = &patient
	
	t.Run("inactive_status_returns_10602_same_code_as_no_match", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 10602 (status!=active 与 no match 同码防枚举)")
		
		bindToken := e.createBindToken("openid_bind_C_inactive")
		w, resp := e.doBindPhone("wechat_code", "", bindToken)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10602, resp.Code, "status 异常应返回 10602")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario D: Already Bound by Other Openid → 10603 + phoneToken
// ─────────────────────────────────────────────────────────────

// TestBindPhoneAlreadyBoundByOtherOpenid_KNOWN_Red wx_openid 已绑定其他微信 → 10603
func TestBindPhoneAlreadyBoundByOtherOpenid_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newBindPhoneEnv(t)
	
	// Fixture: phone_hash 匹配且 wx_openid≠当前 openid
	phone := "13800138002"
	existingOpenid := "openid_EXISTING_WX"
	newOpenid := "openid_bind_D"
	
	patient := samplePatientRowWithHash("P20260003", existingOpenid, phone, "active")
	e.store.patients = append(e.store.patients, &patient)
	e.store.findPatientByPhoneHash = &patient
	
	t.Run("already_bound_other_openid_10603_with_phoneToken", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 10603 + phoneToken (禁止覆盖)")
		
		bindToken := e.createBindToken(newOpenid)
		w, resp := e.doBindPhone("wechat_code", "", bindToken)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10603, resp.Code, "错误码应为 10603 (phone_already_bound)")
		
		// 断言：不修改 wx_openid
		type PhoneTokenData struct {
			PhoneToken string `json:"phone_token"`
		}
		var data PhoneTokenData
		require.NoError(t, json.Unmarshal(resp.Data, &data))
		require.NotEmpty(t, data.PhoneToken)
		
		// 验证 store 未被调用 UPDATE
		assert.Nil(t, e.store.lastUpdateWxOpenid, "不应调用 UPDATE wx_openid (禁止覆盖)")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario H: Concurrency - Exactly One Success
// ─────────────────────────────────────────────────────────────

// TestBindPhoneConcurrency_TwoRequests_KNOWN_RED 并发两请求 → 恰好 1 成功 1 个 10603
func TestBindPhoneConcurrency_TwoRequests_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newBindPhoneEnv(t)
	
	// Fixture: phone_hash 匹配未绑定 active 档案
	phone := "13800138003"
	patient := samplePatientRowWithHash("P20260004", "", phone, "active")
	e.store.patients = append(e.store.patients, &patient)
	e.store.findPatientByPhoneHash = &patient
	
	e.wechatClient.DisableError()
	e.wechatClient.SetPhoneNumber(phone)
	
	t.Run("concurrent_requests_exactly_one_success_one_10603", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 database row lock 保证恰好 1 成功")
		
		bindToken := e.createBindToken("openid_concurrent_H")
		
		var wg sync.WaitGroup
		successCount := 0
		count10603 := 0
		
		// 并发两请求
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				
				w, resp := e.doBindPhone("wechat_code_"+strconv.Itoa(idx), "", bindToken)
				
				if w.Code == http.StatusOK && resp.Code == model.CodeOK {
					successCount++
				} else if w.Code == http.StatusOK && resp.Code == 10603 {
					count10603++
				}
			}(i)
		}
		
		wg.Wait()
		
		// 断言：恰好 1 成功，1 个 10603
		assert.Equal(t, 1, successCount, "应有恰好 1 个成功请求")
		assert.Equal(t, 1, count10603, "应有恰好 1 个 10603 失败请求")
		
		t.Logf("并发结果：%d 成功，%d 个 10603", successCount, count10603)
	})
}

// ─────────────────────────────────────────────────────────────
// Idempotent: Same Openid Repeat Bind → Success
// ─────────────────────────────────────────────────────────────

// TestBindPhoneIdempotentSameOpenid_KNOWN_RED 同一 openid 重复绑定同一手机号 → 幂等成功
func TestBindPhoneIdempotentSameOpenid_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newBindPhoneEnv(t)
	
	// Fixture: phone_hash 匹配且 wx_openid==当前 openid (已绑定自身)
	phone := "13800138004"
	sameOpenid := "openid_idem_H"
	
	patient := samplePatientRowWithHash("P20260005", sameOpenid, phone, "active")
	e.store.patients = append(e.store.patients, &patient)
	e.store.findPatientByPhoneHash = &patient
	
	t.Run("idempotent_same_openid_returns_success", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 200 (UPDATE 条件 wx_openid IS NULL 命中自身时幂等)")
		
		bindToken := e.createBindToken(sameOpenid)
		w, resp := e.doBindPhone("wechat_code", "", bindToken)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, model.CodeOK, resp.Code, "幂等应返回 200 success")
		
		// 断言：允许重复操作但不实际改变状态
	})
}
