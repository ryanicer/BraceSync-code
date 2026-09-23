// T252 11.2 角色增删改端点实现侧测试（admin 权限控制页）
//
// 验收对应（T252 ③ 角色自定义维护）：
//  1. 模板下拉 3 项（T277：与 000017 收敛后的 3 个登录角色一一对应）；
//  2. 新建角色：模板 or 显式 permissions 二选一，重名 409，形状非法 400；
//  3. 编辑：预置角色 name 锁定 400，但描述/启停仍可改；非预置可改名（改名撞名 409）；
//  4. 删除：预置 403；被管理员账号引用 409 + memberCount；不存在 404；
//  5. 三类写操作各留一条 permission_change 审计。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

func t252RoleRow(id, name string) *repo.RoleRow {
	return &repo.RoleRow{RoleID: id, Name: name, Status: "enabled", PermissionsJSON: `{"scope":"team","modules":["patients"]}`}
}

func decodeRoles(t *testing.T, raw json.RawMessage) model.AdminRoleDTO {
	t.Helper()
	var dto model.AdminRoleDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	return dto
}

// ── 模板下拉 ──

func TestT252_ListRoleTemplates(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodGet, "/api/v1/admin/role-templates", nil, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code)

	var list []model.RoleTemplateDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	// T277：模板收口为 3 条 = 设计稿 权限控制.html:203 下拉的 运营管理员 / 医护 / 客服
	//（主任医师 / 主治医师 / 康复师 / 护士 = 职称，不是角色，不得再作为模板出现）
	// T343：第三条显示名随 Boss 2026-09-22 14:21 裁定「医生 → 医护」，**key 仍是 doctor**
	require.Len(t, list, 3)
	assert.Equal(t, []string{"admin", "doctor", "cs"},
		[]string{list[0].Key, list[1].Key, list[2].Key})
	assert.Equal(t, []string{"运营管理员", "医护", "客服"},
		[]string{list[0].Name, list[1].Name, list[2].Name})
	assert.Equal(t, "all", list[0].Permissions.Scope)
	// T345：admin 模板 = 15 页全集（补 review / review_tpl / doctor_acct 三键，与 seed+000026 后的
	// ROLE_ADMIN 同集）。逐键断言而非只断条数：词表一旦被改名或漏项，这里必须变红。
	assert.Equal(t, []string{
		"dashboard", "realtime", "patients", "teams", "devices", "alerts",
		"comm", "orthosis", "install", "review", "review_tpl", "tech",
		"doctor_acct", "perm", "config",
	}, list[0].Permissions.Modules, "运营管理员模板 = 15 个模块全开")
	assert.Equal(t, "team", list[1].Permissions.Scope)
	assert.Equal(t, "all_patients", list[2].Permissions.Scope, "客服：全量患者、仅沟通模块")
	assert.Equal(t, []string{"comm"}, list[2].Permissions.Modules)
}

// ── 新建 ──

