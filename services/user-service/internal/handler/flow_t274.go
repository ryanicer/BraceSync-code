// T274 告警流程画布 handler（2.3 运行态画布 + 2.4 拖拽设计器）
//
// 路由（网关 RBAC 见 services/gateway/cmd/server/rbac.go，本文件另做 handler 层兜底判定）：
//
//	GET    /api/v1/admin/flow/templates                              模板列表（adminOnly）
//	POST   /api/v1/admin/flow/templates                              新建模板（adminOnly）
//	GET    /api/v1/admin/flow/templates/:templateId                   模板详情（adminOnly）
//	PUT    /api/v1/admin/flow/templates/:templateId                   保存模板（adminOnly）
//	DELETE /api/v1/admin/flow/templates/:templateId                   删除模板（adminOnly）
//	POST   /api/v1/admin/flow/instances                              启动实例（staff）
//	GET    /api/v1/admin/flow/instances?alertId=                      按告警取实例（staff）
//	GET    /api/v1/admin/flow/instances/:instanceId/nodes             节点状态（staff）
//	POST   /api/v1/admin/flow/instances/:instanceId/nodes/:nodeId/actions  节点操作（staff）
//	GET    /api/v1/admin/flow/instances/:instanceId/actions           处理时间线（staff）
//
// 契约：docs/api/api-contracts.ts（docs PR #179）—— 字段名、枚举、状态机以契约为准，改一处需改两处。
//
// 🔴 后端对 LogicFlow 图数据「只解析三处」：node.id（含 text.value 供展示）、
// edge.sourceNodeId/targetNodeId（推进流程的有向图）。其余字段原样存取、不改写，
// 因此 2.4 存 lf.getGraphData() 与 2.3 lf.render() 回显无需前端二次转换（选型 §4.1 约定）。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// flow 量程常量（DB 列宽见 migration 000020；超限一律 400，不截断落库）
const (
	flowNameMax    = 64  // flow_template.name VARCHAR(64)
	flowNodeIDMax  = 64  // node_id VARCHAR(64)
	flowRemarkMax  = 512 // remark VARCHAR(512)
	flowAccountMax = 32  // operator / assignee VARCHAR(32)
	flowNodeMax    = 100 // 单模板节点上限（设计稿实测 <10，见选型 R5；防超大 JSONB 行）
	flowEdgeMax    = 300 // 单模板连线上限
	flowAttachMax  = 20  // 单次操作附件数上限
	flowFileIDMax  = 64  // file-service file_id VARCHAR(64)
)

// requireFlowStaff 运行态端点的角色兜底判定（复用包内 staffRoles 表，与 gateway rbac.go staffRoles 同口径）。
// handler 层兜底「绕过网关直连服务」的请求，纵深防御口径同 T190 requireAdminRole；fail-closed。
func requireFlowStaff(c *gin.Context) bool {
	return isStaffRole(c.GetHeader(headerRole))
}

// flowActionLabels action → 中文标签（后端给，前端不硬编码映射；同 auditActionLabels 口径）
var flowActionLabels = map[string]string{
	model.FlowActionConfirm:  "确认处理",
	model.FlowActionReject:   "驳回",
	model.FlowActionTransfer: "转派",
	model.FlowActionUrge:     "加急",
}

// validFlowAction 允许提交的操作集合（DB 侧 flow_node_action.action 同名 CHECK）
var validFlowAction = map[string]bool{
	model.FlowActionConfirm:  true,
	model.FlowActionReject:   true,
	model.FlowActionTransfer: true,
	model.FlowActionUrge:     true,
}

// ─────────────────────────────────────────────────────────────
// LogicFlow 图数据解析与校验
// ─────────────────────────────────────────────────────────────

// flowGraphNodes LogicFlow 2.x graphData.nodes 中后端要读的字段（其余忽略，原文仍存 JSONB）
type flowGraphNodes []struct {
	ID   string `json:"id"`
	Text *struct {
		Value string `json:"value"`
	} `json:"text"`
}

// flowGraphEdges LogicFlow 2.x graphData.edges（端点键名 2.x 为 sourceNodeId/targetNodeId）
type flowGraphEdges []struct {
	ID           string `json:"id"`
	SourceNodeID string `json:"sourceNodeId"`
	TargetNodeID string `json:"targetNodeId"`
}

