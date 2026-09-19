package handler

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// requireAdminRole T190：后台患者管理写端点的 handler 层角色判定（纵深防御）。
// gateway RBAC 矩阵已在入口拦下非 admin，此处兜底「绕过网关直连服务」的请求。
// fail-closed：X-Role 缺失即视为无权限。
func requireAdminRole(c *gin.Context) bool {
	return c.GetHeader(headerRole) == roleAdmin
}

// unbindWechat T085：管理端解绑患者微信（wx_openid 置 NULL + 审计）。
func (h *Handler) unbindWechat(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can unbind patient wechat"))
		return
	}
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}
	op := operatorID(c, "")

	if err := h.store.UnbindWechat(c.Request.Context(), patientID); err != nil {
		if err == repo.ErrPatientNotFound {
			fail(c, model.ErrNotFound("patient not found"))
			return
		}
		fail(c, model.ErrInternal("unbind wechat failed"))
		return
	}

	ctxLogger(c).Info().
		Str("action", "unbind_wechat").
		Str("operator_id", op).
		Str("patient_id", patientID).
		Msg("unbind patient wechat")

	ok(c, gin.H{"patientId": patientID})
}

// updatePhoneRequest PUT /admin/patients/:id/phone 请求体
type updatePhoneRequest struct {
	Phone  string `json:"phone"`
	Reason string `json:"reason"`
}

// updatePatientPhone T085：管理端改手机号（格式校验 → hash 冲突 409 → 同步更新 enc+hash + 审计）。
func (h *Handler) updatePatientPhone(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can update patient phone"))
		return
	}
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}
	var req updatePhoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if !validPhone(req.Phone) {
		fail(c, model.ErrInvalidParam("invalid phone format: must be 11 digits starting with 1"))
		return
	}
	op := operatorID(c, "")

	// 患者存在性校验
	patient, err := h.store.GetPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("query patient failed"))
		return
	}
	if patient == nil {
		fail(c, model.ErrNotFound("patient not found"))
		return
	}

	// phone_hash 冲突校验（排除自身）
	newHash := phone.Hash(req.Phone)
	taken, err := h.store.PatientPhoneHashTaken(c.Request.Context(), newHash, patientID)
	if err != nil {
		fail(c, model.ErrInternal("check phone hash failed"))
		return
	}
	if taken {
		fail(c, model.ErrConflict("phone already exists"))
		return
	}

	// 加密 + 哈希同步更新
	enc, hash, appErr := h.preparePhone(req.Phone)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	if err := h.store.UpdatePatientPhone(c.Request.Context(), patientID, enc, hash); err != nil {
		if err == repo.ErrPatientNotFound {
			fail(c, model.ErrNotFound("patient not found"))
			return
		}
		fail(c, model.ErrInternal("update patient phone failed"))
		return
	}

	// 审计日志（before/after 快照）
	before := ""
	if patient.PhoneEnc != nil {
		before = hex.EncodeToString(patient.PhoneEnc)
	}
	ctxLogger(c).Info().
		Str("action", "update_patient_phone").
		Str("operator_id", op).
		Str("patient_id", patientID).
		Str("before", before).
		Str("after", req.Phone).
		Str("reason", req.Reason).
		Msg("update patient phone")

	ok(c, gin.H{"patientId": patientID})
}

// adminPatientEditRequest PUT /api/v1/admin/patients/:patientId 入参（T248 4.3，指针=nil=不改）。
// 只覆盖 PRD §7D.3「编辑患者弹窗」五项；phone / teamId / primaryDoctorId / status 各有专属端点，
// 由 DisallowUnknownFields 拒掉，避免调用方以为改了其实被静默丢弃。
type adminPatientEditRequest struct {
	Name      *string  `json:"name"`
	Gender    *string  `json:"gender"`
	Age       *int     `json:"age"`
	Diagnosis *string  `json:"diagnosis"`
	CobbAngle *float64 `json:"cobbAngle"`
}

// maxDiagnosisLen patients.diagnosis VARCHAR(255)
const maxDiagnosisLen = 255

// updatePatientAdmin PUT /api/v1/admin/patients/:patientId —— admin 侧患者档案编辑（T248 4.3）。
// Cobb 角度可写系 Boss 2026-09-17 裁定 B 的「临床侧写通道」延伸：患者自助通道仍拒该键
// （见 patient_profile_write.go 头注）。
func (h *Handler) updatePatientAdmin(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can edit patient profile"))
		return
	}
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		fail(c, model.ErrInvalidParam("read request body failed"))
		return
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var req adminPatientEditRequest
	if decErr := dec.Decode(&req); decErr != nil {
		fail(c, model.ErrInvalidParam("request contains fields outside the editable set: %v", decErr))
		return
	}
	in, appErr := buildAdminPatientEdit(&req)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	if err := h.store.UpdatePatientProfile(c.Request.Context(), patientID, *in); err != nil {
		if err == repo.ErrPatientNotFound {
			fail(c, model.ErrNotFound("patient not found: %s", patientID))
			return
		}
		fail(c, model.ErrInternal("update patient failed"))
		return
	}
	row, err := h.store.GetPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("get patient failed"))
		return
	}
	if row == nil {
		fail(c, model.ErrNotFound("patient not found: %s", patientID))
		return
	}
	ok(c, toPatientDTO(*row))
}

// buildAdminPatientEdit 值域校验 + 装配 repo 入参；一个字段都没给 → 400（空编辑无意义）。
func buildAdminPatientEdit(req *adminPatientEditRequest) (*repo.PatientProfileUpdate, *model.AppError) {
	in := &repo.PatientProfileUpdate{}
	anyField := false
	if req.Name != nil {
		name := trimStr(*req.Name)
		if name == "" || runeLen(name) > maxPatientNameLen {
			return nil, model.ErrInvalidParam("name must be 1-%d characters", maxPatientNameLen)
		}
		in.Name = &name
		anyField = true
	}
	if req.Gender != nil {
		if *req.Gender != "male" && *req.Gender != "female" {
			return nil, model.ErrInvalidParam("gender must be male or female")
		}
		in.Gender = req.Gender
		anyField = true
	}
	if req.Age != nil {
		if *req.Age < 0 || *req.Age > 150 { // patients.age CHECK (BETWEEN 0 AND 150)
			return nil, model.ErrInvalidParam("age must be between 0 and 150")
		}
		in.Age = req.Age
		anyField = true
	}
	if req.Diagnosis != nil {
		v := trimStr(*req.Diagnosis)
		if runeLen(v) > maxDiagnosisLen {
			return nil, model.ErrInvalidParam("diagnosis exceeds %d characters", maxDiagnosisLen)
		}
		in.Diagnosis = &v
		anyField = true
	}
	if req.CobbAngle != nil {
		// patients.cobb_angle NUMERIC(5,2)；值域与建档端点 createPatient 一致
		if *req.CobbAngle < 0 || *req.CobbAngle > 180 {
			return nil, model.ErrInvalidParam("invalid cobbAngle range: [0,180]")
		}
		in.CobbAngle = req.CobbAngle
		anyField = true
	}
	if !anyField {
		return nil, model.ErrInvalidParam("no updatable fields in request body")
	}
	return in, nil
}
