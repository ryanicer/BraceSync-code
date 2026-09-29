// Package handler 单测 T487-⑤：POST /auth/change-password 自助改密
//
// 卡面四格（成功 / 旧密码错 / 弱密码 / 未登录）之外，本文件还锁两条本卡特有的坑：
//  1. 「旧密码错」必须是 400/10400 而不是 401/10401 —— 前端会话失效判定把 HTTP 401 一律当令牌过期
//     （apps/admin-web/src/utils/sessionExpiry.ts:33-36，命中即 expiredSession 清令牌跳登录页），
//     回 401 会让「填错一次旧密码」表现为被登出，用户以为是系统坏了；
//  2. 身份只取 X-User-Id，请求体里没有账号字段 —— 否则任何人带别人的 adminId 就能改别人密码。
package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t487AdminID = "A0007"
	t487OldPwd  = "Bravo2026old"
	t487NewPwd  = "Charlie2026new"
)

// t487CPEnv 装配一个「已登录的医护」：X-User-Id 由网关注入，改密只认它
func t487CPEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, true, true)
	e.store.adminByID = &repo.AdminRow{AdminID: t487AdminID, Username: "doc00007", Name: "主诊医生李某",
		PasswordHash: adminHash(t, t487OldPwd), RoleID: "ROLE_DOCTOR", Status: "enabled"}
	return e
}

func t487Body(old, newer any) map[string]any {
	m := map[string]any{}
	if old != nil {
		m["old_password"] = old
	}
	if newer != nil {
		m["new_password"] = newer
	}
	return m
}

// ─────────────────────────────────────────────────────────────
// 判据①：改密成功 + 旧密码失效
// ─────────────────────────────────────────────────────────────

func TestT487ChangePasswordSuccess(t *testing.T) {
	e := t487CPEnv(t)

	w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
		t487Body(t487OldPwd, t487NewPwd), map[string]string{headerUserID: t487AdminID})
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, t487AdminID, e.store.lastAdminReadID, "身份取的是 X-User-Id，不是请求体里的任何字段")

	require.NotEmpty(t, e.store.adminUpdatedHash, "必须落一次新哈希")
	// 🔴 判据「改密后旧密码失效」：新哈希对旧口令校验不过、对新口令校验得过
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(e.store.adminUpdatedHash), []byte(t487NewPwd)),
		"落库哈希应对新口令可验")
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(e.store.adminUpdatedHash), []byte(t487OldPwd)),
		"落库哈希必须验不过旧口令（否则等于没改）")

	require.Len(t, e.store.auditRows, 1, "自助改密要留一行审计")
	a := e.store.auditRows[0]
	assert.Equal(t, t487AdminID, a.OperatorID)
	assert.Equal(t, "ROLE_DOCTOR", a.OperatorRole, "操作人角色取账号行本身，不取请求头")
	assert.Equal(t, auditActionDataModify, a.Action, "动作名沿用 T399 锁定词表，不新开词")
	assert.Equal(t, "admin", a.TargetType)
	assert.Equal(t, t487AdminID, a.TargetID, "目标是本人 ⇒ 只能由 handler 就地埋点（表驱动拿不到自身身份）")
}

// TestT487ChangePasswordAuditCarNoSecret 审计里既不含明文口令也不含哈希（红线同 T314/T477/T480）
func TestT487ChangePasswordAuditCarriesNoSecret(t *testing.T) {
	e := t487CPEnv(t)
	w, _ := e.do(http.MethodPost, "/api/v1/auth/change-password",
		t487Body(t487OldPwd, t487NewPwd), map[string]string{headerUserID: t487AdminID})
	require.Equal(t, http.StatusOK, w.Code)

	require.Len(t, e.store.auditRows, 1)
	raw := e.store.auditRows[0].Description + strings.Join(flattenDetail(e.store.auditRows[0].Detail), "|")
	assert.NotContains(t, raw, t487OldPwd)
	assert.NotContains(t, raw, t487NewPwd)
	assert.NotContains(t, raw, "$2a$")
	assert.NotContains(t, e.store.adminUpdatedHash, t487NewPwd, "落库的是哈希不是明文")
}