// flowGraph 模板图的派生索引（每次请求由 nodes/edges 现算，不落库）
type flowGraph struct {
	nodeIDs     []string            // 全量节点（模板给出顺序）
	edgeCount   int                 // 连线条数（审计文案用；successors 已去重，不能直接取长度）
	nameOf      map[string]string   // node_id → 展示名（text.value，空则不收录）
	successors  map[string][]string // node_id → 出边指向的 node_id（去重保序）
	entryNodeID map[string]bool     // 入边为 0 的节点（实例启动时置 current）
}

func newFlowGraph() *flowGraph {
	return &flowGraph{
		nameOf:      map[string]string{},
		successors:  map[string][]string{},
		entryNodeID: map[string]bool{},
	}
}

// parseFlowGraph 校验并建索引。rawNodes/rawEdges 为空时视为空图（允许先建空模板再拖拽）。
//
// 校验面（契约 createFlowTemplate 已逐条写明）：JSON 形状、id 非空/≤64/唯一、
// 数量上限、边端点必须指向存在的节点。业务属性（properties.kind 等）一律不校验。
func parseFlowGraph(rawNodes, rawEdges json.RawMessage) (*flowGraph, *model.AppError) {
	g := newFlowGraph()

	nodes := flowGraphNodes{}
	if len(rawNodes) > 0 {
		if err := json.Unmarshal(rawNodes, &nodes); err != nil {
			return nil, model.ErrInvalidParam("nodes must be a JSON array: %v", err)
		}
	}
	if len(nodes) > flowNodeMax {
		return nil, model.ErrInvalidParam("nodes must have at most %d items, got %d", flowNodeMax, len(nodes))
	}
	seen := map[string]bool{}
	for i, n := range nodes {
		id := strings.TrimSpace(n.ID)
		if id == "" {
			return nil, model.ErrInvalidParam("nodes[%d].id must not be empty", i)
		}
		if utf8.RuneCountInString(id) > flowNodeIDMax {
			return nil, model.ErrInvalidParam("nodes[%d].id must be at most %d chars", i, flowNodeIDMax)
		}
		if seen[id] {
			return nil, model.ErrInvalidParam("duplicate node id: %s", id)
		}
		seen[id] = true
		g.nodeIDs = append(g.nodeIDs, id)
		if n.Text != nil && strings.TrimSpace(n.Text.Value) != "" {
			g.nameOf[id] = strings.TrimSpace(n.Text.Value)
		}
		g.successors[id] = nil // 保证每个节点都有 key（无出边时回空数组而非 null）
	}

	edges := flowGraphEdges{}
	if len(rawEdges) > 0 {
		if err := json.Unmarshal(rawEdges, &edges); err != nil {
			return nil, model.ErrInvalidParam("edges must be a JSON array: %v", err)
		}
	}
	if len(edges) > flowEdgeMax {
		return nil, model.ErrInvalidParam("edges must have at most %d items, got %d", flowEdgeMax, len(edges))
	}
	targeted := map[string]bool{}
	for i, e := range edges {
		if !seen[e.SourceNodeID] {
			return nil, model.ErrInvalidParam("edges[%d].sourceNodeId %q is not a node in this template", i, e.SourceNodeID)
		}
		if !seen[e.TargetNodeID] {
			return nil, model.ErrInvalidParam("edges[%d].targetNodeId %q is not a node in this template", i, e.TargetNodeID)
		}
		if e.SourceNodeID == e.TargetNodeID {
			return nil, model.ErrInvalidParam("edges[%d] is a self-loop on node %s", i, e.SourceNodeID)
		}
		if !containsString(g.successors[e.SourceNodeID], e.TargetNodeID) {
			g.successors[e.SourceNodeID] = append(g.successors[e.SourceNodeID], e.TargetNodeID)
		}
		targeted[e.TargetNodeID] = true
	}
	g.edgeCount = len(edges)
	for _, id := range g.nodeIDs {
		g.entryNodeID[id] = !targeted[id]
	}
	return g, nil
}

