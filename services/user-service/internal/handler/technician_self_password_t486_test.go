// Package handler T486：技师本人自助改登录口令的 handler 层行为。
//
// 这条通道要钉住三件卡面判据：①旧口令错必拒且零写；②改完落的是新口令的 bcrypt（旧口令当场失效）；
// ③权限口径「重置=管理员、自助=本人」——非技师角色在 handler 层就被挡，且被拒的请求不得触库。
// 网关那一层（401 无 token / 403 错角色）在 services/gateway 的对应测试里钉。
package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t486Tech    = "TECH0001"
	t486Other   = "TECH9999" // 攻击面：body 里塞别人的技师号
	t486OldPwd  = "Br0ldPassw0rd#7"
	t486NewPwd  = "Br1newPassw0rd#7"
	t486Path    = "/api/v1/tech/change-password"
	t486TechHdr = "technician"
)

func t486Hdr(role, techID string) map[string]string {
	h := map[string]string{}
	if role != "" {
		h["X-Role"] = role
	}
	if techID != "" {
		h["X-User-Id"] = techID
	}
	return h
}

// t486Env 一条已设口令的技师 + 放行到底所需的环境。
// password_hash 由 oldPwd 现算：库里只存哈希，比“塞一个常量假哈希”更接近真实形态，
// 也让「旧口令能不能验过」这条判据真的经过 bcrypt 而不只是经过字符串相等。
func t486Env(t *testing.T, oldPwd string) *testEnv {
	t.Helper()
	e := newEnv(t, true, true)
	hash := ""
	if oldPwd != "" {
		raw, err := bcrypt.GenerateFromPassword([]byte(oldPwd), bcrypt.MinCost)
		require.NoError(t, err)
		hash = string(raw)
	}
	e.store.techByID = &repo.TechLoginRow{
		TechID: t486Tech, Name: "技师老陈", PasswordHash: hash,
		TeamID: "TEAM01", Status: "enabled", AuthStatus: "authorized",
	}
	return e
}

func t486Do(t *testing.T, e *testEnv, oldPwd, newPwd string, hdr map[string]string) (int, int, string, json.RawMessage) {
	t.Helper()
	w, resp := e.do(http.MethodPost, t486Path,
		map[string]any{"oldPassword": oldPwd, "newPassword": newPwd}, hdr)
	return w.Code, resp.Code, resp.Message, resp.Data
}

// TestT486_SuccessWritesNewHashAndReturnsNoCredential 判据 2 本体：改密成功 ⇒ 落库哈希验得出新口令、
// 验不出旧口令（旧口令当场失效），响应体里两个口令都不出现。
func TestT486_SuccessWritesNewHashAndReturnsNoCredential(t *testing.T) {
	e := t486Env(t, t486OldPwd)

	code, respCode, msg, data := t486Do(t, e, t486OldPwd, t486NewPwd, t486Hdr(t486TechHdr, t486Tech))
	require.Equal(t, http.StatusOK, code, msg)
	assert.Equal(t, model.CodeOK, respCode)
	assert.Contains(t, string(data), t486Tech, "data 回显本人技师号")

	require.Equal(t, 1, e.store.resetTechPwdCalls, "成功路径必须且只写一次口令")
	assert.Equal(t, t486Tech, e.store.lastResetTechID)
	assert.NotEqual(t, t486NewPwd, e.store.lastResetTechHash, "落库的不是明文")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(e.store.lastResetTechHash), []byte(t486NewPwd)),
		"新口令必须能验过（改密没生效就是这条链断了）")
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(e.store.lastResetTechHash), []byte(t486OldPwd)),
		"旧口令必须验不过（卡面判据：改密成功后旧密码登录失败）")
}

// TestT486_IdentityComesFromJWTNotBody 越权格：body 里塞别人的技师号不改变写入目标。
// 端点本身不收 techId 参数，身份只有 X-User-Id 一个来源 ⇒ 自助通道无法用来改他人口令。
func TestT486_IdentityComesFromJWTNotBody(t *testing.T) {
	e := t486Env(t, t486OldPwd)

	w, resp := e.do(http.MethodPost, t486Path,
		map[string]any{"oldPassword": t486OldPwd, "newPassword": t486NewPwd, "techId": t486Other},
		t486Hdr(t486TechHdr, t486Tech))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	assert.Equal(t, t486Tech, e.store.lastSelfServiceTechID, "读的是 JWT 身份那一行")
	assert.Equal(t, t486Tech, e.store.lastResetTechID, "写的也是本人那一行，不是 body 里的 techId")
	assert.NotEqual(t, t486Other, e.store.lastResetTechID)
}

