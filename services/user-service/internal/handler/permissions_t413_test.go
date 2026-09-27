// T413 角色编辑 PUT 洗 items 缺陷（来源：Ella T345 第 2 轮验收 D-B）实现侧测试。
//
// 缺陷两格：
//  1. 加权方向静默丢授权——权限矩阵页只有模块级复选框（apps/admin-web/src/pages/roles/index.vue），
//     新勾一个模块时 PUT 的 items 仍是打开页面时 GET 回来的旧快照，不含该模块的子权限键；
//  2. null（未细化）被洗成显式清单——GET 把 nil 物化后返回，客户端看不出「原本未细化」，
//     于是任何一次保存都把三态里的第一态永久改掉。
//
// 两条都在 handler.updatePermissions 落库前收口（reconcileItems）。
// 把 reconcileItems 的补齐与保 null 两格删掉，本文件第 1/2/4/5 条必红（修前 main 67d1186 即此形状）。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// decodeStored 把 PUT 真正写进库的那段 JSON 解回来。
// 三态要分得开：库里 "items":null ⇒ nil 切片，"items":[] ⇒ 非 nil 空切片，
// 所以判「有没有被洗」一律用 assert.Nil / assert.NotNil，不用 Empty。
func decodeStored(t *testing.T, e *testEnv) model.RolePermissionsDTO {
	t.Helper()
	require.NotEmpty(t, e.store.lastPermJSON, "PUT 未触达写通道")
	return decodePerms(t, json.RawMessage(e.store.lastPermJSON))
}

