// T257 11.5 子权限粒度实现侧测试（设计稿 admin/权限控制.html:121-187）
//
// 覆盖：
//  1. 目录 = 设计稿 9 组 23 项，key 唯一且前缀与所属模块一致（防手抄错位）；
//  2. 老角色（permissions_json 无 items）读时按目录物化 ⇒ 升级后不会全部按钮消失；
//  3. 显式清单原样存取；`items: []` 与「缺省」语义不同（全不勾 vs 未细化）；
//  4. 未知 key / 重复 / 模块未勾 ⇒ 400 且不触达写通道；
//  5. GET /admin/me/permissions：按网关注入的 X-Role 解析，角色不在 roles 表 ⇒ 200 空清单，
//     缺 X-Role ⇒ 403（fail-closed）。
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

const (
	t257CatalogPath = "/api/v1/admin/permissions/catalog"
	t257MePermsPath = "/api/v1/admin/me/permissions"
)

func decodePerms(t *testing.T, raw json.RawMessage) model.RolePermissionsDTO {
	t.Helper()
	var dto model.RolePermissionsDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	return dto
}

func TestT257_PermissionCatalog_MatchesDesignTree(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodGet, t257CatalogPath, nil, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var cat model.PermissionCatalogDTO
	require.NoError(t, json.Unmarshal(resp.Data, &cat))
	require.Len(t, cat.Groups, 9, "设计稿 权限控制.html:121-187 共 9 个 .perm-group")

	// 逐组条数：照设计稿 .perm-item 数出来的（合计 23）
	wantPerModule := map[string]int{
		"dashboard": 2, "realtime": 2, "patients": 4, "teams": 3, "devices": 3,
		"alerts": 3, "comm": 2, "perm": 2, "config": 2,
	}
	labels := make(map[string]string, 23)
	seen := make(map[string]struct{}, 23)
	total := 0
	for _, g := range cat.Groups {
		assert.NotEmpty(t, g.Label, "分组 %s 缺中文标签", g.Module)
		assert.Len(t, g.Items, wantPerModule[g.Module], "分组 %s 条数与设计稿不符", g.Module)
		for _, it := range g.Items {
			total++
			assert.NotEmpty(t, it.Label, it.Key)
			assert.True(t, strings.HasPrefix(it.Key, g.Module+"."),
				"key %q 前缀须与所属模块 %q 一致（否则物化/校验会错位）", it.Key, g.Module)
			_, dup := seen[it.Key]
			assert.False(t, dup, "key 重复: %s", it.Key)
			seen[it.Key] = struct{}{}
			labels[it.Key] = it.Label
		}
	}
	assert.Equal(t, 23, total, "设计稿共 23 个勾选项")
	assert.Len(t, permissionItemModule, 23, "索引与目录同源同量")

	// 抽查卡片点名的两项（:159「回复患者」/ :163「角色管理」）+ 本卡新增的处理告警
	assert.Equal(t, "回复患者", labels["comm.reply"])
	assert.Equal(t, "角色管理", labels["perm.roles"])
	assert.Equal(t, "处理告警", labels["alerts.process"])
	assert.Equal(t, "查看操作日志", labels["config.audit_logs"])
}

func TestT257_GetPermissions_MaterializesItemsForLegacyRoles(t *testing.T) {
	e := newEnv(t, true, true)
	// seed 预置角色的形状：只有 scope + modules，没有 items
	e.store.role = &repo.RoleRow{RoleID: "ROLE_DOCTOR",
		PermissionsJSON: `{"scope":"team","modules":["alerts","comm","orthosis"]}`}

	w, resp := e.do(http.MethodGet, "/api/v1/admin/roles/ROLE_DOCTOR/permissions", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	perms := decodePerms(t, resp.Data)

	assert.Equal(t, []string{"alerts.view", "alerts.process", "alerts.config_rules",
		"comm.view_feedback", "comm.reply"}, perms.Items,
		"未细化 = 组内全勾（目录顺序），老角色升级后不该突然什么都看不见")
	assert.Contains(t, perms.Modules, "orthosis", "目录里没有的模块保持只有页面级，不被吞掉")
}

func TestT257_UpdatePermissions_StoresExplicitItems(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.updRoleOK = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_DOCTOR/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts"}, "items": []string{"alerts.view"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Contains(t, e.store.lastPermJSON, `"items":["alerts.view"]`)
	assert.Equal(t, []string{"alerts.view"}, decodePerms(t, resp.Data).Items, "显式清单原样回显，不被物化")

	// 未给 items（老前端）⇒ 写 null，读时仍按未细化物化
	w, resp = e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_DOCTOR/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Contains(t, e.store.lastPermJSON, `"items":null`)
}

func TestT257_UpdatePermissions_EmptyItemsDiffersFromOmitted(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.updRoleOK = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/R/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts"}, "items": []string{}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Contains(t, e.store.lastPermJSON, `"items":[]`, "空数组 = 显式全不勾，不能被当成未细化")

	// 把刚写的 JSON 当作库里的行读回来：仍是空清单，不物化
	e.store.role = &repo.RoleRow{RoleID: "R", PermissionsJSON: e.store.lastPermJSON}
	w, resp = e.do(http.MethodGet, "/api/v1/admin/roles/R/permissions", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	perms := decodePerms(t, resp.Data)
	assert.NotNil(t, perms.Items)
	assert.Empty(t, perms.Items)
}

func TestT257_UpdatePermissions_RejectsBadItems(t *testing.T) {
	cases := []struct {
		name  string
		items []string
		msg   string
	}{
		{"目录外的 key", []string{"alerts.delete_all"}, "unknown permission item"},
		{"近似拼写不放行", []string{"alert.view"}, "unknown permission item"},
		{"重复提交", []string{"alerts.view", "alerts.view"}, "duplicate"},
		{"模块未勾", []string{"comm.reply"}, "requires module"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			e.store.updRoleOK = true

			w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/R/permissions",
				map[string]any{"scope": "team", "modules": []string{"alerts"}, "items": tc.items}, nil)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			assert.Contains(t, resp.Message, tc.msg)
			assert.Empty(t, e.store.lastPermJSON, "拒绝时不得触达写通道")
		})
	}
}

