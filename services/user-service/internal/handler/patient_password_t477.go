// Package handler T477：admin 通道给患者设登录口令（患者端 CI 自动化登录的凭据通道）。
//
// 为什么要有这条端点：patients.password_hash 在本仓库此前没有任何 Go 写点——
// API 建档的 INSERT 不带这一列、落库恒为 NULL，而患者手机号+密码登录（T037，
// handler.go patientLogin）比对的就是它，于是「自建患者」在 CI 里永远登不进去，
// 只有 seed 预置的那几行能登（seed 只读，测试不许碰）。
//
// 形态取 T314 医护侧先例（resetDoctorAccountPassword）：口令由服务端生成、只在响应里
// 一次性返回，库里只落 bcrypt 哈希。这样既没有明文进库，也没有明文进仓/进环境变量，
// 调用方（CI）从响应里读，凭据不跨会话留存。
package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// setPatientPassword POST /api/v1/admin/patients/:patientId/password
//
// 判定序与同族 admin 患者写端点一致：角色门禁（fail-closed）→ 参数 → 存在性 → 写。
// 非 admin 一律 403 且不触库；未知患者号 404 且不写。
func (h *Handler) setPatientPassword(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can set patient password"))
		return
	}
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}
	op := operatorID(c, "")

	patient, err := h.store.GetPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("query patient failed"))
		return
	}
	if patient == nil {
		fail(c, model.ErrNotFound("patient not found"))
		return
	}

	// 复用医护侧同一个发号器（字母表/长度一致），不另造第二个口令形态。
	password, err := genDoctorPassword()
	if err != nil {
		fail(c, model.ErrInternal("generate password failed"))
		return
	}
	hash, err := GenerateBcryptHash([]byte(password))
	if err != nil {
		fail(c, model.ErrInternal("hash password failed"))
		return
	}

	if err := h.store.SetPatientPassword(c.Request.Context(), patientID, hash); err != nil {
		if err == repo.ErrPatientNotFound {
			fail(c, model.ErrNotFound("patient not found"))
			return
		}
		fail(c, model.ErrInternal("set patient password failed"))
		return
	}

	// 🔴 审计与运行日志都不带口令（含哈希）：与 audit_t252.go 里 reset-password 那条同口径。
	setAuditTrace(c, fmt.Sprintf("为患者 %s 设置登录口令（新口令一次性返回，不落审计）", patientID), map[string]any{
		"changed": []string{"password_hash"},
	})

	ctxLogger(c).Info().
		Str("action", "set_patient_password").
		Str("operator_id", op).
		Str("patient_id", patientID).
		Msg("set patient password")

	ok(c, model.PatientPasswordSetDTO{PatientID: patientID, Password: password})
}
