// T679 医护待办读侧 handler（PRD V3.45 §7D.16 我的待办；设计稿 医护待办列表.html / 医护待办详情.html）
//
// 路由：
//
//	GET /api/v1/todos            待办列表（pane=mine|all；keyword；分页）
//	GET /api/v1/todos?instanceId= 待办详情（不新增第二端点；含告警详情 + 流程链路 + 处理流水）
//
// 口径（设计稿 spec 逐条）：
//   - 「待我处理」= running 实例 current 节点经 T668 同源判定后归本人；「全部告警」= 本团队全部在途实例；
//   - 团队范围（T350）：doctor 两个页签均按 patients.team_id 收口；无团队归属 fail-closed 空集；
//     admin 不过滤（全院监督）；
//   - 计数（mineCount/allCount）不受 keyword 影响 —— 故 keyword 过滤与分页在 Go 侧做；
//   - 时限 chip：deadline = nodeEnteredAt + properties.timeLimit×timeUnit（缺配置默认 30 分钟），
//     临期阈值 30 分钟（due），已超时 over，超时只标红不升级（R5/R6）。
//
// 🔴 已办结判定用 instance.status == 'completed'，绝不用 current_node_id 是否为空：
// confirm 无后继时实例置 completed 但 current_node_id 仍指向最后节点（repo/flow_t274.go 状态机口径）。
package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	flowTodoDefaultLimit = 30                   // properties.timeLimit 缺省（分钟）
	flowTodoDefaultUnit  = "minutes"            // properties.timeUnit 缺省
	flowTodoDueWindow    = 30 * time.Minute     // R5 临期阈值
)

// flowTodoUnitNames timeUnit → 中文单位（limitText 用）。
var flowTodoUnitNames = map[string]string{
	"minutes": "分钟",
	"hours":   "小时",
	"days":    "天",
}

// flowTodoUnitDuration timeUnit → 单单位时长。
func flowTodoUnitDuration(unit string) time.Duration {
	switch unit {
	case "days":
		return 24 * time.Hour
	case "hours":
		return time.Hour
	default:
		return time.Minute
	}
}

// flowTodoLimit 解析节点时限配置（缺/非法回默认 30 分钟，与设计器默认一致）。
func flowTodoLimit(p flowGraphNodeProps) (int, string) {
	limit := p.TimeLimit
	if limit <= 0 {
		limit = flowTodoDefaultLimit
	}
	unit := strings.TrimSpace(p.TimeUnit)
	if _, ok := flowTodoUnitNames[unit]; !ok {
		unit = flowTodoDefaultUnit
	}
	return limit, unit
}

// flowTodoLimitText 时限 chip 文案，形如「时限 2 小时」「时限 30 分钟」。
func flowTodoLimitText(limit int, unit string) string {
	return fmt.Sprintf("时限 %d %s", limit, flowTodoUnitNames[unit])
}

// flowTodoRemainText 剩余时长的紧凑中文文案（天 > 时 > 分，逐级只带一档下位）。
func flowTodoRemainText(d time.Duration) string {
	total := int(d / time.Minute)
	if total < 0 {
		total = -total
	}
	days := total / (24 * 60)
	hours := (total % (24 * 60)) / 60
	mins := total % 60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%d 天 %d 时", days, hours)
	case days > 0:
		return fmt.Sprintf("%d 天", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%d 时 %d 分", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%d 时", hours)
	default:
		return fmt.Sprintf("%d 分", mins)
	}
}

// flowTodoLeftText 时限 chip 剩余文案 + 色调（over=超时红 / due=临期橙 / ok=正常绿）。
func flowTodoLeftText(remaining time.Duration) (string, string) {
	if remaining < 0 {
		return "超时 " + flowTodoRemainText(remaining), "over"
	}
	tone := "ok"
	if remaining <= flowTodoDueWindow {
		tone = "due"
	}
	return "剩 " + flowTodoRemainText(remaining), tone
}

