// Package handler — T226 患者自助资料写接口（PUT /api/v1/patients/:patientId）实现侧测试
//
// 验收对应（T226 ④）：
//  1. 限本人：X-User-Id == 路径 patientId 才可写；缺失/不一致 → 403 fail-closed；
//     绑定态 JWT（scope=bind）被 scopeGuard 前置拦截 → 403/40301。
//  2. 字段白名单：设计稿 7 字段可写；携带 phone / cobbAngle / diagnosis / status 等白名单外字段 → 400
//     （cobbAngle 于 T230 移出：影像学测量值由临床端写入，Boss 2026-09-17 裁定 B）。
//  3. 值域：gender 枚举 / age 0-150 / height 30-250 / weight 2-300，越界 400。
//  4. 档案缺失 → 404；存储故障 → 500。
package handler

import (
	"bytes"
	"context"
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

const t226ProfilePath = "/api/v1/patients/P20260001"

// ── fakeStore 桩：记录 UpdatePatientProfile 入参 ──

var (
	t226LastUpdatePatient string
	t226LastUpdate        repo.PatientProfileUpdate
	t226UpdateErr         error
)

func (f *fakeStore) UpdatePatientProfile(_ context.Context, patientID string, in repo.PatientProfileUpdate) error {
	t226LastUpdatePatient = patientID
	t226LastUpdate = in
	if f.patientErr != nil { // 复用 patientErr 作为写侧故障开关（用例内显式设置）
		return f.patientErr
	}
	if f.patient == nil {
		return repo.ErrPatientNotFound
	}
	return nil
}

// t085Store（bind_phone_integration_test.go）实现 Store 接口所需
func (s *t085Store) UpdatePatientProfile(context.Context, string, repo.PatientProfileUpdate) error {
	return nil
}

func t226ResetStoreSpies() {
	t226LastUpdatePatient = ""
	t226LastUpdate = repo.PatientProfileUpdate{}
	t226UpdateErr = nil
}

func t226FullPayload() map[string]any {
	h42, w48 := 42.0, 48.5
	age := 14
	return map[string]any{
		"name":                     "患者小明改",
		"gender":                   "female",
		"age":                      age,
		"heightCm":                 h42,
		"weightKg":                 w48,
		"emergencyContactName":     "张建国",
		"emergencyContactPhone":    "13987654321",
		"emergencyContactRelation": "父亲",
	}
}

func TestT226_UpdateProfile_SelfAllowed_WhitelistApplied(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	w, resp := e.do(http.MethodPut, t226ProfilePath, t226FullPayload(), selfHdr("P20260001", "patient"))
	require.Equal(t, http.StatusOK, w.Code, "本人改本人资料应 200")
	assert.Equal(t, "P20260001", t226LastUpdatePatient, "写键=路径 patientId=身份头")

	// 白名单字段逐项落入 repo 入参（字段级取证）
	require.NotNil(t, t226LastUpdate.Name)
	assert.Equal(t, "患者小明改", *t226LastUpdate.Name)
	require.NotNil(t, t226LastUpdate.Gender)
	assert.Equal(t, "female", *t226LastUpdate.Gender)
	require.NotNil(t, t226LastUpdate.Age)
	assert.Equal(t, 14, *t226LastUpdate.Age)
	require.NotNil(t, t226LastUpdate.HeightCm)
	assert.Equal(t, 42.0, *t226LastUpdate.HeightCm)
	require.NotNil(t, t226LastUpdate.WeightKg)
	assert.Equal(t, 48.5, *t226LastUpdate.WeightKg)
	require.NotNil(t, t226LastUpdate.EmergencyContactName)
	assert.Equal(t, "张建国", *t226LastUpdate.EmergencyContactName)
	require.NotNil(t, t226LastUpdate.EmergencyContactPhone)
	assert.Equal(t, "13987654321", *t226LastUpdate.EmergencyContactPhone)
	require.NotNil(t, t226LastUpdate.EmergencyContactRelation)
	assert.Equal(t, "父亲", *t226LastUpdate.EmergencyContactRelation)

	// 响应体回读：更新后档案 DTO（fake store 返回种子行）
	var dto model.AdminPatientDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "P20260001", dto.PatientID)
}

func TestT226_UpdateProfile_CrossPatientForbidden(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	// 身份头是本人，路径指向他人 → 403，数据层零调用
	w, resp := e.do(http.MethodPut, "/api/v1/patients/P20269999", t226FullPayload(), selfHdr("P20260001", "patient"))
	assert.Equal(t, http.StatusForbidden, w.Code, "改他人档案必须拒绝")
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Empty(t, t226LastUpdatePatient, "拒绝时不得触达数据层")

	// 缺失身份头 → 403 fail-closed
	e2 := newEnv(t, true, true)
	p2 := samplePatient()
	e2.store.patient = &p2
	w2, _ := e2.do(http.MethodPut, t226ProfilePath, t226FullPayload(), nil)
	assert.Equal(t, http.StatusForbidden, w2.Code)
	assert.Empty(t, t226LastUpdatePatient)
}

