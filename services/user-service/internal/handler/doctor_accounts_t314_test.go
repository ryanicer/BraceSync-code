// Package handler — T314 医护账号管理·后端四类写端点
//
// 卡面验收：创建/编辑/重置密码/禁用启用（🔴 无 DELETE）、服务端发号 doc+5 位、
// 随机初始密码一次性返回、手机号选填且服务端脱敏、关键操作留痕。
//
// fakeStore 只证 handler 的校验与装配；双表落库与发号唯一性见
// services/user-service/internal/repo/doctor_accounts_t314_it_test.go（CI testcontainers）。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t314Doctor  = "DOC0A1B2C3D4E5"
	t314Team    = "TEAM01"
	t314Account = "doc00006"
)

func t314AdminHdr() map[string]string { return selfHdr("A0001", "ROLE_ADMIN") }

// t314BoundRow 已绑登录账号的医护行（admins 侧三列非 nil）——创建/编辑/启停的回读形态
func t314BoundRow() *repo.DoctorRow {
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	adminID, title, dept := "ADM0A1B2C3D4E5", "护士", "康复科"
	team := t314Team
	return &repo.DoctorRow{
		DoctorID:         t314Doctor,
		Name:             "小林",
		Title:            &title,
		Department:       &dept,
		TeamID:           &team,
		Status:           "enabled",
		PatientCount:     0,
		AdminID:          &adminID,
		Username:         strp(t314Account),
		AccountStatus:    strp("enabled"),
		AccountCreatedAt: &now,
	}
}

// t314Body 组装创建请求体；omit 列出的键不下发（用于验手机号选填与 status 缺省）
func t314Body(fields map[string]any, omit ...string) map[string]any {
	body := map[string]any{
		"name": "小林", "title": "护士", "department": "康复科",
		"teamId": t314Team, "phone": "13800000001", "status": "enabled",
	}
	for k, v := range fields {
		body[k] = v
	}
	for _, k := range omit {
		delete(body, k)
	}
	return body
}

func t314CreateDTO(t *testing.T, raw json.RawMessage) model.DoctorAccountCreateDTO {
	t.Helper()
	var dto model.DoctorAccountCreateDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	return dto
}

// t314DoctorDTO 编辑/启停端点的响应体（裸 DoctorDTO，不带一次性密码）
func t314DoctorDTO(t *testing.T, raw json.RawMessage) model.DoctorDTO {
	t.Helper()
	var dto model.DoctorDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	return dto
}

// ─────────────────────────────────────────────────────────────
// 创建
// ─────────────────────────────────────────────────────────────

func TestT314_CreateDoctorAccount_OK(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.created = t314BoundRow()

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil), t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, "body=%s", resp.Message)
	require.Equal(t, model.CodeOK, resp.Code)

	dto := t314CreateDTO(t, resp.Data)
	assert.Equal(t, t314Doctor, dto.DoctorID)
	assert.Equal(t, t314Account, *dto.Username, "登录账号由服务端发号并回显")
	assert.Equal(t, "138****0001", dto.PhoneMasked, "手机号服务端脱敏（§9.2）")
	assert.NotEmpty(t, dto.InitialPassword)

	in := e.store.docAcct.lastCreate
	assert.Equal(t, "小林", in.Name)
	assert.Equal(t, "护士", in.Title)
	assert.Equal(t, "康复科", in.Department)
	assert.Equal(t, t314Team, in.TeamID)
	assert.Equal(t, "enabled", in.Status)
	assert.Equal(t, phone.Hash("13800000001"), in.PhoneHash, "phone_hash 走 SHA-256 明文哈希")
	require.NotEmpty(t, in.PhoneEnc)
	dec, err := e.h.phone.Decrypt(in.PhoneEnc)
	require.NoError(t, err)
	assert.Equal(t, "13800000001", dec, "phone_enc 可解密回原号")

	// 🔴 落库的是 bcrypt 不是明文：一次性展示完之后任何人都（包括运维查库）取不回密码
	assert.NotEqual(t, dto.InitialPassword, in.PasswordHash)
	require.NoError(t, CompareBcryptHash([]byte(in.PasswordHash), []byte(dto.InitialPassword)))
	assert.True(t, strings.HasPrefix(in.PasswordHash, "$2"), "密码走 bcrypt（§7A.1 口径不变）")
}

