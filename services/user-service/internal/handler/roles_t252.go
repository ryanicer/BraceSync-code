// T252 11.2 角色增删改（设计稿 admin/权限控制.html:105-229）
//
// 路由（均挂 gateway adminOnly RBAC）：
//
//	GET    /api/v1/admin/role-templates          角色模板下拉（从模板创建）
//	POST   /api/v1/admin/roles                   新建角色（重名 409）
//	PUT    /api/v1/admin/roles/:roleId           改角色名/描述/启停（预置角色改名 400）
//	DELETE /api/v1/admin/roles/:roleId           删除角色（预置 403；被账号引用 409 + memberCount）
//
// 🔴 权限矩阵仍走既有 PUT /admin/roles/:roleId/permissions（11.3，T030）——两条写入口职责不同：
//
//	本文件改「角色是什么」，那条改「角色能进哪些页面」。
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// roleTargetType audit_logs.target_type 取值
const roleTargetType = "role"

// roleTemplates 基础权限模板 —— **3 条**（Boss 2026-09-20 裁定，T277 收口）：
// 与设计稿 权限控制.html:203「新增角色 → 基础权限模板」下拉、以及 000017 收敛后的
// 3 个预置登录角色（运营管理员 / 医护 / 客服）一一对应。
// 🔴 「主任医师 / 主治医师 / 康复师 / 护士」是医护**职称**（走 doctors.title），不是角色，
// 不得作为模板 —— T262 收口 roles 表、T263 收口设计稿、本卡收口这个下拉。
// 键名对齐契约 RoleTemplate.key（前端 11.2 按本列表渲染，改键需同步契约）。
var roleTemplates = []model.RoleTemplateDTO{
	{
		Key: "admin", Name: "运营管理员", Description: "系统全部权限",
		// T345：与 seed/000026 后的 ROLE_ADMIN 同集 = 页全集。
		// 词表来源 apps/admin-web/src/router/permissions.ts 的 PAGE_MODULES（模块短键 ↔ 路由路径）。
		// 缺 review / review_tpl / doctor_acct 三键时，「系统全部权限」名不副实：
		// 复查报告、复查模板管理（T135）、医护账号（T315）三页早已上线，模板却没登记。
		// T372：补 abnormal_report 第 16 键（异常报告按 Boss 2026-09-24 裁定拆为独立页 /abnormal-report）。
		Permissions: model.RolePermissionsDTO{Scope: "all", Modules: []string{
			"dashboard", "realtime", "patients", "abnormal_report", "teams", "devices", "alerts",
			"comm", "orthosis", "install", "review", "review_tpl", "tech",
			"doctor_acct", "perm", "config",
		}},
	},
	// ⚠️ doctor 模板与 ROLE_DOCTOR 预置行**不等同**，T368 之后仍是两套值：
	//    - 预置行：seed.sql 现 7 项 = dashboard / realtime / abnormal_report / alerts / orthosis /
	//      review / review_tpl（T368 按 Boss 2026-09-24 09:3x 裁定 (a)「补库」补了 review / review_tpl，
	//      T372 再补 abnormal_report；只跑迁移的库由 000017 播 4 项、再经 000028 → 000029 前滚到同一终态）；
	//    - 本模板：多 patients、comm，缺 review、review_tpl —— 前两个是 T252 沿 000016 旧种子留下的
	//      偏差，T277 只收口模板**条数与名称**，是否连预设一起对齐已登记待 PM 裁定
	//      （改这里即改变新建角色的默认权限）⇒ 本模板仍不动（同 T368 口径），上面对预置行项数的描述才是本卡口径。
	{
		Key: "doctor", Name: "医护", Description: "患者数据+告警处理+沟通",
		Permissions: model.RolePermissionsDTO{Scope: "team", Modules: []string{
			"dashboard", "realtime", "patients", "alerts", "comm", "orthosis",
		}},
	},
	{
		Key: "cs", Name: "客服", Description: "患者沟通模块，全量患者",
		Permissions: model.RolePermissionsDTO{Scope: "all_patients", Modules: []string{"comm"}},
	},
}

