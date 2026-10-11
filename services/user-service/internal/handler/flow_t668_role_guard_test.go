// T668 流程节点角色级权限执法测试（卡面口径 1/2/4/5）：
// 无权角色 confirm 403、admin 例外放行、transfer 越权 403 三最小集 + 角色词映射单元测试，
// 以及 /internal/flow/process-guard 告警旁路守卫（口径 3）。
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

// t668Nodes N2 配「主治医生」角色（执法点）；N1/N3 不配（向后兼容）。
const t668Nodes = `[
  {"id":"N1","type":"rect","text":{"value":"分诊"},"properties":{"kind":"trigger"}},
  {"id":"N2","type":"rect","text":{"value":"医生处理"},"properties":{"kind":"process","assigneeRole":"主治医生"}},
  {"id":"N3","type":"circle","text":{"value":"归档确认"}}
]`

func t668Env(t *testing.T) *testEnv {
	e := flowEnv(t)
	e.store.flow.tpl.Nodes = []byte(t668Nodes)
	e.store.flow.applied = &repo.FlowNodeActionRow{ActionID: 90, NodeID: "N2", Action: "confirm", Operator: "D0001"}
	return e
}

// 最小集 1：无权角色（客服/技师）confirm 配了「主治医生」的 N2 → 403，且不落任何动作。
func TestT668_NodeAction_WrongRole403(t *testing.T) {
	for _, role := range []string{"ROLE_CS", "technician"} {
		t.Run(role, func(t *testing.T) {
			e := t668Env(t)
			w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions",
				map[string]any{"action": "confirm", "remark": "越权尝试"}, hdr(role, "X9001"))
			require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
			assert.Equal(t, model.CodeForbidden, resp.Code)
			// T464：可读拒因只进日志不进响应体（响应固定 403 短句），拒因文本查日志帧：
			t464TechLogContains(t, w, "主治医生")
			assert.Empty(t, e.store.flow.lastAction.NodeID, "403 不得落到 ApplyFlowNodeAction（lastAction 零值）")
		})
	}
}

// 最小集 1 正对照：同节点医生角色 → 放行。
func TestT668_NodeAction_MatchingRoleAllowed(t *testing.T) {
	e := t668Env(t)
	w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions",
		map[string]any{"action": "confirm"}, hdr("ROLE_DOCTOR", "D0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, "N2", e.store.flow.lastAction.NodeID)
}

// t668TwoNodeGraph 两节点图（触发→护士处置），边与节点配套避免 parseFlowGraph 500。
const t668TwoNodeGraph = `{
  "nodes":[
    {"id":"N1","type":"rect","text":{"value":"触发"},"properties":{"kind":"trigger"}},
    {"id":"N2","type":"rect","text":{"value":"护士处置"},"properties":{"kind":"process","assigneeRole":"护士"}}
  ],
  "edges":[{"id":"E1","type":"polyline","sourceNodeId":"N1","targetNodeId":"N2"}]
}`

// 最小集 2：admin 对任意角色节点例外放行（护士节点也能点）。
func TestT668_NodeAction_AdminBypass(t *testing.T) {
	e := flowEnv(t)
	var g map[string]any
	require.NoError(t, json.Unmarshal([]byte(t668TwoNodeGraph), &g))
	nodes, _ := json.Marshal(g["nodes"])
	edges, _ := json.Marshal(g["edges"])
	e.store.flow.tpl.Nodes = nodes
	e.store.flow.tpl.Edges = edges
	e.store.flow.applied = &repo.FlowNodeActionRow{ActionID: 91, NodeID: "N2", Action: "confirm"}
	w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions",
		map[string]any{"action": "confirm"}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
}

// Boss 验收口径：技师（运维）点「护士」节点 → 403。
func TestT668_NodeAction_TechnicianOnNurseNode403(t *testing.T) {
	e := flowEnv(t)
	var g map[string]any
	require.NoError(t, json.Unmarshal([]byte(t668TwoNodeGraph), &g))
	nodes, _ := json.Marshal(g["nodes"])
	edges, _ := json.Marshal(g["edges"])
	e.store.flow.tpl.Nodes = nodes
	e.store.flow.tpl.Edges = edges
	w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions",
		map[string]any{"action": "confirm"}, hdr("technician", "T0002"))
	require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, model.CodeForbidden, resp.Code)
	t464TechLogContains(t, w, "护士")
}

// 最小集 3：transfer 仅当前 assignee 或 admin——
// N2 已转派给 T0002，T0003 越权转派 403；T0002 自己转派与 admin 转派放行。
func TestT668_Transfer_OnlyCurrentAssigneeOrAdmin(t *testing.T) {
	envWithAssignee := func(t *testing.T) *testEnv {
		e := t668Env(t)
		e.store.flow.nodes = []repo.FlowNodeStateRow{
			{NodeID: "N1", Status: model.FlowNodeDone},
			{NodeID: "N2", Status: model.FlowNodeCurrent, Assignee: flowStrPtr("T0002")},
			{NodeID: "N3", Status: model.FlowNodeTodo},
		}
		return e
	}
	body := map[string]any{"action": "transfer", "targetOperator": "T0009"}

	t.Run("非assignee医生也403（账号层优先于角色层）", func(t *testing.T) {
		e := envWithAssignee(t)
		w, _ := e.do(http.MethodPost, flowActionPath+"N2/actions", body, hdr("ROLE_DOCTOR", "D0001"))
		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		t464TechLogContains(t, w, "指派给指定处理人")
	})
	t.Run("当前assignee转派放行", func(t *testing.T) {
		e := envWithAssignee(t)
		w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions", body, hdr("technician", "T0002"))
		require.Equal(t, http.StatusOK, w.Code, resp.Message)
		assert.Equal(t, "T0009", e.store.flow.lastAction.TargetOperator)
	})
	t.Run("admin例外转派放行", func(t *testing.T) {
		e := envWithAssignee(t)
		w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions", body, hdr(flowRoleAdmin, "A0001"))
		require.Equal(t, http.StatusOK, w.Code, resp.Message)
	})
}