// TestT486_WrongOldPasswordRejected 判据 1：旧口令错 ⇒ 统一 401 防枚举 + 零写 + 零审计行。
func TestT486_WrongOldPasswordRejected(t *testing.T) {
	e := t486Env(t, t486OldPwd)

	code, respCode, _, _ := t486Do(t, e, "Br0wrongPass#7", t486NewPwd, t486Hdr(t486TechHdr, t486Tech))
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, model.CodeInvalidCredentials, respCode)
	assert.Zero(t, e.store.resetTechPwdCalls, "旧口令不符不得改任何列")
	assert.Empty(t, e.store.auditRows, "被拒的请求不产生审计行")
}

// TestT486_NeverSetPasswordMatchesWrongPassword 未设口令行（password_hash 为 NULL）与旧口令填错
// 必须是**逐字节相同**的响应：这一列可空（000005 起），若两种情形文案不同，
// 就等于把「哪些技师号还没设过口令」暴露给任意持 tech token 的请求。
func TestT486_NeverSetPasswordMatchesWrongPassword(t *testing.T) {
	nilHash := t486Env(t, "")
	w1, r1 := nilHash.do(http.MethodPost, t486Path,
		map[string]any{"oldPassword": "anything1", "newPassword": t486NewPwd}, t486Hdr(t486TechHdr, t486Tech))

	set := t486Env(t, t486OldPwd)
	w2, r2 := set.do(http.MethodPost, t486Path,
		map[string]any{"oldPassword": "anything1", "newPassword": t486NewPwd}, t486Hdr(t486TechHdr, t486Tech))

	assert.Equal(t, http.StatusUnauthorized, w1.Code)
	assert.Equal(t, w1.Code, w2.Code)
	assert.Equal(t, r1, r2, "未设口令与旧口令填错的响应必须一致")
	assert.Zero(t, nilHash.store.resetTechPwdCalls)
	assert.Zero(t, set.store.resetTechPwdCalls)
}

// TestT486_WeakNewPasswordRejected 卡面「新密码强度规则与管理员重置一致」的负例集合。
// 每条都要能单独判红：去掉强度判定这一格，四条都会走进写分支。
func TestT486_WeakNewPasswordRejected(t *testing.T) {
	for _, tc := range []struct{ name, pwd string }{
		{"五位低于下限", "Ab1#4"},
		{"超过上限", "Ab1#56789012345678"},
		{"纯数字", "12345678"},
		{"纯字母", "abcdefgh"},
		{"空串", ""},
		{"无数字", "abcdefghij"},
		{"无字母", "1234567890"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := t486Env(t, t486OldPwd)
			code, respCode, _, _ := t486Do(t, e, t486OldPwd, tc.pwd, t486Hdr(t486TechHdr, t486Tech))
			assert.Equal(t, http.StatusBadRequest, code, "弱新口令必须被拒（收到的是参数类错误不是放行）")
			assert.Equal(t, model.CodeInvalidParam, respCode)
			assert.Zero(t, e.store.resetTechPwdCalls, "强度不过不得写库")
			assert.Empty(t, e.store.auditRows)
		})
	}
}

// TestT486_AdminGeneratedPasswordPassesStrengthRule 「强度与管理员重置一致」的正向对拍：
// 管理员重置通道发的口令必须能原样通过本端点的新口令校验，否则技师第一次自助改密就被自己的
// 现有口令卡住（规则比发号器更严 = 把 T480 的出口堵了一半）。穷举多轮以覆盖发号器的随机面。
func TestT486_AdminGeneratedPasswordPassesStrengthRule(t *testing.T) {
	for i := 0; i < 200; i++ {
		pwd, err := genDoctorPassword()
		require.NoError(t, err)
		assert.True(t, validTechPassword(pwd),
			"管理员重置口令必须落在自助改密的强度窗口内，实测坏样本长度=%d", len(pwd))
	}
}

// TestT486_WindowBoundaryAccepted 窗口边界本身：下限 6 位与上限 16 位必须放行。
// 只测负例的话，把下限写成 7、上限写成 15 也能全绿，故夹具长度先自证。
func TestT486_WindowBoundaryAccepted(t *testing.T) {
	for _, tc := range []struct {
		pwd  string
		want int
	}{
		{"ab1def", techPasswordMinLen},         // 6
		{"ab1defghijklmnop", techPasswordMaxLen}, // 16
	} {
		require.Len(t, []rune(tc.pwd), tc.want, "夹具长度必须与声称的边界一致：%s", tc.pwd)
		e := t486Env(t, t486OldPwd)
		code, _, msg, _ := t486Do(t, e, t486OldPwd, tc.pwd, t486Hdr(t486TechHdr, t486Tech))
		assert.Equal(t, http.StatusOK, code, "边界内口令不得被误拒：%s（%s）", tc.pwd, msg)
		assert.Equal(t, 1, e.store.resetTechPwdCalls)
	}
}

