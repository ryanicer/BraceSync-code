// T350：医护数据范围「仅本团队患者」在服务端落地（PRD §7D.11 数据范围规则）
//
// 口径（三条，全部按网关注入的身份头推导，绝不接受客户端自报的 teamId）：
//   - ROLE_ADMIN（运营管理员）：全量数据，无团队隔离
//   - ROLE_DOCTOR（医护）：仅限本团队患者
//   - ROLE_CS（客服）：按矩阵取值 = 全量患者，不受团队隔离
//
// 团队的唯一事实源是 doctors.team_id，而登录身份（JWT sub / X-User-Id）是 admins.admin_id，
// 故必须走 doctors.admin_id 这一跳解析（与 T314 医护账号链路同一关联键）。
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
)

// roleDoctor 医护角色键（与 presetRoles、网关 rbac.go:42-44 字面量一致，枚举键不改）
const roleDoctor = "ROLE_DOCTOR"

// teamScope 调用者的团队数据范围
type teamScope struct {
	// limited 为 false 表示该角色不受团队限制（运营 / 客服），维持全量语义。
	limited bool
	// teamID 所属团队。limited 为真且 teamID 为空 = 该医护账号无团队归属，
	// 按 fail-closed 处理：列表返回空集、单资源返回 403，绝不退化成「不过滤」。
	teamID string
}

// resolveTeamScope 从身份头推导团队范围。
// ok 为 false 表示响应已写出（403 身份缺失 / 500 解析失败），调用方必须直接 return。
func (h *Handler) resolveTeamScope(c *gin.Context) (teamScope, bool) {
	if c.GetHeader(headerRole) != roleDoctor {
		return teamScope{}, true
	}
	adminID := c.GetHeader(headerUserID)
	if adminID == "" {
		fail(c, model.ErrForbidden("missing user identity"))
		return teamScope{}, false
	}
	teamID, _, err := h.store.DoctorTeamByAdmin(c.Request.Context(), adminID)
	if err != nil {
		fail(c, model.ErrInternal("resolve doctor team failed"))
		return teamScope{}, false
	}
	return teamScope{limited: true, teamID: teamID}, true
}

// filterTeamID 列表查询要落到 SQL 的团队过滤值：
// 无团队归属的医护返回空串并由 TeamScoped 标记落空集；不受限角色同样返回空串，
// 但调用方不会置 TeamScoped，故仍是运营侧的「不按团队过滤」。
func (s teamScope) filterTeamID(override string) string {
	if !s.limited {
		return override // 运营 / 客服：保留客户端 teamId 作为筛选条件
	}
	return s.teamID // 医护：一律按自己的团队，客户端传的 teamId 被忽略
}

// denyCrossTeam 单资源越界访问统一按 403。
//
// T350 判据③ 的口径修正：本函数同时承接「档案不存在」——受限范围下的详情读走
// repo.GetPatientInTeam（团队谓词在同一条 SQL 里），无行即回本函数，因此对医护这一端点
// 永远不出 404，患者号存在性不可辨。不受限角色（运营 / 客服）不进这条路径，仍吃原 404。
func denyCrossTeam(c *gin.Context, patientID string) {
	fail(c, model.ErrForbidden("patient %s is out of your data scope", patientID))
}