// findRoleTemplate 按 key 取模板
func findRoleTemplate(key string) (model.RoleTemplateDTO, bool) {
	for _, t := range roleTemplates {
		if t.Key == key {
			return t, true
		}
	}
	return model.RoleTemplateDTO{}, false
}

// roleNameMax / roleDescMax 对齐 roles 列宽（000001:20-21 VARCHAR(64)/VARCHAR(255)；
// PG 的 VARCHAR(n) 按字符计，故用 rune 计数而非字节长度）
const (
	roleNameMax = 64
	roleDescMax = 255
)

// validatePermissions 权限形状校验（scope 白名单 + modules 非空，口径同 T030 updatePermissions）
func validatePermissions(p model.RolePermissionsDTO) *model.AppError {
	if _, scopeOK := validScopes[p.Scope]; !scopeOK {
		return model.ErrInvalidParam("invalid scope: %s (all|team|all_patients)", p.Scope)
	}
	if len(p.Modules) == 0 {
		return model.ErrInvalidParam("modules must not be empty")
	}
	return nil
}

// validateRoleName 角色名量程（空/超长 → 400）
func validateRoleName(field, name string) *model.AppError {
	if name == "" || utf8.RuneCountInString(name) > roleNameMax {
		return model.ErrInvalidParam("%s must be 1-%d chars", field, roleNameMax)
	}
	return nil
}

// listRoleTemplates GET /api/v1/admin/role-templates
func (h *Handler) listRoleTemplates(c *gin.Context) {
	ok(c, roleTemplates)
}

// createAdminRole POST /api/v1/admin/roles
func (h *Handler) createAdminRole(c *gin.Context) {
	var req model.CreateAdminRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if appErr := validateRoleName("name", name); appErr != nil {
		fail(c, appErr)
		return
	}
	desc := strings.TrimSpace(derefStr(req.Description))
	if utf8.RuneCountInString(desc) > roleDescMax {
		fail(c, model.ErrInvalidParam("description must be at most %d chars", roleDescMax))
		return
	}

	var perms model.RolePermissionsDTO
	switch {
	case req.Permissions != nil:
		perms = *req.Permissions
	case req.Template != nil && *req.Template != "":
		tpl, found := findRoleTemplate(*req.Template)
		if !found {
			fail(c, model.ErrInvalidParam("unknown template %q (admin|doctor|cs)", *req.Template))
			return
		}
		perms = tpl.Permissions
	default:
		fail(c, model.ErrInvalidParam("either template or permissions is required"))
		return
	}
	if appErr := validatePermissions(perms); appErr != nil {
		fail(c, appErr)
		return
	}

	ctx := c.Request.Context()
	taken, err := h.store.RoleNameTaken(ctx, name, "")
	if err != nil {
		fail(c, model.ErrInternal("check role name failed"))
		return
	}
	if taken {
		fail(c, model.ErrConflict("role name already exists: %s", name))
		return
	}

	payload, err := json.Marshal(perms)
	if err != nil {
		fail(c, model.ErrInternal("marshal permissions failed"))
		return
	}
	row, err := h.store.CreateRole(ctx, name, desc, string(payload))
	if err != nil {
		fail(c, model.ErrInternal("create role failed"))
		return
	}
	h.audit(c, repo.AuditInput{
		Action:      auditActionPermissions,
		TargetType:  roleTargetType,
		TargetID:    row.RoleID,
		Description: fmt.Sprintf("新增角色「%s」（%s，模板 %s）", name, row.RoleID, templateKeyOf(req.Template)),
		Detail:      map[string]any{"permissions": perms},
	})
	ok(c, toRoleDTO(*row))
}