func flattenDetail(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+toStringSlice(v))
	}
	return out
}

func toStringSlice(v any) string {
	switch t := v.(type) {
	case []string:
		return strings.Join(t, ",")
	default:
		return ""
	}
}

// ─────────────────────────────────────────────────────────────
// 判据②：旧密码错 —— 400 而非 401
// ─────────────────────────────────────────────────────────────

func TestT487ChangePasswordWrongOldRejected(t *testing.T) {
	e := t487CPEnv(t)

	w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
		t487Body("totally-wrong-old", t487NewPwd), map[string]string{headerUserID: t487AdminID})
	require.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code, "🔴 不得是 10401：前端见 401 即清令牌跳登录页")
	assert.Empty(t, e.store.adminUpdatedHash, "旧密码没过一律不落新哈希")

	// 反证：同一条请求若把 X-User-Id 去掉，走的必须是「未登录」那一格（401），两格不混
	w2, resp2 := e.do(http.MethodPost, "/api/v1/auth/change-password",
		t487Body("totally-wrong-old", t487NewPwd), nil)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)
	assert.Equal(t, model.CodeUnauthorized, resp2.Code)
}

// ─────────────────────────────────────────────────────────────
// 判据③：弱密码 —— 写之前就被截住，不触库
// ─────────────────────────────────────────────────────────────

func TestT487ChangePasswordWeakNewPassword(t *testing.T) {
	weak := []struct{ name, pwd string }{
		{"短于 8 字节", "Ab1cdef"},
		{"纯数字", "12345678"},
		{"纯字母", "abcdefghijkl"},
		{"空串", ""},
		{"超过 64 字节", strings.Repeat("a1", 33)},
	}
	for _, tc := range weak {
		t.Run(tc.name, func(t *testing.T) {
			e := t487CPEnv(t)
			w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
				t487Body(t487OldPwd, tc.pwd), map[string]string{headerUserID: t487AdminID})
			assert.Equal(t, http.StatusBadRequest, w.Code, "%s 应被拒", tc.name)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			assert.Equal(t, "", e.store.lastAdminReadID, "强度判定是纯字符串检查，弱密码不该惊动库（读账号都不必）")
			assert.Empty(t, e.store.adminUpdatedHash)
			assert.Empty(t, e.store.auditRows, "被拒的写不留审计行")
		})
	}
}

// TestT487ChangePasswordStrengthAnchorGeneratedPwd 🔴 强度规则的锚点不变量：
// 管理员代重置发给医护的初始口令，必须能通过自助改密的强度校验——
// 否则「先发一个自己改不掉的密码」，医护登录后想立刻换掉就会被自己的规则挡住。
func TestT487ChangePasswordStrengthAnchorGeneratedPwd(t *testing.T) {
	for i := 0; i < 50; i++ {
		pwd, err := genDoctorPassword()
		require.NoError(t, err)
		require.True(t, validNewAdminPassword(pwd),
			"代重置生成的初始口令必须通过自助改密强度校验，坏样本：%s", pwd)
	}
}

// TestT487ChangePasswordNonAsciiLetter 汉字算字母位：中文口令不被「必须含字母」误伤
func TestT487ChangePasswordNonAsciiLetter(t *testing.T) {
	e := t487CPEnv(t)
	w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
		t487Body(t487OldPwd, "矫治通骨科2026"), map[string]string{headerUserID: t487AdminID})
	assert.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.NotEmpty(t, e.store.adminUpdatedHash)
}

// ─────────────────────────────────────────────────────────────
// 判据④：未登录 / 身份异常
// ─────────────────────────────────────────────────────────────

func TestT487ChangePasswordMissingIdentity(t *testing.T) {
	e := t487CPEnv(t)
	w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
		t487Body(t487OldPwd, t487NewPwd), nil) // 没有 X-User-Id
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, model.CodeUnauthorized, resp.Code)
	assert.Equal(t, "", e.store.lastAdminReadID, "没身份就不该查库")
	assert.Empty(t, e.store.adminUpdatedHash)
}