// TestT314_CreateDoctorAccount_OneTimePasswordShape 密码形态：Br + 12 位随机 + #7，共 16 位；
// 且多次生成的随机段不得相同（crypto/rand，🔴 不是设计稿演示用的 Math.random）。
func TestT314_CreateDoctorAccount_OneTimePasswordShape(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.created = t314BoundRow()

	seen := map[string]bool{}
	pattern := regexp.MustCompile(`^Br[A-Za-z0-9]{12}#7$`)
	for i := 0; i < 30; i++ {
		w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil), t314AdminHdr())
		require.Equal(t, http.StatusOK, w.Code, "body=%s", resp.Message)
		pwd := t314CreateDTO(t, resp.Data).InitialPassword
		assert.Len(t, pwd, doctorPasswordLen, "长度须与设计稿 genPwd 形态同量级")
		assert.Regexp(t, pattern, pwd)
		for _, c := range []byte(pwd[2:14]) {
			assert.GreaterOrEqual(t, strings.IndexByte(doctorPwdAlphabet, c), 0,
				"随机段含字母表外的字符 %q", c)
		}
		assert.False(t, seen[pwd], "第 %d 次生成了重复密码 %s", i, pwd)
		seen[pwd] = true
	}
}

// TestT314_CreateDoctorAccount_PhoneOptional 手机号选填（V3.19 由必填改选填）：
// 不下发 → 两列都落 NULL；下发空串 → 同样按未填处理。
func TestT314_CreateDoctorAccount_PhoneOptional(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.created = t314BoundRow()

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil, "phone"), t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Empty(t, e.store.docAcct.lastCreate.PhoneEnc)
	assert.Empty(t, e.store.docAcct.lastCreate.PhoneHash, "空串不得落 sha256(空串) 占位哈希")

	w, resp = e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(map[string]any{"phone": "  "}), t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Empty(t, e.store.docAcct.lastCreate.PhoneHash)
}

// TestT314_CreateDoctorAccount_StatusDefaultsEnabled 初始状态缺省 = 启用（PRD（4）默认启用）
func TestT314_CreateDoctorAccount_StatusDefaultsEnabled(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.created = t314BoundRow()

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil, "status"), t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, accountStatusEnabled, e.store.docAcct.lastCreate.Status)
}

func TestT314_CreateDoctorAccount_DisabledStatusAllowed(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.created = t314BoundRow()

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors",
		t314Body(map[string]any{"status": "disabled"}), t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, accountStatusDisabled, e.store.docAcct.lastCreate.Status)
}

// TestT314_CreateDoctorAccount_RequiredFields PRD（4）：姓名/科室/所属团队/职称必填
func TestT314_CreateDoctorAccount_RequiredFields(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	for _, tc := range []struct{ name, key string }{
		{"姓名缺失", "name"}, {"职称缺失", "title"}, {"科室缺失", "department"}, {"团队缺失", "teamId"},
	} {
		w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil, tc.key), t314AdminHdr())
		assert.Equal(t, http.StatusBadRequest, w.Code, tc.name)
		assert.Equal(t, model.CodeInvalidParam, resp.Code, tc.name)
		assert.Contains(t, resp.Message, tc.key, tc.name)
	}
	// 全空白同样按未填处理，不得把 "   " 落进库
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors",
		t314Body(map[string]any{"name": "   "}), t314AdminHdr())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Empty(t, e.store.docAcct.created, "非法入参不得触库")
	assert.Equal(t, "", e.store.docAcct.lastCreate.Name)
}

