package handler

import (
	"encoding/hex"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// unbindWechat T085：管理端解绑患者微信（wx_openid 置 NULL + 审计）。
func (h *Handler) unbindWechat(c *gin.Context) {
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
