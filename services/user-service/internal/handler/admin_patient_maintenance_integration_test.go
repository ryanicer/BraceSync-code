// Package handler T085 Admin 患者档案维护契约 KNOWN_RED 测试
//
// 覆盖 §5.6 Admin 侧操作：
//   - POST /api/v1/admin/patients/:patientId/unbind-wechat → wx_openid=NULL
//   - PUT /api/v1/admin/patients/:patientId/phone → format validation / 409 conflict
//   - audit log: operator_id/action/before/after
package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
	"github.com/bracesync/bracesync/services/testhelper"
)

const (
	testAdminSecret = "T085-test-secret-for-admin-maintenance-only"
	testSuperAdminID = "ADM_SUPER_001"
)

// ─────────────────────────────────────────────────────────────
// Fixtures & Helpers
// ─────────────────────────────────────────────────────────────

func newAdminMaintenanceEnv(t *testing.T) (*adminMaintTestEnv, *testhelper.LogCaptureHook) {
	t.Helper()
	
	signer, err := token.NewSigner(testAdminSecret, time.Hour)
	require.NoError(t, err)
	
	lc := testhelper.NewLogCaptureHook()
	
	store := &fakeStore{}
	h := New(store, signer, nil)
	
	return &adminMaintTestEnv{
		t:            t,
		signer:       signer,
		store:        store,
		h:            h,
		logCapture:   lc,
	}, lc
}

type adminMaintTestEnv struct {
	t            *testing.T
	signer       *token.Signer
	store        *fakeStore
	h            *Handler
	logCapture   *testhelper.LogCaptureHook
}

// createSuperAdminJWT 签发 super admin JWT (scope=all)
func (e *adminMaintTestEnv) createSuperAdminJWT(adminID string) string {
	token, _ := e.signer.Sign(
		adminID,           // subject
		"",                // username
		//name:    "超级管理员",      // name
		"ROLE_SUPER_ADMIN", // roleID
	)
	return token
}

// samplePatientRow 装配 PatientRow 样本（带 phone_enc+hash）
func samplePatientRowWithPhone(id, openID, phoneEnc, phoneHash, status string) repo.PatientRow {
	return repo.PatientRow{
		PatientID: id, Name: "患者小明", PhoneEnc: phoneEnc, 
		PhoneHash: phoneHash, WxOpenid: openID, Status: status,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

// doUnbindWechat 发起 unbind-wechat 请求
func (e *adminMaintTestEnv) doUnbindWechat(patientID string, authToken string) (*httptest.ResponseRecorder, *model.BaseResponse) {
	e.t.Helper()
	
	w := httptest.NewRequest(http.MethodPost, "/api/v1/admin/patients/"+patientID+"/unbind-wechat", nil)
	w.Header.Set("Content-Type", "application/json")
	w.Header.Set("Authorization", "Bearer "+authToken)
	
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, w)
	
	resp := &model.BaseResponse{}
	json.Unmarshal(rec.Body.Bytes(), resp)
	
	return rec, resp
}

// doPutPhone 发起 PUT phone 请求
func (e *adminMaintTestEnv) doPutPhone(patientID, newPhone, reason string, authToken string) (*httptest.ResponseRecorder, *model.BaseResponse) {
	e.t.Helper()
	
	body := map[string]string{"phone": newPhone, "reason": reason}
	bodyBytes, _ := json.Marshal(body)
	
	w := httptest.NewRequest(http.MethodPut, "/api/v1/admin/patients/"+patientID+"/phone", strings.NewReader(string(bodyBytes)))
	w.Header.Set("Content-Type", "application/json")
	w.Header.Set("Authorization", "Bearer "+authToken)
	
	rec := httptest.NewRecorder()
	e.h.Router().ServeHTTP(rec, w)
	
	resp := &model.BaseResponse{}
	json.Unmarshal(rec.Body.Bytes(), resp)
	
	return rec, resp
}

// ─────────────────────────────────────────────────────────────
// Scenario: Unbind WeChat
// ─────────────────────────────────────────────────────────────

// TestUnbindWechat_SuccessfullySetsNULL_KNOWN_RED POST unbind-wechat → wx_openid=NULL + audit log
func TestUnbindWechat_SuccessfullySetsNULL_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	env, lc := newAdminMaintenanceEnv(t)
	
	// Fixture: patient 已绑定 wx_openid
	patient := samplePatientRowWithPhone("P20260007", "openid_TO_UNBIND", "enc_xyz", "hash_xyz", "active")
	env.store.patients = append(env.store.patients, &patient)
	env.store.findPatientByID = &patient
	
	authToken := env.createSuperAdminJWT(testSuperAdminID)
	
	t.Run("unbind_wechat_sets_wx_openid_to_null_with_audit_log", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未实现解绑逻辑，预期 wx_openid=NULL + 审计日志 action=unbind_wechat")
		
		w, resp := env.doUnbindWechat("P20260007", authToken)
		
		assert.Equal(t, http.StatusOK, w.Code, "成功应返回 200")
		assert.Equal(t, model.CodeOK, resp.Code)
		
		// TODO: Winner 实现后断言以下逻辑
		assert.NotNil(t, env.store.lastUpdateWxOpenid, "store 应被调用更新 wx_openid")
		assert.Equal(t, "", env.store.lastUpdateWxOpenid.Openid, "wx_openid 应置为 NULL")
		
		// 审计日志断言
		entry := lc.FindEventByAction("unbind_wechat")
		require.NotNil(t, entry, "应记录 unbind_wechat 审计日志")
		
		assert.Contains(t, entry, "operator_id", "日志应含 operator_id")
		assert.Contains(t, entry["operator_id"], testSuperAdminID, "operator_id 应为当前 admin ID")
	})
}

