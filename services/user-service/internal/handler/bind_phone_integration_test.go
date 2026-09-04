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
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
	"github.com/bracesync/bracesync/services/user-service/internal/wechat"
)

// testResp 统一响应体（Data 用 json.RawMessage 便于二次反序列化，避免 any 类型断言 panic）
type testResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// ─────────────────────────────────────────────────────────────
// t085Store：嵌入 fakeStore，额外记录 T085 写操作调用
// ─────────────────────────────────────────────────────────────

// t085Store 包装 fakeStore，用于 T085 契约测试断言写操作副作用。
// 嵌入 *fakeStore 以复用 repo.Store 接口的全部现有方法实现。
// 当 Winner 扩展 Store 接口加入绑定/解绑/改号写方法后，fakeStore 会补齐 stub，
// t085Store 通过嵌入自动获得这些方法；如需记录调用，可在本文件追加同名方法
// 遮蔽嵌入实现并递增对应计数器。
type t085Store struct {
	*fakeStore

	// RWMutex：GetPatientWXOpenID 用读锁（模拟 DB SELECT 不阻塞），
	// Bind/Unbind/Update 用写锁（模拟 UPDATE 原子行锁）。
	mu sync.RWMutex
	// 写操作调用记录（Winner 实现后由遮蔽方法填充；当前 stub handler 不调用故为 0）
	bindOpenidCalls   int
	lastBindPatient   string
	lastBindOpenid    string
	unbindOpenidCalls int
	updatePhoneCalls  int
}

func newT085Store() *t085Store {
	return &t085Store{fakeStore: &fakeStore{}}
}

// t085WXClient 包装 testhelper.MockWechatClient 以实现 handler.wxClientI。
// DoCode2Session 返回 code 作为 openid（测试简化）；GetPhoneNumber 委托 mock。
type t085WXClient struct {
	*testhelper.MockWechatClient
}

func (c *t085WXClient) DoCode2Session(_ context.Context, code string) (*wechat.Code2SessionResult, error) {
	return &wechat.Code2SessionResult{OpenID: code}, nil
}

func (c *t085WXClient) GetPhoneNumber(_ context.Context, code string) (string, string, error) {
	return c.MockWechatClient.GetPhoneNumber(code)
}

// fakeWXClient.GetPhoneNumber 补齐 wxClientI 接口（既有 fakeWXClient 仅实现 DoCode2Session）。
func (f *fakeWXClient) GetPhoneNumber(_ context.Context, _ string) (string, string, error) {
	return "", "", nil
}

// ─────────────────────────────────────────────────────────────
// fakeStore: 补齐 T085 扩展 Store 接口方法（使 *fakeStore 仍满足 repo.Store）
// t085Store 通过嵌入 *fakeStore 获得这些方法，并按需遮蔽以记录调用。
// ─────────────────────────────────────────────────────────────

func (f *fakeStore) GetPatientWXOpenID(_ context.Context, patientID string) (string, error) {
	for openid, p := range f.wxPatientByOpenID {
		if p != nil && p.PatientID == patientID {
			return openid, nil
		}
	}
	return "", nil
}

func (f *fakeStore) BindPatientOpenid(_ context.Context, patientID, openid string) error {
	for _, p := range f.wxPatientByOpenID {
		if p != nil && p.PatientID == patientID {
			return repo.ErrAlreadyBound
		}
	}
	return nil
}

func (f *fakeStore) UnbindWechat(_ context.Context, _ string) error { return nil }

func (f *fakeStore) UpdatePatientPhone(_ context.Context, _ string, _ []byte, _ string) error {
	return nil
}

func (f *fakeStore) PatientPhoneHashTaken(_ context.Context, _, _ string) (bool, error) {
	return f.phoneTaken, f.takenErr
}

// ─────────────────────────────────────────────────────────────
// t085Store: T085 扩展 Store 方法（绑定/解绑/改号 + openid 查询 + hash 查重）
// ─────────────────────────────────────────────────────────────

// GetPatientWXOpenID 反查 wxPatientByOpenID：返回该 patient 当前绑定的 openid（未绑定返回 ""）。
// 使用读锁，模拟 DB SELECT 不阻塞 UPDATE（允许并发读，可能读到过期值）。
func (s *t085Store) GetPatientWXOpenID(_ context.Context, patientID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for openid, p := range s.wxPatientByOpenID {
		if p != nil && p.PatientID == patientID {
			return openid, nil
		}
	}
	return "", nil
}

