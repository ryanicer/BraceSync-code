// T687 账号形态归一（写侧落库前归一 + 读侧执法/回读兼容）。
//
// 断链背景：flow_node_state.assignee 由 transfer 落库，入参 targetOperator 三形态混用——
// 前端名册发 doctors.doctor_id（D0001）、自由输入发 admins.username（doctor_li）、提示文案发
// admins.admin_id（A0002）；而执法侧 OperatorID 恒为 JWT sub（= admins.admin_id），精确串比对
// 把「本人被转派后自己操作自己的节点」判成越界 403（T687【P55】卡面）。
//
// 修法（max 2026-10-15 批准的改动面）：写侧 resolve 后落库（新数据恒 canonical）；
// 读侧在执法输入装配处与 DTO 回读处 resolve（历史 username/doctor_id 形存量行兼容）。
// enforceFlowNodeAssignment 函数体零变更（T694 按动作分流收窄 admin 例外在 joe 线，
// 本卡不踩）；677 状态机语义（confirm/reject 推进、nextNodeIds 收窄）零触碰。
package handler

import (
	"context"
	"strings"
)

// resolveFlowAssignee 账号形态归一：username/doctor_id 形 → admin_id 形；
// 空串原样返回（未转派快路径）；查库失败回退原值——归一设施故障不得把本人锁死，
// 回退后行为与归一前逐位一致（宁可维持旧 403 也不放大成新的失败面）。
func (h *Handler) resolveFlowAssignee(ctx context.Context, raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	canonical, err := h.store.ResolveFlowAccountID(ctx, v)
	if err != nil || strings.TrimSpace(canonical) == "" {
		return v
	}
	return strings.TrimSpace(canonical)
}

// resolveFlowAssigneePtr 指针版（DTO 装配用；nil 保持 nil，不把「未转派」编造成空串指针）。
func (h *Handler) resolveFlowAssigneePtr(ctx context.Context, p *string) *string {
	if p == nil {
		return nil
	}
	v := h.resolveFlowAssignee(ctx, *p)
	return &v
}
