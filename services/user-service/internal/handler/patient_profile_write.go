// Package handler — T226 患者自助资料写接口（PUT /api/v1/patients/:patientId）
//
// 白名单（对齐设计稿 docs/design/patient/profile.html 编辑表单，去除手机号）：
// name / gender / age / cobbAngle / heightCm / weightKg / 紧急联系人×3。
// 🔴 phone 不在白名单：手机号由微信登录授权写入，患者不可自助改、患者端无任何填号入口
// （PM 2026-09-16 裁定）；请求体携带 phone 或任何白名单外字段 → 400。
// 限本人：X-User-Id（gateway 从 JWT sub 注入）必须等于路径 patientId，缺失或不一致一律 403（fail-closed）。
package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

func trimStr(s string) string { return strings.TrimSpace(s) }

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// updatePatientProfileRequest 白名单字段（指针=nil=不改）。
// decode 用 DisallowUnknownFields：diagnosis/status/deviceId/teamId/phone 等非白名单键一律拒绝。
type updatePatientProfileRequest struct {
	Name                     *string  `json:"name"`
	Gender                   *string  `json:"gender"`
	Age                      *int     `json:"age"`
	CobbAngle                *float64 `json:"cobbAngle"`
	HeightCm                 *float64 `json:"heightCm"`
	WeightKg                 *float64 `json:"weightKg"`
	EmergencyContactName     *string  `json:"emergencyContactName"`
	EmergencyContactPhone    *string  `json:"emergencyContactPhone"`
	EmergencyContactRelation *string  `json:"emergencyContactRelation"`
}

// 值域（DB CHECK + 设计稿表单 min/max）
const (
	maxPatientNameLen    = 64 // patients.name VARCHAR(64)
	maxEmergencyFieldLen = 64 // 紧急联系人姓名/关系 VARCHAR(64)/VARCHAR(32) 取严
	maxEmergencyPhoneLen = 32 // emergency_contact_phone VARCHAR(32)
)

func (h *Handler) updatePatientProfile(c *gin.Context) {
	patientID := c.GetHeader(headerUserID)
	if patientID == "" {
		fail(c, model.ErrForbidden("patient identity required"))
		return
	}
	if patientID != c.Param("patientId") {
		fail(c, model.ErrForbidden("patients may only update their own profile"))
		return
	}

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		fail(c, model.ErrInvalidParam("read request body failed"))
		return
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var req updatePatientProfileRequest
	if err := dec.Decode(&req); err != nil {
		fail(c, model.ErrInvalidParam("request contains fields outside the editable whitelist: %v", err))
		return
	}

	in, appErr := buildPatientProfileUpdate(&req)
	if appErr != nil {
		fail(c, appErr)
		return
	}

	if err := h.store.UpdatePatientProfile(c.Request.Context(), patientID, *in); err != nil {
		if errors.Is(err, repo.ErrPatientNotFound) {
			fail(c, model.ErrNotFound("patient not found: %s", patientID))
			return
		}
		fail(c, model.ErrInternal("update patient profile failed"))
		return
	}

	row, err := h.store.GetPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("get patient profile failed"))
		return
	}
	if row == nil {
		fail(c, model.ErrNotFound("patient not found: %s", patientID))
		return
	}
	ok(c, toPatientDTO(*row))
}

// buildPatientProfileUpdate 白名单字段值域校验 + 装配 repo 入参；全空 → 400。
func buildPatientProfileUpdate(req *updatePatientProfileRequest) (*repo.PatientProfileUpdate, *model.AppError) {
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
	if req.CobbAngle != nil {
		if *req.CobbAngle < 0 || *req.CobbAngle > 180 { // 设计稿表单 0–180
			return nil, model.ErrInvalidParam("cobbAngle must be between 0 and 180")
		}
		in.CobbAngle = req.CobbAngle
		anyField = true
	}
	if req.HeightCm != nil {
		if *req.HeightCm < 30 || *req.HeightCm > 250 { // 设计稿表单 30–250
			return nil, model.ErrInvalidParam("heightCm must be between 30 and 250")
		}
		in.HeightCm = req.HeightCm
		anyField = true
	}
	if req.WeightKg != nil {
		if *req.WeightKg < 2 || *req.WeightKg > 300 { // 设计稿表单 2–300
			return nil, model.ErrInvalidParam("weightKg must be between 2 and 300")
		}
		in.WeightKg = req.WeightKg
		anyField = true
	}
	if req.EmergencyContactName != nil {
		v := trimStr(*req.EmergencyContactName)
		if runeLen(v) > maxEmergencyFieldLen {
			return nil, model.ErrInvalidParam("emergencyContactName too long")
		}
		in.EmergencyContactName = &v
		anyField = true
	}
	if req.EmergencyContactPhone != nil {
		v := trimStr(*req.EmergencyContactPhone)
		if runeLen(v) > maxEmergencyPhoneLen {
			return nil, model.ErrInvalidParam("emergencyContactPhone too long")
		}
		in.EmergencyContactPhone = &v
		anyField = true
	}
	if req.EmergencyContactRelation != nil {
		v := trimStr(*req.EmergencyContactRelation)
		if runeLen(v) > maxEmergencyFieldLen {
			return nil, model.ErrInvalidParam("emergencyContactRelation too long")
		}
		in.EmergencyContactRelation = &v
		anyField = true
	}
	if !anyField {
		return nil, model.ErrInvalidParam("no updatable fields in request body")
	}
	return in, nil
}
