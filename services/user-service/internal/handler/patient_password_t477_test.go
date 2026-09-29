// Package handler T477：admin 通道患者设密端点的 handler 层行为。
//
// 这条端点是 patients.password_hash 唯一的 Go 写点。它存在的意义是：API 建档的患者
// 该列恒为 NULL，而患者手机号+密码登录（T037）比对的就是它 ⇒ 自建患者在 CI 里登不进去，
// 只有 seed 那几行能登（seed 只读，测试不许碰）。所以这里除了门禁判定，
// 必须把「设密之后真的能登录」这条链验通——那才是卡面判据。
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

const t477Patient = "P20260001"

func t477Path() string { return "/api/v1/admin/patients/" + t477Patient + "/password" }

// t477Env 放行到底所需的前置夹具：一条 active 患者档案行
func t477Env(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p
	return e
}

// t477DTO 解响应体（明文口令只在这一个信封里出现一次）
func t477DTO(t *testing.T, raw json.RawMessage) model.PatientPasswordSetDTO {
	t.Helper()
	var dto model.PatientPasswordSetDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	return dto
}

// TestT477_SetPatientPasswordAdminAllowed admin 放行：一次性口令 + 落库的是 bcrypt 哈希 + 审计不带口令
func TestT477_SetPatientPasswordAdminAllowed(t *testing.T) {
	e := t477Env(t)

	w, resp := e.do(http.MethodPost, t477Path(), nil, adminHdr)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	require.Equal(t, model.CodeOK, resp.Code)

	dto := t477DTO(t, resp.Data)
	assert.Equal(t, t477Patient, dto.PatientID)
	require.NotEmpty(t, dto.Password, "口令必须一次性返回，否则 CI 拿不到凭据")
	assert.Len(t, dto.Password, doctorPasswordLen, "复用 T314 发号器：长度形态不变")
	assert.True(t, strings.HasPrefix(dto.Password, doctorPwdPrefix) && strings.HasSuffix(dto.Password, doctorPwdSuffix),
		"复用 T314 发号器：前后缀不变")

	// 写库恰好一次，且写的是能验出该口令的 bcrypt 哈希
	require.Equal(t, 1, e.store.setPwdCalls, "一次请求只许一次写")
	assert.Equal(t, t477Patient, e.store.lastSetPwdPatient)
	require.NotEmpty(t, e.store.lastSetPwdHash)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(e.store.lastSetPwdHash), []byte(dto.Password)),
		"落库哈希必须能验出响应里的一次性口令")
	assert.NotEqual(t, dto.Password, e.store.lastSetPwdHash, "落库的不是明文")

	// 审计留痕一条，且口令与哈希都不出现在审计里
	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Equal(t, auditActionDataModify, row.Action)
	assert.Equal(t, "patient", row.TargetType)
	assert.Equal(t, t477Patient, row.TargetID)
	assert.Contains(t, row.Description, "设置登录口令")
	assert.False(t, strings.Contains(row.Description, dto.Password), "审计描述不得带出口令：%s", row.Description)
	detail, err := json.Marshal(row.Detail)
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(detail), dto.Password), "审计 detail 不得带出口令：%s", detail)
	assert.False(t, strings.Contains(string(detail), e.store.lastSetPwdHash), "审计 detail 不得带出哈希：%s", detail)
}

// TestT477_SetPatientPasswordNonAdminForbidden 非 admin（含缺失 X-Role）→ 403，且 store 零触达
// 纵深防御：gateway RBAC 已在入口收口，这里兜「绕网关直连服务」。
func TestT477_SetPatientPasswordNonAdminForbidden(t *testing.T) {
	lowRoles := []struct {
		name    string
		headers map[string]string
	}{
		{"patient", map[string]string{"X-Role": "patient", "X-User-Id": t477Patient}},
		{"doctor", map[string]string{"X-Role": "ROLE_DOCTOR", "X-User-Id": "D0001"}},
		{"cs", map[string]string{"X-Role": "ROLE_CS", "X-User-Id": "CS001"}},
		{"technician", map[string]string{"X-Role": "technician", "X-User-Id": "TECH001"}},
		{"missing-role-header", map[string]string{"X-User-Id": "P20260002"}},
		{"forged-self-id", map[string]string{"X-Role": "patient", "X-User-Id": t477Patient}},
	}

	for _, lr := range lowRoles {
		t.Run(lr.name, func(t *testing.T) {
			e := t477Env(t)

			w, resp := e.do(http.MethodPost, t477Path(), nil, lr.headers)

			assert.Equal(t, http.StatusForbidden, w.Code, "角色 %s 应 403", lr.name)
			assert.Equal(t, model.CodeForbidden, resp.Code)
			assert.Equal(t, 0, e.store.setPwdCalls, "不得触达 store.SetPatientPassword")
			assert.Empty(t, e.store.lastPatientQuery, "不得触达 store.GetPatient")
			assert.Empty(t, e.store.auditRows, "被拒的请求不产生审计行")
		})
	}
}

