// Package handler — T361 手机号读侧三态（接口层可辨「没有」与「解不开」）
//
// 缺陷原貌：Cipher.Masked 把「密文列为空」返回 ""、「密文解不开」返回 "***"，
// 两个语义在 JSON 里都只是 phoneMasked 的一个字符串，调用方无从分辨。
// admin-web 医护账号页因此把脱敏串预填进可编辑输入框，运营清空保存即触发
// 服务端「空串即清空手机号」的写语义，把库内真号洗成 NULL。
//
// 本文件证：三态在响应里各归各值，且展示串口径与改造前逐字相同（只加语义、不改文案）。
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

func t361Enc(t *testing.T, plain string) []byte {
	t.Helper()
	c, err := phone.NewCipher(testPhoneKey)
	require.NoError(t, err)
	enc, err := c.Encrypt(plain)
	require.NoError(t, err)
	return enc
}

func TestT361_DoctorReadThreeStates(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.doctors = []repo.DoctorRow{
		{DoctorID: "D-A", Name: "有号", PhoneEnc: t361Enc(t, "13800001111"), Status: "enabled"},
		{DoctorID: "D-B", Name: "无号", Status: "enabled"},
		{DoctorID: "D-C", Name: "解不开", PhoneEnc: []byte{0x00}, Status: "enabled"}, // seed '\x00'::bytea
	}

	w, resp := e.do(http.MethodGet, "/api/v1/doctors", nil, t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var list []model.DoctorDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 3)

	assert.Equal(t, "138****1111", list[0].PhoneMasked, "脱敏文案与改造前一致")
	assert.Equal(t, string(phone.PhoneStateMasked), list[0].PhoneState)

	assert.Equal(t, "", list[1].PhoneMasked, "无手机号仍是空串，前端渲染破折号")
	assert.Equal(t, string(phone.PhoneStateAbsent), list[1].PhoneState,
		"「库里没有」必须与「解不开」分家")

	assert.Equal(t, phone.MaskUnavailable, list[2].PhoneMasked)
	assert.Equal(t, string(phone.PhoneStateUnreadable), list[2].PhoneState,
		"seed 占位密文必须报 unreadable，前端据此禁止把展示串当可编辑值")
}

func TestT361_TechnicianReadThreeStates(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.techs = []repo.TechnicianRow{
		{TechID: "T-A", Name: "有号", PhoneEnc: t361Enc(t, "13900002222"), Status: "enabled", AuthStatus: "authorized"},
		{TechID: "T-B", Name: "无号", Status: "enabled", AuthStatus: "authorized"},
		{TechID: "T-C", Name: "解不开", PhoneEnc: make([]byte, 32), Status: "enabled", AuthStatus: "authorized"},
	}
	e.store.techTotal = 3

	w, resp := e.do(http.MethodGet, "/api/v1/technicians", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var page struct {
		List []model.TechnicianDTO `json:"list"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	require.Len(t, page.List, 3)

	assert.Equal(t, "139****2222", page.List[0].PhoneMasked)
	assert.Equal(t, string(phone.PhoneStateMasked), page.List[0].PhoneState)
	assert.Equal(t, "", page.List[1].PhoneMasked)
	assert.Equal(t, string(phone.PhoneStateAbsent), page.List[1].PhoneState)
	assert.Equal(t, phone.MaskUnavailable, page.List[2].PhoneMasked)
	assert.Equal(t, string(phone.PhoneStateUnreadable), page.List[2].PhoneState,
		"长度够 nonce 但认证失败（密钥轮换 / 数据损坏）也算 unreadable")
}

// TestT361_NilCipherDoesNotFakeAbsent PHONE_ENC_KEY 未配置时，库里存了密文的行不得被说成「没填手机号」
func TestT361_NilCipherDoesNotFakeAbsent(t *testing.T) {
	e := newEnv(t, true, false) // h.phone == nil
	require.Nil(t, e.h.phone)
	e.store.doctors = []repo.DoctorRow{
		{DoctorID: "D-N1", Name: "无号", Status: "enabled"},
		{DoctorID: "D-N2", Name: "有密文无密钥", PhoneEnc: []byte{0x00}, Status: "enabled"},
	}

	w, resp := e.do(http.MethodGet, "/api/v1/doctors", nil, t314AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var list []model.DoctorDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 2)

	assert.Equal(t, string(phone.PhoneStateAbsent), list[0].PhoneState)
	assert.Equal(t, "", list[0].PhoneMasked)
	assert.Equal(t, string(phone.PhoneStateUnreadable), list[1].PhoneState,
		"密钥缺失是环境问题，不得伪装成用户没填")
}

// TestT361_TeamMemberWriteCarriesState 成员写响应同样带三态（TeamMemberDTO 与 DoctorDTO 口径一致）
func TestT361_TeamMemberWriteCarriesState(t *testing.T) {
	cases := []struct {
		name string
		enc  []byte
		want string
	}{
		{"masked", t361Enc(t, "13700003333"), string(phone.PhoneStateMasked)},
		{"absent", nil, string(phone.PhoneStateAbsent)},
		{"unreadable", []byte{0x00}, string(phone.PhoneStateUnreadable)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			row := sampleTeamMember()
			row.PhoneEnc = tc.enc
			e.store.addedMember = ptrToTeamMember(row)
			w, resp := e.do(http.MethodPost, "/api/v1/teams/TEAM26001/members",
				model.AddMemberRequestDTO{MemberType: "doctor", MemberID: "D0002", Role: "主治医师"}, nil)
			require.Equal(t, http.StatusOK, w.Code, resp.Message)
			var dto model.TeamMemberDTO
			require.NoError(t, json.Unmarshal(resp.Data, &dto))
			assert.Equal(t, tc.want, dto.PhoneState)
		})
	}
}
