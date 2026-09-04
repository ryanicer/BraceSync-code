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
//
// KNOWN_RED 机制：当前 wx-login handler 仍保持 T069 旧逻辑（openid 不存在自动创建）
// → 本测试全部 FAIL，属预期红态。Winner 实现后需移除 stub 并返回新契约行为。
//
// 删除约束策略：禁止自动创建患者（T085 核心改动）。
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

const testJWTSecret = "T085-test-secret-for-wxlogin-only-do-not-use-in-prod"

// ─────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────

func newWxLoginEnv(t *testing.T) *wxLoginTestEnv {
	t.Helper()
	
	signer, err := token.NewSigner(testJWTSecret, time.Hour)
	require.NoError(t, err)
	
	fc := testhelper.NewFixedClock(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	store := &fakeStore{}
	h := New(store, signer, nil)
	
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
	t      *testing.T
	store  *fakeStore
	signer *token.Signer
	h      *Handler
	fixedClock *testhelper.FixedClock
	wechatClient *testhelper.MockWechatClient
}

// fakeWechatOpenid 模拟微信登录返回的 openid
const fakeWechatOpenid = "openid_ABC123XYZ789"

// samplePatientRow 装配 PatientRow 样本（实现方 store 返回行）
func samplePatientRow(id string, openID string, phoneHash string, status string) repo.PatientRow {
	return repo.PatientRow{
		PatientID: id,
		Name:      "患者小明",
		PhoneEnc:  "encrypted_phone_data",
		PhoneHash: phoneHash,
		WxOpenid:  openID,
		Status:    status,
		CreatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
	}
}

// do 发起 wx-login HTTP 请求
func (e *wxLoginTestEnv) do(code string) (*httptest.ResponseRecorder, *model.BaseResponse) {
	e.t.Helper()
	
	body := map[string]string{"code": code}
	w := httptest.NewRequest(http.MethodPost, "/api/v1/patient/wx-login", strings.NewReader(`{"code":"`+code+`"}`))
	w.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, w)
	
	resp := &model.BaseResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), resp); err != nil {
		e.t.Fatalf("Failed to unmarshal response: %v", err)
	}
	
	return rec, resp
}

// ─────────────────────────────────────────────────────────────
// Scenario A-1: openid 已绑定 + active → 直接登录
// ─────────────────────────────────────────────────────────────

// TestWxLoginBoundActive_DirectLogin_KNOWN_RED openid 已绑定且档案 active 时直接签发正常 JWT
//
// 预期：200 + JWT 8h (scope=full) + patientDTO (patientId/name/role)
// 当前 stub 返回 500 CodeInternal → 断言 FAIL（预期红态）。
func TestWxLoginBoundActive_DirectLogin_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newWxLoginEnv(t)
	
	// Fixture: 模拟 openid 已存在且 status=active
	const activeStatus = "active"
	patient := samplePatientRow("P20260001", fakeWechatOpenid, "hash_abc123", activeStatus)
	e.store.patientByWxOpenid = &patient
	
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
		assert.Greater(t, claims.ExpireAt.Unix(), claims.IssuedAt.Unix()+int64(8*time.Hour), "JWT 有效期应为 8h")
		
		// DTO 字段透传校验
		assert.Equal(t, "P20260001", loginResult.PatientID)
		assert.Equal(t, "患者小明", loginResult.Name)
		assert.Equal(t, "patient", loginResult.Role)
		
		// 入参透传到 store 的校验（实现方需调用 repo.FindPatientByWxOpenid）
		assert.NotNil(t, e.store.lastFindByWxOpenid, "store 应被调用查询 openid")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario A-2: openid 已绑定 + status≠active → 401 防枚举
// ─────────────────────────────────────────────────────────────

// TestWxLoginBoundInactive_Return401_KNOWN_RED openid 已绑定但档案非 active 时返回 401 统一文案
//
// 预期：HTTP 401 + 10001 invalid_credentials (与不存在同码，防止枚举)
// 当前 stub 返回 500 → 断言 FAIL（预期红态）。
func TestWxLoginBoundInactive_Return401_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newWxLoginEnv(t)
	
	// Fixture: 模拟 openid 已存在但 status=pending
	const pendingStatus = "pending"
	patient := samplePatientRow("P20260001", fakeWechatOpenid, "hash_abc123", pendingStatus)
	e.store.patientByWxOpenid = &patient
	
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

// TestWxLoginUnbound_OpenidNotCreated_KNOWN_RED openid 未绑定时禁止自动创建患者记录
//
// 预期：patients 表行数不变 + 10601 patient_not_bound + bindToken
// 当前 stub 自动创建患者（T069 遗留）→ patients 行数 +1 → 断言 FAIL（预期红态）。
func TestWxLoginUnbound_OpenidNotCreated_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newWxLoginEnv(t)
	
	// Fixture: openid 不存在于系统中
	e.store.patientByWxOpenid = nil
	e.store.createPatientErr = nil // 期望不调用 create
	
	t.Run("openid_not_created_table_row_count_unchanged", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500 并创建患者，预期 10601 + bindToken (禁止自动创建)")
		
		initialCount := len(e.store.patients)
		
		w, resp := e.do(fakeWechatOpenid)
		
		// 断言：未创建患者
		assert.Equal(t, initialCount, len(e.store.patients), "patients 表行数应不变（禁止自动创建）")
		
		// 断言：返回业务码 10601
		assert.Equal(t, http.StatusOK, w.Code, "未绑定场景返回 HTTP 200 + business code")
		assert.Equal(t, 10601, resp.Code, "错误码应为 10601 (patient_not_bound)")
		
		// 断言：返回 bindToken
		require.NotNil(t, resp.Data, "响应应包含 bindToken data")
	})
}

// TestWxLoginUnbound_ReturnsBindTokenClaims_KNOWN_RED 未绑定场景下发 bindToken 的 claims 布局校验
//
// 预期：bindToken claims (sub=openid / role=patient / scope=bind / exp=30min)
// 当前 stub 不签发任何 token → 断言 FAIL（预期红态）。
func TestWxLoginUnbound_ReturnsBindTokenClaims_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	e := newWxLoginEnv(t)
	
	// Fixture: openid 不存在
	e.store.patientByWxOpenid = nil
	
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
		assert.Equal(t, "bind", claims.Scope, "scope 应为 bind")
		
		// exp 校验：30min TTL
		expDuration := time.Duration(claims.ExpireAt.Unix() - claims.IssuedAt.Unix())
		assert.Equal(t, 30*time.Minute, expDuration, "bindToken 有效期应为 30min")
	})
}