// TestT314_CreateDoctorAccount_BadStatus 状态不在 CHECK 枚举内 → 400，不让运维看到库约束 500
func TestT314_CreateDoctorAccount_BadStatus(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors",
		t314Body(map[string]any{"status": "停用"}), t314AdminHdr())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Contains(t, resp.Message, "status")
}

func TestT314_CreateDoctorAccount_BadPhone(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	for _, p := range []string{"1380000000", "23800000001", "1380000000a"} {
		w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors",
			t314Body(map[string]any{"phone": p}), t314AdminHdr())
		assert.Equal(t, http.StatusBadRequest, w.Code, "phone=%s", p)
		assert.Equal(t, model.CodeInvalidParam, resp.Code, "phone=%s", p)
	}
}

// TestT314_CreateDoctorAccount_FieldTooLong 超列宽先回 400（admins.name/doctors.department VARCHAR(64/128)）
func TestT314_CreateDoctorAccount_FieldTooLong(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	for _, tc := range []struct {
		key, label string
		val        string
		max        int
	}{
		{"name", "name", strings.Repeat("长", doctorNameMaxLen+1), doctorNameMaxLen},
		{"title", "title", strings.Repeat("长", doctorTitleMaxLen+1), doctorTitleMaxLen},
		{"department", "department", strings.Repeat("长", doctorDeptMaxLen+1), doctorDeptMaxLen},
		{"teamId", "teamId", strings.Repeat("T", doctorTeamMaxLen+1), doctorTeamMaxLen},
	} {
		w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors",
			t314Body(map[string]any{tc.key: tc.val}), t314AdminHdr())
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s 超长应 400", tc.label)
		assert.Contains(t, resp.Message, tc.label)
	}
}

// TestT314_CreateDoctorAccount_TeamNotFound 团队 FK 前置：不存在回 400 而不是 23503 的 500
func TestT314_CreateDoctorAccount_TeamNotFound(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = false
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil), t314AdminHdr())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Contains(t, resp.Message, t314Team)
}

// TestT314_CreateDoctorAccount_NoEncryptionKey 手机号加密密钥缺失 → 500 配置错误，不静默落明文
func TestT314_CreateDoctorAccount_NoEncryptionKey(t *testing.T) {
	e := newEnv(t, true, false)
	e.store.teamExists = true
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil), t314AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)
}

// TestT314_CreateDoctorAccount_UsernameExhausted 发号连续撞已占用序号 = 数据异常 → 500，
// 不得压成 400 让运营以为是自己的入参问题。
func TestT314_CreateDoctorAccount_UsernameExhausted(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.createErr = repo.ErrUsernameExhausted
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil), t314AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, resp.Message, "sequence")
}

// ─────────────────────────────────────────────────────────────
// 编辑
// ─────────────────────────────────────────────────────────────

func TestT314_UpdateDoctorAccount_OK(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.updated = t314BoundRow()

	w, resp := e.do(http.MethodPut, "/api/v1/admin/doctors/"+t314Doctor,
		map[string]any{"name": "小林娜", "title": "护师", "department": "骨科", "teamId": "TEAM02",
			"phone": "13900000002"}, t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, "body=%s", resp.Message)

	assert.Equal(t, t314Doctor, e.store.docAcct.lastUpdateID)
	in := e.store.docAcct.lastUpdate
	require.NotNil(t, in.Name)
	assert.Equal(t, "小林娜", *in.Name)
	require.NotNil(t, in.TeamID)
	assert.Equal(t, "TEAM02", *in.TeamID)
	require.NotNil(t, in.PhoneHash)
	assert.Equal(t, phone.Hash("13900000002"), *in.PhoneHash)
	assert.Equal(t, t314Account, *t314DoctorDTO(t, resp.Data).Username, "响应回显 admins 侧登录账号")
}

