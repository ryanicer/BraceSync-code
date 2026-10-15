// T687 账号形态归一测试（max 批准改动面的验收读数）：
// 写侧 transfer 落库前归一、执法 StateAssignee 读侧兼容、getFlowNodeStates DTO 回读归一、
// process-guard / 待办判定读侧兼容、解析失败回退不放大失败面。
//
// 三例差分（卡面验收口径）：本人放行 + 非指派人 403 + admin 例外——存量 username 形落库
// （doctor_li）+ 执法侧 admin_id 形操作者（A0002）下逐一读数。
package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// t687Env 在 t668Env（N2 配「主治医生」+ current）基础上把 N2 的运行态 assignee
// 置为 username 形存量值 doctor_li，并挂 doctor_li→A0002 / D0001→A0002 的归一映射。
func t687Env(t *testing.T) *testEnv {
	e := t668Env(t)
	e.store.flow.nodes = []repo.FlowNodeStateRow{
		{NodeID: "N1", Status: model.FlowNodeDone},
		{NodeID: "N2", Status: model.FlowNodeCurrent, Assignee: flowStrPtr("doctor_li")},
		{NodeID: "N3", Status: model.FlowNodeTodo},
	}
	e.store.flow.accountIDs = map[string]string{
		"doctor_li": "A0002",
		"D0001":     "A0002",
		"cs_wang":   "A0003",
	}
	return e
}

// 写侧归一：transfer 的 targetOperator（username/doctor_id 形）落库前解析为 admin_id 形；
// 非 admins 域账号（技师 tech_id）原样透传。
func TestT687_TransferWrite_NormalizesTargetOperator(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"username形→admin_id", "doctor_li", "A0002"},
		{"doctor_id形→admin_id", "D0001", "A0002"},
		{"已是admin_id原样", "A0003", "A0003"},
		{"技师tech_id透传", "T0009", "T0009"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := t687Env(t)
			w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions",
				map[string]any{"action": "transfer", "targetOperator": tc.raw}, hdr(flowRoleAdmin, "A0001"))
			require.Equal(t, http.StatusOK, w.Code, resp.Message)
			assert.Equal(t, tc.want, e.store.flow.lastAction.TargetOperator,
				"T687 写侧必须落 canonical 形，杜绝新数据再造混形")
		})
	}
}

// 三例差分：存量 username 形 assignee（doctor_li）+ 操作者 admin_id 形（A0002）。
//   - 本人（A0002，即 doctor_li 归一后的账号）→ 放行（归一前 403，正是 T687 缺陷）；
//   - 非指派人（A0009）→ 403（收窄不得放过别人）；
//   - admin → 例外放行（T668 口径不变）。
func TestT687_AssigneeLegacyForm_ThreeWayDiff(t *testing.T) {
	body := map[string]any{"action": "confirm"}

	t.Run("本人username形落库admin_id形操作者放行", func(t *testing.T) {
		e := t687Env(t)
		w, _ := e.do(http.MethodPost, flowActionPath+"N2/actions", body, hdr("ROLE_DOCTOR", "A0002"))
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		assert.Equal(t, "A0002", e.store.flow.lastAction.Operator)
	})

	t.Run("非指派人仍403", func(t *testing.T) {
		e := t687Env(t)
		w, _ := e.do(http.MethodPost, flowActionPath+"N2/actions", body, hdr("ROLE_DOCTOR", "A0009"))
		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		t464TechLogContains(t, w, "指派给指定处理人")
		assert.Empty(t, e.store.flow.lastAction.NodeID, "403 不得落到 ApplyFlowNodeAction")
	})

	t.Run("admin例外放行", func(t *testing.T) {
		e := t687Env(t)
		w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions", body, hdr(flowRoleAdmin, "A0001"))
		require.Equal(t, http.StatusOK, w.Code, resp.Message)
	})
}

