// Package handler T480：技师初始口令（创建发号 + admin 通道重置）的 handler 层行为。
//
// 这条链此前的缺陷是「新建即登不进」：createTechnician 的 INSERT 不带 password_hash，
// 而 techLogin 比对的就是它 ⇒ 该列恒为 NULL 的行永远验不过。所以这里除了门禁判定，
// 必须把「创建/重置之后真的能登录」这条链验通——那才是卡面判据。
package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const t480Tech = "TECH0001"

const t480Phone = "13800001111"

// t480Team T627 方案乙之后的形态：技师建的是「维护班组」行，不是医疗团队行。
// 这条链的判据是口令（初始口令能登进来），团队 id 本身不被断言，所以只需与侧别判据自洽。
const t480Team = "TEAM04"

func t480ResetPath() string { return "/api/v1/admin/technicians/" + t480Tech + "/reset-password" }

// t480Env 放行到底所需的前置夹具：一条在档技师（重置的存在性判定要读它、创建要认团队）
func t480Env(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, true, true)
	row := repo.TechnicianRow{
		TechID: t480Tech, Name: "技师老陈", TeamID: strPtr(t480Team),
		Status: "enabled", AuthStatus: "authorized",
	}
	e.store.tech = &row
	e.store.createdTech = &row
	e.store.teamExists = true
	e.store.teamType = "maintenance" // T627 方案乙：创建腿现在多问一次侧别，医疗团队会 400
	return e
}

// t480Create 走一次真实创建请求
func t480Create(t *testing.T, e *testEnv) (int, int, string, json.RawMessage) {
	t.Helper()
	w, resp := e.do(http.MethodPost, "/api/v1/admin/technicians",
		map[string]any{"name": "新技师", "phone": t480Phone, "teamId": t480Team}, adminHdr)
	return w.Code, resp.Code, resp.Message, resp.Data
}

// t480Reset 走一次真实重置请求
func t480Reset(t *testing.T, e *testEnv, headers map[string]string) (int, int, string, json.RawMessage) {
	t.Helper()
	w, resp := e.do(http.MethodPost, t480ResetPath(), nil, headers)
	return w.Code, resp.Code, resp.Message, resp.Data
}

// t480AssertOneTimePwd 一次性口令的形态：复用医护侧发号器，长度/前后缀都不变
func t480AssertOneTimePwd(t *testing.T, pwd string) {
	t.Helper()
	require.NotEmpty(t, pwd, "口令必须一次性返回，否则管理员拿不到凭据")
	assert.Len(t, pwd, doctorPasswordLen)
	assert.True(t, strings.HasPrefix(pwd, doctorPwdPrefix) && strings.HasSuffix(pwd, doctorPwdSuffix),
		"复用 T314 发号器：前后缀不变")
}

// TestT480_CreateWritesHashAndReturnsItOnce 判据 1：创建返回明文，且落库的是能验出该明文的 bcrypt 哈希
func TestT480_CreateWritesHashAndReturnsItOnce(t *testing.T) {
	e := t480Env(t)

	code, respCode, msg, data := t480Create(t, e)
	require.Equal(t, http.StatusOK, code, msg)
	require.Equal(t, model.CodeOK, respCode)

	var dto model.TechnicianCreateDTO
	require.NoError(t, json.Unmarshal(data, &dto))
	assert.Equal(t, t480Tech, dto.TechID)
	t480AssertOneTimePwd(t, dto.InitialPassword)

	in := e.store.lastTechInput
	require.NotEmpty(t, in.PasswordHash, "INSERT 必须带上 password_hash —— 这一列为 NULL 就是本卡缺陷本体")
	assert.NotEqual(t, dto.InitialPassword, in.PasswordHash, "落库的不是明文")
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(in.PasswordHash), []byte(dto.InitialPassword)),
		"落库哈希必须能验出响应里的一次性口令")
	// 手机号哈希与口令哈希是两个不同的量，别把 phone_hash 当 password_hash 写
	assert.NotEqual(t, in.PhoneHash, in.PasswordHash)
}

