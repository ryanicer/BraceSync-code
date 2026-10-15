package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
)

// revealPatientPhoneResponse 按需解密响应：仅明文手机号 + 患者号，供沟通页「复制号码」拨号回访。
type revealPatientPhoneResponse struct {
	PatientID string `json:"patientId"`
	Phone     string `json:"phone"`
}

// revealPatientPhone T691 r134（方案2a·留言回访唯一触达通道）：
// POST /api/v1/admin/patients/:patientId/phone/reveal —— 取患者登录手机号密文 → 解密回明文。
//
// 隐私边界（PRD §7D.7 V3.48:32 / T361/T491 不破）：
//   - 明文只走这一条独立端点，绝不随反馈列表/详情等常规读接口回传；
//   - 每次成功查看经 auditRoutes 表驱动中间件落一行 sensitive_read 审计
//     （操作人/时间/患者ID/来源＝患者沟通页），审计 detail 只记脱敏值，🔴 明文绝不入审计行；
//   - ROLE_ADMIN 独占：gateway RBAC 入口已拦非 admin，此处 requireAdminRole 兜底「绕过网关直连」。
func (h *Handler) revealPatientPhone(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can reveal patient phone"))
		return
	}
	patientID := c.Param("patientId")
	// PHONE_ENC_KEY 未配置：与技师写入同口径 fail-closed 500，不退化成「无手机号」。
	if h.phone == nil {
		fail(c, model.ErrInternal("phone cipher not configured"))
		return
	}
	p, err := h.store.GetPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("load patient failed"))
		return
	}
	if p == nil {
		fail(c, model.ErrPatientNotFound("patient not found: %s", patientID))
		return
	}
	if len(p.PhoneEnc) == 0 {
		// 该患者登录时未落手机号密文（异常存档）：无明文可揭示，按缺数据回 404 语义、不伪造。
		fail(c, model.ErrNotFound("patient phone not found"))
		return
	}
	plain, err := h.phone.Decrypt(p.PhoneEnc)
	if err != nil {
		fail(c, model.ErrInternal("decrypt patient phone failed"))
		return
	}
	// 审计 detail 仅落脱敏派生值 + 来源页；明文不出现在此处，也不出现在 audit 行任何字段。
	setAuditTrace(c, "", map[string]any{
		"patientId":    patientID,
		"phoneMasked":  phone.Mask(plain),
		"source":       "患者沟通页",
		"revealResult": "success",
	})
	ok(c, revealPatientPhoneResponse{PatientID: patientID, Phone: plain})
}