// TestT314_UpdateDoctorAccount_DoesNotTouchPassword 🔴 编辑端点无密码通道（PRD（4）编辑态密码分组隐藏）：
// 请求里塞 password 也必须被忽略，且不得调用改密方法。
func TestT314_UpdateDoctorAccount_DoesNotTouchPassword(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.updated = t314BoundRow()

	w, resp := e.do(http.MethodPut, "/api/v1/admin/doctors/"+t314Doctor,
		map[string]any{"name": "小林娜", "title": "护师", "department": "骨科", "teamId": t314Team,
			"password": "BrHACKEDHACKED#7", "username": "doc99999"}, t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, t314Account, *e.store.docAcct.updated.Username, "登录账号不因入参改写")
	assert.Equal(t, "", e.store.docAcct.lastPwHash, "编辑不得改密码哈希")
}

// TestT314_UpdateDoctorAccount_PhoneAbsentKeeps 指针语义：phone 不下发 = 保持原号；
// 下发空串 = 清空（enc 传 nil、hash 传空串指针，由 repo 落两列 NULL）。
func TestT314_UpdateDoctorAccount_PhoneAbsentKeeps(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.updated = t314BoundRow()

	w, resp := e.do(http.MethodPut, "/api/v1/admin/doctors/"+t314Doctor,
		map[string]any{"name": "小林娜", "title": "护师", "department": "骨科", "teamId": t314Team},
		t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Nil(t, e.store.docAcct.lastUpdate.PhoneHash, "未下发 phone 不得清空")

	w, resp = e.do(http.MethodPut, "/api/v1/admin/doctors/"+t314Doctor,
		map[string]any{"name": "小林娜", "title": "护师", "department": "骨科", "teamId": t314Team,
			"phone": ""}, t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	require.NotNil(t, e.store.docAcct.lastUpdate.PhoneHash)
	assert.Equal(t, "", *e.store.docAcct.lastUpdate.PhoneHash)
	assert.Empty(t, e.store.docAcct.lastUpdate.PhoneEnc)
}

func TestT314_UpdateDoctorAccount_NotFound(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.updateErr = repo.ErrDoctorNotFound
	w, resp := e.do(http.MethodPut, "/api/v1/admin/doctors/DOCNOPE",
		map[string]any{"name": "甲", "title": "护士", "department": "骨科", "teamId": t314Team},
		t314AdminHdr())
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)
	assert.Contains(t, resp.Message, "DOCNOPE")
}

// TestT314_UpdateDoctorAccount_PartialBody 给了任一项档案字段即按整组必填（模态框本来就带全 4 项）
func TestT314_UpdateDoctorAccount_PartialBody(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodPut, "/api/v1/admin/doctors/"+t314Doctor,
		map[string]any{"name": "只有姓名"}, t314AdminHdr())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp.Message, "title")
}

// ─────────────────────────────────────────────────────────────
// 重置密码
// ─────────────────────────────────────────────────────────────

func TestT314_ResetDoctorAccountPassword_OK(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.docAcct.pwRow = t314BoundRow()

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors/"+t314Doctor+"/reset-password", nil, t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, "body=%s", resp.Message)

	var dto model.DoctorAccountResetDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, t314Doctor, dto.DoctorID)
	assert.Equal(t, t314Account, dto.Username)
	require.Len(t, dto.Password, doctorPasswordLen)
	assert.NotEmpty(t, e.store.docAcct.lastPwHash)
	// 返回的明文与落库哈希必须配对，否则「重置成功」是假的
	require.NoError(t, CompareBcryptHash([]byte(e.store.docAcct.lastPwHash), []byte(dto.Password)))
}

