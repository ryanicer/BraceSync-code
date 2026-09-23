// T350：医护数据范围「仅本团队患者」在告警读链路的服务端落地（PRD §7D.11）。
//
// 与 user-service 同名 helper 同一口径（跨服务不共享代码，靠契约对齐）：
//   - ROLE_ADMIN / ROLE_CS：不受团队隔离（客服按矩阵取全量患者），本函数直接放行不改 filter；
//   - ROLE_DOCTOR：团队只能由 doctors.team_id 推导（admins.admin_id → doctors 这一跳），
//     不接受客户端自报，故 /alerts?patientId= 之外没有任何团队入参；
//   - 身份缺失或推导失败：fail-closed（403 / 500），绝不退化成「不过滤」。
//
// 无团队归属的医护（doctors 行缺失或 team_id 为 NULL）留 TeamScoped=true + TeamID="" ——
// repo 侧据此落恒假谓词，返回空集。
package handler

import (
	"net/http"

	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

// roleDoctor 医护角色键（与网关 rbac.go:43、presetRoles 字面量一致，枚举键不改）
const roleDoctor = "ROLE_DOCTOR"

// applyDoctorTeamScope 把团队范围写进告警查询条件。
// 返回 false 表示响应已写出（403 身份缺失 / 500 推导失败），调用方必须直接 return。
func (h *Handler) applyDoctorTeamScope(w http.ResponseWriter, r *http.Request, f *repo.AlertQueryFilter) bool {
	if r.Header.Get(headerRole) != roleDoctor {
		return true
	}
	adminID := r.Header.Get(headerUserID)
	if adminID == "" {
		h.reject(w, codeForbidden, "missing user identity")
		return false
	}
	teamID, _, err := h.public.DoctorTeamByAdmin(r.Context(), adminID)
	if err != nil {
		h.reject(w, codeInternalError, "resolve doctor team failed")
		return false
	}
	f.TeamScoped = true
	f.TeamID = teamID
	return true
}