// 未配角色的节点（N1）维持 staff 可操作现状（旧模板不锁死）。
func TestT668_UnconfiguredNode_StaffAllowed(t *testing.T) {
	e := t668Env(t)
	e.store.flow.applied = &repo.FlowNodeActionRow{ActionID: 92, NodeID: "N1", Action: "confirm"}
	w, resp := e.do(http.MethodPost, flowActionPath+"N1/actions",
		map[string]any{"action": "confirm"}, hdr("ROLE_CS", "C0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
}

// 角色词映射单元测试（含码值形/混形/空配/未知词 fail-closed）。
func TestT668_FlowNodeAllowsRole(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
		role string
		want bool
	}{
		{"未配置不限制", "", "ROLE_CS", true},
		{"主治医生-医生", "主治医生", "ROLE_DOCTOR", true},
		{"主治医生-客服拒", "主治医生", "ROLE_CS", false},
		{"主治医生-技师拒", "主治医生", "technician", false},
		{"值班医生同映射doctor", "值班医生", "ROLE_DOCTOR", true},
		{"护士-技师拒（Boss验收口径）", "护士", "technician", false},
		{"护士-医生也拒（护士非登录角色）", "护士", "ROLE_DOCTOR", false},
		{"系统自动-任意staff拒", "系统自动", "ROLE_DOCTOR", false},
		{"管理员词-医生拒", "管理员", "ROLE_DOCTOR", false},
		{"码值形ROLE_DOCTOR", "ROLE_DOCTOR", "ROLE_DOCTOR", true},
		{"码值形ROLE_DOCTOR对技师拒", "ROLE_DOCTOR", "technician", false},
		{"混形admin小写头", "管理员", "admin", true},
		{"未知业务词fail-closed", "外星角色", "ROLE_DOCTOR", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, flowNodeAllowsRole(c.cfg, c.role))
		})
	}
}

// ── 口径 3：/internal/flow/process-guard 告警处理旁路守卫 ──

func t668GuardEnv(t *testing.T) *testEnv {
	e := t668Env(t)
	// 实例 running、N2 为 current
	e.store.flow.inst = &repo.FlowInstanceRow{
		InstanceID: "FLOW_I_GUARD", TemplateID: "FLOW_T0A1B2C3D4",
		AlertID: 9527, CurrentNodeID: flowStrPtr("N2"), Status: model.FlowInstanceRunning,
	}
	e.store.flow.byAlert = []repo.FlowInstanceRow{*e.store.flow.inst}
	e.store.flow.nodes = []repo.FlowNodeStateRow{
		{NodeID: "N1", Status: model.FlowNodeDone},
		{NodeID: "N2", Status: model.FlowNodeCurrent},
		{NodeID: "N3", Status: model.FlowNodeTodo},
	}
	return e
}

func TestT668_ProcessGuard_NoInstance_NotEnforced(t *testing.T) {
	e := t668Env(t) // byAlert 空
	w, resp := e.do(http.MethodPost, "/internal/flow/process-guard",
		map[string]any{"alertId": 9527, "operatorId": "D0001", "role": "ROLE_DOCTOR"}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var g flowProcessGuardResponse
	require.NoError(t, json.Unmarshal(resp.Data, &g))
	assert.False(t, g.Enforced, "无实例不启用执法，staff 现状放行")
	assert.True(t, g.Allowed)
}

func TestT668_ProcessGuard_RoleMismatch_Denied(t *testing.T) {
	e := t668GuardEnv(t)
	w, resp := e.do(http.MethodPost, "/internal/flow/process-guard",
		map[string]any{"alertId": 9527, "operatorId": "C0001", "role": "ROLE_CS"}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var g flowProcessGuardResponse
	require.NoError(t, json.Unmarshal(resp.Data, &g))
	assert.True(t, g.Enforced)
	assert.False(t, g.Allowed)
	assert.Contains(t, g.Reason, "主治医生")
	assert.Equal(t, "N2", g.CurrentNodeID)
}

func TestT668_ProcessGuard_RoleMatch_Allowed(t *testing.T) {
	e := t668GuardEnv(t)
	w, resp := e.do(http.MethodPost, "/internal/flow/process-guard",
		map[string]any{"alertId": 9527, "operatorId": "D0001", "role": "ROLE_DOCTOR"}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var g flowProcessGuardResponse
	require.NoError(t, json.Unmarshal(resp.Data, &g))
	assert.True(t, g.Enforced)
	assert.True(t, g.Allowed)
}

func TestT668_ProcessGuard_AdminBypass(t *testing.T) {
	e := t668GuardEnv(t)
	w, resp := e.do(http.MethodPost, "/internal/flow/process-guard",
		map[string]any{"alertId": 9527, "operatorId": "A0001", "role": "ROLE_ADMIN"}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var g flowProcessGuardResponse
	require.NoError(t, json.Unmarshal(resp.Data, &g))
	assert.True(t, g.Allowed)
}

func TestT668_ProcessGuard_BadRequest(t *testing.T) {
	e := t668GuardEnv(t)
	w, _ := e.do(http.MethodPost, "/internal/flow/process-guard",
		map[string]any{"alertId": 0, "operatorId": "X"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