// ─────────────────────────────────────────────────────────────
// Scenario: Update Phone Format Validation & 409 Conflict
// ─────────────────────────────────────────────────────────────

// TestUpdatePhone_FormatValidation_RejectInvalid_KNOWN_RED PUT phone 格式错误 → 400 Bad Request
func TestUpdatePhone_FormatValidation_RejectInvalid_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	env, _ := newAdminMaintenanceEnv(t)
	
	authToken := env.createSuperAdminJWT(testSuperAdminID)
	
	type InvalidPhoneCase struct {
		name     string
		phone    string
		wantCode int
	}
	
	cases := []InvalidPhoneCase{
		{"too_short", "138", http.StatusBadRequest, "手机号过短"},
		{"wrong_format", "abc-def-ghij", http.StatusBadRequest, "非数字格式"},
		{"international_invalid", "+1-800-555-1234", http.StatusBadRequest, "国际号码格式不支持"},
	}
	
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Log("KNOWN_RED: stub 未做格式校验，预期 400 CodeInvalidParam")
			
			w, resp := env.doPutPhone("P20260007", tc.phone, "更换手机号原因", authToken)
			
			assert.Equal(t, tc.wantCode, w.Code, "%s 应拒绝非法格式", tc.name)
			assert.Equal(t, model.CodeInvalidParam, resp.Code, "错误码应为 CodeInvalidParam")
			
			// 断言：不执行后续 UPDATE
		})
	}
}

