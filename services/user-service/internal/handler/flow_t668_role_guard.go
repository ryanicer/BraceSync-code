// T668 流程节点/告警处理角色级权限执法（Boss 2026-10-11 17:3x「开，就按建议逻辑来」）。
//
// 断链背景：T653 设计器把「处理角色」存进模板节点 properties.assigneeRole（展示面），
// 运行态 submitFlowNodeAction 只验 staff 身份不读它——任意 staff（运维/医生/客服）均可
// 操作任意节点。本文件补执法面，配置与校验重新接通：
//
//	节点配了处理角色 → 操作者 X-Role 必须匹配（admin 例外）；
//	节点被转派到具体账号（flow_node_state.assignee）→ 操作者必须是该账号（admin 例外）；
//	两者都没配 → 维持 staff 可操作现状（旧模板/未配置不锁死）。
package handler

import (
	"strings"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// flowGraphNodeProps 只取执法/待办要读的属性；其余业务属性后端仍不解析（迁移 000020 口径不变）。
// JSON 键 "assigneeRole" = T653 设计器落库键（apps/admin-web/.../designer/kinds.ts:199）；
// T679 新增 timeLimit/timeUnit（时限 chip 依据）/note（节点指引），键名同设计器 properties 形状。
type flowGraphNodeProps struct {
	AssigneeRole string `json:"assigneeRole"`
	TimeLimit    int    `json:"timeLimit"`
	TimeUnit     string `json:"timeUnit"`
	Note         string `json:"note"`
}

// flowNodeRoleMembers 模板「处理角色」中文业务词（设计器 ROLE_OPTIONS 词表：
// 主治医生/值班医生/护士/系统自动/管理员）→ 网关 X-Role 登录角色集合。
//
// 🔴 词表断层（PRD V3.16）：护士是 doctors.title 职称、不是后台登录角色；「系统自动」
// 无真人操作者——两者映射为空集，含义 = 该类节点只有 admin（例外通道）能手工操作，
// 这正是 Boss 验收口径「运维账号点护士节点应 403」。
// map key 一律用 normalizeFlowXRole 归一形（去 ROLE_ 前缀、小写）。
var flowNodeRoleMembers = map[string]map[string]bool{
	"管理员":  {"admin": true},
	"主治医生": {"doctor": true},
	"值班医生": {"doctor": true},
	"护士":   {},
	"系统自动": {},
}

// flowXRoleAdmin 管理员角色头（网关 X-Role 词形见 services/gateway/cmd/server/rbac.go:42-45；
// 技师为小写 technician、医生/客服为 ROLE_DOCTOR/ROLE_CS——非 admin 角色经 normalizeFlowXRole 归一比较）。
const flowXRoleAdmin = "ROLE_ADMIN"

// normalizeFlowXRole 把 X-Role 归一为比较形：去 ROLE_ 前缀、转小写。
// 同时兼容历史混形（staffRoles 表里 admin 与 ROLE_ADMIN 并存）。
func normalizeFlowXRole(role string) string {
	r := strings.TrimSpace(role)
	r = strings.TrimPrefix(r, "ROLE_")
	return strings.ToLower(r)
}

// flowNodeAllowsRole 模板配置的处理角色是否接纳该 X-Role。
//   - 配置空串（节点未配角色）→ 接纳（不限制，向后兼容）；
//   - 配置码值形（ROLE_DOCTOR，契约 :1197 示例形）→ 按归一化后的登录角色比；
//   - 配置中文业务词 → 查 flowNodeRoleMembers；
//   - 配置了但词表无映射（含护士/系统自动）→ 不接纳任何普通角色（仅 admin 例外放行）。
func flowNodeAllowsRole(configuredRole, xRole string) bool {
	cfg := strings.TrimSpace(configuredRole)
	if cfg == "" {
		return true
	}
	// 码值形（ROLE_ADMIN/ROLE_DOCTOR/ROLE_CS 或技师登录的小写 technician）直接归一比对。
	if strings.HasPrefix(strings.ToUpper(cfg), "ROLE_") || cfg == "technician" {
		return normalizeFlowXRole(cfg) == normalizeFlowXRole(xRole)
	}
	if members, ok := flowNodeRoleMembers[cfg]; ok {
		return members[normalizeFlowXRole(xRole)]
	}
	// 已配置但无法识别的业务词：fail-closed（不猜测、不旁路），仅 admin 例外。
	return false
}

// flowNodeGuardInput 一次节点执法判定的输入。
type flowNodeGuardInput struct {
	ConfiguredRole string // 模板节点 properties.assigneeRole（空=未配角色）
	StateAssignee  string // 运行态 flow_node_state.assignee（空=未转派到具体账号）
	OperatorID     string // gateway 注入 X-User-Id
	XRole          string // gateway 注入 X-Role
}

// enforceFlowNodeAssignment T668 节点级执法，返回 false 时 denyReason 为可读拒因（落 403 响应）。
//
// 规则（卡面口径 1+2，confirm/reject/transfer/urge 全部走此门；分层短路，不叠加）：
//  1. admin 恒放行（例外通道，运维兜底/纠偏不能被节点配置锁死）；
//  2. 节点已指派到具体账号（state.assignee 非空，仅 transfer 会写）→ 只认该账号：
//     是本人即放行（不再卡角色——转派目标可跨角色，显式指派优先于模板角色），不是本人即拒；
//  3. 未指派时，节点配了处理角色 → 操作者角色必须匹配；
//  4. 两层都没配 → staff 兜底已在外层 requireFlowStaff 验过，放行。
func enforceFlowNodeAssignment(in flowNodeGuardInput) (bool, string) {
	if normalizeFlowXRole(in.XRole) == normalizeFlowXRole(flowXRoleAdmin) {
		return true, ""
	}
	if assignee := strings.TrimSpace(in.StateAssignee); assignee != "" {
		if in.OperatorID != assignee {
			return false, "该节点已指派给指定处理人，仅当前处理人或管理员可操作"
		}
		return true, "" // 显式指派到本人：账号层短路，跳过模板角色
	}
	if cfg := strings.TrimSpace(in.ConfiguredRole); cfg != "" {
		if !flowNodeAllowsRole(cfg, in.XRole) {
			return false, "当前账号角色无权处理该节点（要求处理角色：" + cfg + "）"
		}
	}
	return true, ""
}

// nodeStateAssignee 从实例节点状态集合里取指定节点的运行态 assignee（空=未转派）。
func nodeStateAssignee(states []repo.FlowNodeStateRow, nodeID string) string {
	for i := range states {
		if states[i].NodeID == nodeID {
			if states[i].Assignee != nil {
				return strings.TrimSpace(*states[i].Assignee)
			}
			return ""
		}
	}
	return ""
}