// TestT477_SetPatientPasswordUnknownPatient 未知患者号 → 404 且零写（不新建、不部分写）
func TestT477_SetPatientPasswordUnknownPatient(t *testing.T) {
	e := newEnv(t, true, true) // 不放任何患者夹具

	w, resp := e.do(http.MethodPost, "/api/v1/admin/patients/P-NOT-EXIST/password", nil, adminHdr)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)
	assert.Equal(t, 0, e.store.setPwdCalls, "存在性判定必须排在写之前")
	assert.Empty(t, e.store.auditRows)
}

// TestT477_SetPatientPasswordStoreFailure 写库失败 → 500，不留审计行（HTTP>=400 中间件跳过）
func TestT477_SetPatientPasswordStoreFailure(t *testing.T) {
	e := t477Env(t)
	e.store.setPwdErr = assert.AnError

	w, resp := e.do(http.MethodPost, t477Path(), nil, adminHdr)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotEqual(t, model.CodeOK, resp.Code)
	assert.Empty(t, e.store.auditRows, "写失败不得记成成功留痕")
}

// TestT477_SetThenPatientLoginRoundTrip 落地判据本体：设密之后，患者端能用手机号+口令登进来。
// 这一条同时锁住两端的格式约定（写侧 GenerateBcryptHash / 读侧 bcrypt 比对）不脱钩。
func TestT477_SetThenPatientLoginRoundTrip(t *testing.T) {
	e := t477Env(t)

	w, resp := e.do(http.MethodPost, t477Path(), nil, adminHdr)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	dto := t477DTO(t, resp.Data)

	// 把落库的哈希喂回登录查询投影（PGStore 里就是同一列）
	p := samplePatient()
	e.store.patientLogin = &repo.PatientLoginRow{
		PatientID: p.PatientID, Name: p.Name, PasswordHash: e.store.lastSetPwdHash, Status: p.Status,
	}

	okBody := map[string]string{"phone": "13800138000", "password": dto.Password}
	w2, resp2 := e.do(http.MethodPost, "/api/v1/patient/login", okBody, nil)
	require.Equal(t, http.StatusOK, w2.Code, "设密后必须登录成功：%s", resp2.Message)
	var login model.PatientLoginResultDTO
	require.NoError(t, json.Unmarshal(resp2.Data, &login))
	assert.Equal(t, t477Patient, login.PatientID)
	assert.Equal(t, "patient", login.Role)
	assert.NotEmpty(t, login.Token)

	w3, resp3 := e.do(http.MethodPost, "/api/v1/patient/login",
		map[string]string{"phone": "13800138000", "password": "wrong-password"}, nil)
	assert.Equal(t, http.StatusUnauthorized, w3.Code)
	assert.NotEqual(t, model.CodeOK, resp3.Code)
}

// TestT477_ReSetPasswordInvalidatesPrevious 重设后旧口令即时失效（T314 同语义）
func TestT477_ReSetPasswordInvalidatesPrevious(t *testing.T) {
	e := t477Env(t)

	w1, resp1 := e.do(http.MethodPost, t477Path(), nil, adminHdr)
	require.Equal(t, http.StatusOK, w1.Code, resp1.Message)
	first := t477DTO(t, resp1.Data).Password

	w2, resp2 := e.do(http.MethodPost, t477Path(), nil, adminHdr)
	require.Equal(t, http.StatusOK, w2.Code, resp2.Message)
	second := t477DTO(t, resp2.Data).Password
	require.NotEqual(t, first, second, "两次发号必须不同")
	require.Equal(t, 2, e.store.setPwdCalls)

	p := samplePatient()
	e.store.patientLogin = &repo.PatientLoginRow{
		PatientID: p.PatientID, Name: p.Name, PasswordHash: e.store.lastSetPwdHash, Status: p.Status,
	}

	wOld, _ := e.do(http.MethodPost, "/api/v1/patient/login",
		map[string]string{"phone": "13800138000", "password": first}, nil)
	assert.Equal(t, http.StatusUnauthorized, wOld.Code, "旧口令在重设后必须立即失效")

	wNew, respNew := e.do(http.MethodPost, "/api/v1/patient/login",
		map[string]string{"phone": "13800138000", "password": second}, nil)
	assert.Equal(t, http.StatusOK, wNew.Code, "新口令必须可用：%s", respNew.Message)
}