// TestUpdatePhone_409_ConflictWithExistingHash_KNOWN_Red phone_hash 冲突 → 409 Conflict
func TestUpdatePhone_409_ConflictWithExistingHash_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	env, _ := newAdminMaintenanceEnv(t)
	
	// Fixture: 两个患者共享同一 phone_hash（模拟 409 冲突场景）
	hashSame := "hash_conflict_same_phone"
	patient1 := samplePatientRowWithPhone("P20260008", "", "enc_a", hashSame, "active")
	patient2 := samplePatientRowWithPhone("P20260009", "", "enc_b", hashSame, "active")
	env.store.patients = append(env.store.patients, &patient1, &patient2)
	
	// 查找时返回 P20260008
	env.store.findPatientByID = &patient1
	
	authToken := env.createSuperAdminJWT(testSuperAdminID)
	
	t.Run("conflicting_phone_hash_returns_409", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未检查 phone_hash 唯一性，预期 409 CodeConflict")
		
		w, resp := env.doPutPhone("P20260008", "13800138000", "更新手机号", authToken)
		
		assert.Equal(t, http.StatusConflict, w.Code, "phone_hash 冲突应返回 409")
		assert.Equal(t, model.CodeConflict, resp.Code, "错误码应为 409")
		
		// 断言：不执行 UPDATE
		assert.Nil(t, env.store.lastUpdatePatientPhone, "不应调用 UPDATE 接口")
	})
}

// TestUpdatePhone_Success_EncAndHashSyncUpdated_KNOWN_RED PUT phone 成功 → phone_enc+phone_hash 同步更新
func TestUpdatePhone_Success_EncAndHashSyncUpdated_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	env, _ := newAdminMaintenanceEnv(t)
	
	// Fixture: patient 存在且 phone_hash 唯一
	phone := "13800138001"
	newEnc := "new_encrypted_phone_data"
	newHash := "hash_new_phone_001"
	
	oldPatient := samplePatientRowWithPhone("P20260010", "openid_existing", "old_enc", "old_hash", "active")
	env.store.patients = append(env.store.patients, &oldPatient)
	env.store.findPatientByID = &oldPatient
	
	authToken := env.createSuperAdminJWT(testSuperAdminID)
	
	t.Run("update_phone_enc_and_hash_synchronously", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未实现加密哈希逻辑，预期同步更新 phone_enc+phone_hash")
		
		w, resp := env.doPutPhone("P20260010", phone, "患者新手机号", authToken)
		
		assert.Equal(t, http.StatusOK, w.Code, "成功应返回 200")
		assert.Equal(t, model.CodeOK, resp.Code)
		
		// TODO: Winner 实现后断言：
		// assert.NotNil(t, env.store.lastUpdatePatientPhone)
		// assert.Equal(t, newEnc, env.store.lastUpdatePatientPhone.Enc)
		// assert.Equal(t, newHash, env.store.lastUpdatePatientPhone.Hash)
	})
}

// TestUpdatePhone_AuditLogFieldsWritten_KNOWN_RED audit fields (operator_id/action/before/after) 落日志断言
func TestUpdatePhone_AuditLogFieldsWritten_KNOWN_RED(t *testing.T) {
	t.Parallel()
	
	env, lc := newAdminMaintenanceEnv(t)
	
	// Fixture: patient 存在
	oldPatient := samplePatientRowWithPhone("P20260011", "openid_old", "old_enc", "old_hash", "active")
	env.store.patients = append(env.store.patients, &oldPatient)
	env.store.findPatientByID = &oldPatient
	
	authToken := env.createSuperAdminJWT(testSuperAdminID)
	
	t.Run("audit_log_fields_written_correctly", func(t *testing.T) {
		t.Log("KNOWN_RED: stub 未记录审计日志，预期 structured log 含 operator_id/action/before/after")
		
		_, _ = env.doPutPhone("P20260011", "13800138002", "为患者变更手机号", authToken)
		
		// 查找 audit 日志条目
		entry := lc.FindEventByAction("update_patient_phone")
		require.NotNil(t, entry, "应记录 update_patient_phone 审计日志")
		
		// 断言字段存在性
		assert.Contains(t, entry, "operator_id", "日志应含 operator_id")
		assert.Equal(t, "update_patient_phone", entry["action"], "action 应为 update_patient_phone")
		
		// before/after 快照 (JSON 序列化)
		assert.Contains(t, entry, "before", "日志应含变更前快照")
		assert.Contains(t, entry, "after", "日志应含变更后快照")
		
		t.Logf("审计日志完整：%+v", entry)
	})
}
