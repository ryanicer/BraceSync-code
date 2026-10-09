// Package handler — T186 患者本人只读档案（GET /api/v1/patient/profile）实现侧测试
//
// 验收对应：
//  1. 患者以本人身份 → 200 + §7A.5 只读字段齐（真实 TCP + 真实签发 JWT，t.Logf 打脱敏 curl 与响应原文）；
//  2. 无身份头 → 403（fail-closed）；绑定态 JWT（scope=bind）→ 403/40301；
//     非患者身份（X-User-Id=ADM001）→ 404，且查询键恒等于身份头 ⇒ 路径无患者 ID，结构上无法越权；
//  3. DB 故障 → 500。
//
// 本文件不新增 DTO：断言对象即 handler 复用的 model.AdminPatientDTO。
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const profilePath = "/api/v1/patient/profile"

// selfHdr 模拟 gateway jwtAuth 注入的身份头（架构 §5.2：伪造头被剥离后由 JWT sub 重新写入）
func selfHdr(userID, role string) map[string]string {
	return map[string]string{"X-User-Id": userID, "X-Role": role}
}

// seedProfilePatient 装载 samplePatient 到 fake store 并返回该行
func seedProfilePatient(e *testEnv) repo.PatientRow {
	p := samplePatient()
	e.store.patient = &p
	return p
}

