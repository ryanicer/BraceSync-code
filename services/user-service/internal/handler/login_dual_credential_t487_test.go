// Package handler 单测 T487-③：/auth/login 双凭证（用户名 或 手机号）
//
// 本卡把「只能用户名登」改成「用户名或手机号都能登」，最容易坏的不是能不能登进来，
// 而是下面这三条只在测试里才证得到的性质：
//  1. 解析序是先用户名后手机号 —— 存量账号的登录路径必须一字不变（不因为新增手机号支而多查一次库）；
//  2. 手机号支要过 Normalize 再算 hash —— 前端粘进来的 "+86 138-0013-8000" 这种形状得能命中，
//     且命中键与写侧（医护账号创建/编辑）算的是同一个 hash；
//  3. 所有「查无此人」的分支回一模一样的 401（文案统一防枚举）——
//     攻击者不能靠响应差别判断「这个手机号注册过没有」。
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// t487Phone 测试用手机（11 位 1 开头，非真实号段）
const t487Phone = "13800138000"

// t487Doctor 医护登录行：用户名由发号器产（doc + 5 位），与手机号形状天然不重叠
func t487Doctor(t *testing.T, pwd string) *repo.AdminRow {
	t.Helper()
	return &repo.AdminRow{AdminID: "A0007", Username: "doc00007", Name: "主诊医生李某",
		PasswordHash: adminHash(t, pwd), RoleID: "ROLE_DOCTOR", Status: "enabled"}
}

// decodeLogin 把统一响应体的 data 解成登录结果
func decodeLogin(t *testing.T, raw json.RawMessage) model.LoginResultDTO {
	t.Helper()
	var dto model.LoginResultDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	return dto
}

// TestT487LoginByUsernameStillFirst 用户名命中时不得再查手机号支（存量行为零变化的机器可读形式）。
func TestT487LoginByUsernameStillFirst(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.admin = t487Doctor(t, "admin123")
	e.store.adminByPhone = &repo.AdminRow{AdminID: "A9999", Username: "doc99999", Name: "不该被登进来的人",
		PasswordHash: adminHash(t, "admin123"), RoleID: "ROLE_DOCTOR", Status: "enabled"}
	e.store.scope = "all"

	w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "doc00007", "password": "admin123"}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	dto := decodeLogin(t, resp.Data)
	assert.Equal(t, "A0007", dto.AdminID, "用户名命中时必须是用户名那一行，不能被手机号支抢走")
	assert.Equal(t, "", e.store.lastAdminPhoneHash, "用户名已命中，手机号那一支一次都不该被调用")
}

// TestT487LoginByPhone 用户名未命中 + 输入是手机号形状 → 按 phone_hash 命中并正常签发。
// 同时锁「命中键算法与写侧一致」：断言传进 store 的就是 Normalize 后的 Hash。
func TestT487LoginByPhone(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.adminByPhone = t487Doctor(t, "admin123") // admin（用户名支）留 nil = 用户名查不到
	e.store.scope = "team"

	w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": t487Phone, "password": "admin123"}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	assert.Equal(t, phone.Hash(t487Phone), e.store.lastAdminPhoneHash,
		"手机号支必须以 phone.Hash(Normalize(输入)) 查库 —— 与写侧同一把键")

	dto := decodeLogin(t, resp.Data)
	assert.Equal(t, "A0007", dto.AdminID)
	assert.Equal(t, "doc00007", dto.Username, "会话身份仍是账号本体，不因走手机号而变")
	assert.Equal(t, "team", dto.Scope)

	require.Len(t, e.store.auditRows, 1, "手机号登录同样要留一条登录审计")
	assert.Equal(t, "A0007", e.store.auditRows[0].OperatorID)
	assert.Equal(t, auditActionLogin, e.store.auditRows[0].Action)
}

// TestT487LoginByPhoneNormalized 粘贴进来的带前缀/带分隔符手机号，命中键必须与干净号相同。
// 不这么做的话「前端改了输入框、后端登不进去」会以「用户说密码没错」的形式回来。
func TestT487LoginByPhoneNormalized(t *testing.T) {
	for _, raw := range []string{"+86 138-0013-8000", " 138 0013 8000 ", "8613800138000"} {
		t.Run(raw, func(t *testing.T) {
			e := newEnv(t, true, true)
			e.store.adminByPhone = t487Doctor(t, "admin123")
			e.store.scope = "all"

			w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
				map[string]string{"username": raw, "password": "admin123"}, nil)
			require.Equal(t, http.StatusOK, w.Code, "形状 %q 应经 Normalize 后命中: %s", raw, resp.Message)
			assert.Equal(t, phone.Hash(t487Phone), e.store.lastAdminPhoneHash)
		})
	}
}

