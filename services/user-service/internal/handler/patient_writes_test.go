// Package handler T057 患者管理写功能 KNOWN_RED 测试
//
// 覆盖 3 个新端点（设计源：docs/design/admin/患者管理.html）：
//
//	POST /api/v1/admin/patients                创建患者（含手机号，phone 唯一键判重）
//	PUT  /api/v1/admin/patients/:patientId/team 分配/更改团队（幂等）
//	POST /api/v1/admin/patients/batch-bind      批量绑定到团队（部分失败不回滚）
//
// 预期红态：当前 handler.go 中 3 个 stub handler 统一返回 500 CodeInternal，
// 不调用 store、不做参数校验、不映射业务错误。本文件断言"实现后的契约行为"
// （200/400/404/409 + DTO 字段 + 入参透传），因此在 stub 阶段全部 FAIL，
// 属 TDD 预期红态。
//
// 实现方转绿清单（单 Agent 全栈）：
//  1. createPatient：校验 name 非空 + phone validPhone → preparePhone(PhoneEnc/PhoneHash)
//     → store.CreatePatient → 成功 200+AdminPatientDTO（phone 脱敏）；
//     ErrPatientExists 映射 409 CodeConflict（phone_hash 查重命中）；其余 error 500
//  2. assignPatientTeam：校验 teamId 非空 → store.AssignPatientTeam → 成功 200+AdminPatientDTO；
//     patient 不存在映射 404 CodeNotFound；幂等（同 teamId 再分配返回 200）
//  3. batchBindPatients：校验 patientIds 非空 + teamId 非空 → store.BatchBindPatients
//     → 200+BatchBindResultDTO（部分失败 HTTP 仍 200）
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// ─────────────────────────────────────────────────────────────
// 创建患者 POST /api/v1/admin/patients
// ─────────────────────────────────────────────────────────────

// TestCreatePatient_KNOWN_RED 创建患者端点契约
//
// 预期：成功 200 + AdminPatientDTO（含脱敏 phone）；缺 name/phone 400；phone 格式非法 400；phone 重复 409。
// 当前 stub 统一返回 500 → 全部断言 FAIL（预期红态）。
func TestCreatePatient_KNOWN_RED(t *testing.T) {
	e := newEnv(t, true, true)

	// 成功：返回新建患者 DTO（含脱敏手机号）+ 入参透传到 store
	t.Run("success_200_with_AdminPatientDTO", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 + AdminPatientDTO（含脱敏 phone）")
		p := samplePatient()
		e.store.createdPatient = &p
		e.store.createPatientErr = nil
		body := model.CreatePatientRequestDTO{
			Name:      "患者小明",
			Phone:     "13800138000",
			Gender:    strPtr("male"),
			Age:       intPtr(14),
			Diagnosis: strPtr("胸椎右侧凸"),
			CobbAngle: floatPtr(28.0),
			TeamID:    strPtr("TEAM01"),
			DoctorID:  strPtr("D0001"),
		}
		w, resp := e.do(http.MethodPost, "/api/v1/admin/patients", body, nil)
		assert.Equal(t, http.StatusOK, w.Code, "成功应返回 200")
		assert.Equal(t, model.CodeOK, resp.Code)
		var dto model.AdminPatientDTO
		require.NoError(t, json.Unmarshal(resp.Data, &dto))
		assert.Equal(t, "P20260001", dto.PatientID)
		assert.Equal(t, "患者小明", dto.Name)
		assert.NotEmpty(t, dto.Phone, "响应应含脱敏手机号")
		// 入参透传断言（实现方需将 DTO 字段映射到 PatientInput，phone 经 preparePhone 加密+哈希）
		assert.Equal(t, "患者小明", e.store.lastCreateInput.Name)
		assert.NotEmpty(t, e.store.lastCreateInput.PhoneHash, "phone_hash 应由 preparePhone 生成并透传到 store")
		require.NotNil(t, e.store.lastCreateInput.TeamID, "stub 未调用 store，转绿后此处应通过")
		assert.Equal(t, "TEAM01", *e.store.lastCreateInput.TeamID)
	})

	// 缺 name → 400 CodeInvalidParam
	t.Run("missing_name_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（name 必填）")
		e.store.createdPatient = nil
		e.store.createPatientErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/admin/patients",
			model.CreatePatientRequestDTO{Phone: "13800138000"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// 缺 phone → 400 CodeInvalidParam
	t.Run("missing_phone_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（phone 必填）")
		e.store.createdPatient = nil
		e.store.createPatientErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/admin/patients",
			model.CreatePatientRequestDTO{Name: "无手机号患者"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// phone 格式非法（少位/非数字）→ 400 CodeInvalidParam
	t.Run("invalid_phone_format_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（phone 须 11 位 1 开头数字）")
		e.store.createdPatient = nil
		e.store.createPatientErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/admin/patients",
			model.CreatePatientRequestDTO{Name: "格式非法患者", Phone: "123"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// phone 已存在（store 按 phone_hash 查重命中 → ErrPatientExists）→ 409 CodeConflict
	t.Run("duplicate_phone_409", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 409 CodeConflict（phone_hash 查重命中 ErrPatientExists）")
		e.store.createdPatient = nil
		e.store.createPatientErr = repo.ErrPatientExists
		w, resp := e.do(http.MethodPost, "/api/v1/admin/patients",
			model.CreatePatientRequestDTO{Name: "重复手机号患者", Phone: "13800138000"}, nil)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, model.CodeConflict, resp.Code)
	})
}