// flowAttachmentsJSON 校验附件入参：必须是 fileId 字符串数组（元素 ≤64 字符，≤20 个）。
// 返回规范化后的 JSON 文本（空数组 → "[]"），供 repo 落 jsonb。
func flowAttachmentsJSON(raw json.RawMessage) (string, *model.AppError) {
	if len(raw) == 0 {
		return "[]", nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return "", model.ErrInvalidParam(`attachments must be an array of fileId strings: %v`, err)
	}
	if len(ids) > flowAttachMax {
		return "", model.ErrInvalidParam("attachments must have at most %d items, got %d", flowAttachMax, len(ids))
	}
	for i, v := range ids {
		if strings.TrimSpace(v) == "" {
			return "", model.ErrInvalidParam("attachments[%d] must not be empty", i)
		}
		if utf8.RuneCountInString(v) > flowFileIDMax {
			return "", model.ErrInvalidParam("attachments[%d] must be at most %d chars", i, flowFileIDMax)
		}
	}
	if len(ids) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return "", model.ErrInternal("marshal attachments failed")
	}
	return string(b), nil
}

// validateFlowName 模板名量程（空/超长 → 400）
func validateFlowName(name string) *model.AppError {
	if name == "" || utf8.RuneCountInString(name) > flowNameMax {
		return model.ErrInvalidParam("name must be 1-%d chars", flowNameMax)
	}
	return nil
}

// validateFlowAccount 账号 ID 量程（operator / assignee 列宽 VARCHAR(32)）
func validateFlowAccount(field, v string) *model.AppError {
	if utf8.RuneCountInString(v) > flowAccountMax {
		return model.ErrInvalidParam("%s must be at most %d chars", field, flowAccountMax)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// DTO 装配
// ─────────────────────────────────────────────────────────────

func jsonOrEmpty(raw []byte) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("[]")
	}
	return json.RawMessage(raw)
}

func flowTimeStr(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func flowTimeStrPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func flowTemplateToDTO(r *repo.FlowTemplateRow) model.FlowTemplateDTO {
	return model.FlowTemplateDTO{
		TemplateID:    r.TemplateID,
		Name:          r.Name,
		Nodes:         jsonOrEmpty(r.Nodes),
		Edges:         jsonOrEmpty(r.Edges),
		Creator:       r.Creator,
		CreatorName:   r.CreatorName,
		Version:       r.Version,
		CreatedAt:     flowTimeStr(r.CreatedAt),
		UpdatedAt:     flowTimeStr(r.UpdatedAt),
		InstanceCount: r.InstanceCount,
	}
}

func flowInstanceToDTO(r *repo.FlowInstanceRow) model.FlowInstanceDTO {
	return model.FlowInstanceDTO{
		InstanceID:    r.InstanceID,
		TemplateID:    r.TemplateID,
		TemplateName:  r.TemplateName,
		AlertID:       strconv.FormatInt(r.AlertID, 10),
		CurrentNodeID: r.CurrentNodeID,
		Status:        r.Status,
		StartedAt:     flowTimeStr(r.StartedAt),
		EndedAt:       flowTimeStrPtr(r.EndedAt),
	}
}

func flowActionToDTO(r *repo.FlowNodeActionRow, nodeName *string) model.FlowNodeActionDTO {
	label := flowActionLabels[r.Action]
	if label == "" {
		label = r.Action // 历史值不在四枚举内时原样回显（同 auditActionLabels 口径）
	}
	return model.FlowNodeActionDTO{
		ActionID:       strconv.FormatInt(r.ActionID, 10),
		NodeID:         r.NodeID,
		NodeName:       nodeName,
		Action:         r.Action,
		ActionLabel:    label,
		Operator:       r.Operator,
		OperatorName:   r.OperatorName,
		Remark:         r.Remark,
		Attachments:    jsonOrEmpty(r.Attachments),
		TargetOperator: r.TargetOperator,
		CreatedAt:      flowTimeStr(r.CreatedAt),
	}
}

// ─────────────────────────────────────────────────────────────
// repo sentinel → HTTP
// ─────────────────────────────────────────────────────────────

// failFlowTemplateErr 模板读写的错误映射（NotFound → 404，其余 → 500）。
// 调用方已处理的重名 / 在用两条 sentinel 不在此处，保留在各 handler 内。
func failFlowTemplateErr(c *gin.Context, msg string, err error) {
	if errors.Is(err, repo.ErrFlowTemplateNotFound) {
		fail(c, model.ErrNotFound("flow template not found: %s", c.Param("templateId")))
		return
	}
	fail(c, model.ErrInternal("%s", msg))
}

// failFlowInstanceErr 实例侧错误映射。loadFlowInstanceGraph 会把「模板行损坏」
// 折成 errFlowGraphInvalid 抛出 —— 那是数据问题不是入参问题，一律 500。
func failFlowInstanceErr(c *gin.Context, msg string, err error) {
	switch {
	case errors.Is(err, repo.ErrFlowInstanceNotFound):
		fail(c, model.ErrNotFound("flow instance not found: %s", c.Param("instanceId")))
	case errors.Is(err, repo.ErrFlowTemplateNotFound):
		fail(c, model.ErrInternal("%s", msg)) // 实例引用的模板行不见了 = 数据不一致，不对外暴露细节
	default:
		fail(c, model.ErrInternal("%s", msg))
	}
}

// ─────────────────────────────────────────────────────────────
// 模板 CRUD（2.4 设计器）
// ─────────────────────────────────────────────────────────────

// listFlowTemplates GET /api/v1/admin/flow/templates
func (h *Handler) listFlowTemplates(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can manage flow templates"))
		return
	}
	page, pageSize, appErr := parsePaging(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	rows, total, err := h.store.ListFlowTemplates(c.Request.Context(), c.Query("keyword"), page, pageSize)
	if err != nil {
		fail(c, model.ErrInternal("list flow templates failed"))
		return
	}
	list := make([]model.FlowTemplateDTO, 0, len(rows))
	for i := range rows {
		list = append(list, flowTemplateToDTO(&rows[i]))
	}
	ok(c, model.PageData{List: list, Total: total, Page: page, PageSize: pageSize})
}

// getFlowTemplate GET /api/v1/admin/flow/templates/:templateId
func (h *Handler) getFlowTemplate(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can manage flow templates"))
		return
	}
	row, err := h.store.GetFlowTemplate(c.Request.Context(), c.Param("templateId"))
	if err != nil {
		failFlowTemplateErr(c, "get flow template failed", err)
		return
	}
	ok(c, flowTemplateToDTO(row))
}