func TestT487ChangePasswordParamValidation(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"缺 old_password", t487Body(nil, t487NewPwd)},
		{"缺 new_password", t487Body(t487OldPwd, nil)},
		{"两个都缺", t487Body(nil, nil)},
		{"空串两格", t487Body("", "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := t487CPEnv(t)
			w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password", tc.body,
				map[string]string{headerUserID: t487AdminID})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			assert.Equal(t, "", e.store.lastAdminReadID, "参数检查排在触库之前")
			assert.Empty(t, e.store.adminUpdatedHash)
		})
	}

	t.Run("非法 JSON", func(t *testing.T) {
		e := t487CPEnv(t)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", strings.NewReader("{"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(headerUserID, t487AdminID)
		w := httptest.NewRecorder()
		e.h.Router().ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, "body 解析失败必须在参数步就回 400")
		assert.Equal(t, "", e.store.lastAdminReadID)
	})
}

func TestT487ChangePasswordSameAsOldRejected(t *testing.T) {
	e := t487CPEnv(t)
	w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
		t487Body(t487OldPwd, t487OldPwd), map[string]string{headerUserID: t487AdminID})
	assert.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Empty(t, e.store.adminUpdatedHash, "新旧同值不得落库：否则「改密成功」与「密码没变」同时成立")
	assert.Empty(t, e.store.auditRows)
}

func TestT487ChangePasswordAccountStates(t *testing.T) {
	t.Run("禁用账号 → 403 且不写", func(t *testing.T) {
		e := t487CPEnv(t)
		e.store.adminByID.Status = "disabled"
		w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
			t487Body(t487OldPwd, t487NewPwd), map[string]string{headerUserID: t487AdminID})
		assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
		assert.Equal(t, model.CodeForbidden, resp.Code)
		assert.Empty(t, e.store.adminUpdatedHash)
	})

	t.Run("X-User-Id 不是 admins 行（技师/患者令牌）→ 404 不是 500", func(t *testing.T) {
		e := t487CPEnv(t)
		e.store.adminByID = nil
		w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
			t487Body(t487OldPwd, t487NewPwd), map[string]string{headerUserID: "T0001"})
		assert.Equal(t, http.StatusNotFound, w.Code, resp.Message)
		assert.Equal(t, model.CodeNotFound, resp.Code)
		assert.Equal(t, "T0001", e.store.lastAdminReadID)
		assert.Empty(t, e.store.adminUpdatedHash)
	})

	t.Run("查库失败 → 500，不伪装成「账号不存在」", func(t *testing.T) {
		e := t487CPEnv(t)
		e.store.adminByIDErr = errors.New("db down")
		w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
			t487Body(t487OldPwd, t487NewPwd), map[string]string{headerUserID: t487AdminID})
		assert.Equal(t, http.StatusInternalServerError, w.Code, resp.Message)
		assert.Equal(t, model.CodeInternal, resp.Code)
	})

	t.Run("写库失败 → 500 且不写审计", func(t *testing.T) {
		e := t487CPEnv(t)
		e.store.adminByID.AdminID = "A0999" // 与 UpdateAdminPasswordHash 的匹配键错开 → 写失败
		w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password",
			t487Body(t487OldPwd, t487NewPwd), map[string]string{headerUserID: t487AdminID})
		assert.Equal(t, http.StatusInternalServerError, w.Code, resp.Message)
		assert.Equal(t, model.CodeInternal, resp.Code)
		assert.Empty(t, e.store.auditRows, "没改成就不留「已改密」的痕")
	})
}

// TestT487ChangePasswordIgnoresIdentityInBody 请求体里塞别人的 adminId 不得生效（横向越权面）
func TestT487ChangePasswordIgnoresIdentityInBody(t *testing.T) {
	e := t487CPEnv(t)
	body := t487Body(t487OldPwd, t487NewPwd)
	body["adminId"] = "A9999"
	body["username"] = "ops_admin"

	w, resp := e.do(http.MethodPost, "/api/v1/auth/change-password", body,
		map[string]string{headerUserID: t487AdminID})
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, t487AdminID, e.store.lastAdminReadID, "读写两侧都必须只用 X-User-Id")
}