// flowTodoDeadline 当前节点时限截止时刻。
func flowTodoDeadline(enteredAt time.Time, limit int, unit string) time.Time {
	return enteredAt.Add(time.Duration(limit) * flowTodoUnitDuration(unit))
}

// flowTodoNodeName 节点展示名（模板 text.value，空则回退 node_id）。
func flowTodoNodeName(g *flowGraph, nodeID string) string {
	if name := g.nameOf[nodeID]; name != "" {
		return name
	}
	return nodeID
}

// flowTodoNodeDTO 列表「当前节点」列 DTO。
func flowTodoNodeDTO(g *flowGraph, nodeID string, enteredAt time.Time) *model.FlowTodoNodeDTO {
	limit, unit := flowTodoLimit(g.props[nodeID])
	leftText, tone := flowTodoLeftText(time.Until(flowTodoDeadline(enteredAt, limit, unit)))
	return &model.FlowTodoNodeDTO{
		NodeID:    nodeID,
		Name:      flowTodoNodeName(g, nodeID),
		LimitText: flowTodoLimitText(limit, unit),
		LeftText:  leftText,
		Tone:      tone,
	}
}

// flowTodoIsMine 「待我处理」判定：running 且当前节点经 T668 同源执法归本人。
//
// 🔴 采用 T668 同源判定（admin 恒放行 / 显式 assignee 只认该账号 / 否则模板角色须匹配 / 都没配放行）。
// 与「严格只认 assigneeRole 命中」的差异已在 PR 卡评论登记（避免第二套判定漂移）。
func flowTodoIsMine(r *repo.FlowTodoRow, g *flowGraph, operatorID, xRole string) bool {
	if r.InstanceStatus != model.FlowInstanceRunning {
		return false
	}
	if r.CurrentNodeID == nil || strings.TrimSpace(*r.CurrentNodeID) == "" {
		return false
	}
	nodeID := strings.TrimSpace(*r.CurrentNodeID)
	assignee := ""
	if r.NodeAssignee != nil {
		assignee = strings.TrimSpace(*r.NodeAssignee)
	}
	allowed, _ := enforceFlowNodeAssignment(flowNodeGuardInput{
		ConfiguredRole: g.assigneeRole[nodeID],
		StateAssignee:  assignee,
		OperatorID:     operatorID,
		XRole:          xRole,
	})
	return allowed
}

// flowTodoItemDTO 单行装配（Node 仅 running 实例有值；completed 行 node=null → 前端显示「已办结」）。
func flowTodoItemDTO(r *repo.FlowTodoRow, g *flowGraph) model.FlowTodoItemDTO {
	dto := model.FlowTodoItemDTO{
		InstanceID:        r.InstanceID,
		AlertID:           strconv.FormatInt(r.AlertID, 10),
		PatientID:         r.AlertPatientID,
		AlertType:         r.AlertType,
		AlertTypeName:     model.FlowTodoAlertTypeName(r.AlertType),
		TriggeredAt:       flowTimeStr(r.AlertTS),
		ProcessStatus:     r.ProcessStatus,
		ProcessStatusText: model.FlowTodoProcessStatusText(r.ProcessStatus),
	}
	if r.PatientName != nil {
		dto.PatientName = *r.PatientName
	}
	if r.InstanceStatus == model.FlowInstanceRunning && r.CurrentNodeID != nil && strings.TrimSpace(*r.CurrentNodeID) != "" {
		dto.Node = flowTodoNodeDTO(g, strings.TrimSpace(*r.CurrentNodeID), r.NodeEnteredAt)
	}
	return dto
}

