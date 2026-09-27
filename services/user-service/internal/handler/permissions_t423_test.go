// T423（来源：T413 工程线独立复验报告第六节 E3，Joe 实测；PM 2026-09-27 14:2x 裁定另立卡，
// 见 T413 卡内 cid 1133005753001001882）实现侧测试。
//
// 缺陷：库里 permissions_json 已是**显式清单**时，PUT 请求体省略 items 键，落库变 null。
// null = 未细化 = 读侧按目录物化成「组内全勾」，等于把一份收窄过的授权静默升格，方向是放权。
// 根因形状：RolePermissionsDTO.Items 是 []string，「省略键」与「显式 null」解出来都是 nil，
// handler 拿不到这一格差别，reconcileItems 第一格把两者一起当未细化写回。
//
// 收口后三态：省略键 = 子权限维度不动（保库内原值）/ 显式 null = 落未细化 / 给清单 = 显式细化。
// 判红方向：把 handler.updatePermissions 里 itemsOmitted 那一支（改回一律走 reconcileItems）
// 或把 itemsKeptFromStored 改成 return nil，本文件第 1/2/3/4 条必红——
// 修前头 4460b78 上这五条里第 1/2/3/4 条即此形状（第 5 条是 E4 顺手项的读数对平，不判红）。
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const t423Role = "R_T423"

// t423Stored 库里的一条现状。narrowed 是「显式收窄过」的清单：alerts 勾了 4 条里的 1 条。
const t423NarrowedJSON = `{"scope":"team","modules":["alerts","comm"],"items":["alerts.view","comm.reply"]}`

func t423Env(t *testing.T, storedJSON string) *testEnv {
	t.Helper()
	e := newEnv(t, true, true)
	e.store.role = &repo.RoleRow{RoleID: t423Role, PermissionsJSON: storedJSON}
	e.store.updRoleOK = true
	return e
}

// TestT423_Put_OmittedItemsKeepsNarrowedList 判据 1（卡面要求一，修前必红的那一根）：
// 库里是显式收窄清单 + 请求省略 items 键 ⇒ 落库必须还是那份清单，不许变 null。
func TestT423_Put_OmittedItemsKeepsNarrowedList(t *testing.T) {
	e := t423Env(t, t423NarrowedJSON)

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/"+t423Role+"/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts", "comm"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	stored := decodeStored(t, e)
	assert.NotNil(t, stored.Items, `省略 items 键不许把显式清单洗成 "items":null（未细化 = 组内全勾，放权方向）`)
	assert.Equal(t, []string{"alerts.view", "comm.reply"}, stored.Items, "库内原值原样保住")
	assert.NotContains(t, e.store.lastPermJSON, `"items":null`)

	// 落库那段再读回来：也不能是物化后的全勾
	e.store.role = &repo.RoleRow{RoleID: t423Role, PermissionsJSON: e.store.lastPermJSON}
	w, resp = e.do(http.MethodGet, "/api/v1/admin/roles/"+t423Role+"/permissions", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	items := decodePerms(t, resp.Data).Items
	assert.Equal(t, []string{"alerts.view", "comm.reply"}, items)
	assert.NotContains(t, items, "alerts.process", "收窄过的显式清单读回来不许自动补勾")
}

// TestT423_Put_OmittedItemsOnUnrefinedKeepsUnrefined 判据 2（省略腿的第三格）：
// 库里本就未细化 ⇒ 省略 items 仍写 null（未细化不被洗成清单），这条与判据 1 合起来才说明
// 「省略 = 保持原值」而不是「省略 = 一律写清单」或「一律写 null」。
func TestT423_Put_OmittedItemsOnUnrefinedKeepsUnrefined(t *testing.T) {
	e := t423Env(t, `{"scope":"team","modules":["alerts","comm"]}`)

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/"+t423Role+"/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts", "comm"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	stored := decodeStored(t, e)
	assert.Nil(t, stored.Items, `库里未细化 + 省略 items ⇒ 仍是 "items":null`)

	e.store.role = &repo.RoleRow{RoleID: t423Role, PermissionsJSON: e.store.lastPermJSON}
	w, resp = e.do(http.MethodGet, "/api/v1/admin/roles/"+t423Role+"/permissions", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, materializeItems([]string{"alerts", "comm"}), decodePerms(t, resp.Data).Items,
		"读侧仍按目录物化（未细化的语义没变）")
}

// TestT423_Put_OmittedItemsOnEmptyListKeepsEmpty 判据 3（[] ≢ null 不许被省略腿抹平）：
// 库里显式全不勾 + 请求省略 items ⇒ 落库还是 []。
func TestT423_Put_OmittedItemsOnEmptyListKeepsEmpty(t *testing.T) {
	e := t423Env(t, `{"scope":"team","modules":["alerts"],"items":[]}`)

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/"+t423Role+"/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	stored := decodeStored(t, e)
	assert.NotNil(t, stored.Items, `[] 是显式全不勾，省略 items 键不许把它升格成未细化`)
	assert.Empty(t, stored.Items)
	assert.Contains(t, e.store.lastPermJSON, `"items":[]`)
}

// TestT423_Put_ExplicitNullStillMeansUnrefined 判据 4（卡面要求二，显式 null 腿）：
// 客户端明写 "items": null ⇒ 落未细化。这是唯一一条能把显式清单写成 null 的请求形状，
// 与判据 1 对照才成立：省略 ≠ 显式 null，两态在请求侧已经分开。
func TestT423_Put_ExplicitNullStillMeansUnrefined(t *testing.T) {
	e := t423Env(t, t423NarrowedJSON)

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/"+t423Role+"/permissions",
		json.RawMessage(`{"scope":"team","modules":["alerts","comm"],"items":null}`), nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	stored := decodeStored(t, e)
	assert.Nil(t, stored.Items, `显式 "items":null 必须落未细化（客户端明确声明的行为）`)
	assert.Contains(t, e.store.lastPermJSON, `"items":null`)
}

// TestT423_Put_FullListLegStillValidates 判据 5（卡面要求二，全量清单腿）：
// 给了 items 数组 ⇒ 走 T413 的细化路径（校验 + 新模块补齐），这条不许被省略腿的收口带偏。
// 库里现状同为收窄清单，与判据 1 的差别只在请求体带不带 items 键。
func TestT423_Put_FullListLegStillValidates(t *testing.T) {
	e := t423Env(t, t423NarrowedJSON)

	// 客户端只报 alerts 一条 + 新勾 config ⇒ 新模块 config 的两条按目录并入，
	// 既有模块 alerts 被显式漏掉的键不复活（T413 语义没回潮）
	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/"+t423Role+"/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts", "comm", "config"},
			"items": []string{"alerts.view"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, []string{"alerts.view", "config.basic", "config.audit_logs"},
		decodeStored(t, e).Items)

	// 清单里的键所属模块没勾上 ⇒ 400 且不落库（校验没因为「反正会保住原值」被绕开）
	e2 := t423Env(t, t423NarrowedJSON)
	w, resp = e2.do(http.MethodPut, "/api/v1/admin/roles/"+t423Role+"/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts"},
			"items": []string{"comm.reply"}}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Contains(t, resp.Message, "requires module")
	assert.Empty(t, e2.store.lastPermJSON, "拒绝时不得触达写通道")
}

