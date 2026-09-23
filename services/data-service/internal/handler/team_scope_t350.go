// T350：data-service 患者域读端点的医护团队范围校验（PRD §7D.11 数据范围规则）。
//
// 缺陷证据（T340 现网复验 C4）：医生令牌 doctor_li（TEAM269b0f495d）用
// GET /patients/{P20260003..P20260005}/realtime 直接读到 TEAM02/TEAM03 患者的真帧数据。
// 根因是 assertAdminOrSelf 把「staff」当成一个整体放行，医生因此继承了运营的跨患者读能力。
//
// 口径：
//   - 只有 ROLE_DOCTOR 收紧；ROLE_ADMIN / ROLE_CS / technician 走原路径（只收紧不放宽）；
//   - 团队由 admins.admin_id → doctors.team_id 在服务端推导，不接受任何客户端入参；
//   - fail-closed：身份头缺失 403、推导失败 500，二者都不得退化成放行；
//   - 校验放在「患者档案存在性」判定之前（T340 口径：存在性不泄露给无权调用方）。
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// roleDoctor 医护角色键（与网关 rbac.go:43、presetRoles 字面量一致，枚举键不改）
const roleDoctor = "ROLE_DOCTOR"

// assertTeamScope 患者域单资源端点（realtime / records / health-reports）的团队范围闸门。
// 返回 false 表示响应已写出，调用方必须直接 return。
func (h *Handler) assertTeamScope(c *gin.Context, patientID string) bool {
	if c.GetHeader(headerRole) != roleDoctor {
		return true // 运营 / 客服 / 技师：维持 T264 既有语义，本卡不动
	}
	adminID := c.GetHeader(headerUserID)
	if adminID == "" {
		fail(c, model.ErrForbidden("missing user identity"))
		return false
	}
	if h.patients == nil {
		fail(c, model.ErrInternal("patient lookup not configured"))
		return false
	}
	allowed, err := h.patients.PatientInAdminTeam(c.Request.Context(), patientID, adminID)
	if err != nil {
		fail(c, model.ErrInternal("check patient data scope failed"))
		return false
	}
	if !allowed {
		fail(c, model.ErrForbidden("patient %s is out of your data scope", patientID))
		return false
	}
	return true
}

// dashboardScope 数据概览 6 端点的聚合范围（T350）。
//
// 网关把 6 条 /admin/dashboard/* 读路由对医生开放（rbac.go:201-206 staffOnlyPatterns，
// 医生有 /dashboard 页），故这里的收口是唯一执行点、不是纵深防御。
// 团队同样只从 X-User-Id → doctors.team_id 推导：端点没有、也不新增任何 teamId 入参。
//
// 返回 false = 响应已写出（403/500），调用方直接 return；
// 无团队的医生拿到 ScopeTeam("")，repo 侧谓词恒假 → 全零聚合（空集），不是全院。
func (h *Handler) dashboardScope(c *gin.Context) (model.TeamScope, bool) {
	if c.GetHeader(headerRole) != roleDoctor {
		return model.ScopeAll(), true // 运营 / 客服 / 技师：全院口径不变
	}
	adminID := c.GetHeader(headerUserID)
	if adminID == "" {
		fail(c, model.ErrForbidden("missing user identity"))
		return model.TeamScope{}, false
	}
	if h.patients == nil {
		fail(c, model.ErrInternal("patient lookup not configured"))
		return model.TeamScope{}, false
	}
	// repo 保证 ok == (teamID != "")，无医生行同样返回空串 ⇒ 同一条 fail-closed 路径
	teamID, _, err := h.patients.DoctorTeamByAdmin(c.Request.Context(), adminID)
	if err != nil {
		fail(c, model.ErrInternal("resolve doctor team failed"))
		return model.TeamScope{}, false
	}
	return model.ScopeTeam(teamID), true
}