// listMyTodos GET /api/v1/todos —— 医护待办列表（两页签 + keyword + 分页）。
func (h *Handler) listMyTodos(c *gin.Context) {
	if !requireFlowStaff(c) {
		fail(c, model.ErrForbidden("only internal staff can access todos"))
		return
	}
	scope, allowed := h.resolveTeamScope(c)
	if !allowed {
		return
	}
	if instanceID := strings.TrimSpace(c.Query("instanceId")); instanceID != "" {
		h.getMyTodoDetail(c, scope, instanceID)
		return
	}
	page, pageSize, appErr := parsePaging(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}

	rows, err := h.store.ListFlowTodos(c.Request.Context(), repo.FlowTodoFilter{
		TeamID:     scope.filterTeamID(""),
		TeamScoped: scope.limited,
	})
	if err != nil {
		fail(c, model.ErrInternal("list todos failed"))
		return
	}

	opID := operatorID(c, "")
	xRole := c.GetHeader(headerRole)
	keyword := strings.ToLower(strings.TrimSpace(c.Query("keyword")))
	pane := strings.TrimSpace(c.Query("pane"))
	if pane == "" {
		pane = "mine"
	}

	type todoItem struct {
		dto  model.FlowTodoItemDTO
		mine bool
	}
	all := make([]todoItem, 0, len(rows))
	var mineCount int64
	for i := range rows {
		r := rows[i]
		g, gErr := parseFlowGraph(r.Nodes, nil)
		if gErr != nil {
			g = newFlowGraph() // 模板 JSON 异常不阻塞列表（节点名回退 node_id）
		}
		mine := flowTodoIsMine(&r, g, opID, xRole)
		if mine {
			mineCount++
		}
		all = append(all, todoItem{dto: flowTodoItemDTO(&r, g), mine: mine})
	}
	allCount := int64(len(all))

	// 展示行：先按页签筛，再按 keyword 筛（计数已在上面算完，不受 keyword 影响）。
	filtered := make([]model.FlowTodoItemDTO, 0, len(all))
	for _, it := range all {
		if pane == "mine" && !it.mine {
			continue
		}
		if keyword != "" {
			name := strings.ToLower(it.dto.PatientName)
			pid := strings.ToLower(it.dto.PatientID)
			if !strings.Contains(name, keyword) && !strings.Contains(pid, keyword) {
				continue
			}
		}
		filtered = append(filtered, it.dto)
	}
	total := int64(len(filtered))

	start := (page - 1) * pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}

	ok(c, model.FlowTodoListDTO{
		List:      filtered[start:end],
		Total:     total,
		Page:      page,
		PageSize:  pageSize,
		MineCount: mineCount,
		AllCount:  allCount,
	})
}