func TestT252_CreateAdminRole_FromTemplate(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.createdRole = t252RoleRow("ROLE_C0A1B2C3D4", "值班医师")
	tpl := "doctor"

	w, resp := e.do(http.MethodPost, "/api/v1/admin/roles",
		map[string]any{"name": "  值班医师  ", "description": "工作日白班", "template": tpl}, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	assert.Equal(t, "值班医师", e.store.lastRoleName, "角色名去首尾空格")
	assert.Equal(t, "工作日白班", e.store.lastRoleDesc)
	var perms model.RolePermissionsDTO
	require.NoError(t, json.Unmarshal([]byte(e.store.lastRolePerms), &perms))
	want, found := findRoleTemplate(tpl)
	require.True(t, found)
	assert.Equal(t, want.Permissions, perms, "模板权限整体落库")

	dto := decodeRoles(t, resp.Data)
	assert.Equal(t, "ROLE_C0A1B2C3D4", dto.RoleID)
	assert.False(t, dto.Preset, "运营新建角色不是预置角色")

	require.Len(t, e.store.auditRows, 1)
	assert.Equal(t, "permission_change", e.store.auditRows[0].Action)
	assert.Equal(t, "role", e.store.auditRows[0].TargetType)
	assert.Equal(t, "ROLE_C0A1B2C3D4", e.store.auditRows[0].TargetID)
	assert.Contains(t, e.store.auditRows[0].Description, "模板 doctor")
	assert.NotNil(t, e.store.auditRows[0].Detail["permissions"], "权限快照留痕，便于回滚")
}

func TestT252_CreateAdminRole_ExplicitPermissionsOverrideTemplate(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.createdRole = t252RoleRow("ROLE_C1", "自定义")
	tpl := "cs"

	w, resp := e.do(http.MethodPost, "/api/v1/admin/roles", map[string]any{
		"name":        "自定义",
		"template":    tpl,
		"permissions": map[string]any{"scope": "all", "modules": []string{"patients", "devices"}},
	}, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	var perms model.RolePermissionsDTO
	require.NoError(t, json.Unmarshal([]byte(e.store.lastRolePerms), &perms))
	assert.Equal(t, "all", perms.Scope, "显式 permissions 优先于模板")
	assert.Equal(t, []string{"patients", "devices"}, perms.Modules)
}

func TestT252_CreateAdminRole_RejectsBadRequests(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.createdRole = t252RoleRow("ROLE_C1", "不该被创建")

	cases := []struct {
		name string
		body map[string]any
		msg  string
	}{
		{"缺名称", map[string]any{"template": "cs"}, "invalid request body"},
		{"名称全空格", map[string]any{"name": "   ", "template": "cs"}, "must be 1-64 chars"},
		{"名称超长", map[string]any{"name": strings.Repeat("角", 65), "template": "cs"}, "must be 1-64 chars"},
		{"描述超长", map[string]any{"name": "角色", "description": strings.Repeat("说", 256), "template": "cs"}, "description must be at most 255"},
		{"未知模板", map[string]any{"name": "角色", "template": "superman"}, "unknown template"},
		{"模板与权限都缺", map[string]any{"name": "角色"}, "template or permissions is required"},
		{"scope 非法", map[string]any{"name": "角色", "permissions": map[string]any{"scope": "org", "modules": []string{"patients"}}}, "invalid scope"},
		{"modules 为空", map[string]any{"name": "角色", "permissions": map[string]any{"scope": "team", "modules": []string{}}}, "modules must not be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.store.auditRows = nil
			e.store.lastRoleDesc = ""
			w, resp := e.do(http.MethodPost, "/api/v1/admin/roles", tc.body, t252AdminHdr())
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			assert.Contains(t, resp.Message, tc.msg)
			assert.Empty(t, e.store.lastRoleDesc, "拒绝时不得触达 CreateRole")
			assert.Empty(t, e.store.auditRows)
		})
	}
}

func TestT252_CreateAdminRole_DuplicateNameConflicts(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.roleNameTaken = true

	w, resp := e.do(http.MethodPost, "/api/v1/admin/roles",
		map[string]any{"name": "主任医师", "template": "cs"}, t252AdminHdr())
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, model.CodeConflict, resp.Code)
	assert.Contains(t, resp.Message, "role name already exists")
	assert.Equal(t, "主任医师", e.store.lastRoleName, "查重按去空格后的名字")
	assert.Empty(t, e.store.lastRolePerms, "重名时不得落库")
}

func TestT252_CreateAdminRole_StoreFailures(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.roleNameErr = errors.New("db")
	w, resp := e.do(http.MethodPost, "/api/v1/admin/roles",
		map[string]any{"name": "角色A", "template": "cs"}, t252AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, resp.Message, "check role name")

	e2 := newEnv(t, true, true)
	e2.store.createRoleErr = errors.New("db")
	w2, _ := e2.do(http.MethodPost, "/api/v1/admin/roles",
		map[string]any{"name": "角色B", "template": "cs"}, t252AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w2.Code)
	assert.Empty(t, e2.store.auditRows, "创建失败不写权限变更审计")
}

// ── 编辑 ──

func TestT252_UpdateAdminRole_PresetNameLockedOthersEditable(t *testing.T) {
	e := newEnv(t, true, true)
	// T262：预置角色只剩 3 个（ROLE_ADMIN/ROLE_DOCTOR/ROLE_CS），改用其中之一做锁定语义用例
	e.store.updatedRole = t252RoleRow("ROLE_ADMIN", "运营管理员")

	// 预置角色改名 → 400，且不改设计稿既有语义
	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_ADMIN",
		map[string]any{"name": "万能管理员"}, t252AdminHdr())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp.Message, "preset role name is locked")
	assert.Nil(t, e.store.roleUpdDesc)

	// 预置角色改描述/启停 → 放行
	w, resp = e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_ADMIN",
		map[string]any{"description": "IT 部门专用", "status": "disabled"}, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	require.NotNil(t, e.store.roleUpdDesc)
	assert.Equal(t, "IT 部门专用", *e.store.roleUpdDesc)
	require.NotNil(t, e.store.roleUpdStatus)
	assert.Equal(t, "disabled", *e.store.roleUpdStatus)
	assert.Nil(t, e.store.roleUpdName)
	require.Len(t, e.store.auditRows, 1)
	assert.Equal(t, "permission_change", e.store.auditRows[0].Action)
	assert.Equal(t, "ROLE_ADMIN", e.store.auditRows[0].TargetID)
}

