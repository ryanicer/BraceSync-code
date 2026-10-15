// T668 流程节点/告警处理角色级权限执法（Boss 2026-10-11 17:3x「开，就按建议逻辑来」）。
//
// 断链背景：T653 设计器把「处理角色」存进模板节点 properties.assigneeRole（展示面），
// 运行态 submitFlowNodeAction 只验 staff 身份不读它——任意 staff（运维/医生/客服）均可
// 操作任意节点。本文件补执法面，配置与校验重新接通：
//
//	节点配了处理角色 → 操作者 X-Role 必须匹配；
//	节点被转派到具体账号（flow_node_state.assignee）→ 操作者必须是该账号；
//	两者都没配 → 维持 staff 可操作现状（旧模板/未配置不锁死）。
//
// T694 甲案（分动作收窄，max 2026-10-15 裁定）：admin 豁免不再内置在执法门里，改按
// 动作在调用面分流——推进类（confirm/reject）去豁免同走门；纠偏类（transfer/urge）
// 在 submitFlowNodeAction 装配处显式豁免（保 T668 兜底本意：运维纠偏不被节点配置锁死）。
package handler

import (
	"strings"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
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
// 无真人操作者——两者映射为空集，无登录角色可命中（Boss 验收口径「运维账号点护士节点
// 应 403」由本空集保证）。T694 甲案后 admin 的推进类豁免也已移除：该类节点 confirm/reject
// 无人可手工推进，推进通路 = admin 走纠偏类转派到具体账号后由本人确认（或系统自动流转）。
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
//   - 配置了但词表无映射（含护士/系统自动）→ 不接纳任何普通角色（admin 例外在调用面分流）。
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
	// 已配置但无法识别的业务词：fail-closed（不猜测、不旁路）；admin 走调用面分流。
	return false
}

// flowNodeGuardInput 一次节点执法判定的输入。
type flowNodeGuardInput struct {
	ConfiguredRole string // 模板节点 properties.assigneeRole（空=未配角色）
	StateAssignee  string // 运行态 flow_node_state.assignee（空=未转派到具体账号）
	OperatorID     string // gateway 注入 X-User-Id
	XRole          string // gateway 注入 X-Role
	// Action 本次动作（T694 ⑦ max 点头：规则 2 拒因按动作分文案）——只影响拒因措辞，
	// 不影响放行/拒绝判定。空值按推进类缺省形（transfer/urge 才保留「或管理员」原句）；
	// process-guard（告警标已处理=推进类）与待办可见性不设此字段即得推进形。
	Action string
}

// enforceFlowNodeAssignment T668 节点级执法，返回 false 时 denyReason 为可读拒因（落 403 响应）。
//
// 规则（卡面口径 1+2，confirm/reject/transfer/urge 全部走此门；分层短路，不叠加）：
//  1. 节点已指派到具体账号（state.assignee 非空，仅 transfer 会写）→ 只认该账号：
//     是本人即放行（不再卡角色——转派目标可跨角色，显式指派优先于模板角色），不是本人即拒
//     （拒因按 in.Action 分文案，T694 ⑦：推进类去「或管理员」、纠偏类保留，见规则 1 内注）；
//  2. 未指派时，节点配了处理角色 → 操作者角色必须匹配；
//  3. 两层都没配 → staff 兜底已在外层 requireFlowStaff 验过，放行。
//
// T694 甲案：原「admin 恒放行」第 0 层短路已删（本函数只留上列规则，签名与规则体不动）。
// admin 豁免改在调用面按动作分流——纠偏类（transfer/urge）在 submitFlowNodeAction 装配处
// 显式豁免；待办可见性在 flowTodoIsMine 装配处显式放行（可见性不等于可操作性，B 问裁定）。
func enforceFlowNodeAssignment(in flowNodeGuardInput) (bool, string) {
	if assignee := strings.TrimSpace(in.StateAssignee); assignee != "" {
		if in.OperatorID != assignee {
			// T694 ⑦（按动作分文案）：推进类下 admin 也无豁免，文案若再提「或管理员」
			// 会自相矛盾污染排障（拒因只进技术日志，T464 双通道）；纠偏类保留原句。
			if flowActionIsRemedial(in.Action) {
				return false, "该节点已指派给指定处理人，仅当前处理人或管理员可操作"
			}
			return false, "该节点已指派给指定处理人，仅当前处理人可操作"
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

// flowActionIsRemedial T694 甲案的动作分流白名单：纠偏类动作保留 admin 例外，
// 推进类（confirm/reject）不豁免。显式列白名单——未知动作 fail-closed 落门内同走。
func flowActionIsRemedial(action string) bool {
	return action == model.FlowActionTransfer || action == model.FlowActionUrge
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