// getMyTodoDetail GET /api/v1/todos?instanceId= —— 待办详情（告警详情 + 流程链路 + 处理流水）。
func (h *Handler) getMyTodoDetail(c *gin.Context, scope teamScope, instanceID string) {
	rows, err := h.store.ListFlowTodos(c.Request.Context(), repo.FlowTodoFilter{
		InstanceID: instanceID,
		TeamID:     scope.filterTeamID(""),
		TeamScoped: scope.limited,
	})
	if err != nil {
		fail(c, model.ErrInternal("get todo detail failed"))
		return
	}
	if len(rows) == 0 {
		if scope.limited {
			// T350 判据③：受限范围下「不存在」与「跨团队」同一次带谓词读、同一 403，不做存在性 oracle。
			denyCrossTeam(c, instanceID)
			return
		}
		fail(c, model.ErrNotFound("todo not found: %s", instanceID))
		return
	}
	r := rows[0]

	g, gErr := parseFlowGraph(r.Nodes, nil)
	if gErr != nil {
		g = newFlowGraph()
	}
	states, err := h.store.ListNodeStates(c.Request.Context(), instanceID)
	if err != nil {
		fail(c, model.ErrInternal("load todo node states failed"))
		return
	}
	actions, err := h.store.ListFlowNodeActions(c.Request.Context(), instanceID)
	if err != nil {
		fail(c, model.ErrInternal("load todo actions failed"))
		return
	}

	dto := model.FlowTodoDetailDTO{
		InstanceID:        r.InstanceID,
		TemplateID:        r.TemplateID,
		TemplateName:      r.TemplateName,
		InstanceStatus:    r.InstanceStatus,
		AlertID:           strconv.FormatInt(r.AlertID, 10),
		PatientID:         r.AlertPatientID,
		DeviceID:          r.DeviceID,
		AlertType:         r.AlertType,
		AlertTypeName:     model.FlowTodoAlertTypeName(r.AlertType),
		TriggeredAt:       flowTimeStr(r.AlertTS),
		ProcessStatus:     r.ProcessStatus,
		ProcessStatusText: model.FlowTodoProcessStatusText(r.ProcessStatus),
		Detail:            r.Detail,
		SensorPoint:       r.SensorPoint,
		ThresholdValue:    r.ThresholdValue,
		ActualValue:       r.ActualValue,
		Resolved:          r.ResolvedStatus == "resolved",
		ResolvedAt:        flowTimeStrPtr(r.ResolvedAt),
	}
	if r.PatientName != nil {
		dto.PatientName = *r.PatientName
	}

	// 流程链路（按模板节点顺序，模板外节点追加在后）。
	statusOf := map[string]string{}
	for _, st := range states {
		statusOf[st.NodeID] = st.Status
	}
	steps := make([]model.FlowTodoStepDTO, 0, len(states))
	placed := map[string]bool{}
	for _, id := range g.nodeIDs {
		st, exists := statusOf[id]
		if !exists {
			continue
		}
		steps = append(steps, model.FlowTodoStepDTO{
			NodeID: id, Name: flowTodoNodeName(g, id), Role: g.assigneeRole[id], Status: st,
		})
		placed[id] = true
	}
	for _, st := range states {
		if placed[st.NodeID] {
			continue
		}
		steps = append(steps, model.FlowTodoStepDTO{
			NodeID: st.NodeID, Name: flowTodoNodeName(g, st.NodeID), Role: g.assigneeRole[st.NodeID], Status: st.Status,
		})
	}
	dto.Steps = steps
	dto.Total = len(steps)

	// 当前步：办结置满格；running 按 current_node_id 在链路中的位次。
	if r.InstanceStatus == model.FlowInstanceCompleted {
		dto.Step = dto.Total
	} else if r.CurrentNodeID != nil {
		cur := strings.TrimSpace(*r.CurrentNodeID)
		for i, s := range steps {
			if s.NodeID == cur {
				dto.Step = i + 1
				break
			}
		}
	}

	// 当前节点指引（仅 running）。
	if r.InstanceStatus == model.FlowInstanceRunning && r.CurrentNodeID != nil && strings.TrimSpace(*r.CurrentNodeID) != "" {
		cur := strings.TrimSpace(*r.CurrentNodeID)
		props := g.props[cur]
		limit, unit := flowTodoLimit(props)
		leftText, tone := flowTodoLeftText(time.Until(flowTodoDeadline(r.NodeEnteredAt, limit, unit)))
		note := strings.TrimSpace(props.Note)
		if note == "" {
			note = model.FlowTodoDefaultNote
		}
		dto.Node = &model.FlowTodoNodeDetailDTO{
			NodeID:    cur,
			Name:      flowTodoNodeName(g, cur),
			Role:      g.assigneeRole[cur],
			LimitText: flowTodoLimitText(limit, unit),
			LeftText:  leftText,
			Tone:      tone,
			Note:      note,
		}
	}

	// 处理流水（时间线，nodeName 由模板解析补全）。
	records := make([]model.FlowNodeActionDTO, 0, len(actions))
	for i := range actions {
		var nodeName *string
		if n := g.nameOf[actions[i].NodeID]; n != "" {
			nodeName = &n
		}
		records = append(records, flowActionToDTO(&actions[i], nodeName))
	}
	dto.Records = records

	ok(c, dto)
}