// createFlowTemplate POST /api/v1/admin/flow/templates
func (h *Handler) createFlowTemplate(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can manage flow templates"))
		return
	}
	var req model.CreateFlowTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if appErr := validateFlowName(name); appErr != nil {
		fail(c, appErr)
		return
	}
	g, appErr := parseFlowGraph(req.Nodes, req.Edges)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ctx := c.Request.Context()
	taken, err := h.store.FlowTemplateNameTaken(ctx, name, "")
	if err != nil {
		fail(c, model.ErrInternal("check flow template name failed"))
		return
	}
	if taken {
		fail(c, model.ErrConflict("flow template name already exists: %s", name))
		return
	}
	op := operatorID(c, "")
	if appErr := validateFlowAccount("operator", op); appErr != nil {
		fail(c, appErr)
		return
	}
	row, err := h.store.CreateFlowTemplate(ctx, name, string(jsonOrEmpty(req.Nodes)), string(jsonOrEmpty(req.Edges)), op)
	if err != nil {
		fail(c, model.ErrInternal("create flow template failed"))
		return
	}
	nodeCount, edgeCount := len(g.nodeIDs), g.edgeCount
	h.audit(c, repo.AuditInput{
		Action:      auditActionConfig,
		TargetType:  "flow_template",
		TargetID:    row.TemplateID,
		Description: fmt.Sprintf("新增流程模板「%s」（%s，节点 %d / 连线 %d）", name, row.TemplateID, nodeCount, edgeCount),
	})
	ok(c, flowTemplateToDTO(row))
}

