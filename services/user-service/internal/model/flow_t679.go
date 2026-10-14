// T679 医护待办 DTO（PRD V3.45 §7D.16 我的待办；设计稿 docs/design/admin/医护待办列表.html
// + 医护待办详情.html，定稿 commit 5994aa53）
//
// 范围：纯读侧派生，不新建表（设计稿 spec：数据来源 GET /todos；flow_instance(running/completed)
// → flow_node_state(current) → flow_template.nodes 取 name/kind/properties → JOIN alerts/patients，
// doctors.team_id 收口）。
//
// 🔴 JSON key 一律 camelCase（与 t252.go / flow_t274.go 同口径）。
package model

// FlowTodoDefaultNote 节点未配置 note 时的兜底指引句（设计稿 R8 两层兜底：后端默认句 → 前端兜底）。
const FlowTodoDefaultNote = "请核对该告警并按流程处置"

// FlowTodoProcessStatusText alert.process_status → 中文状态文案（设计稿状态列三态）。
func FlowTodoProcessStatusText(s string) string {
	switch s {
	case "pending":
		return "待处理"
	case "processing":
		return "处理中"
	case "processed":
		return "已处理"
	default:
		return s
	}
}

// FlowTodoAlertTypeName 告警类型 → 中文名（复用 T653 词表，未命中回退原值不编造）。
func FlowTodoAlertTypeName(t string) string {
	if name, ok := FlowBindAlertTypeNames[t]; ok {
		return name
	}
	return t
}

// FlowTodoNodeDTO 列表「当前节点」列（node-action 文本 + 时限 chip）。
// Tone 取值 over/due/ok（设计稿配色：over 红 / due 橙 / ok 绿）；LeftText 形如「超时 47 分」「剩 1 时 40 分」。
type FlowTodoNodeDTO struct {
	NodeID    string `json:"nodeId"`
	Name      string `json:"name"`
	LimitText string `json:"limitText"`
	LeftText  string `json:"leftText"`
	Tone      string `json:"tone"`
}

// FlowTodoItemDTO 待办列表一行。Node 为 nil = 实例已办结（设计稿办结行显示「已办结」）。
type FlowTodoItemDTO struct {
	InstanceID        string           `json:"instanceId"`
	AlertID           string           `json:"alertId"`
	PatientID         string           `json:"patientId"`
	PatientName       string           `json:"patientName"`
	AlertType         string           `json:"alertType"`
	AlertTypeName     string           `json:"alertTypeName"`
	TriggeredAt       string           `json:"triggeredAt"`
	ProcessStatus     string           `json:"processStatus"`
	ProcessStatusText string           `json:"processStatusText"`
	Node              *FlowTodoNodeDTO `json:"node"`
}

// FlowTodoListDTO 待办列表容器。MineCount/AllCount 为两个页签的角标计数，
// 🔴 不受 keyword 影响（设计稿 tRender 先算计数再按 keyword 过滤展示行）。
type FlowTodoListDTO struct {
	List      []FlowTodoItemDTO `json:"list"`
	Total     int64             `json:"total"`
	Page      int               `json:"page"`
	PageSize  int               `json:"pageSize"`
	MineCount int64             `json:"mineCount"`
	AllCount  int64             `json:"allCount"`
}

// FlowTodoStepDTO 详情页流程进度只读链路一步。
type FlowTodoStepDTO struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// FlowTodoNodeDetailDTO 详情页当前节点指引（含 note 兜底句）。
type FlowTodoNodeDetailDTO struct {
	NodeID    string `json:"nodeId"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	LimitText string `json:"limitText"`
	LeftText  string `json:"leftText"`
	Tone      string `json:"tone"`
	Note      string `json:"note"`
}

// FlowTodoDetailDTO 待办详情（GET /todos?instanceId= 单条）。不新增第二端点。
type FlowTodoDetailDTO struct {
	InstanceID        string                 `json:"instanceId"`
	TemplateID        string                 `json:"templateId"`
	TemplateName      string                 `json:"templateName"`
	InstanceStatus    string                 `json:"instanceStatus"`
	AlertID           string                 `json:"alertId"`
	PatientID         string                 `json:"patientId"`
	PatientName       string                 `json:"patientName"`
	DeviceID          string                 `json:"deviceId"`
	AlertType         string                 `json:"alertType"`
	AlertTypeName     string                 `json:"alertTypeName"`
	TriggeredAt       string                 `json:"triggeredAt"`
	ProcessStatus     string                 `json:"processStatus"`
	ProcessStatusText string                 `json:"processStatusText"`
	Detail            *string                `json:"detail"`
	SensorPoint       *string                `json:"sensorPoint"`
	ThresholdValue    *float64               `json:"thresholdValue"`
	ActualValue       *float64               `json:"actualValue"`
	Resolved          bool                   `json:"resolved"`
	ResolvedAt        *string                `json:"resolvedAt"`
	Step              int                    `json:"step"`
	Total             int                    `json:"total"`
	Node              *FlowTodoNodeDetailDTO `json:"node"`
	Steps             []FlowTodoStepDTO      `json:"steps"`
	Records           []FlowNodeActionDTO    `json:"records"`
}