// BindPatientOpenid 模拟行锁绑定：mutex 串行化，已绑定任意 openid → ErrAlreadyBound。
// 幂等由 handler 层 GetPatientWXOpenID 判断保证；store 层严格模拟 DB 行锁语义。
//
// 并发模拟：进入写锁前短暂让步，确保并发请求的 GetPatientWXOpenID（读锁）先于
// 任一写入完成，从而复现 DB 中"两个 SELECT 均返回空，仅一个 UPDATE 成功"的行锁竞争。
func (s *t085Store) BindPatientOpenid(_ context.Context, patientID, openid string) error {
	time.Sleep(30 * time.Millisecond) // 让并发读先完成，模拟行锁竞争窗口
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.wxPatientByOpenID {
		if p != nil && p.PatientID == patientID {
			return repo.ErrAlreadyBound
		}
	}
	s.bindOpenidCalls++
	s.lastBindPatient = patientID
	s.lastBindOpenid = openid
	if s.wxPatientByOpenID == nil {
		s.wxPatientByOpenID = make(map[string]*repo.PatientLoginRow)
	}
	s.wxPatientByOpenID[openid] = &repo.PatientLoginRow{PatientID: patientID}
	return nil
}

// UnbindWechat 移除该 patient 的 openid 绑定。
func (s *t085Store) UnbindWechat(_ context.Context, patientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unbindOpenidCalls++
	for oid, p := range s.wxPatientByOpenID {
		if p != nil && p.PatientID == patientID {
			delete(s.wxPatientByOpenID, oid)
		}
	}
	return nil
}

// UpdatePatientPhone 改手机号（同步更新 enc+hash）。
func (s *t085Store) UpdatePatientPhone(_ context.Context, _ string, _ []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updatePhoneCalls++
	return nil
}

// PatientPhoneHashTaken phone_hash 是否已被占用（排除自身）。
func (s *t085Store) PatientPhoneHashTaken(_ context.Context, _, _ string) (bool, error) {
	return s.phoneTaken, s.takenErr
}

// ─────────────────────────────────────────────────────────────
// Fixtures & Helpers
// ─────────────────────────────────────────────────────────────

