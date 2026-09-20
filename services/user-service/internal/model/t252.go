// T252 admin 后端第2批 DTO（2.2 告警规则 / 11.2 角色 CRUD / 12.3 审计日志）
// 契约：docs/api/api-contracts.ts（docs PR #146）
package model

import "encoding/json"

// ─────────────────────────────────────────────────────────────
// 2.2 告警规则配置（设计稿 admin/告警管理.html:255-296 Tab2）
// ─────────────────────────────────────────────────────────────

// AlertPointRuleDTO 单个采集点规则（4×5 网格的一格）
type AlertPointRuleDTO struct {
	PointID         string   `json:"pointId"`
	Row             int      `json:"row"`
	Col             int      `json:"col"`
	Label           string   `json:"label"`
	Monitored       bool     `json:"monitored"`
	UpperN          *float64 `json:"upperN"`          // null = 跟随统一上限
	LowerN          *float64 `json:"lowerN"`          // null = 统一下限
	EffectiveUpperN float64  `json:"effectiveUpperN"` // 后端合成，前端不再算
	EffectiveLowerN float64  `json:"effectiveLowerN"`
}

// AlertGlobalRulesDTO 全局告警规则四项（设计稿 Tab2 第二卡）
type AlertGlobalRulesDTO struct {
	// DeviceOfflineMinutes T257 12.4：与 §7D.12 wearInterruptMinutes 同键
	// （sys_configs threshold_wear_interrupt_minutes），字段名沿用设计稿不改契约
	DeviceOfflineMinutes   float64 `json:"deviceOfflineMinutes"`
	DailyWearMinHours      float64 `json:"dailyWearMinHours"`
	ContinuousWearMaxHours float64 `json:"continuousWearMaxHours"`
	ReportTimeoutMinutes   float64 `json:"reportTimeoutMinutes"`
}

// AlertRulesDTO 规则聚合视图（一次 GET 渲染整个 Tab2）
type AlertRulesDTO struct {
	UnifiedUpperN float64             `json:"unifiedUpperN"`
	UnifiedLowerN float64             `json:"unifiedLowerN"`
	Points        []AlertPointRuleDTO `json:"points"`
	GlobalRules   AlertGlobalRulesDTO `json:"globalRules"`
}

// AlertPointRuleUpdateDTO 保存时的点位增量（pointId 合法性由 handler 校验 P01–P20）
type AlertPointRuleUpdateDTO struct {
	PointID   string   `json:"pointId"`
	Monitored *bool    `json:"monitored"`
	UpperN    *float64 `json:"upperN"` // null / 缺省 = 清除独立阈值、回退统一值
	LowerN    *float64 `json:"lowerN"`
}

// UpdateAlertPointRulesRequest PUT /admin/alert-rules/points
// 统一上下限用指针区分「未给出」；points 为增量（未列出的点位保持原值）。
type UpdateAlertPointRulesRequest struct {
	UnifiedUpperN *float64                  `json:"unifiedUpperN"`
	UnifiedLowerN *float64                  `json:"unifiedLowerN"`
	Points        []AlertPointRuleUpdateDTO `json:"points"`
}

// UpdateAlertGlobalRulesRequest PUT /admin/alert-rules/global（nil = 不改该键）
type UpdateAlertGlobalRulesRequest struct {
	DeviceOfflineMinutes   *float64 `json:"deviceOfflineMinutes"`
	DailyWearMinHours      *float64 `json:"dailyWearMinHours"`
	ContinuousWearMaxHours *float64 `json:"continuousWearMaxHours"`
	ReportTimeoutMinutes   *float64 `json:"reportTimeoutMinutes"`
}

// ─────────────────────────────────────────────────────────────
// 11.2 角色增删改（设计稿 admin/权限控制.html:105-229）
// ─────────────────────────────────────────────────────────────

// RoleTemplateDTO 角色模板（设计稿「从模板创建」下拉）
type RoleTemplateDTO struct {
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Permissions RolePermissionsDTO `json:"permissions"`
}

// CreateAdminRoleRequest POST /admin/roles
// Template 非空时以模板 permissions 为基线；Permissions 显式给出则整体覆盖模板。
type CreateAdminRoleRequest struct {
	Name        string              `json:"name" binding:"required"`
	Description *string             `json:"description"`
	Template    *string             `json:"template"`
	Permissions *RolePermissionsDTO `json:"permissions"`
}

// UpdateAdminRoleRequest PUT /admin/roles/:roleId（nil = 不改；不含 permissions，
// 权限矩阵仍走 PUT /admin/roles/:roleId/permissions，两条写入口职责不同）
type UpdateAdminRoleRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
}

// ─────────────────────────────────────────────────────────────
// 12.3 审计日志（PRD §8.2 · 设计稿 admin/系统配置.html:150-175）
// ─────────────────────────────────────────────────────────────

// AuditLogDTO 审计流水行（description 取自 detail->>'description'，PRD 无独立列）
type AuditLogDTO struct {
	LogID        int64           `json:"logId"`
	OperatorID   *string         `json:"operatorId"`
	OperatorName *string         `json:"operatorName"`
	OperatorRole *string         `json:"operatorRole"`
	Action       string          `json:"action"`
	ActionLabel  string          `json:"actionLabel"`
	TargetType   *string         `json:"targetType"`
	TargetID     *string         `json:"targetId"`
	Description  string          `json:"description"`
	Detail       json.RawMessage `json:"detail"`
	IP           *string         `json:"ip"`
	Ts           string          `json:"ts"`
}