// TestT480_CreateThenTechLoginRoundTrip 落地判据本体：新建技师之后，技师端能用手机号+口令登进来
func TestT480_CreateThenTechLoginRoundTrip(t *testing.T) {
	e := t480Env(t)

	_, _, _, data := t480Create(t, e)
	var dto model.TechnicianCreateDTO
	require.NoError(t, json.Unmarshal(data, &dto))

	// 把落库的哈希喂回登录查询投影（PGStore 里就是同一列）
	e.store.techLogin = &repo.TechLoginRow{
		TechID: t480Tech, Name: "新技师",
		PasswordHash: e.store.lastTechInput.PasswordHash,
		TeamID:       t480Team, Status: "enabled", AuthStatus: "authorized",
	}

	w, resp := e.do(http.MethodPost, "/api/v1/tech/login",
		map[string]string{"phone": t480Phone, "password": dto.InitialPassword}, nil)
	require.Equal(t, http.StatusOK, w.Code, "新建技师必须能登录：%s", resp.Message)
	var login model.TechLoginResultDTO
	require.NoError(t, json.Unmarshal(resp.Data, &login))
	assert.Equal(t, t480Tech, login.TechID)
	assert.Equal(t, "technician", login.Role)
	assert.NotEmpty(t, login.Token)
}

// TestT480_LoginKeepsUnified401Copy 判据 4：防枚举文案不变 —— 口令错 vs 手机号查无此人，两条腿逐字同形
func TestT480_LoginKeepsUnified401Copy(t *testing.T) {
	e := t480Env(t)
	e.store.techLogin = &repo.TechLoginRow{
		TechID: t480Tech, Name: "技师老陈", PasswordHash: adminHash(t, "right-password"),
		Status: "enabled", AuthStatus: "authorized",
	}
	wWrong, respWrong := e.do(http.MethodPost, "/api/v1/tech/login",
		map[string]string{"phone": t480Phone, "password": "wrong-password"}, nil)
	require.Equal(t, http.StatusUnauthorized, wWrong.Code)
	require.Equal(t, model.CodeUnauthorized, respWrong.Code)

	eNoSuch := newEnv(t, true, true) // techLogin 留空 = 手机号查无此人
	wNoSuch, respNoSuch := eNoSuch.do(http.MethodPost, "/api/v1/tech/login",
		map[string]string{"phone": "13900009999", "password": "whatever"}, nil)
	require.Equal(t, http.StatusUnauthorized, wNoSuch.Code)
	require.Equal(t, model.CodeUnauthorized, respNoSuch.Code)

	assert.Equal(t, respWrong.Message, respNoSuch.Message, "防枚举：两条失败腿的提示必须逐字相同")
}

// TestT480_ResetPasswordAdminAllowed 判据 2：admin 重置 → 一次性新口令 + 落 bcrypt + 审计不带口令
func TestT480_ResetPasswordAdminAllowed(t *testing.T) {
	e := t480Env(t)

	code, respCode, msg, data := t480Reset(t, e, adminHdr)
	require.Equal(t, http.StatusOK, code, msg)
	require.Equal(t, model.CodeOK, respCode)

	var dto model.TechnicianPasswordResetDTO
	require.NoError(t, json.Unmarshal(data, &dto))
	assert.Equal(t, t480Tech, dto.TechID)
	t480AssertOneTimePwd(t, dto.Password)

	require.Equal(t, 1, e.store.resetTechPwdCalls, "一次请求只许一次写")
	assert.Equal(t, t480Tech, e.store.lastResetTechID)
	require.NotEmpty(t, e.store.lastResetTechHash)
	assert.NotEqual(t, dto.Password, e.store.lastResetTechHash, "落库的不是明文")
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(e.store.lastResetTechHash), []byte(dto.Password)),
		"落库哈希必须能验出响应里的一次性口令")

	// 审计留痕一条，且口令与哈希都不出现在审计里
	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Equal(t, auditActionDataModify, row.Action)
	assert.Equal(t, "technician", row.TargetType)
	assert.Equal(t, t480Tech, row.TargetID)
	assert.Contains(t, row.Description, "重置")
	assert.False(t, strings.Contains(row.Description, dto.Password), "审计描述不得带出口令：%s", row.Description)
	detail, err := json.Marshal(row.Detail)
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(detail), dto.Password), "审计 detail 不得带出口令：%s", detail)
	assert.False(t, strings.Contains(string(detail), e.store.lastResetTechHash), "审计 detail 不得带出哈希：%s", detail)
}

