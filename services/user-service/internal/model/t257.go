package model

// T257 11.5 子权限粒度（设计稿 admin/权限控制.html:121-187 菜单权限分配树）
//
// 🔴 PM 裁定 Q2 = (c)：子权限**只用于前端隐藏菜单/按钮，不参与后端鉴权**——
// 网关与服务层仍只判到角色级（roles.role_id → gateway RBAC 矩阵 / 服务层 scope）。
// 因此本组 DTO 承载的是「呈现口径」，不是「授权口径」：后端不会因为某个 item
// 缺失而拒绝请求。真要按子权限鉴权，须另立卡（涉及网关矩阵 + 全部 handler）。

// PermissionItemDTO 子权限条目（设计稿 .perm-item 勾选项）
type PermissionItemDTO struct {
	Key   string `json:"key"`   // 形如「模块.动作」，如 alerts.process
	Label string `json:"label"` // 设计稿中文标签，前端直接渲染
}

// PermissionGroupDTO 页面分组（设计稿 .perm-group；Module 与 permissions_json.modules 同一套词表）
type PermissionGroupDTO struct {
	Module string              `json:"module"`
	Label  string              `json:"label"`
	Items  []PermissionItemDTO `json:"items"`
}

// PermissionCatalogDTO GET /api/v1/admin/permissions/catalog —— 全量子权限目录（单一来源，
// 前端渲染勾选树 + 后端校验 items 都用它，避免两边各写一份词表漂移）
type PermissionCatalogDTO struct {
	Groups []PermissionGroupDTO `json:"groups"`
}

// MyPermissionsDTO GET /api/v1/admin/me/permissions —— 当前登录人的有效权限
// （PM 裁定 Q2 要求补的端点：前端据此渲染菜单/按钮，不用再自己按 roleId 硬编码）
type MyPermissionsDTO struct {
	AdminID string   `json:"adminId"` // 网关注入的 X-User-Id
	RoleID  string   `json:"roleId"`  // 网关注入的 X-Role
	Scope   string   `json:"scope"`   // 数据范围（all|team|all_patients）；角色未落库时为空
	Modules []string `json:"modules"` // 页面级权限（既有口径，不变）
	Items   []string `json:"items"`   // 子权限（已按目录物化，前端直接渲染）
}