// TestT314_ResetDoctorAccountPassword_NotSameCodeAsLogin
// PRD（6）：重置/禁用的反馈走写接口自身的返回码，🔴 不得复用登录侧 10401（凭据错误与禁用同码）。
func TestT314_ResetDoctorAccountPassword_NotSameCodeAsLogin(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.docAcct.pwErr = repo.ErrDoctorNotFound
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors/DOCNOPE/reset-password", nil, t314AdminHdr())
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)
	assert.NotEqual(t, model.CodeUnauthorized, resp.Code, "不得回登录侧 10401")
}

// TestT314_ResetDoctorAccountPassword_ProfileWithoutAccount seed D0002/D0003 这类
// admin_id 为 NULL 的存量档案：档案在、无凭据可改 ⇒ 409 讲清差在哪，不是 404 也不是假成功。
func TestT314_ResetDoctorAccountPassword_ProfileWithoutAccount(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.docAcct.pwErr = repo.ErrDoctorNoAccount
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors/D0002/reset-password", nil, t314AdminHdr())
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, model.CodeConflict, resp.Code)
	assert.Contains(t, resp.Message, "D0002")
	assert.Contains(t, resp.Message, "no login account")
}

// ─────────────────────────────────────────────────────────────
// 禁用 / 启用
// ─────────────────────────────────────────────────────────────

func TestT314_SetDoctorAccountStatus_Disable(t *testing.T) {
	e := newEnv(t, true, true)
	disabled := "disabled"
	row := t314BoundRow()
	row.AccountStatus = &disabled
	row.Status = disabled
	e.store.docAcct.statusRow = row

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors/"+t314Doctor+"/status",
		map[string]string{"action": "disable"}, t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, "body=%s", resp.Message)
	assert.Equal(t, t314Doctor, e.store.docAcct.lastStatID)
	assert.Equal(t, accountStatusDisabled, e.store.docAcct.lastStatus)
	dto := t314DoctorDTO(t, resp.Data)
	require.NotNil(t, dto.AccountStatus)
	assert.Equal(t, disabled, *dto.AccountStatus, "响应回显登录层状态，前端据此翻开关")
}

func TestT314_SetDoctorAccountStatus_Enable(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.docAcct.statusRow = t314BoundRow()
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors/"+t314Doctor+"/status",
		map[string]string{"action": "enable"}, t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, accountStatusEnabled, e.store.docAcct.lastStatus)
}

func TestT314_SetDoctorAccountStatus_BadAction(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors/"+t314Doctor+"/status",
		map[string]string{"action": "stop"}, t314AdminHdr())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Empty(t, e.store.docAcct.lastStatus, "非法 action 不得触库")
}

func TestT314_SetDoctorAccountStatus_ProfileWithoutAccount(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.docAcct.statusErr = repo.ErrDoctorNoAccount
	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors/D0003/status",
		map[string]string{"action": "disable"}, t314AdminHdr())
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, model.CodeConflict, resp.Code)
}

// ─────────────────────────────────────────────────────────────
// 无删除端点（PRD（2）/卡面 6：接口不含 DELETE）
// ─────────────────────────────────────────────────────────────

// TestT314_NoDeleteEndpointRegistered 「删除」在 PRD（2）无入口 ⇒ 路由组里不得有 DELETE。
// 直连 Router 而非 e.do：gin 的未注册路径回的是纯文本 404，套不进响应信封。
func TestT314_NoDeleteEndpointRegistered(t *testing.T) {
	e := newEnv(t, true, true)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/doctors/"+t314Doctor, nil)
	req.Header.Set("X-User-Id", "A0001")
	req.Header.Set("X-Role", "ROLE_ADMIN")
	w := httptest.NewRecorder()
	e.h.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code, "DELETE /admin/doctors/:doctorId 不应存在")
	assert.NotContains(t, w.Body.String(), `"code"`, "未注册路径不得回业务信封")
}

// ─────────────────────────────────────────────────────────────
// 留痕（PRD §7D.10（6）「本页全部写操作计入操作日志」）
// ─────────────────────────────────────────────────────────────