// TestT423_Put_OmittedItemsDropKeysOfRemovedModules 省略腿关掉模块：库内原值里属于被关模块的键
// 跟着收口，留下的仍是这份清单而不是 null。显式提交同一份清单时 validatePermissionItems
// 会以「模块未勾」拒掉，省略键不该绕过同一条校验留下一条渲染不出来的死配置。
func TestT423_Put_OmittedItemsDropKeysOfRemovedModules(t *testing.T) {
	e := t423Env(t, t423NarrowedJSON)

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/"+t423Role+"/permissions",
		map[string]any{"scope": "team", "modules": []string{"alerts"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	stored := decodeStored(t, e)
	assert.NotNil(t, stored.Items, "关掉一个模块不许把清单洗成未细化")
	assert.Equal(t, []string{"alerts.view"}, stored.Items)
	assert.NotContains(t, stored.Items, "comm.reply", "被关模块的子权限随 modules 收口")
}

// TestT423_Put_ItemsWrongTypeRejected items 键给了非数组（形状错）⇒ 400。
// 收进 json.RawMessage 后这条判定不能丢：显式给错要拒，不能悄悄当省略处理。
func TestT423_Put_ItemsWrongTypeRejected(t *testing.T) {
	e := t423Env(t, t423NarrowedJSON)

	w, resp := e.do(http.MethodPut, "/api/v1/admin/roles/"+t423Role+"/permissions",
		json.RawMessage(`{"scope":"team","modules":["alerts","comm"],"items":"alerts.view"}`), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Contains(t, resp.Message, "items")
	assert.Empty(t, e.store.lastPermJSON, "拒绝时不得触达写通道")
}

// TestT423_MyPermissions_SameRenderingAsRoleGet E4 顺手项（T413 报告第六节 E4）：
// getMyPermissions 不再自己内联物化，与 GET /admin/roles/{id}/permissions 共用 renderPermissions。
// 读数同形，所以本卡改前改后都绿——钉的是「两路口径不许将来分家」。
func TestT423_MyPermissions_SameRenderingAsRoleGet(t *testing.T) {
	cases := []struct {
		name       string
		storedJSON string
	}{
		{"未细化", `{"scope":"team","modules":["alerts","comm"]}`},
		{"显式收窄", t423NarrowedJSON},
		{"显式全不勾", `{"scope":"team","modules":["alerts"],"items":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := t423Env(t, tc.storedJSON)

			w, resp := e.do(http.MethodGet, "/api/v1/admin/roles/"+t423Role+"/permissions", nil, nil)
			require.Equal(t, http.StatusOK, w.Code)
			var roleGet model.RolePermissionsDTO
			require.NoError(t, json.Unmarshal(resp.Data, &roleGet))

			w, resp = e.do(http.MethodGet, t257MePermsPath, nil,
				map[string]string{"X-User-Id": "A0001", "X-Role": t423Role})
			require.Equal(t, http.StatusOK, w.Code)
			var mine model.MyPermissionsDTO
			require.NoError(t, json.Unmarshal(resp.Data, &mine))

			assert.Equal(t, roleGet.Items, mine.Items, "两路口径必须同源（都走 renderPermissions）")
			assert.Equal(t, roleGet.Modules, mine.Modules)
			assert.NotNil(t, mine.Items, "me/permissions 恒回数组，前端不用自己判 null")
		})
	}
}