// updateFlowTemplate PUT /api/v1/admin/flow/templates/:templateId —— 设计器全量保存
func (h *Handler) updateFlowTemplate(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can manage flow templates"))
		return
	}
	templateID := c.Param("templateId")
	var req model.UpdateFlowTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if req.Name == nil && req.Nodes == nil && req.Edges == nil {
		fail(c, model.ErrInvalidParam("nothing to update"))
		return
	}

	ctx := c.Request.Context()
	// 图数据整体覆盖：新 edges 必须落在新 nodes 上，故两者都要按最终形状校验
	base, err := h.store.GetFlowTemplate(ctx, templateID)
	if err != nil {
		failFlowTemplateErr(c, "get flow template failed", err)
		return
	}
	newNodes, newEdges := base.Nodes, base.Edges
	if req.Nodes != nil {
		newNodes = []byte(req.Nodes)
	}
	if req.Edges != nil {
		newEdges = []byte(req.Edges)
	}
	if _, appErr := parseFlowGraph(newNodes, newEdges); appErr != nil {
		fail(c, appErr)
		return
	}

	var nameArg *string
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if appErr := validateFlowName(name); appErr != nil {
			fail(c, appErr)
			return
		}
		taken, err := h.store.FlowTemplateNameTaken(ctx, name, templateID)
		if err != nil {
			fail(c, model.ErrInternal("check flow template name failed"))
			return
		}
		if taken {
			fail(c, model.ErrConflict("flow template name already exists: %s", name))
			return
		}
		nameArg = &name
	}
	var nodesArg, edgesArg *string
	if req.Nodes != nil {
		v := string(newNodes)
		nodesArg = &v
	}
	if req.Edges != nil {
		v := string(newEdges)
		edgesArg = &v
	}
	row, err := h.store.UpdateFlowTemplate(ctx, templateID, nameArg, nodesArg, edgesArg)
	if err != nil {
		failFlowTemplateErr(c, "update flow template failed", err)
		return
	}
	h.audit(c, repo.AuditInput{
		Action:      auditActionConfig,
		TargetType:  "flow_template",
		TargetID:    templateID,
		Description: fmt.Sprintf("保存流程模板 %s（改名=%v 改节点=%v 改连线=%v，版本→%d）", templateID, req.Name != nil, req.Nodes != nil, req.Edges != nil, row.Version),
	})
	ok(c, flowTemplateToDTO(row))
}

// deleteFlowTemplate DELETE /api/v1/admin/flow/templates/:templateId
func (h *Handler) deleteFlowTemplate(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can manage flow templates"))
		return
	}
	templateID := c.Param("templateId")
	if err := h.store.DeleteFlowTemplate(c.Request.Context(), templateID); err != nil {
		var inUse *repo.ErrFlowTemplateInUse
		switch {
		case errors.As(err, &inUse):
			fail(c, model.ErrConflict("flow template %s is used by %d instance(s); instances must be removed first", templateID, inUse.InstanceCount))
		case errors.Is(err, repo.ErrFlowTemplateNotFound):
			fail(c, model.ErrNotFound("flow template not found: %s", templateID))
		default:
			fail(c, model.ErrInternal("delete flow template failed"))
		}
		return
	}
	h.audit(c, repo.AuditInput{
		Action:      auditActionConfig,
		TargetType:  "flow_template",
		TargetID:    templateID,
		Description: fmt.Sprintf("删除流程模板 %s", templateID),
	})
	c.JSON(http.StatusOK, jsonResp{Code: model.CodeOK, Message: "success", Data: nil})
}

// ─────────────────────────────────────────────────────────────
// 实例与节点状态（2.3 运行态）
// ─────────────────────────────────────────────────────────────