func TestT314_WritesAreAuditedWithoutPassword(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.created = t314BoundRow()
	e.store.docAcct.updated = t314BoundRow()
	e.store.docAcct.statusRow = t314BoundRow()
	e.store.docAcct.pwRow = t314BoundRow()

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil), t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	plain := t314CreateDTO(t, resp.Data).InitialPassword

	_, _ = e.do(http.MethodPut, "/api/v1/admin/doctors/"+t314Doctor,
		map[string]any{"name": "小林娜", "title": "护师", "department": "骨科", "teamId": t314Team}, t314AdminHdr())
	_, _ = e.do(http.MethodPost, "/api/v1/admin/doctors/"+t314Doctor+"/status",
		map[string]string{"action": "disable"}, t314AdminHdr())
	_, _ = e.do(http.MethodPost, "/api/v1/admin/doctors/"+t314Doctor+"/reset-password", nil, t314AdminHdr())

	require.Len(t, e.store.auditRows, 4, "创建/编辑/启停/重置四类写操作都要留痕")
	want := map[string]bool{"创建医护账号": false, "编辑医护账号": false, "启停医护账号": false, "重置医护账号": false}
	for _, row := range e.store.auditRows {
		assert.Equal(t, "data_modify", row.Action)
		assert.Equal(t, "doctor", row.TargetType)
		assert.False(t, strings.Contains(row.Description, plain),
			"审计描述不得带出一次性密码：%s", row.Description)
		for k := range want {
			if strings.Contains(row.Description, k) {
				want[k] = true
			}
		}
	}
	for k, hit := range want {
		assert.True(t, hit, "缺少「%s」的留痕", k)
	}
	assert.Equal(t, "A0001", e.store.auditRows[0].OperatorID, "操作人取 gateway 注入头")
}

// TestT314_FailedWritesNotAudited 校验失败/不存在的请求不产生审计噪声（与 T252 中间件同口径）
func TestT314_FailedWritesNotAudited(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.updateErr = errors.New("db down")

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors",
		t314Body(map[string]any{"status": "停用"}), t314AdminHdr())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code, "status 非枚举应 400：%s", resp.Message)
	assert.Empty(t, e.store.auditRows)

	w, resp = e.do(http.MethodPut, "/api/v1/admin/doctors/"+t314Doctor,
		map[string]any{"name": "甲", "title": "护士", "department": "骨科", "teamId": t314Team}, t314AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w.Code, resp.Message)
	assert.Empty(t, e.store.auditRows)
}

// ─────────────────────────────────────────────────────────────
// 读侧：GET /api/v1/doctors 的 admins 侧三列
// ─────────────────────────────────────────────────────────────

func TestT314_ListDoctors_ExposesAccountColumns(t *testing.T) {
	e := newEnv(t, true, true)
	bound := t314BoundRow()
	// seed 同形态：D0002 没有登录账号
	unbound := &repo.DoctorRow{DoctorID: "D0002", Name: "王医师", Status: "enabled"}
	e.store.doctors = []repo.DoctorRow{*bound, *unbound}

	w, resp := e.do(http.MethodGet, "/api/v1/doctors", nil, t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var list []model.DoctorDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 2)

	assert.Equal(t, t314Account, *list[0].Username)
	require.NotNil(t, list[0].CreatedAt)
	assert.Equal(t, "2026-09-22T10:00:00Z", *list[0].CreatedAt, "UTC + Z 标记，与既有 createdAt 同格式")
	require.NotNil(t, list[0].AccountStatus)
	assert.Equal(t, "enabled", *list[0].AccountStatus)

	assert.Nil(t, list[1].Username, "未绑账号须回 null，不得填空串冒充有值")
	assert.Nil(t, list[1].CreatedAt)
	assert.Nil(t, list[1].AccountStatus)
	assert.Equal(t, "", list[1].PhoneMasked, "无手机号回空串，前端渲染破折号")
}