// ─────────────────────────────────────────────────────────────
// 分配团队 PUT /api/v1/admin/patients/:patientId/team
// ─────────────────────────────────────────────────────────────

// TestAssignPatientTeam_KNOWN_RED 分配/更改团队端点契约
//
// 预期：成功 200 + AdminPatientDTO（含新 teamId）；patient 不存在 404；
// 缺 teamId 400；幂等（同 teamId 再分配返回 200）。
// 当前 stub 统一返回 500 → 全部断言 FAIL（预期红态）。
func TestAssignPatientTeam_KNOWN_RED(t *testing.T) {
	e := newEnv(t, true, true)

	// 成功：返回更新后的患者（teamId 变更为 TEAM02）
	t.Run("success_200_with_updated_team", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 + AdminPatientDTO（teamId=TEAM02）")
		p := samplePatient()
		teamID := "TEAM02"
		p.TeamID = &teamID
		e.store.assignedPatient = &p
		e.store.assignPatientErr = nil
		body := model.AssignTeamRequestDTO{TeamID: "TEAM02"}
		w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001/team", body, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, model.CodeOK, resp.Code)
		var dto model.AdminPatientDTO
		require.NoError(t, json.Unmarshal(resp.Data, &dto))
		assert.Equal(t, "P20260001", dto.PatientID)
		require.NotNil(t, dto.TeamID)
		assert.Equal(t, "TEAM02", *dto.TeamID)
		// 入参透传
		assert.Equal(t, "P20260001", e.store.lastAssignPatient)
		assert.Equal(t, "TEAM02", e.store.lastAssignTeam)
	})

	// patient 不存在 → 404 CodeNotFound
	t.Run("patient_not_found_404", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 404 CodeNotFound（patient 不存在）")
		e.store.assignedPatient = nil
		e.store.assignPatientErr = repo.ErrPatientNotFound
		body := model.AssignTeamRequestDTO{TeamID: "TEAM01"}
		w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P-NOTEXIST/team", body, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, model.CodeNotFound, resp.Code)
	})

	// 缺 teamId → 400 CodeInvalidParam
	t.Run("missing_teamId_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（teamId 必填）")
		e.store.assignedPatient = nil
		e.store.assignPatientErr = nil
		w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001/team",
			model.AssignTeamRequestDTO{}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})
}

// ─────────────────────────────────────────────────────────────
// 批量绑定 POST /api/v1/admin/patients/batch-bind
// ─────────────────────────────────────────────────────────────

// TestBatchBindPatients_KNOWN_RED 批量绑定端点契约
//
// 预期：成功 200 + BatchBindResultDTO（含部分失败明细，HTTP 仍 200）；
// patientIds 为空 400；缺 teamId 400。
// 当前 stub 统一返回 500 → 全部断言 FAIL（预期红态）。
func TestBatchBindPatients_KNOWN_RED(t *testing.T) {
	e := newEnv(t, true, true)

	// 成功：3 条绑定，2 成功 1 失败（部分失败不回滚，HTTP 200）
	t.Run("success_200_with_partial_failure", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 + BatchBindResultDTO（2 成功 1 失败）")
		e.store.batchBindResult = &repo.BatchBindResult{
			Success: []string{"P001", "P002"},
			Failed: []repo.BatchBindFailure{
				{PatientID: "P003", Reason: "patient not found"},
			},
		}
		e.store.batchBindErr = nil
		body := model.BatchBindRequestDTO{
			PatientIDs: []string{"P001", "P002", "P003"},
			TeamID:     "TEAM01",
		}
		w, resp := e.do(http.MethodPost, "/api/v1/admin/patients/batch-bind", body, nil)
		assert.Equal(t, http.StatusOK, w.Code, "部分失败 HTTP 仍 200")
		assert.Equal(t, model.CodeOK, resp.Code)
		var dto model.BatchBindResultDTO
		require.NoError(t, json.Unmarshal(resp.Data, &dto))
		assert.Equal(t, 2, dto.SuccessCount)
		assert.Equal(t, 1, dto.FailedCount)
		require.Len(t, dto.Failures, 1)
		assert.Equal(t, "P003", dto.Failures[0].PatientID)
		assert.Equal(t, "patient not found", dto.Failures[0].Reason)
		// 入参透传
		assert.Equal(t, []string{"P001", "P002", "P003"}, e.store.lastBatchIDs)
		assert.Equal(t, "TEAM01", e.store.lastBatchTeam)
	})

	// patientIds 为空 → 400 CodeInvalidParam
	t.Run("empty_patientIds_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（patientIds 不可为空）")
		e.store.batchBindResult = nil
		e.store.batchBindErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/admin/patients/batch-bind",
			model.BatchBindRequestDTO{TeamID: "TEAM01"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// 缺 teamId → 400 CodeInvalidParam
	t.Run("missing_teamId_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（teamId 必填）")
		e.store.batchBindResult = nil
		e.store.batchBindErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/admin/patients/batch-bind",
			model.BatchBindRequestDTO{PatientIDs: []string{"P001"}}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})
}