// startFlowInstance POST /api/v1/admin/flow/instances
//
// 派发单的端点表没有实例写入口 ⇒ 2.3 画布将永远无数据，故本卡补此端点（契约与 PR 均已登记）。
// 触发方（告警产生时自动 vs 人工选模板）派发单未定 ⇒ 先做显式调用，不改 alert-service。
func (h *Handler) startFlowInstance(c *gin.Context) {
	if !requireFlowStaff(c) {
		fail(c, model.ErrForbidden("only staff can start flow instance"))
		return
	}
	var req model.StartFlowInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	alertID, err := strconv.ParseInt(strings.TrimSpace(req.AlertID), 10, 64)
	if err != nil || alertID <= 0 {
		fail(c, model.ErrInvalidParam("alertId must be a positive integer, got %q", req.AlertID))
		return
	}
	op := operatorID(c, "")
	if appErr := validateFlowAccount("operator", op); appErr != nil {
		fail(c, appErr)
		return
	}

	ctx := c.Request.Context()
	tpl, err := h.store.GetFlowTemplate(ctx, req.TemplateID)
	if err != nil {
		if errors.Is(err, repo.ErrFlowTemplateNotFound) {
			// 本端点没有 :templateId 路径参数，不能用 failFlowTemplateErr（它读 param 会打出空 ID）
			fail(c, model.ErrNotFound("flow template not found: %s", req.TemplateID))
		} else {
			fail(c, model.ErrInternal("get flow template failed"))
		}
		return
	}
	g, appErr := parseFlowGraph(tpl.Nodes, tpl.Edges)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	entries := make([]string, 0, len(g.nodeIDs))
	for _, id := range g.nodeIDs {
		if g.entryNodeID[id] {
			entries = append(entries, id)
		}
	}
	row, err := h.store.CreateFlowInstance(ctx, req.TemplateID, alertID, g.nodeIDs, entries)
	if err != nil {
		var exists *repo.ErrFlowInstanceExists
		switch {
		case errors.Is(err, repo.ErrFlowTemplateNotFound):
			fail(c, model.ErrNotFound("flow template not found: %s", req.TemplateID))
		case errors.Is(err, repo.ErrFlowAlertNotFound):
			fail(c, model.ErrInvalidParam("alert not found: %s", req.AlertID))
		case errors.As(err, &exists):
			// 幂等入口：一条告警一个实例，重复启动回 409 + 已有实例，前端直接跳详情
			fail(c, model.ErrConflict("flow instance already exists for alert %s: %s", req.AlertID, exists.Existing.InstanceID))
		default:
			fail(c, model.ErrInternal("create flow instance failed"))
		}
		return
	}
	h.audit(c, repo.AuditInput{
		Action:      auditActionDataModify,
		TargetType:  "flow_instance",
		TargetID:    row.InstanceID,
		Description: fmt.Sprintf("为告警 %s 启动流程实例 %s（模板「%s」）", req.AlertID, row.InstanceID, tpl.Name),
	})
	// 409 分支的 data 需要带已有实例，统一由 GET instances?alertId 取；此处成功直接回新实例
	ok(c, flowInstanceToDTO(row))
}

// getFlowInstances GET /api/v1/admin/flow/instances?alertId=xxx
func (h *Handler) getFlowInstances(c *gin.Context) {
	if !requireFlowStaff(c) {
		fail(c, model.ErrForbidden("only staff can read flow instances"))
		return
	}
	raw := c.Query("alertId")
	alertID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || alertID <= 0 {
		fail(c, model.ErrInvalidParam("alertId query is required and must be a positive integer, got %q", raw))
		return
	}
	rows, err := h.store.ListFlowInstancesByAlert(c.Request.Context(), alertID)
	if err != nil {
		fail(c, model.ErrInternal("list flow instances failed"))
		return
	}
	list := make([]model.FlowInstanceDTO, 0, len(rows))
	for i := range rows {
		list = append(list, flowInstanceToDTO(&rows[i]))
	}
	ok(c, gin.H{"list": list})
}

// getFlowNodeStates GET /api/v1/admin/flow/instances/:instanceId/nodes —— 2.3 画布着色数据源
func (h *Handler) getFlowNodeStates(c *gin.Context) {
	if !requireFlowStaff(c) {
		fail(c, model.ErrForbidden("only staff can read flow nodes"))
		return
	}
	instanceID := c.Param("instanceId")
	ctx := c.Request.Context()
	inst, graph, err := h.loadFlowInstanceGraph(ctx, instanceID)
	if err != nil {
		failFlowInstanceErr(c, "load flow instance failed", err)
		return
	}
	_ = inst
	rows, err := h.store.ListNodeStates(ctx, instanceID)
	if err != nil {
		failFlowInstanceErr(c, "list flow node states failed", err)
		return
	}
	list := make([]model.FlowNodeStateDTO, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		list = append(list, model.FlowNodeStateDTO{
			NodeID:       r.NodeID,
			Status:       r.Status,
			Operator:     r.Operator,
			OperatorName: r.OperatorName,
			OperatedAt:   flowTimeStrPtr(r.OperatedAt),
			Remark:       r.Remark,
			Attachments:  jsonOrEmpty(r.Attachments),
			Assignee:     r.Assignee,
			AssigneeName: r.AssigneeName,
			NextNodeIDs:  successorsOf(graph, r.NodeID),
		})
	}
	ok(c, gin.H{"list": list})
}