// TestT487LoginUnified401 四格「进不去但不是禁用」必须回一模一样的响应（HTTP + code + message 逐字同），
// 否则响应差别就是一份可枚举的账号清单。
func TestT487LoginUnified401(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.admin = t487Doctor(t, "right") // 只有 doc00007 存在；手机号只有 t487Phone 注册过

	cases := []struct{ name, identifier, pwd string }{
		{"A 用户名不存在且形状非手机号", "ghost", "right"},
		{"B 手机号未注册", "13900139001", "right"},
		{"C 账号存在密码错", "doc00007", "wrong"},
		{"D 手机号命中密码错", t487Phone, "wrong"},
	}
	var firstBody string
	for i, tc := range cases {
		w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
			map[string]string{"username": tc.identifier, "password": tc.pwd}, nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s", tc.name)
		assert.Equal(t, model.CodeUnauthorized, resp.Code, "%s", tc.name)
		body := fmt.Sprintf("%d|%s", w.Code, resp.Message)
		if i == 0 {
			firstBody = body
			continue
		}
		assert.Equal(t, firstBody, body, "四格 401 文案必须逐字相同（防枚举）: %s", tc.name)
	}
}

// TestT487LoginNonPhoneShapeSkipsHashBranch 单独证「形状过滤」：只有像手机号的输入才去撞 hash。
func TestT487LoginNonPhoneShapeSkipsHashBranch(t *testing.T) {
	for _, id := range []string{"ghost", "doc00007", "1380013800", "138001380000", "23800138000", "ops_admin"} {
		t.Run(id, func(t *testing.T) {
			e := newEnv(t, true, true)
			e.store.adminByPhone = t487Doctor(t, "admin123") // 手机号支备着：一旦被调用就会「登进来」
			e.store.scope = "all"

			w, _ := e.do(http.MethodPost, "/api/v1/auth/login",
				map[string]string{"username": id, "password": "admin123"}, nil)
			assert.NotEqual(t, http.StatusOK, w.Code, "输入 %q 不该经手机号支登进来", id)
			assert.Equal(t, "", e.store.lastAdminPhoneHash, "输入 %q 形状不是有效手机号，不该查 hash", id)
		})
	}
}

// TestT487LoginLegacyAccountWithoutPhone 无手机号的存量账号（admins.phone_hash 为 NULL）：
// 用户名照常登；拿手机号试就是查不到，不得回 500 也不得误命中别人。
func TestT487LoginLegacyAccountWithoutPhone(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.admin = &repo.AdminRow{AdminID: "A0001", Username: "ops_admin", Name: "运营小张",
		PasswordHash: adminHash(t, "admin123"), RoleID: "ROLE_ADMIN", Status: "enabled"}
	e.store.scope = "all"

	w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "ops_admin", "password": "admin123"}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, "", e.store.lastAdminPhoneHash)

	// 同一账号若有人拿手机号试（它没录手机号）→ 401，不是 500
	w, resp = e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": t487Phone, "password": "admin123"}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, model.CodeUnauthorized, resp.Code)
	require.NotEqual(t, "", e.store.lastAdminPhoneHash, "手机号支确实被调用过（NULL 那一格由集成用例在真库证）")
}

// TestT487LoginPhoneStoreError 手机号支的库错误要照旧冒到 500，不能被吞成「未命中」而伪装成 401。
func TestT487LoginPhoneStoreError(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.adminByPhoneErr = errors.New("db down")

	w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": t487Phone, "password": "p"}, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)
}

// TestT487LoginDisabledAccountViaPhone 禁用检查在手机号支同样生效（身份来源不影响状态判定）。
func TestT487LoginDisabledAccountViaPhone(t *testing.T) {
	e := newEnv(t, true, true)
	doc := t487Doctor(t, "admin123")
	doc.Status = "disabled"
	e.store.adminByPhone = doc

	w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": t487Phone, "password": "admin123"}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, model.CodeUnauthorized, resp.Code)
}

// TestT487LoginUsernameThatLooksLikePhone 万一「用户名恰好是一串 11 位数字」：解析序保证用户名优先，
// 不会因为形状像手机号就被另一行的 phone_hash 抢走身份。
func TestT487LoginUsernameThatLooksLikePhone(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.admin = &repo.AdminRow{AdminID: "A1111", Username: t487Phone, Name: "撞号用户名",
		PasswordHash: adminHash(t, "pw123456"), RoleID: "ROLE_CS", Status: "enabled"}
	e.store.adminByPhone = &repo.AdminRow{AdminID: "A2222", Username: "doc00002", Name: "真机主",
		PasswordHash: adminHash(t, "pw123456"), RoleID: "ROLE_DOCTOR", Status: "enabled"}
	e.store.scope = "all"

	w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": t487Phone, "password": "pw123456"}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	dto := decodeLogin(t, resp.Data)
	assert.Equal(t, "A1111", dto.AdminID, "用户名支先命中 ⇒ 身份是撞号的那个账号")
	assert.Equal(t, "", e.store.lastAdminPhoneHash)
}