// TestT413_Put_FillsItemsForNewlyGrantedModule 判据 1（加权方向）：
// 库里已是显式清单的角色，新勾一个模块 ⇒ 该模块在目录下的全部子权限并入落库。
func TestT413_Put_FillsItemsForNewlyGrantedModule(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.role = &repo.RoleRow{RoleID: "R_CUSTOM",
		PermissionsJSON: `{"scope":"team","modules":["patients"],"items":["patients.view","patients.edit"]}`}
	e.store.updRoleOK = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/R_CUSTOM/permissions",
		map[string]any{"scope": "team", "modules": []string{"patients", "teams"},
			"items": []string{"patients.view", "patients.edit"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	assert.Equal(t, []string{"patients.view", "patients.edit",
		"teams.view", "teams.edit", "teams.manage_members"}, decodeStored(t, e).Items,
		"新模块 teams 的 3 条子权限必须补上（顺序 = 客户端清单 + 目录顺序）")
	assert.Contains(t, resp.Message+string(resp.Data), "teams.manage_members",
		"响应回显落库后的有效清单，前端不必重新拉一次")
}

// TestT413_Put_PresetRoleKeepsUnrefined 判据 2（预置角色腿，卡面自陈未测）：
// seed 预置三个角色的形状是 permissions_json 里没有 items ⇒ 加权一次保存后仍必须是 null，
// 且新模块的子权限靠「未细化 = 组内全勾」自动生效。
func TestT413_Put_PresetRoleKeepsUnrefined(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.role = &repo.RoleRow{RoleID: "ROLE_DOCTOR",
		PermissionsJSON: `{"scope":"team","modules":["alerts","comm","orthosis"]}`}
	e.store.updRoleOK = true

	// 页面按 GET 回来的物化清单原样发回，外加新勾的 patients
	snapshot := materializeItems([]string{"alerts", "comm", "orthosis"})
	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_DOCTOR/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts", "comm", "orthosis", "patients"},
			"items": snapshot}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	stored := decodeStored(t, e)
	assert.Nil(t, stored.Items, `未细化的预置角色保存后必须仍是 "items":null，不被洗成显式清单`)

	// 落库那段再读回来：新模块的子权限照样按目录物化出来（加权没丢）
	e.store.role = &repo.RoleRow{RoleID: "ROLE_DOCTOR", PermissionsJSON: e.store.lastPermJSON}
	w, resp = e.do(http.MethodGet, "/api/v1/admin/roles/ROLE_DOCTOR/permissions", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.ElementsMatch(t, materializeItems([]string{"alerts", "comm", "orthosis", "patients"}),
		decodePerms(t, resp.Data).Items, "读回的有效清单要含新模块 patients 的全部子权限")
	assert.Contains(t, string(resp.Data), "patients.delete")
}

// TestT413_Put_KeepsDeliberateUnchecks 判据 3（不复活）：
// 库里显式清单里被故意漏掉的键，加权时不许按「模块还勾着」补回来——
// 补齐只认「相对库里新增了哪些模块」。
func TestT413_Put_KeepsDeliberateUnchecks(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.role = &repo.RoleRow{RoleID: "R_PARTIAL",
		PermissionsJSON: `{"scope":"team","modules":["alerts","comm"],"items":["alerts.view","comm.reply"]}`}
	e.store.updRoleOK = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/R_PARTIAL/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts", "comm", "config"},
			"items": []string{"alerts.view", "comm.reply"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	items := decodeStored(t, e).Items
	assert.Equal(t, []string{"alerts.view", "comm.reply", "config.basic", "config.audit_logs"}, items,
		"只补新模块 config 的两条")
	assert.NotContains(t, items, "alerts.process", "既有模块 alerts 被显式漏掉的键不许复活")
	assert.NotContains(t, items, "comm.view_feedback", "同上：comm 只留 comm.reply")
}

// TestT413_Put_ExplicitFullListStaysExplicit 判据 4（三态不反转）：
// 库里已是「显式全勾」的角色，保存后仍存显式清单，不许被改写成 null
// （显式全勾 ≢ 未细化，差别在目录日后扩项时会不会自动放权）。
func TestT413_Put_ExplicitFullListStaysExplicit(t *testing.T) {
	e := newEnv(t, true, true)
	full := materializeItems([]string{"alerts"})
	e.store.role = &repo.RoleRow{RoleID: "R_EXPLICIT_FULL",
		PermissionsJSON: `{"scope":"team","modules":["alerts"],"items":["alerts.view","alerts.process","alerts.config_rules"]}`}
	e.store.updRoleOK = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/R_EXPLICIT_FULL/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts"}, "items": full}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	stored := decodeStored(t, e)
	assert.NotNil(t, stored.Items, `显式全勾不许反转成 "items":null`)
	assert.Equal(t, full, stored.Items)
	assert.NotContains(t, e.store.lastPermJSON, `"items":null`)
}

// TestT413_Put_EmptyItemsStaysEmpty 判据 5（[] ≢ null 的另一腿）：
// 库里显式全不勾（items: []）的角色，加权后补的是新模块的键，
// 既不许写回 null，也不许把老模块的键补回来。
func TestT413_Put_EmptyItemsStaysEmpty(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.role = &repo.RoleRow{RoleID: "R_NONE",
		PermissionsJSON: `{"scope":"team","modules":["alerts"],"items":[]}`}
	e.store.updRoleOK = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/R_NONE/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts", "comm"}, "items": []string{}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	stored := decodeStored(t, e)
	assert.NotNil(t, stored.Items, `[] 是显式全不勾，不许被当成未细化写成 null`)
	assert.Equal(t, []string{"comm.view_feedback", "comm.reply"}, stored.Items)
	assert.NotContains(t, stored.Items, "alerts.view")
}

// TestT413_Put_NarrowingOnUnrefinedKeepsUnrefined 减权方向（T372 已修的那格不许回潮）：
// 未细化角色关掉一个模块 ⇒ 前端剪过清单，落库仍是 null，且读回来的清单不含被关模块。
func TestT413_Put_NarrowingOnUnrefinedKeepsUnrefined(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.role = &repo.RoleRow{RoleID: "ROLE_ADMIN",
		PermissionsJSON: `{"scope":"all","modules":["alerts","comm"]}`}
	e.store.updRoleOK = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/ROLE_ADMIN/permissions",
		map[string]any{"scope": "all", "modules": []string{"alerts"},
			"items": []string{"alerts.view", "alerts.process", "alerts.config_rules"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Nil(t, decodeStored(t, e).Items, "关掉一个模块不该顺带把未细化洗成清单")

	e.store.role = &repo.RoleRow{RoleID: "ROLE_ADMIN", PermissionsJSON: e.store.lastPermJSON}
	w, resp = e.do(http.MethodGet, "/api/v1/admin/roles/ROLE_ADMIN/permissions", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	items := decodePerms(t, resp.Data).Items
	assert.Equal(t, []string{"alerts.view", "alerts.process", "alerts.config_rules"}, items)
	assert.NotContains(t, items, "comm.reply", "被关模块的子权限随 modules 自动收口")
}

// TestT413_Put_StoreReadFailure_WritesNothing 合并要读库里现状：读失败 / 库里 JSON 脏
// ⇒ 500 且不触写通道（宁可拒一次保存，也不许猜一个现状把授权算错）。
func TestT413_Put_StoreReadFailure_WritesNothing(t *testing.T) {
	cases := []struct {
		name string
		role *repo.RoleRow
		err  error
	}{
		{"读库失败", &repo.RoleRow{RoleID: "R", PermissionsJSON: `{"scope":"team","modules":["alerts"]}`}, errors.New("db")},
		{"库里 permissions_json 是脏的", &repo.RoleRow{RoleID: "R", PermissionsJSON: `{bad`}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			e.store.role, e.store.roleErr = tc.role, tc.err
			e.store.updRoleOK = true

			w, _ := e.do(http.MethodPut, "/api/v1/admin/roles/R/permissions",
				map[string]any{"scope": "team", "modules": []string{"alerts", "comm"},
					"items": []string{"alerts.view"}}, nil)
			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Empty(t, e.store.lastPermJSON, "算不出现状时不得落库")
		})
	}
}