// TestT486_SameAsOldPasswordRejected 卡面「新旧不得相同」：旧口令本来就验得过，
// 少了这一格就等于允许「提交成功但什么都没改」的假动作（前端还会据此跳登录页）。
func TestT486_SameAsOldPasswordRejected(t *testing.T) {
	e := t486Env(t, t486OldPwd)
	code, respCode, _, _ := t486Do(t, e, t486OldPwd, t486OldPwd, t486Hdr(t486TechHdr, t486Tech))
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, model.CodeInvalidParam, respCode)
	assert.Zero(t, e.store.resetTechPwdCalls, "新旧相同不得写库（写了也只是同一把哈希，却给用户成功提示）")
	assert.Empty(t, e.store.auditRows)
}

// TestT486_NonTechRolesRejected 权限口径「自助=本人」：后台三类角色与患者角色一律 403。
// 断言排在触库之前 ⇒ 同时钉住「被拒请求不得读库」。
func TestT486_NonTechRolesRejected(t *testing.T) {
	for _, role := range []string{roleAdmin, roleDoctor, "ROLE_CS", "patient", ""} {
		t.Run("role="+role, func(t *testing.T) {
			e := t486Env(t, t486OldPwd)
			code, respCode, _, _ := t486Do(t, e, t486OldPwd, t486NewPwd, t486Hdr(role, t486Tech))
			assert.Equal(t, http.StatusForbidden, code, "角色 %q 不得自助改技师口令", role)
			assert.Equal(t, model.CodeForbidden, respCode)
			assert.Empty(t, e.store.lastSelfServiceTechID, "角色门禁排在读库之前，被拒请求不得触库")
			assert.Zero(t, e.store.resetTechPwdCalls)
			assert.Empty(t, e.store.auditRows)
		})
	}
}

// TestT486_MissingIdentityRejected 未带身份（缺 X-User-Id）⇒ 401 且零写。
// 网关侧「完全不带 Authorization」的 401 由 services/gateway 的 T486 测试钉，这里钉服务层兜底。
func TestT486_MissingIdentityRejected(t *testing.T) {
	e := t486Env(t, t486OldPwd)
	code, respCode, _, _ := t486Do(t, e, t486OldPwd, t486NewPwd, t486Hdr(t486TechHdr, ""))
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, model.CodeUnauthorized, respCode)
	assert.Zero(t, e.store.resetTechPwdCalls)
	assert.Empty(t, e.store.lastSelfServiceTechID, "身份缺失时不得去查任何技师行")
}

// TestT486_AuditRowCarriesNoCredential 留痕口径：成功一次只写一行，描述/detail 里不含口令也不含哈希。
// 同族 T477/T480 已定这条红线，新通道不能把明文带进 audit_logs（操作日志页 admin 可读）。
func TestT486_AuditRowCarriesNoCredential(t *testing.T) {
	e := t486Env(t, t486OldPwd)
	code, _, msg, _ := t486Do(t, e, t486OldPwd, t486NewPwd, t486Hdr(t486TechHdr, t486Tech))
	require.Equal(t, http.StatusOK, code, msg)

	require.Len(t, e.store.auditRows, 1, "一次成功改密只留一行审计")
	row := e.store.auditRows[0]
	assert.Equal(t, auditActionDataModify, row.Action)
	assert.Equal(t, t486Tech, row.OperatorID)
	assert.Equal(t, t486TechHdr, row.OperatorRole)
	assert.Equal(t, "technician", row.TargetType)
	assert.Contains(t, row.Description, "自助修改登录口令")
	detail, err := json.Marshal(row.Detail)
	require.NoError(t, err)
	// 口令、哈希都不得进审计：操作日志页 admin 可读，落了明文等于把 T480 的红线倒着走
	for _, s := range []string{row.Description, string(detail), e.store.lastResetTechHash} {
		assert.NotContains(t, s, t486OldPwd)
		assert.NotContains(t, s, t486NewPwd)
	}
	assert.NotContains(t, string(detail), e.store.lastResetTechHash, "审计 detail 不得带出哈希")
}

// TestT486_StoreErrorLeavesNoAuditRow 写失败 ⇒ 500 且不记「成功留痕」（口径同 T480/T477）。
func TestT486_StoreErrorLeavesNoAuditRow(t *testing.T) {
	e := t486Env(t, t486OldPwd)
	e.store.resetTechPwdErr = assert.AnError
	code, respCode, _, _ := t486Do(t, e, t486OldPwd, t486NewPwd, t486Hdr(t486TechHdr, t486Tech))
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, model.CodeInternal, respCode)
	assert.Empty(t, e.store.auditRows, "写失败不得记成成功留痕")
}