func TestT252_UpdateAdminRole_NonPresetRenameChecksConflict(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.updatedRole = t252RoleRow("ROLE_C1", "新名字")

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_C1",
		map[string]any{"name": "  新名字  "}, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, "ROLE_C1", e.store.lastRoleID)
	require.NotNil(t, e.store.roleUpdName)
	assert.Equal(t, "新名字", *e.store.roleUpdName)
	assert.False(t, decodeRoles(t, resp.Data).Preset)

	e2 := newEnv(t, true, true)
	e2.store.roleNameTaken = true
	w2, resp2 := e2.do(http.MethodPut, "/api/v1/admin/roles/ROLE_C2",
		map[string]any{"name": "值班医师"}, t252AdminHdr())
	assert.Equal(t, http.StatusConflict, w2.Code)
	assert.Contains(t, resp2.Message, "role name already exists")
	assert.Empty(t, e2.store.auditRows)
}

func TestT252_UpdateAdminRole_RejectsBadRequests(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		msg  string
	}{
		{"空提交", map[string]any{}, "nothing to update"},
		{"状态非法", map[string]any{"status": "deleted"}, "invalid status"},
		{"名称清空", map[string]any{"name": "   "}, "must be 1-64 chars"},
		{"描述超长", map[string]any{"description": strings.Repeat("x", 256)}, "at most 255"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_C9", tc.body, t252AdminHdr())
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, resp.Message, tc.msg)
			assert.Equal(t, "", e.store.lastRoleID, "拒绝时不触达 UpdateRole")
		})
	}
}

func TestT252_UpdateAdminRole_NotFoundAndFailure(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.updateRoleErr = repo.ErrRoleNotFound
	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_CX",
		map[string]any{"description": "改点东西"}, t252AdminHdr())
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	e2 := newEnv(t, true, true)
	e2.store.updateRoleErr = errors.New("db")
	w2, _ := e2.do(http.MethodPut, "/api/v1/admin/roles/ROLE_CY",
		map[string]any{"description": "改点东西"}, t252AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w2.Code)

	// repo 返回 (nil, nil) 也按 404 处理（不 panic）
	e3 := newEnv(t, true, true)
	w3, _ := e3.do(http.MethodPut, "/api/v1/admin/roles/ROLE_CZ",
		map[string]any{"description": "改点东西"}, t252AdminHdr())
	assert.Equal(t, http.StatusNotFound, w3.Code)
}

// ── 删除 ──

func TestT252_DeleteAdminRole(t *testing.T) {
	// 预置角色禁删（T262：预置集收敛为 3 个，用 ROLE_CS 做用例）
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodDelete, "/api/v1/admin/roles/ROLE_CS", nil, t252AdminHdr())
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Contains(t, resp.Message, "preset role cannot be deleted")
	assert.Equal(t, "", e.store.lastRoleID)

	// 不存在 → 404（先查行拿名字写审计）
	e2 := newEnv(t, true, true)
	w2, _ := e2.do(http.MethodDelete, "/api/v1/admin/roles/ROLE_GHOST", nil, t252AdminHdr())
	assert.Equal(t, http.StatusNotFound, w2.Code)
	assert.Empty(t, e2.store.auditRows)

	// 被账号引用 → 409 + 剩余账号数
	e3 := newEnv(t, true, true)
	e3.store.role = t252RoleRow("ROLE_C3", "值班医师")
	e3.store.deleteRoleErr = &repo.ErrRoleInUse{MemberCount: 3}
	w3, resp3 := e3.do(http.MethodDelete, "/api/v1/admin/roles/ROLE_C3", nil, t252AdminHdr())
	assert.Equal(t, http.StatusConflict, w3.Code)
	assert.Contains(t, resp3.Message, "used by 3 admin account")
	assert.Empty(t, e3.store.auditRows, "删除失败不落审计")

	// 成功 → 200 + 审计带角色名
	e4 := newEnv(t, true, true)
	e4.store.role = t252RoleRow("ROLE_C4", "临时角色")
	w4, resp4 := e4.do(http.MethodDelete, "/api/v1/admin/roles/ROLE_C4", nil, t252AdminHdr())
	require.Equal(t, http.StatusOK, w4.Code, resp4.Message)
	assert.Equal(t, "ROLE_C4", e4.store.lastRoleID)
	require.Len(t, e4.store.auditRows, 1)
	assert.Equal(t, "permission_change", e4.store.auditRows[0].Action)
	assert.Contains(t, e4.store.auditRows[0].Description, "临时角色")
}