// DTO 回读归一：存量 username 形 assignee 经 getFlowNodeStates 回读为 admin_id 形，
// 前端 canOperateCurrent（auth.user.adminId === s.assignee）才咬得住。
func TestT687_NodeStatesDTO_ReadbackNormalized(t *testing.T) {
	e := t687Env(t)
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/nodes", nil, hdr("ROLE_DOCTOR", "A0002"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var out struct {
		List []model.FlowNodeStateDTO `json:"list"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &out))
	var n2 *model.FlowNodeStateDTO
	for i := range out.List {
		if out.List[i].NodeID == "N2" {
			n2 = &out.List[i]
		}
	}
	require.NotNil(t, n2, "N2 状态行应在场")
	require.NotNil(t, n2.Assignee)
	assert.Equal(t, "A0002", *n2.Assignee, "T687 回读必须归一为 admin_id 形")
}

// 解析失败回退：ResolveFlowAccountID 出错时行为与归一前逐位一致——
// 写侧原样落库、执法维持精确比对（本人 403），不放大成 500。
func TestT687_ResolveFailure_FallbackToRaw(t *testing.T) {
	e := t687Env(t)
	e.store.flow.resolveErr = assert.AnError

	t.Run("写侧回退原值", func(t *testing.T) {
		w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions",
			map[string]any{"action": "transfer", "targetOperator": "doctor_li"}, hdr(flowRoleAdmin, "A0001"))
		require.Equal(t, http.StatusOK, w.Code, resp.Message)
		assert.Equal(t, "doctor_li", e.store.flow.lastAction.TargetOperator, "解析失败回退原值，不得写空/报错")
	})

	t.Run("执法回退后维持归一前语义", func(t *testing.T) {
		w, _ := e.do(http.MethodPost, flowActionPath+"N2/actions",
			map[string]any{"action": "confirm"}, hdr("ROLE_DOCTOR", "A0002"))
		require.Equal(t, http.StatusForbidden, w.Code, "回退原值后 doctor_li != A0002 维持 403（与归一前一致，不新增失败面）")
	})
}

// process-guard（告警旁路守卫）读侧兼容：current 节点存量 username 形 assignee 归一后，
// 本人（admin_id 形 operatorId）放行、他人仍拒。
func TestT687_ProcessGuard_LegacyAssigneeNormalized(t *testing.T) {
	newGuardEnv := func(t *testing.T) *testEnv {
		e := t687Env(t)
		e.store.flow.inst = &repo.FlowInstanceRow{
			InstanceID: "FLOW_I_GUARD", TemplateID: "FLOW_T0A1B2C3D4",
			AlertID: 9527, CurrentNodeID: flowStrPtr("N2"), Status: model.FlowInstanceRunning,
		}
		e.store.flow.byAlert = []repo.FlowInstanceRow{*e.store.flow.inst}
		return e
	}

	t.Run("本人放行", func(t *testing.T) {
		e := newGuardEnv(t)
		w, resp := e.do(http.MethodPost, "/internal/flow/process-guard",
			map[string]any{"alertId": 9527, "operatorId": "A0002", "role": "ROLE_DOCTOR"}, nil)
		require.Equal(t, http.StatusOK, w.Code, resp.Message)
		var g flowProcessGuardResponse
		require.NoError(t, json.Unmarshal(resp.Data, &g))
		assert.True(t, g.Enforced)
		assert.True(t, g.Allowed, "存量 username 形 assignee 归一后本人应放行")
	})

	t.Run("他人仍拒", func(t *testing.T) {
		e := newGuardEnv(t)
		w, resp := e.do(http.MethodPost, "/internal/flow/process-guard",
			map[string]any{"alertId": 9527, "operatorId": "A0009", "role": "ROLE_DOCTOR"}, nil)
		require.Equal(t, http.StatusOK, w.Code, resp.Message)
		var g flowProcessGuardResponse
		require.NoError(t, json.Unmarshal(resp.Data, &g))
		assert.True(t, g.Enforced)
		assert.False(t, g.Allowed, "归一只认同一人，不放生别人")
	})
}

// 待办「待我处理」判定读侧兼容：存量 username 形 assignee 归一后归本人（mineCount 含之），
// 他人 pane=mine 不含。
func TestT687_TodosMine_LegacyAssigneeNormalized(t *testing.T) {
	newTodosEnv := func(t *testing.T) *testEnv {
		e := t687Env(t)
		now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
		e.store.flow.todos = []repo.FlowTodoRow{{
			InstanceID: "FLOW_I0A1B2C3D4", TemplateID: "FLOW_T0A1B2C3D4", TemplateName: "告警处置默认流程",
			Nodes:         []byte(t668Nodes),
			AlertID:       9527,
			CurrentNodeID: flowStrPtr("N2"), InstanceStatus: model.FlowInstanceRunning,
			StartedAt: now, AlertType: "low_pressure", AlertTS: now,
			ProcessStatus: "pending", DeviceID: "DEV1", AlertPatientID: "P0001",
			NodeAssignee: flowStrPtr("doctor_li"), NodeEnteredAt: now,
		}}
		return e
	}

	t.Run("本人pane含该行", func(t *testing.T) {
		e := newTodosEnv(t)
		w, resp := e.do(http.MethodGet, "/api/v1/todos?pane=mine", nil, hdr("ROLE_CS", "A0002"))
		require.Equal(t, http.StatusOK, w.Code, resp.Message)
		var out model.FlowTodoListDTO
		require.NoError(t, json.Unmarshal(resp.Data, &out))
		assert.Equal(t, int64(1), out.MineCount, "存量 username 形 assignee 归一后应判给本人")
		require.Len(t, out.List, 1)
	})

	t.Run("他人pane为空", func(t *testing.T) {
		e := newTodosEnv(t)
		w, resp := e.do(http.MethodGet, "/api/v1/todos?pane=mine", nil, hdr("ROLE_CS", "A0009"))
		require.Equal(t, http.StatusOK, w.Code, resp.Message)
		var out model.FlowTodoListDTO
		require.NoError(t, json.Unmarshal(resp.Data, &out))
		assert.Equal(t, int64(0), out.MineCount, "归一只认同一人")
	})
}