func TestT186_PatientProfile_SelfScope(t *testing.T) {
	// ① 本人 → 200 + 字段齐
	e := newEnv(t, true, true)
	row := seedProfilePatient(e)

	w, resp := e.do(http.MethodGet, profilePath, nil, selfHdr(row.PatientID, "patient"))
	require.Equal(t, http.StatusOK, w.Code, "本人身份应可读自己的档案")
	assert.Equal(t, "P20260001", e.store.lastPatientQuery, "查询键必须等于注入的 X-User-Id")

	var dto model.AdminPatientDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "P20260001", dto.PatientID)
	assert.Equal(t, "患者小明", dto.Name, "§7A.5 姓名")
	require.NotNil(t, dto.Gender)
	assert.Equal(t, "male", *dto.Gender, "§7A.5 性别")
	require.NotNil(t, dto.Age)
	assert.Equal(t, 14, *dto.Age, "§7A.5 年龄")
	require.NotNil(t, dto.Diagnosis)
	assert.Equal(t, "胸椎右侧凸", *dto.Diagnosis, "§7A.5 诊断信息")
	require.NotNil(t, dto.CobbAngle)
	assert.Equal(t, 28.0, *dto.CobbAngle)
	require.NotNil(t, dto.TeamID)
	assert.Equal(t, "TEAM01", *dto.TeamID, "§7A.5 team 卡片")
	require.NotNil(t, dto.TeamName)
	assert.Equal(t, "脊柱矫形一组", *dto.TeamName)
	require.NotNil(t, dto.DoctorID)
	assert.Equal(t, "D0001", *dto.DoctorID, "§7A.5 主治医生卡片")
	require.NotNil(t, dto.DoctorName)
	assert.Equal(t, "李医师", *dto.DoctorName)
	require.NotNil(t, dto.DeviceID)
	assert.Equal(t, "PRS-001", *dto.DeviceID, "§7A.5 设备 ID")
	assert.Equal(t, "active", dto.Status)
	assert.Equal(t, "2026-07-01T00:00:00Z", dto.CreatedAt)
	assert.Equal(t, "2026-07-02T00:00:00Z", dto.UpdatedAt)

	// ② 无身份头 → 403（fail-closed：gateway 不放行无 token 请求，直连服务亦不得读到数据）
	e2 := newEnv(t, true, true)
	seedProfilePatient(e2)
	w, resp = e2.do(http.MethodGet, profilePath, nil, nil)
	assert.Equal(t, http.StatusForbidden, w.Code, "缺失身份头必须拒绝")
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Empty(t, e2.store.lastPatientQuery, "拒绝时不得触达数据层")

	// ③ 非患者身份：查询键仍是身份头本身 ⇒ 无任何入参可指定他人
	e3 := newEnv(t, true, true)
	seedProfilePatient(e3)
	w, resp = e3.do(http.MethodGet, profilePath, nil, selfHdr("ADM001", "ROLE_ADMIN"))
	assert.Equal(t, http.StatusNotFound, w.Code, "管理员身份查自己的 patient 档案 → 不存在")
	assert.Equal(t, model.CodeNotFound, resp.Code)
	assert.Equal(t, "ADM001", e3.store.lastPatientQuery, "查询键取自 X-User-Id，而非患者 ID 入参")

	// ④ 患者身份但档案缺失 → 404（不泄漏他人数据）
	e4 := newEnv(t, true, true)
	e4.store.patient = nil
	w, _ = e4.do(http.MethodGet, profilePath, nil, selfHdr("P-NOT-EXIST", "patient"))
	assert.Equal(t, http.StatusNotFound, w.Code)

	// ⑤ 存储故障 → 500
	e5 := newEnv(t, true, true)
	seedProfilePatient(e5)
	e5.store.patientErr = errors.New("db down")
	w, _ = e5.do(http.MethodGet, profilePath, nil, selfHdr("P20260001", "patient"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestT186_PatientProfile_BindScopeJWT_Rejected 绑定态 JWT（sub=openid_*）不得读档案：
// scopeGuard 在路由前拦截，403 + 40301，且数据层零调用。
func TestT186_PatientProfile_BindScopeJWT_Rejected(t *testing.T) {
	e := newEnv(t, true, true)
	seedProfilePatient(e)

	bindTok, err := e.signer.Sign(scopeBindPrefix+"openid_t186", "", "测试患者", "patient")
	require.NoError(t, err)

	w, resp := e.do(http.MethodGet, profilePath, nil, map[string]string{"Authorization": "Bearer " + bindTok})
	assert.Equal(t, http.StatusForbidden, w.Code, "绑定态 token 不可访问本人档案")
	assert.Equal(t, model.CodeForbiddenScope, resp.Code)
	assert.Empty(t, e.store.lastPatientQuery, "拦截发生在数据层之前")
}

// TestT186_PatientProfile_RealWire 真实 TCP + 真实签发 JWT 的验收取证：
// 按 gateway jwtAuth 的同一做法从验签结果取 sub 作为 X-User-Id（伪造头的剥离由
// gateway 侧 T186 用例取证），打出可直接复现的脱敏 curl 与响应原文。
// 本端点不返回手机号，响应体无需脱敏；token 截断展示。
func TestT186_PatientProfile_RealWire(t *testing.T) {
	e := newEnv(t, true, true)
	seedProfilePatient(e)

	srv := httptest.NewServer(e.h.Router())
	t.Cleanup(srv.Close)

	tok, err := e.signer.Sign("P20260001", "", "患者小明", "patient")
	require.NoError(t, err)
	claims, err := e.signer.Verify(tok)
	require.NoError(t, err)
	require.Equal(t, "P20260001", claims.Subject)

	req, err := http.NewRequest(http.MethodGet, srv.URL+profilePath, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-User-Id", claims.Subject)
	req.Header.Set("X-Role", "patient")
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	t.Logf("脱敏 curl:\n  curl -s -H 'Authorization: Bearer <jwt %s...>' '%s%s'",
		tok[:12], srv.URL, profilePath)
	t.Logf("响应原文 (HTTP %d):\n  %s", res.StatusCode, string(raw))

	assert.Equal(t, http.StatusOK, res.StatusCode)
	var envelope struct {
		Code int                   `json:"code"`
		Data model.AdminPatientDTO `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	assert.Equal(t, 0, envelope.Code)
	assert.Equal(t, "P20260001", envelope.Data.PatientID, "读到的是 token sub 对应的档案")
	assert.Equal(t, "P20260001", e.store.lastPatientQuery)
}