// TestT480_ResetPasswordNonAdminForbidden 非 admin（含缺失 X-Role）→ 403，且 store 零触达。
// 纵深防御：gateway RBAC 已在入口收口，这里兜「绕网关直连服务」。
func TestT480_ResetPasswordNonAdminForbidden(t *testing.T) {
	lowRoles := []struct {
		name    string
		headers map[string]string
	}{
		{"technician", map[string]string{"X-Role": "technician", "X-User-Id": t480Tech}},
		{"patient", map[string]string{"X-Role": "patient", "X-User-Id": "P20260001"}},
		{"doctor", map[string]string{"X-Role": "ROLE_DOCTOR", "X-User-Id": "D0001"}},
		{"cs", map[string]string{"X-Role": "ROLE_CS", "X-User-Id": "CS001"}},
		{"missing-role-header", map[string]string{"X-User-Id": "TECH0002"}},
		{"forged-self-id", map[string]string{"X-Role": "technician", "X-User-Id": t480Tech}},
	}

	for _, lr := range lowRoles {
		t.Run(lr.name, func(t *testing.T) {
			e := t480Env(t)

			code, respCode, _, _ := t480Reset(t, e, lr.headers)

			assert.Equal(t, http.StatusForbidden, code, "角色 %s 应 403", lr.name)
			assert.Equal(t, model.CodeForbidden, respCode)
			assert.Equal(t, 0, e.store.resetTechPwdCalls, "不得触达 store.SetTechnicianPassword")
			assert.Empty(t, e.store.lastTechQuery, "不得触达 store.GetTechnician")
			assert.Empty(t, e.store.auditRows, "被拒的请求不产生审计行")
		})
	}
}

// TestT480_ResetPasswordUnknownTechnician 未知技师号 → 404 且零写（存在性判定排在写之前）
func TestT480_ResetPasswordUnknownTechnician(t *testing.T) {
	e := newEnv(t, true, true) // 不放任何技师夹具

	code, respCode, _, _ := t480Reset(t, e, adminHdr)

	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, model.CodeNotFound, respCode)
	assert.Equal(t, 0, e.store.resetTechPwdCalls, "存在性判定必须排在写之前")
	assert.Empty(t, e.store.auditRows)
}

// TestT480_ResetPasswordStoreFailure 写库失败 → 500 不留审计行；读库失败不得继续写
func TestT480_ResetPasswordStoreFailure(t *testing.T) {
	e := t480Env(t)
	e.store.resetTechPwdErr = assert.AnError

	code, respCode, _, _ := t480Reset(t, e, adminHdr)
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.NotEqual(t, model.CodeOK, respCode)
	assert.Empty(t, e.store.auditRows, "写失败不得记成成功留痕")

	e2 := t480Env(t)
	e2.store.techErr = assert.AnError
	code2, _, _, _ := t480Reset(t, e2, adminHdr)
	assert.Equal(t, http.StatusInternalServerError, code2)
	assert.Equal(t, 0, e2.store.resetTechPwdCalls, "存在性读失败不得继续写")
}

// TestT480_ReSetInvalidatesPrevious 重设后旧口令即时失效（T314 同语义）
func TestT480_ReSetInvalidatesPrevious(t *testing.T) {
	e := t480Env(t)

	_, _, _, d1 := t480Reset(t, e, adminHdr)
	var dto1 model.TechnicianPasswordResetDTO
	require.NoError(t, json.Unmarshal(d1, &dto1))
	firstHash := e.store.lastResetTechHash

	_, _, _, d2 := t480Reset(t, e, adminHdr)
	var dto2 model.TechnicianPasswordResetDTO
	require.NoError(t, json.Unmarshal(d2, &dto2))
	require.NotEqual(t, dto1.Password, dto2.Password, "两次发号必须不同")
	require.Equal(t, 2, e.store.resetTechPwdCalls)
	assert.NotEqual(t, firstHash, e.store.lastResetTechHash, "两次落库的哈希必须不同（bcrypt 含盐）")

	// 库里只剩最后一次写的哈希 ⇒ 旧口令在库侧已无可验对象
	e.store.techLogin = &repo.TechLoginRow{
		TechID: t480Tech, Name: "技师老陈", PasswordHash: e.store.lastResetTechHash,
		Status: "enabled", AuthStatus: "authorized",
	}
	wOld, respOld := e.do(http.MethodPost, "/api/v1/tech/login",
		map[string]string{"phone": t480Phone, "password": dto1.Password}, nil)
	assert.Equal(t, http.StatusUnauthorized, wOld.Code, "旧口令在重设后必须立即失效：%s", respOld.Message)

	wNew, respNew := e.do(http.MethodPost, "/api/v1/tech/login",
		map[string]string{"phone": t480Phone, "password": dto2.Password}, nil)
	assert.Equal(t, http.StatusOK, wNew.Code, "新口令必须可用：%s", respNew.Message)
}