// templateKeyOf 审计文案用：未走模板时给 "-"
func templateKeyOf(t *string) string {
	if t == nil || *t == "" {
		return "-"
	}
	return *t
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// updateAdminRole PUT /api/v1/admin/roles/:roleId —— 改角色档案（name/description/status）
//
// 预置角色（T262 收敛后的 3 个登录角色）的 **name 锁定 400**：设计稿按名字渲染角色，
// 改名会让前端矩阵与设计稿对不上；description / status 仍可改（契约 updateAdminRole）。
// 删除预置角色另行拦 403，见 deleteAdminRole。
func (h *Handler) updateAdminRole(c *gin.Context) {
	roleID := c.Param("roleId")
	var req model.UpdateAdminRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	_, preset := presetRoles[roleID]
	if preset && req.Name != nil {
		fail(c, model.ErrInvalidParam("preset role name is locked: %s", roleID))
		return
	}

	ctx := c.Request.Context()
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if appErr := validateRoleName("name", name); appErr != nil {
			fail(c, appErr)
			return
		}
		req.Name = &name
		taken, err := h.store.RoleNameTaken(ctx, name, roleID)
		if err != nil {
			fail(c, model.ErrInternal("check role name failed"))
			return
		}
		if taken {
			fail(c, model.ErrConflict("role name already exists: %s", name))
			return
		}
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(desc) > roleDescMax {
			fail(c, model.ErrInvalidParam("description must be at most %d chars", roleDescMax))
			return
		}
		req.Description = &desc
	}
	if req.Status != nil && *req.Status != "enabled" && *req.Status != "disabled" {
		fail(c, model.ErrInvalidParam("invalid status %q (enabled|disabled)", *req.Status))
		return
	}
	if req.Name == nil && req.Description == nil && req.Status == nil {
		fail(c, model.ErrInvalidParam("nothing to update"))
		return
	}

	row, err := h.store.UpdateRole(ctx, roleID, req.Name, req.Description, req.Status)
	if errors.Is(err, repo.ErrRoleNotFound) {
		fail(c, model.ErrNotFound("role not found: %s", roleID))
		return
	}
	if err != nil {
		fail(c, model.ErrInternal("update role failed"))
		return
	}
	if row == nil {
		fail(c, model.ErrNotFound("role not found: %s", roleID))
		return
	}
	h.audit(c, repo.AuditInput{
		Action:      auditActionPermissions,
		TargetType:  roleTargetType,
		TargetID:    roleID,
		Description: fmt.Sprintf("编辑角色 %s（改名=%v 改描述=%v 启停=%v）", roleID, req.Name != nil, req.Description != nil, derefStr(req.Status)),
	})
	ok(c, toRoleDTO(*row))
}

// deleteAdminRole DELETE /api/v1/admin/roles/:roleId
func (h *Handler) deleteAdminRole(c *gin.Context) {
	roleID := c.Param("roleId")
	if _, preset := presetRoles[roleID]; preset {
		fail(c, model.ErrForbidden("preset role cannot be deleted: %s", roleID))
		return
	}
	ctx := c.Request.Context()
	row, err := h.store.GetRole(ctx, roleID)
	if err != nil {
		fail(c, model.ErrInternal("get role failed"))
		return
	}
	if row == nil {
		fail(c, model.ErrNotFound("role not found: %s", roleID))
		return
	}
	if err := h.store.DeleteRole(ctx, roleID); err != nil {
		var inUse *repo.ErrRoleInUse
		switch {
		case errors.As(err, &inUse):
			fail(c, model.ErrConflict("role %s is used by %d admin account(s); reassign them first", roleID, inUse.MemberCount))
		case errors.Is(err, repo.ErrRoleNotFound):
			fail(c, model.ErrNotFound("role not found: %s", roleID))
		default:
			fail(c, model.ErrInternal("delete role failed"))
		}
		return
	}
	h.audit(c, repo.AuditInput{
		Action:      auditActionPermissions,
		TargetType:  roleTargetType,
		TargetID:    roleID,
		Description: fmt.Sprintf("删除角色「%s」（%s）", row.Name, roleID),
	})
	c.JSON(http.StatusOK, jsonResp{Code: model.CodeOK, Message: "success", Data: nil})
}
