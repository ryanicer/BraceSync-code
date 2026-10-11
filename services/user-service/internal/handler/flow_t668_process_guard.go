// T668 卡面口径 3：告警 process/processing 旁路收口的服务间守卫端点。
//
// 背景：绑定流程的告警在 alert-service 侧 POST /alerts/:id/process(ing) 原来只过
// 网关 staff 拦截——流程里规定某角色/某人才能处置的节点，任意 staff 可在告警列表
// 直接标「已处理」绕过节点执法。alert-service 不持有流程数据，故在打处理落库前
// 调本端点一问（与 T653 auto-start 同构：内网直连、不经网关、不鉴 JWT）。
//
// 判定：
//   - 该告警无 running 流程实例（未绑定/已完成/未启动）→ enforced=false，alert-service 按 staff 现状放行；
//   - 有 running 实例 → 操作者必须通过「任一 current 节点」的节点执法（admin 例外）。
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// flowProcessGuardRequest 服务间请求体（alert-service 从网关注入头转抄，不自行鉴权）。
type flowProcessGuardRequest struct {
	AlertID    int64  `json:"alertId"`
	OperatorID string `json:"operatorId"`
	Role       string `json:"role"`
}

// flowProcessGuardResponse enforced=false 时调用方按 staff 现状放行；
// enforced=true 且 allowed=false 时调用方回 403 + reason 可读短句。
type flowProcessGuardResponse struct {
	Enforced      bool   `json:"enforced"`
	Allowed       bool   `json:"allowed"`
	Reason        string `json:"reason,omitempty"`
	InstanceID    string `json:"instanceId,omitempty"`
	CurrentNodeID string `json:"currentNodeId,omitempty"`
}

// flowProcessGuard POST /internal/flow/process-guard
func (h *Handler) flowProcessGuard(c *gin.Context) {
	var req flowProcessGuardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if req.AlertID <= 0 {
		fail(c, model.ErrInvalidParam("alertId must be a positive integer"))
		return
	}

	ctx := c.Request.Context()
	rows, err := h.store.ListFlowInstancesByAlert(ctx, req.AlertID)
	if err != nil {
		fail(c, model.ErrInternal("list flow instances failed"))
		return
	}

	var inst *repo.FlowInstanceRow
	for i := range rows {
		if rows[i].Status == model.FlowInstanceRunning {
			inst = &rows[i]
			break
		}
	}
	if inst == nil {
		// 无 running 实例：未绑定流程或流程已结束 → 不启用节点执法，staff 可处理。
		ok(c, flowProcessGuardResponse{Enforced: false, Allowed: true})
		return
	}

	states, err := h.store.ListNodeStates(ctx, inst.InstanceID)
	if err != nil {
		fail(c, model.ErrInternal("list node states failed"))
		return
	}

	// 先取模板角色映射（每个 current 节点的 configuredRole 来自模板 properties）。
	_, graph, appErr := h.loadFlowInstanceGraph(ctx, inst.InstanceID)
	if appErr != nil {
		failFlowInstanceErr(c, "load flow instance failed", appErr)
		return
	}

	// admin 例外：不看节点直接放行。
	guardIn := flowNodeGuardInput{OperatorID: req.OperatorID, XRole: req.Role}
	if normalizeFlowXRole(req.Role) == normalizeFlowXRole(flowXRoleAdmin) {
		ok(c, flowProcessGuardResponse{Enforced: true, Allowed: true, InstanceID: inst.InstanceID})
		return
	}

	lastReason := "流程进行中，当前节点未授权该账号处理"
	for i := range states {
		if states[i].Status != model.FlowNodeCurrent {
			continue
		}
		nodeID := states[i].NodeID
		assignee := ""
		if states[i].Assignee != nil {
			assignee = *states[i].Assignee
		}
		guardIn.ConfiguredRole = graph.assigneeRole[nodeID]
		guardIn.StateAssignee = assignee
		if allowed, reason := enforceFlowNodeAssignment(guardIn); allowed {
			ok(c, flowProcessGuardResponse{
				Enforced: true, Allowed: true,
				InstanceID: inst.InstanceID, CurrentNodeID: nodeID,
			})
			return
		} else if reason != "" {
			lastReason = reason
		}
	}

	// 有 current 节点但无一通过（或无 current 节点的异常态）：执法拒绝。
	curNode := ""
	if inst.CurrentNodeID != nil {
		curNode = *inst.CurrentNodeID
	}
	ok(c, flowProcessGuardResponse{
		Enforced: true, Allowed: false, Reason: lastReason,
		InstanceID: inst.InstanceID, CurrentNodeID: curNode,
	})
}