// submitFlowNodeAction POST /api/v1/admin/flow/instances/:instanceId/nodes/:nodeId/actions
//
// 状态机（权威在后端，契约 submitFlowNodeAction 已写明）：
//   - confirm：current → done，出边后继（可按 nextNodeIds 收窄）置 current；无后继则实例 completed
//   - reject ：current → skipped，不推进
//   - transfer：仅改 assignee；urge：仅留痕
func (h *Handler) submitFlowNodeAction(c *gin.Context) {
	if !requireFlowStaff(c) {
		fail(c, model.ErrForbidden("only staff can operate flow nodes"))
		return
	}
	instanceID, nodeID := c.Param("instanceId"), c.Param("nodeId")
	var req model.FlowNodeActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if !validFlowAction[req.Action] {
		fail(c, model.ErrInvalidParam("invalid action %q (confirm|reject|transfer|urge)", req.Action))
		return
	}
	if utf8.RuneCountInString(req.Remark) > flowRemarkMax {
		fail(c, model.ErrInvalidParam("remark must be at most %d chars", flowRemarkMax))
		return
	}
	attachJSON, appErr := flowAttachmentsJSON(req.Attachments)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	op := operatorID(c, "")
	if appErr := validateFlowAccount("operator", op); appErr != nil {
		fail(c, appErr)
		return
	}
	if req.Action == model.FlowActionTransfer {
		if strings.TrimSpace(req.TargetOperator) == "" {
			fail(c, model.ErrInvalidParam("targetOperator is required when action=transfer"))
			return
		}
		if appErr := validateFlowAccount("targetOperator", req.TargetOperator); appErr != nil {
			fail(c, appErr)
			return
		}
	}

	ctx := c.Request.Context()
	_, graph, err := h.loadFlowInstanceGraph(ctx, instanceID)
	if err != nil {
		failFlowInstanceErr(c, "load flow instance failed", err)
		return
	}
	if _, isNode := graph.successors[nodeID]; !isNode {
		fail(c, model.ErrNotFound("node %s not found in this instance's template", nodeID))
		return
	}

	in := repo.FlowActionWrite{
		InstanceID: instanceID, NodeID: nodeID, Action: req.Action,
		Operator: op, Remark: req.Remark, Attachments: attachJSON,
		TargetOperator: req.TargetOperator,
	}
	switch req.Action {
	case model.FlowActionConfirm:
		next, appErr := pickFlowSuccessors(graph, nodeID, req.NextNodeIDs)
		if appErr != nil {
			fail(c, appErr)
			return
		}
		in.NodeStatus = model.FlowNodeDone
		in.Successors = next
		if len(next) == 0 {
			in.Complete = true
			in.CurrentNodeID = nodeID // 走完：指针停在最后一个节点
		} else {
			in.CurrentNodeID = sortedFirstString(next)
		}
	case model.FlowActionReject:
		in.NodeStatus = model.FlowNodeSkipped // 不推进、不动实例指针
	}

	row, err := h.store.ApplyFlowNodeAction(ctx, in)
	if err != nil {
		var notCur *repo.ErrFlowNodeNotCurrent
		switch {
		case errors.Is(err, repo.ErrFlowInstanceNotFound):
			fail(c, model.ErrNotFound("flow instance not found: %s", instanceID))
		case errors.Is(err, repo.ErrFlowNodeNotFound):
			fail(c, model.ErrNotFound("node %s has no state row in instance %s", nodeID, instanceID))
		case errors.Is(err, repo.ErrFlowInstanceCompleted):
			fail(c, model.ErrConflict("flow instance %s is already completed", instanceID))
		case errors.As(err, &notCur):
			fail(c, model.ErrConflict("%s; only the current node can be confirmed or rejected", notCur.Error()))
		default:
			fail(c, model.ErrInternal("apply flow node action failed"))
		}
		return
	}
	h.audit(c, repo.AuditInput{
		Action:     auditActionDataModify,
		TargetType: "flow_instance",
		TargetID:   instanceID,
		Description: fmt.Sprintf("流程节点 %s 执行%s%s", nodeID, flowActionLabels[req.Action],
			flowAuditRemarkSuffix(req.Remark)),
	})
	dto := flowActionToDTO(row, nameOfNode(graph, nodeID))
	ok(c, dto)
}