func newBindPhoneEnv(t *testing.T) *bindPhoneTestEnv {
	t.Helper()

	// 登录 JWT TTL 对齐 T037 定稿：8h
	signer, err := token.NewSigner(t085TestJWTSecret, 8*time.Hour)
	require.NoError(t, err)

	fc := testhelper.NewFixedClock(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	store := newT085Store()
	h := New(store, signer, nil)
	wechatClient := testhelper.NewMockWechatClient("13800138000")
	h.SetWXClient(&t085WXClient{MockWechatClient: wechatClient})
	h.SetPhoneTokenSecret(testPhoneTokenSecret)

	return &bindPhoneTestEnv{
		t:               t,
		store:           store,
		signer:          signer,
		h:               h,
		fixedClock:      fc,
		wechatClient:    wechatClient,
		bindTokenSigner: MustNewBindTokenSigner(),
	}
}

type bindPhoneTestEnv struct {
	t               *testing.T
	store           *t085Store
	signer          *token.Signer
	h               *Handler
	fixedClock      *testhelper.FixedClock
	wechatClient    *testhelper.MockWechatClient
	bindTokenSigner *token.Signer
}

// MustNewBindTokenSigner 创建绑定态 JWT Signer (30min TTL)
func MustNewBindTokenSigner() *token.Signer {
	s, err := token.NewSigner(t085TestJWTSecret, 30*time.Minute)
	if err != nil {
		panic(err)
	}
	return s
}

// createBindToken 签发绑定态 JWT (sub=openid, scope=bind)
func (e *bindPhoneTestEnv) createBindToken(openid string) string {
	e.fixedClock.Set(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	tok, _ := e.bindTokenSigner.Sign(
		openid,    // subject
		"",        // username (patient 无 username)
		"测试患者",    // name
		"patient", // roleID
	)
	return tok
}

// patientLoginForPhone 构造 phone_hash 匹配场景的 PatientLoginRow fixture。
// 对应真实 PGStore.GetPatientByPhoneHash 返回行（含 PatientID/Name/Status）。
// phoneHash 参数用于语义标注，实际 fakeStore.GetPatientByPhoneHash 忽略入参
// 统一返回 fakeStore.patientLogin，因此调用方需自行设置 e.store.patientLogin。
func patientLoginForPhone(patientID, name, status string) *repo.PatientLoginRow {
	return &repo.PatientLoginRow{
		PatientID: patientID,
		Name:      name,
		Status:    status,
	}
}

// hashPhoneNumber 返回手机号的 SHA-256 十六进制（与真实 patients.phone_hash 同口径）
func hashPhoneNumber(phone string) string {
	return testhelper.SHA256Hex(phone)
}

// doBindPhone 发起 bind-phone HTTP 请求
func (e *bindPhoneTestEnv) doBindPhone(phoneCode, phoneToken, authToken string) (*httptest.ResponseRecorder, *testResp) {
	e.t.Helper()

	type ReqBody struct {
		Code  string `json:"phone_code,omitempty"`
		Token string `json:"phone_token,omitempty"`
	}

	body := ReqBody{Code: phoneCode, Token: phoneToken}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRequest(http.MethodPost, "/api/v1/patient/bind-phone", strings.NewReader(string(bodyBytes)))
	w.Header.Set("Content-Type", "application/json")
	w.Header.Set("Authorization", "Bearer "+authToken)

	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, w)

	resp := &testResp{}
	_ = json.Unmarshal(rec.Body.Bytes(), resp)

	return rec, resp
}

// ─────────────────────────────────────────────────────────────
// Scenario B: Happy Path - Successful Binding
// ─────────────────────────────────────────────────────────────

// TestBindPhoneHappyPath_SuccessfulBinding 手机号匹配唯一未绑定档案 → 绑定成功
func TestBindPhoneHappyPath_SuccessfulBinding(t *testing.T) {
	t.Parallel()

	e := newBindPhoneEnv(t)

	// Fixture: phone_hash 匹配 unique active patient with wx_openid=NULL
	phone := "13800138000"
	_ = hashPhoneNumber(phone) // 语义标记：mock 微信返回此 phone，handler 计算 hash 后查 store
	e.store.patientLogin = patientLoginForPhone("P20260001", "患者小明", "active")

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
		assert.Equal(t, "P20260001", claims.Subject, "sub 应等于 PatientID")
		assert.Equal(t, "patient", claims.RoleID, "role 应为 patient")

		// JWT 有效期断言：T037 定稿 8h；token.Signer exp-iat 精确等于 ttl
		ttl := claims.ExpireAt - claims.IssuedAt
		assert.Equal(t, int64(8*3600), ttl, "JWT 有效期应精确等于 8h (T037 定稿)")

		// 断言：store 被调用写入 wx_openid（Winner 扩展 Store 接口后生效）
		assert.Equal(t, 1, e.store.bindOpenidCalls, "应恰好调用 1 次 wx_openid 写入")
		assert.Equal(t, "P20260001", e.store.lastBindPatient, "写入的 PatientID 应匹配")
		assert.Equal(t, "openid_bind_B", e.store.lastBindOpenid, "写入的 openid 应等于绑定态 sub")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario C: No Match → 10602 + phoneToken
// ─────────────────────────────────────────────────────────────

// TestBindPhoneNoMatch_UnregisteredPhone phone_hash 无匹配 → 10602 + phoneToken
func TestBindPhoneNoMatch_UnregisteredPhone(t *testing.T) {
	t.Parallel()

	e := newBindPhoneEnv(t)

	// Fixture: phone_hash 不存在（patientLogin 保持 nil）
	e.wechatClient.DisableError()
	e.wechatClient.SetPhoneNumber("19900001111")

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

		// 断言：未调用 wx_openid 写入
		assert.Equal(t, 0, e.store.bindOpenidCalls, "无匹配场景不应写入 wx_openid")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario C Variant: status!=active → 10602 (防枚举同码)
// ─────────────────────────────────────────────────────────────

// TestBindPhoneInactiveStatus_Returns10602 档案 status≠active → 10602 (与无匹配同码)
func TestBindPhoneInactiveStatus_Returns10602(t *testing.T) {
	t.Parallel()

	e := newBindPhoneEnv(t)

	// Fixture: phone_hash 匹配但 status=pending
	e.store.patientLogin = patientLoginForPhone("P20260002", "患者小明", "pending")
	e.wechatClient.DisableError()
	e.wechatClient.SetPhoneNumber("13800138002")

	t.Run("inactive_status_returns_10602_same_code_as_no_match", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 10602 (status!=active 与 no match 同码防枚举)")

		bindToken := e.createBindToken("openid_bind_C_inactive")
		w, resp := e.doBindPhone("wechat_code", "", bindToken)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10602, resp.Code, "status 异常应返回 10602")

		// 断言：未调用 wx_openid 写入
		assert.Equal(t, 0, e.store.bindOpenidCalls, "非 active 档案不应写入 wx_openid")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario D: Already Bound by Other Openid → 10603 + phoneToken
// ─────────────────────────────────────────────────────────────

// TestBindPhoneAlreadyBoundByOtherOpenid wx_openid 已绑定其他微信 → 10603
func TestBindPhoneAlreadyBoundByOtherOpenid(t *testing.T) {
	t.Parallel()

	e := newBindPhoneEnv(t)

	// Fixture: phone_hash 匹配且 wx_openid≠当前 openid
	// patientLogin 表示 phone 匹配到 P20260003；
	// wxPatientByOpenID["openid_other_bound"] 表示该患者已被 openid_other_bound 绑定。
	patient := patientLoginForPhone("P20260003", "患者小明", "active")
	e.store.patientLogin = patient
	e.store.wxPatientByOpenID = map[string]*repo.PatientLoginRow{
		"openid_other_bound": patient,
	}
	e.wechatClient.DisableError()
	e.wechatClient.SetPhoneNumber("13800138003")

	t.Run("already_bound_other_openid_10603_with_phoneToken", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 10603 + phoneToken (禁止覆盖)")

		newOpenid := "openid_bind_D"
		bindToken := e.createBindToken(newOpenid)
		w, resp := e.doBindPhone("wechat_code", "", bindToken)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10603, resp.Code, "错误码应为 10603 (phone_already_bound)")

		// 断言：返回 phoneToken
		type PhoneTokenData struct {
			PhoneToken string `json:"phone_token"`
		}
		var data PhoneTokenData
		require.NoError(t, json.Unmarshal(resp.Data, &data))
		require.NotEmpty(t, data.PhoneToken)

		// 断言：不修改 wx_openid（禁止覆盖已绑定 openid）
		assert.Equal(t, 0, e.store.bindOpenidCalls, "已绑定其他 openid 时不得覆盖写入")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario H: Concurrency - Exactly One Success
// ─────────────────────────────────────────────────────────────

// TestBindPhoneConcurrency_TwoRequests 并发两请求 → 恰好 1 成功 1 个 10603
func TestBindPhoneConcurrency_TwoRequests(t *testing.T) {
	t.Parallel()

	e := newBindPhoneEnv(t)

	// Fixture: phone_hash 匹配未绑定 active 档案
	phone := "13800138003"
	e.store.patientLogin = patientLoginForPhone("P20260004", "患者小明", "active")

	e.wechatClient.DisableError()
	e.wechatClient.SetPhoneNumber(phone)

	t.Run("concurrent_requests_exactly_one_success_one_10603", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 database row lock 保证恰好 1 成功")

		bindToken := e.createBindToken("openid_concurrent_H")

		var wg sync.WaitGroup
		var mu sync.Mutex
		successCount := 0
		count10603 := 0

		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				w, resp := e.doBindPhone("wechat_code_"+strconv.Itoa(idx), "", bindToken)

				mu.Lock()
				defer mu.Unlock()
				if w.Code == http.StatusOK && resp.Code == model.CodeOK {
					successCount++
				} else if w.Code == http.StatusOK && resp.Code == 10603 {
					count10603++
				}
			}(i)
		}

		wg.Wait()

		assert.Equal(t, 1, successCount, "应有恰好 1 个成功请求")
		assert.Equal(t, 1, count10603, "应有恰好 1 个 10603 失败请求")
	})
}

// ─────────────────────────────────────────────────────────────
// Idempotent: Same Openid Repeat Bind → Success
// ─────────────────────────────────────────────────────────────

// TestBindPhoneIdempotentSameOpenid 同一 openid 重复绑定同一手机号 → 幂等成功
func TestBindPhoneIdempotentSameOpenid(t *testing.T) {
	t.Parallel()

	e := newBindPhoneEnv(t)

	// Fixture: phone_hash 匹配且 wx_openid==当前 openid (已绑定自身)
	sameOpenid := "openid_idem_H"
	patient := patientLoginForPhone("P20260005", "患者小明", "active")
	e.store.patientLogin = patient
	e.store.wxPatientByOpenID = map[string]*repo.PatientLoginRow{
		sameOpenid: patient,
	}
	e.wechatClient.DisableError()
	e.wechatClient.SetPhoneNumber("13800138005")

	t.Run("idempotent_same_openid_returns_success", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 返回 500，预期 200 (UPDATE 条件 wx_openid IS NULL 命中自身时幂等)")

		bindToken := e.createBindToken(sameOpenid)
		w, resp := e.doBindPhone("wechat_code", "", bindToken)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, model.CodeOK, resp.Code, "幂等应返回 200 success")

		// 断言：允许重复操作但不实际改变状态（不报错即幂等成功）
	})
}
