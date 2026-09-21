// T257 11.5 子权限粒度（设计稿 admin/权限控制.html:121-187「菜单权限分配」树）
//
// 路由：
//
//	GET /api/v1/admin/permissions/catalog  子权限目录（9 组 23 项，前端渲染勾选树的单一来源）
//	GET /api/v1/admin/me/permissions       当前登录人的有效权限（PM 裁定 Q2 要求补的端点）
//
// 既有端点 GET/PUT /api/v1/admin/roles/:roleId/permissions 的 permissions_json 增加 items 字段，
// 无 DDL（jsonb 内扩展），老行 items 缺省 ⇒ 读时按目录物化为「modules 下全部子权限」。
//
// 🔴 鉴权口径不变（PM 裁定 Q2 = (c)）：子权限只供前端隐藏菜单/按钮，
// gateway RBAC 矩阵与服务层 scope 判定仍只看角色，不消费 items。
package handler

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
)

// permissionCatalog 子权限目录。
//
// 词表来源：设计稿 权限控制.html:121-187 的 9 个 .perm-group 与 23 个 .perm-item（逐条抄录，
// 未增未减）；module 取值沿用 roles.permissions_json.modules 既有词表（seed.sql:8-13），
// 因此「数据概览」= dashboard、「权限控制」= perm、「系统配置」= config，不新造模块名。
//
// 设计稿另有 3 个 seed 模块（orthosis / install / tech）在本树里没有分组——设计稿未给子权限，
// 保持只有页面级；给它们凭空造 item 会让前端渲染出设计稿上不存在的勾选项。
var permissionCatalog = []model.PermissionGroupDTO{
	{Module: "dashboard", Label: "数据概览", Items: []model.PermissionItemDTO{
		{Key: "dashboard.view_stats", Label: "查看统计面板"},
		{Key: "dashboard.view_alert_summary", Label: "查看告警汇总"},
	}},
	{Module: "realtime", Label: "实时监控", Items: []model.PermissionItemDTO{
		{Key: "realtime.view_patient", Label: "查看患者实时数据"},
		{Key: "realtime.compare_points", Label: "多采集点对比"},
	}},
	{Module: "patients", Label: "患者管理", Items: []model.PermissionItemDTO{
		{Key: "patients.view", Label: "查看患者列表"},
		{Key: "patients.edit", Label: "编辑患者信息"},
		{Key: "patients.bind_team", Label: "团队绑定"},
		{Key: "patients.delete", Label: "删除患者"},
	}},
	{Module: "teams", Label: "团队管理", Items: []model.PermissionItemDTO{
		{Key: "teams.view", Label: "查看团队"},
		{Key: "teams.edit", Label: "编辑团队"},
		{Key: "teams.manage_members", Label: "管理成员"},
	}},
	{Module: "devices", Label: "设备管理", Items: []model.PermissionItemDTO{
		{Key: "devices.view", Label: "查看设备列表"},
		{Key: "devices.edit", Label: "设备编辑"},
		{Key: "devices.ota", Label: "OTA管理"},
	}},
	{Module: "alerts", Label: "告警管理", Items: []model.PermissionItemDTO{
		{Key: "alerts.view", Label: "查看告警列表"},
		{Key: "alerts.process", Label: "处理告警"},
		{Key: "alerts.config_rules", Label: "配置规则"},
	}},
	{Module: "comm", Label: "患者沟通", Items: []model.PermissionItemDTO{
		{Key: "comm.view_feedback", Label: "查看反馈"},
		{Key: "comm.reply", Label: "回复患者"},
	}},
	{Module: "perm", Label: "权限控制", Items: []model.PermissionItemDTO{
		{Key: "perm.roles", Label: "角色管理"},
		{Key: "perm.assign", Label: "权限分配"},
	}},
	{Module: "config", Label: "系统配置", Items: []model.PermissionItemDTO{
		{Key: "config.basic", Label: "基本设置"},
		{Key: "config.audit_logs", Label: "查看操作日志"},
	}},
}

// permissionItemModule 子权限 key → 所属模块（校验用，启动时构建一次）
var permissionItemModule = func() map[string]string {
	idx := make(map[string]string, 23)
	for _, g := range permissionCatalog {
		for _, it := range g.Items {
			idx[it.Key] = g.Module
		}
	}
	return idx
}()

// materializeItems 按目录物化「这些模块下的全部子权限」（目录顺序，稳定可断言）。
// 用于 items 缺省的老角色：语义 = 未细化 ⇒ 组内全勾，与设计稿初始态一致。
func materializeItems(modules []string) []string {
	granted := make(map[string]struct{}, len(modules))
	for _, m := range modules {
		granted[m] = struct{}{}
	}
	out := make([]string, 0, len(permissionItemModule))
	for _, g := range permissionCatalog {
		if _, ok := granted[g.Module]; !ok {
			continue
		}
		for _, it := range g.Items {
			out = append(out, it.Key)
		}
	}
	return out
}

// validatePermissionItems 子权限清单校验：key 必须在目录内、重复拒绝、
// 且所属模块必须已在 modules 里勾上（否则是一条永远渲染不出来的死配置）。
func validatePermissionItems(items, modules []string) *model.AppError {
	granted := make(map[string]struct{}, len(modules))
	for _, m := range modules {
		granted[m] = struct{}{}
	}
	seen := make(map[string]struct{}, len(items))
	for _, key := range items {
		module, ok := permissionItemModule[key]
		if !ok {
			return model.ErrInvalidParam("unknown permission item %q (see GET /admin/permissions/catalog)", key)
		}
		if _, dup := seen[key]; dup {
			return model.ErrInvalidParam("duplicate permission item %q", key)
		}
		seen[key] = struct{}{}
		if _, ok := granted[module]; !ok {
			return model.ErrInvalidParam("permission item %q requires module %q in modules", key, module)
		}
	}
	return nil
}

// getPermissionCatalog GET /api/v1/admin/permissions/catalog
func (h *Handler) getPermissionCatalog(c *gin.Context) {
	ok(c, model.PermissionCatalogDTO{Groups: permissionCatalog})
}

// getMyPermissions GET /api/v1/admin/me/permissions
//
// 身份取网关注入头（X-User-Id / X-Role，架构 §5.2；gateway 已先剥离外部同名头）。
// 角色不在 roles 表（technician / patient / 已删角色）⇒ 200 + 空清单：本端点只驱动
// 前端渲染，空清单等于「什么都不显示」，不构成放权，故不按 404/403 处理。
func (h *Handler) getMyPermissions(c *gin.Context) {
	roleID := c.GetHeader(headerRole)
	if roleID == "" {
		fail(c, model.ErrForbidden("missing role identity"))
		return
	}
	dto := model.MyPermissionsDTO{
		AdminID: operatorID(c, ""),
		RoleID:  roleID,
		Modules: []string{},
		Items:   []string{},
	}
	row, err := h.store.GetRole(c.Request.Context(), roleID)
	if err != nil {
		fail(c, model.ErrInternal("get role failed"))
		return
	}
	if row != nil {
		var perms model.RolePermissionsDTO
		if err := json.Unmarshal([]byte(row.PermissionsJSON), &perms); err != nil {
			fail(c, model.ErrInternal("invalid permissions_json for role %s", row.RoleID))
			return
		}
		dto.Scope, dto.Modules = perms.Scope, perms.Modules
		dto.Items = perms.Items
		if dto.Items == nil {
			dto.Items = materializeItems(perms.Modules)
		}
	}
	ok(c, dto)
}