// TestT486_BodyKeysAreCamelCase 契约面：入参只认 camelCase（全仓 HTTP 入参口径）。
// 若哪天有人把 tag 改回下划线，前端发的 oldPassword 会被静默丢掉 ⇒ 恒定报「旧口令不正确」，
// 这一格把它钉住：下划线写法必须不被接受。
func TestT486_BodyKeysAreCamelCase(t *testing.T) {
	e := t486Env(t, t486OldPwd)
	w, resp := e.do(http.MethodPost, t486Path,
		map[string]any{"old_password": t486OldPwd, "new_password": t486NewPwd}, t486Hdr(t486TechHdr, t486Tech))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "下划线键名不被识别（等价于旧口令缺失）")
	assert.Equal(t, model.CodeInvalidCredentials, resp.Code)
	assert.Zero(t, e.store.resetTechPwdCalls)
}

// t486RuleTable 跨语言对拍夹具（前端改密页与本端点各读同一份，判据分叉时两侧腿同时判红）。
type t486RuleTable struct {
	MinLength int    `json:"minLength"`
	MaxLength int    `json:"maxLength"`
	Cases     []struct {
		Password string `json:"password"`
		OK       bool   `json:"ok"`
		Why      string `json:"why"`
	} `json:"cases"`
}

// TestT486_RuleTableParityGoSide 强度规则逐条对拍共享表。
//
// 为什么要跨语言钉：前端按「字符数」算长度、Go 的 len() 是「字节数」，中文口令下两侧读数不同
// （一个汉字 1 字符 3 字节）。前端比后端松 ⇒ 用户设出一个后端拒收的口令；
// 前端比后端紧 ⇒ 报「不合法」的口令其实是合法的。两种分叉都只在真机上暴露，单测各绿各的。
// 表里同时钉边界常量：改了 Go 常量忘了改表 ⇒ 这条红。
func TestT486_RuleTableParityGoSide(t *testing.T) {
	raw, err := os.ReadFile("testdata/tech-password-rule.json")
	require.NoError(t, err, "缺共享强度表 testdata/tech-password-rule.json（前端 apps/tech-miniapp/test/password-rule.spec.ts 读的是同一份）")
	var tbl t486RuleTable
	require.NoError(t, json.Unmarshal(raw, &tbl), "共享强度表不是合法 JSON")

	require.Equal(t, techPasswordMinLen, tbl.MinLength, "表里的 minLength 与 Go 常量分叉")
	require.Equal(t, techPasswordMaxLen, tbl.MaxLength, "表里的 maxLength 与 Go 常量分叉")
	require.NotEmpty(t, tbl.Cases, "强度表为空等于没钉")

	var accepted, rejected int
	for _, tc := range tbl.Cases {
		if tc.OK {
			accepted++
		} else {
			rejected++
		}
		t.Run(tc.Why, func(t *testing.T) {
			assert.Equal(t, tc.OK, validTechPassword(tc.Password),
				"Go 侧判定与共享表分叉：%q（字节 %d / 字符 %d）", tc.Password, len(tc.Password), len([]rune(tc.Password)))
		})
	}
	// 单侧全绿的表没有判定力：全 ok 证不了拦得住，全 reject 证不了放得开
	assert.Greater(t, accepted, 0, "强度表一条正例都没有：规则放宽到什么都过也照样绿")
	assert.Greater(t, rejected, 0, "强度表一条负例都没有：规则收多紧都测不出来")
}

// TestT486_NonAsciiPasswordRejectedEndToEnd 非 ASCII 走完整 HTTP 路径也必须被拒（不落库）。
// 单元级 validTechPassword 对了但分支被挪到写之后，这条会红。
func TestT486_NonAsciiPasswordRejectedEndToEnd(t *testing.T) {
	for _, pwd := range []string{"密密码abc1", "ab1dea\u200bx9"} {
		e := t486Env(t, t486OldPwd)
		code, respCode, _, _ := t486Do(t, e, t486OldPwd, pwd, t486Hdr(t486TechHdr, t486Tech))
		assert.Equal(t, http.StatusBadRequest, code, "含不可打印/非 ASCII 字符的口令必须被拒：%q", pwd)
		assert.Equal(t, model.CodeInvalidParam, respCode)
		assert.Zero(t, e.store.resetTechPwdCalls, "强度不过不得写库")
	}
}
