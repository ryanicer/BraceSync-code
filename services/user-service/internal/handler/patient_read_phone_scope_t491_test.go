// T491 取舍落码：补进来的 phone_enc 投影只供服务内脱敏取数，患者读接口的对外形状一个键都不变。
//
// 卡面第①条要求先定取舍：「列表与详情要不要回密文、回给谁」。结论（甲案）是
// 密文只到 PatientRow（与医护域 doctorColumns / 技师域 techColumns 同口径），
// 两个只读端点不回手机号、也不新增三态字段；唯一带回脱敏号的是写响应（建档 / 分配团队）。
//
// 本文件证的是「取舍没走偏」，不是缺陷本体 —— 缺陷（投影缺列）在这层看不见，
// 因为 fakeStore 直接往行结构体塞 PhoneEnc；那一格由 repo 包的
// patient_phone_projection_t491_it_test.go 在真库里钉。
// 这里防的是反向事故：为了修审计把密文或脱敏号塞进列表/详情响应。
package handler

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// t491RowWithRealCiphertext 行结构体带「能解开的真密文」——fakeStore 递到 handler 的就是这一份，
// 与真库补投影后 GetPatient 回行的形状一致。
func t491RowWithRealCiphertext(t *testing.T, withPhone bool) repo.PatientRow {
	t.Helper()
	p := samplePatient()
	if withPhone {
		p.PhoneEnc = t450CipherPhone(t, t450PhoneBefore)
	}
	return p
}

// t491AssertNoCiphertextLeak 响应里不得出现密文（或其十六进制串）与明文手机号；
// 三态字段也不许外扩到患者域。脱敏号本身在写响应是合法产物，所以单列一条给读接口判。
func t491AssertNoCiphertextLeak(t *testing.T, body string, enc []byte) {
	t.Helper()
	assert.NotContains(t, body, hex.EncodeToString(enc), "密文十六进制串不得出现在响应里")
	assert.NotContains(t, body, t450PhoneBefore, "明文手机号不得出现在响应里")
	assert.NotContains(t, body, "phoneState", "患者域读接口不新增三态字段（医护域口径不外扩）")
}

// TestT491_PatientReadEndpointsStillCarryNoPhone 列表与详情两条读法：
// 服务内拿得到密文（下面先断言 fakeStore 确实给了行），对外 phone 仍是空串、且无任何手机号痕迹。
func TestT491_PatientReadEndpointsStillCarryNoPhone(t *testing.T) {
	e := newEnv(t, true, true)
	p := t491RowWithRealCiphertext(t, true)
	enc := p.PhoneEnc
	require.NotEmpty(t, enc, "前提：行结构体里确实带着密文（补投影后真库就是这个形状）")
	e.store.patient = &p
	e.store.patients = []repo.PatientRow{p}
	e.store.patientTotal = 1

	t.Run("列表", func(t *testing.T) {
		w, resp := e.do(http.MethodGet, "/api/v1/admin/patients", nil, nil)
		require.Equal(t, http.StatusOK, w.Code)
		var page model.PageData
		require.NoError(t, json.Unmarshal(resp.Data, &page))
		var list []model.AdminPatientDTO
		raw, err := json.Marshal(page.List)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &list))
		require.Len(t, list, 1)
		assert.Equal(t, "", list[0].Phone, "列表不回手机号（T361 起就这样，本卡不改对外形状）")
		t491AssertNoCiphertextLeak(t, w.Body.String(), enc)
		assert.NotContains(t, w.Body.String(), "138****", "读接口不得回脱敏手机号")
	})

	t.Run("详情", func(t *testing.T) {
		w, resp := e.do(http.MethodGet, "/api/v1/admin/patients/P20260001", nil, nil)
		require.Equal(t, http.StatusOK, w.Code)
		var dto model.AdminPatientDTO
		require.NoError(t, json.Unmarshal(resp.Data, &dto))
		assert.Equal(t, "", dto.Phone, "详情不回手机号")
		t491AssertNoCiphertextLeak(t, w.Body.String(), enc)
		assert.NotContains(t, w.Body.String(), "138****", "读接口不得回脱敏手机号")
	})
}

// TestT491_AssignTeamWriteResponseCarriesMaskedPhone 取舍的另一半：写响应回填脱敏号是
// 契约既有口径（shared-types Patient.phone 注释点名 POST 与 PUT team 两条），
// 缺陷期间它只能恒回空串；投影补上后必须真的带回脱敏值。
func TestT491_AssignTeamWriteResponseCarriesMaskedPhone(t *testing.T) {
	e := newEnv(t, true, true)
	p := t491RowWithRealCiphertext(t, true)
	enc := p.PhoneEnc
	e.store.assignedPatient = &p
	e.store.assignPatientErr = nil

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001/team",
		model.AssignTeamRequestDTO{TeamID: "TEAM02"}, adminHdr)
	require.Equal(t, http.StatusOK, w.Code)

	var dto model.AdminPatientDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "138****1111", dto.Phone, "写响应回填脱敏号（缺陷期间恒为空串）")
	t491AssertNoCiphertextLeak(t, w.Body.String(), enc)
}

// TestT491_AssignTeamWriteResponseForNoPhoneStaysEmpty 库里没号时写响应回空串而不是 "***"：
// 三态在患者域被压成单列，absent 必须是空串，占位符只属于「解不开」（phone.View 的既定语义）。
func TestT491_AssignTeamWriteResponseForNoPhoneStaysEmpty(t *testing.T) {
	e := newEnv(t, true, true)
	p := t491RowWithRealCiphertext(t, false)
	require.Empty(t, p.PhoneEnc)
	e.store.assignedPatient = &p

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001/team",
		model.AssignTeamRequestDTO{TeamID: "TEAM02"}, adminHdr)
	require.Equal(t, http.StatusOK, w.Code)

	var dto model.AdminPatientDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "", dto.Phone, "没存号的行不得回占位符，否则「无手机号」被读成「有号但读不出」")
	assert.NotContains(t, w.Body.String(), phone.MaskUnavailable)
}