func TestT226_UpdateProfile_NonWhitelistRejected(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	// phone：微信授权写入，患者不可自助改（PM 2026-09-16 裁定）→ 白名单外 400
	// cobbAngle：影像学测量值由临床端写入，患者不可自助改（T230 / Boss 2026-09-17 裁定 B）→ 白名单外 400
	for _, key := range []string{"phone", "cobbAngle", "diagnosis", "status", "deviceId", "teamId"} {
		payload := t226FullPayload()
		payload[key] = "hacked"
		w, resp := e.do(http.MethodPut, t226ProfilePath, payload, selfHdr("P20260001", "patient"))
		assert.Equal(t, http.StatusBadRequest, w.Code, "白名单外字段 %q 必须 400", key)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
		assert.Empty(t, t226LastUpdatePatient, "%q 被拒时不得触达数据层", key)
	}
}

func TestT226_UpdateProfile_RangeValidation(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	cases := []struct {
		key string
		val any
	}{
		{"gender", "unknown"}, {"age", 200},
		{"heightCm", 29.9}, {"weightKg", 1.0},
	}
	for _, tc := range cases {
		payload := t226FullPayload()
		payload[tc.key] = tc.val
		w, resp := e.do(http.MethodPut, t226ProfilePath, payload, selfHdr("P20260001", "patient"))
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s=%v 越界必须 400", tc.key, tc.val)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
		assert.Empty(t, t226LastUpdatePatient)
	}
}

func TestT226_UpdateProfile_EmptyBody_400(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	w, _ := e.do(http.MethodPut, t226ProfilePath, map[string]any{}, selfHdr("P20260001", "patient"))
	assert.Equal(t, http.StatusBadRequest, w.Code, "空对象=无可写字段，必须 400")
	assert.Empty(t, t226LastUpdatePatient)
}

func TestT226_UpdateProfile_BindScopeJWT_Rejected(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	bindTok, err := e.signer.Sign(scopeBindPrefix+"openid_t226", "", "测试患者", "patient")
	require.NoError(t, err)
	w, resp := e.do(http.MethodPut, t226ProfilePath, t226FullPayload(), map[string]string{"Authorization": "Bearer " + bindTok})
	assert.Equal(t, http.StatusForbidden, w.Code, "绑定态 token 不可写资料")
	assert.Equal(t, model.CodeForbiddenScope, resp.Code)
	assert.Empty(t, t226LastUpdatePatient, "scopeGuard 前置拦截，数据层零调用")
}

func TestT226_UpdateProfile_StoreErrors(t *testing.T) {
	t226ResetStoreSpies()
	// 档案缺失 → 404
	e := newEnv(t, true, true)
	e.store.patient = nil
	w, _ := e.do(http.MethodPut, t226ProfilePath, t226FullPayload(), selfHdr("P20260001", "patient"))
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 存储故障 → 500
	e2 := newEnv(t, true, true)
	p := samplePatient()
	e2.store.patient = &p
	e2.store.patientErr = errors.New("db down")
	w2, _ := e2.do(http.MethodPut, t226ProfilePath, t226FullPayload(), selfHdr("P20260001", "patient"))
	assert.Equal(t, http.StatusInternalServerError, w2.Code)
}

// TestT226_UpdateProfile_RealWire 真实 TCP + 真实签发 JWT 的验收取证（对齐 T186 RealWire 风格）：
// 打出可直接复现的脱敏 curl 与响应原文，供自报引用。
func TestT226_UpdateProfile_RealWire(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	srv := httptest.NewServer(e.h.Router())
	t.Cleanup(srv.Close)

	tok, err := e.signer.Sign("P20260001", "", "患者小明", "patient")
	require.NoError(t, err)
	claims, err := e.signer.Verify(tok)
	require.NoError(t, err)
	require.Equal(t, "P20260001", claims.Subject)

	payload := t226FullPayload()
	rawPayload, err := json.Marshal(payload)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPut, srv.URL+t226ProfilePath, bytes.NewReader(rawPayload))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-User-Id", claims.Subject) // gateway jwtAuth 验签后按 JWT sub 重写（伪造头被剥离）
	req.Header.Set("Content-Type", "application/json")
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	t.Logf("脱敏 curl:\n  curl -s -X PUT -H 'Authorization: Bearer <jwt %s...>' -H 'Content-Type: application/json' -d '%s' '%s%s'",
		tok[:12], rawPayload, srv.URL, t226ProfilePath)
	t.Logf("响应原文 (HTTP %d):\n  %s", res.StatusCode, raw)

	assert.Equal(t, http.StatusOK, res.StatusCode)
	var envelope struct {
		Code int                   `json:"code"`
		Data model.AdminPatientDTO `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	assert.Equal(t, 0, envelope.Code)
	assert.Equal(t, "P20260001", envelope.Data.PatientID)
}