func TestT257_GetMyPermissions(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.role = &repo.RoleRow{RoleID: "ROLE_CS",
		PermissionsJSON: `{"scope":"all_patients","modules":["comm"],"items":["comm.reply"]}`}

	w, resp := e.do(http.MethodGet, t257MePermsPath, nil,
		map[string]string{"X-User-Id": "A0003", "X-Role": "ROLE_CS"})
	require.Equal(t, http.StatusOK, w.Code)
	var mine model.MyPermissionsDTO
	require.NoError(t, json.Unmarshal(resp.Data, &mine))
	assert.Equal(t, "A0003", mine.AdminID)
	assert.Equal(t, "ROLE_CS", mine.RoleID)
	assert.Equal(t, "all_patients", mine.Scope)
	assert.Equal(t, []string{"comm"}, mine.Modules)
	assert.Equal(t, []string{"comm.reply"}, mine.Items, "已细化的角色直接回显存下来的清单")

	// 角色不在 roles 表（technician / patient / 已删角色）⇒ 200 + 空清单：
	// 本端点只驱动前端渲染，空清单等于什么都不显示，不构成放权
	e.store.role = nil
	w, resp = e.do(http.MethodGet, t257MePermsPath, nil,
		map[string]string{"X-User-Id": "T0001", "X-Role": "technician"})
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(resp.Data, &mine))
	assert.Equal(t, "technician", mine.RoleID)
	assert.Empty(t, mine.Modules)
	assert.Empty(t, mine.Items)

	// 缺 X-Role ⇒ 403（fail-closed，口径同 requireAdminRole）
	w, resp = e.do(http.MethodGet, t257MePermsPath, nil, map[string]string{"X-User-Id": "A0001"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, model.CodeForbidden, resp.Code)

	// 脏 JSON / DB 错 ⇒ 500
	e.store.role = &repo.RoleRow{RoleID: "R", PermissionsJSON: `{bad`}
	w, _ = e.do(http.MethodGet, t257MePermsPath, nil, map[string]string{"X-Role": "R"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	e.store.roleErr = errors.New("db")
	w, _ = e.do(http.MethodGet, t257MePermsPath, nil, map[string]string{"X-Role": "R"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestT257_CatalogModulesAreGrantable 目录里的每个模块都必须在 ROLE_ADMIN 的预置 modules 里
// （seed.sql:8-9）——否则权限页会渲染出一个连超管都没被授权的勾选项，
// 保存时又被 validatePermissionItems 以「模块未勾」拒掉，页面自己锁死自己。
func TestT257_CatalogModulesAreGrantable(t *testing.T) {
	seedAdminModules := []string{"dashboard", "realtime", "patients", "teams", "devices",
		"alerts", "comm", "orthosis", "install", "tech", "perm", "config"}

	items := materializeItems(seedAdminModules)
	total := 0
	for _, g := range permissionCatalog {
		total += len(g.Items)
	}
	assert.Len(t, items, total, "超管应能勾上目录里的全部 %d 项", total)

	// 反向：目录没覆盖的 seed 模块（orthosis/install/tech）不产出任何 item，保持只有页面级
	for _, key := range items {
		module := permissionItemModule[key]
		assert.Contains(t, seedAdminModules, module, "目录模块 %s 不在预置词表里", module)
		assert.NotContains(t, []string{"orthosis", "install", "tech"}, module)
	}
}