// getFlowInstanceActions GET /api/v1/admin/flow/instances/:instanceId/actions —— 处理时间线
func (h *Handler) getFlowInstanceActions(c *gin.Context) {
	if !requireFlowStaff(c) {
		fail(c, model.ErrForbidden("only staff can read flow actions"))
		return
	}
	instanceID := c.Param("instanceId")
	ctx := c.Request.Context()
	_, graph, err := h.loadFlowInstanceGraph(ctx, instanceID)
	if err != nil {
		failFlowInstanceErr(c, "load flow instance failed", err)
		return
	}
	rows, err := h.store.ListFlowNodeActions(ctx, instanceID)
	if err != nil {
		failFlowInstanceErr(c, "list flow node actions failed", err)
		return
	}
	list := make([]model.FlowNodeActionDTO, 0, len(rows))
	for i := range rows {
		list = append(list, flowActionToDTO(&rows[i], nameOfNode(graph, rows[i].NodeID)))
	}
	ok(c, gin.H{"list": list})
}

// ─────────────────────────────────────────────────────────────
// 内部装配辅助
// ─────────────────────────────────────────────────────────────

// loadFlowInstanceGraph 取实例 + 其模板的图索引（时间线/节点状态都要用模板反查节点名与后继）。
// 实例只存 template_id，渲染时实时读模板 —— 模板被改不影响在途实例的既有状态行，
// 但若模板节点被删，该节点的状态行仍在（读端点按 nodeId join，取不到名字回 null）。
func (h *Handler) loadFlowInstanceGraph(ctx context.Context, instanceID string) (*repo.FlowInstanceRow, *flowGraph, error) {
	inst, err := h.store.GetFlowInstance(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	tpl, err := h.store.GetFlowTemplate(ctx, inst.TemplateID)
	if err != nil {
		return nil, nil, err
	}
	g, appErr := parseFlowGraph(tpl.Nodes, tpl.Edges)
	if appErr != nil {
		return nil, nil, errFlowGraphInvalid
	}
	return inst, g, nil
}

// pickFlowSuccessors 计算 confirm 后要置 current 的后继：
// nextNodeIds 给出 → 只走列出的（判断/并行分叉由前端选），且必须是该节点的真实出边；
// 缺省 → 走全部出边。
func pickFlowSuccessors(g *flowGraph, nodeID string, raw json.RawMessage) ([]string, *model.AppError) {
	all := g.successors[nodeID]
	if len(raw) == 0 {
		return append([]string{}, all...), nil
	}
	var want []string
	if err := json.Unmarshal(raw, &want); err != nil {
		return nil, model.ErrInvalidParam("nextNodeIds must be an array of node ids: %v", err)
	}
	out := make([]string, 0, len(want))
	for _, w := range want {
		if !containsString(all, w) {
			return nil, model.ErrInvalidParam("nextNodeIds %q is not an outgoing edge target of node %s", w, nodeID)
		}
		if !containsString(out, w) {
			out = append(out, w)
		}
	}
	return out, nil
}

func successorsOf(g *flowGraph, nodeID string) []string {
	if g == nil {
		return []string{}
	}
	return append([]string{}, g.successors[nodeID]...)
}

func nameOfNode(g *flowGraph, nodeID string) *string {
	if g == nil {
		return nil
	}
	if v, hit := g.nameOf[nodeID]; hit {
		return &v
	}
	return nil
}

func flowAuditRemarkSuffix(remark string) string {
	if strings.TrimSpace(remark) == "" {
		return ""
	}
	return "，意见「" + remark + "」"
}

// errFlowGraphInvalid 模板图数据损坏（DB 里存了不合法 JSON）——内部错误，不是用户入参问题
var errFlowGraphInvalid = errors.New("stored flow template graph is invalid")

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func sortedFirstString(list []string) string {
	if len(list) == 0 {
		return ""
	}
	first := list[0]
	for _, v := range list[1:] {
		if v < first {
			first = v
		}
	}
	return first
}
