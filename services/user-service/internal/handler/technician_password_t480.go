// Package handler T480：admin 通道重置技师登录口令。
//
// 为什么要有这条端点：technicians.password_hash 在本仓库此前没有任何 Go 写点 ——
// 后台新建技师的 INSERT 不带这一列（handler.go createTechnician → repo/pg.go CreateTechnician），
// 落库恒为 NULL，而技师手机号+密码登录（techLogin）比对的就是它，
// 于是「新建即登不进小程序」，且没有第二条路能把口令补上（医护侧 T314 有 reset-password，技师侧没有）。
//
// 形态照抄 T314/T477：口令由服务端生成、只在响应里一次性返回，库里只落 bcrypt 哈希。
package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
)

// resetTechnicianPassword POST /api/v1/admin/technicians/:techId/reset-password
//
// 判定序与同族 admin 写端点（T477 患者设密）一致：角色门禁（fail-closed）→ 参数 → 存在性 → 写。
// 存在性判定排在写之前 ⇒ 非 admin 403 不触库、未知技师号 404 且零写（不产生半成品行）。
func (h *Handler) resetTechnicianPassword(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can reset technician password"))
		return
	}
	techID := c.Param("techId")
	if techID == "" {
		fail(c, model.ErrInvalidParam("techId is required"))
		return
	}
	op := operatorID(c, "")

	tech, err := h.store.GetTechnician(c.Request.Context(), techID)
	if err != nil {
		fail(c, model.ErrInternal("query technician failed"))
		return
	}
	if tech == nil {
		fail(c, model.ErrNotFound("technician not found: %s", techID))
		return
	}

	// 复用医护侧同一个发号器（字母表/长度一致），不另造第二种口令形态。
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

	if err := h.store.SetTechnicianPassword(c.Request.Context(), techID, hash); err != nil {
		fail(c, model.ErrInternal("reset technician password failed"))
		return
	}

	// 🔴 审计与运行日志都不带口令（含哈希）：与 doctor reset-password 那条同口径。
	setAuditTrace(c, fmt.Sprintf("重置技师账号 %s 的登录口令（新口令一次性返回，不落审计）", techID), map[string]any{
		"changed": []string{"password_hash"},
	})

	ctxLogger(c).Info().
		Str("action", "reset_technician_password").
		Str("operator_id", op).
		Str("tech_id", techID).
		Msg("reset technician password")

	ok(c, model.TechnicianPasswordResetDTO{TechID: techID, Password: password})
}
