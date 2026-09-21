// T274 告警流程画布 DTO（2.3 运行态画布 + 2.4 拖拽设计器）
// 契约：docs/api/api-contracts.ts（docs PR #179）· 表：migration 000020
//
// 🔴 JSON key 一律 camelCase（DB 列名 snake_case 只到 repo 层为止），与 t252.go 同口径。
package model

import "encoding/json"

// ─────────────────────────────────────────────────────────────
// 枚举（DB 侧同名 CHECK，见 000020）
// ─────────────────────────────────────────────────────────────

// FlowNodeStatus 节点运行状态（设计稿 Tab3 图例四色）
const (
	FlowNodeDone    = "done"
	FlowNodeCurrent = "current"
	FlowNodeTodo    = "todo"
	FlowNodeSkipped = "skipped"
)

// FlowAction 节点操作类型（设计稿 Tab3 右侧四个按钮）
const (
	FlowActionConfirm  = "confirm"
	FlowActionReject   = "reject"
	FlowActionTransfer = "transfer"
	FlowActionUrge     = "urge"
)

// FlowInstanceStatus 实例状态
const (
	FlowInstanceRunning    = "running"
	FlowInstanceCompleted  = "completed"
	FlowInstanceTerminated = "terminated"
)

// ─────────────────────────────────────────────────────────────
// 请求体
// ─────────────────────────────────────────────────────────────

// CreateFlowTemplateRequest POST /admin/flow/templates
// Nodes/Edges 收 LogicFlow graphData 原样数组（后端不解析业务属性，仅校验 id 与边端点自洽）。
// name 不加 binding:"required" —— 量程与空值统一由 handler 的 validateFlowName 报同一条 400 文案。
type CreateFlowTemplateRequest struct {
	Name  string          `json:"name"`
	Nodes json.RawMessage `json:"nodes"`
	Edges json.RawMessage `json:"edges"`
}

// UpdateFlowTemplateRequest PUT /admin/flow/templates/:templateId
// 设计器「保存」是全量覆盖（nil = 该字段不动），nodes/edges 一旦给出即整体替换。
type UpdateFlowTemplateRequest struct {
	Name  *string         `json:"name"`
	Nodes json.RawMessage `json:"nodes"`
	Edges json.RawMessage `json:"edges"`
}

// StartFlowInstanceRequest POST /admin/flow/instances
type StartFlowInstanceRequest struct {
	TemplateID string `json:"templateId" binding:"required"`
	AlertID    string `json:"alertId" binding:"required"`
}

// FlowNodeActionRequest POST /admin/flow/instances/:id/nodes/:nodeId/actions
type FlowNodeActionRequest struct {
	Action         string          `json:"action" binding:"required"`
	Remark         string          `json:"remark"`
	Attachments    json.RawMessage `json:"attachments"`
	TargetOperator string          `json:"targetOperator"`
	// NextNodeIDs 仅 confirm 在判断/并行分叉上有用：给出则只推进列出的后继，缺省推进全部出边。
	NextNodeIDs json.RawMessage `json:"nextNodeIds"`
}

// ─────────────────────────────────────────────────────────────
// 响应 DTO
// ─────────────────────────────────────────────────────────────

// FlowTemplateDTO 流程模板（2.4 设计器产物）
type FlowTemplateDTO struct {
	TemplateID    string          `json:"templateId"`
	Name          string          `json:"name"`
	Nodes         json.RawMessage `json:"nodes"`
	Edges         json.RawMessage `json:"edges"`
	Creator       string          `json:"creator"`
	CreatorName   *string         `json:"creatorName"`
	Version       int             `json:"version"`
	CreatedAt     string          `json:"createdAt"`
	UpdatedAt     string          `json:"updatedAt"`
	InstanceCount int             `json:"instanceCount"`
}

// FlowInstanceDTO 流程实例（一条告警一个）
type FlowInstanceDTO struct {
	InstanceID    string  `json:"instanceId"`
	TemplateID    string  `json:"templateId"`
	TemplateName  string  `json:"templateName"`
	AlertID       string  `json:"alertId"`
	CurrentNodeID *string `json:"currentNodeId"`
	Status        string  `json:"status"`
	StartedAt     string  `json:"startedAt"`
	EndedAt       *string `json:"endedAt"`
}

// FlowNodeStateDTO 节点运行状态（2.3 画布着色数据源）
type FlowNodeStateDTO struct {
	NodeID       string          `json:"nodeId"`
	Status       string          `json:"status"`
	Operator     *string         `json:"operator"`
	OperatorName *string         `json:"operatorName"`
	OperatedAt   *string         `json:"operatedAt"`
	Remark       *string         `json:"remark"`
	Attachments  json.RawMessage `json:"attachments"`
	Assignee     *string         `json:"assignee"`
	AssigneeName *string         `json:"assigneeName"`
	NextNodeIDs  []string        `json:"nextNodeIds"`
}

// FlowNodeActionDTO 节点操作流水（2.3 处理时间线数据源）
type FlowNodeActionDTO struct {
	ActionID       string          `json:"actionId"`
	NodeID         string          `json:"nodeId"`
	NodeName       *string         `json:"nodeName"`
	Action         string          `json:"action"`
	ActionLabel    string          `json:"actionLabel"`
	Operator       string          `json:"operator"`
	OperatorName   *string         `json:"operatorName"`
	Remark         *string         `json:"remark"`
	Attachments    json.RawMessage `json:"attachments"`
	TargetOperator *string         `json:"targetOperator"`
	CreatedAt      string          `json:"createdAt"`
}
