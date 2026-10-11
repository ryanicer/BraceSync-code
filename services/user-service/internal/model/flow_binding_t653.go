// T653 告警类型 ↔ 流程模板绑定 DTO（PRD V3.44 §8.2 flow_type_bindings 提案行落地）
// 契约：docs/api/api-contracts.ts · 表：migration 000037
//
// 语义三条（Boss 2026-10-09 R8 全裁按甲）：
//  1. 显式绑定无兜底 —— 类型无绑定行时不自动建实例，告警照常产生与通知；
//  2. 一告警一实例幂等（CreateFlowInstance 既有 409 语义不变）；
//  3. 绑定变更不追溯在途实例（实例自持 template_id）。
package model

// 可绑定告警类型词表（与 000037 CHECK 同形；对齐 000018 现行告警类型）。
// pressure_fluctuation 已砍（000018 保留 CHECK 仅为历史告警行可读），不入绑定；
// 无 data_timeout 类型（数据上报超时仅阈值参数，不产生告警）。
const (
	FlowBindAlertTypePressureHigh     = "pressure_high"        // 压力偏高
	FlowBindAlertTypeWearInterrupt    = "wear_interrupt"       // 设备离线（000018：「设备离线」≡ wear_interrupt）
	FlowBindAlertTypeSensorDrift      = "sensor_drift"         // 传感器标定异常
	FlowBindAlertTypeWearDurationShort = "wear_duration_short" // 佩戴时长不足
)

// FlowBindAlertTypes 规范四类型（GET 固定按此顺序回四行，未绑定行 templateId=null）。
var FlowBindAlertTypes = []string{
	FlowBindAlertTypePressureHigh,
	FlowBindAlertTypeWearInterrupt,
	FlowBindAlertTypeSensorDrift,
	FlowBindAlertTypeWearDurationShort,
}

// FlowBindAlertTypeNames 类型 → 中文名（后端给，前端不硬编码；口径 = PRD §7D.6）。
var FlowBindAlertTypeNames = map[string]string{
	FlowBindAlertTypePressureHigh:      "压力偏高",
	FlowBindAlertTypeWearInterrupt:     "设备离线",
	FlowBindAlertTypeSensorDrift:       "传感器标定异常",
	FlowBindAlertTypeWearDurationShort: "佩戴时长不足",
}

// ValidFlowBindAlertType 是否可绑定类型。
func ValidFlowBindAlertType(t string) bool {
	for _, v := range FlowBindAlertTypes {
		if v == t {
			return true
		}
	}
	return false
}

// FlowTypeBindingDTO 一行类型绑定（未绑定：TemplateID/TemplateName/UpdatedBy/UpdatedAt 均为 nil）。
type FlowTypeBindingDTO struct {
	AlertType     string  `json:"alertType"`
	AlertTypeName string  `json:"alertTypeName"`
	TemplateID    *string `json:"templateId"`
	TemplateName  *string `json:"templateName"`
	UpdatedBy     *string `json:"updatedBy"`
	UpdatedByName *string `json:"updatedByName"`
	UpdatedAt     *string `json:"updatedAt"`
}

// FlowTypeBindingItem 单项保存载荷：TemplateID 空串/缺省 = 解绑（删除该行）。
type FlowTypeBindingItem struct {
	AlertType  string `json:"alertType"`
	TemplateID string `json:"templateId"`
}

// UpdateFlowTypeBindingsRequest PUT /admin/flow/type-bindings
// 部分覆盖语义：仅提交的类型被 upsert/删除；UI 每次固定提交四类型。
type UpdateFlowTypeBindingsRequest struct {
	Bindings []FlowTypeBindingItem `json:"bindings"`
}

// AutoStartFlowRequest 服务间内部端点 POST /internal/flow/auto-start 请求体
// （alert-service 告警创建成功后调用；不经网关、不鉴 JWT）。
type AutoStartFlowRequest struct {
	AlertID   string `json:"alertId"`
	AlertType string `json:"alertType"`
}

// AutoStartFlowResponse 内部端点 data：
// started=true 新建实例成功；started=false 时 reason 为 unbound（无绑定）/
// exists（该告警已有实例，幂等）。
type AutoStartFlowResponse struct {
	Started    bool    `json:"started"`
	Reason     string  `json:"reason,omitempty"`
	InstanceID *string `json:"instanceId,omitempty"`
}
