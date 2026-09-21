// T274 流程画布 handler 测试：模板 CRUD（2.4 设计器）+ 实例/节点状态/操作/时间线（2.3 运行态）
//
// 本文件验的是 handler 层职责：入参校验、角色门禁、LogicFlow 图解析、DTO 装配、审计埋点、
// sentinel → HTTP 映射。状态机本身（事务、行锁、节点推进落库）在 repo 层，
// 由 repo/flow_integration_test.go 用 testcontainers 真 PG 覆盖（CI go-integration job 执行）。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// 三节点线性模板：分诊 → 矫形工程师处理 → 归档确认（N1 无入边 = 起始节点）
const (
	fixFlowNodes = `[{"id":"N1","type":"rect","text":{"value":"分诊"},"x":100,"y":100,"properties":{"godl":{"fill":"#fff"}}},` +
		`{"id":"N2","type":"rect","text":{"value":"矫形工程师处理"},"x":300,"y":100},` +
		`{"id":"N3","type":"circle","text":{"value":"归档确认"},"x":500,"y":100}]`
	fixFlowEdges = `[{"id":"E1","type":"polyline","sourceNodeId":"N1","targetNodeId":"N2"},` +
		`{"id":"E2","type":"polyline","sourceNodeId":"N2","targetNodeId":"N3"}]`
)

func fixFlowTemplateRow() *repo.FlowTemplateRow {
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	return &repo.FlowTemplateRow{
		TemplateID: "FLOW_T0A1B2C3D4", Name: "告警处置默认流程",
		Nodes: []byte(fixFlowNodes), Edges: []byte(fixFlowEdges),
		Creator: "A0001", CreatorName: flowStrPtr("运营小张"), Version: 1,
		CreatedAt: now, UpdatedAt: now, InstanceCount: 0,
	}
}

func fixFlowInstanceRow() *repo.FlowInstanceRow {
	return &repo.FlowInstanceRow{
		InstanceID: "FLOW_I0A1B2C3D4", TemplateID: "FLOW_T0A1B2C3D4",
		TemplateName: "告警处置默认流程", AlertID: 9527,
		CurrentNodeID: flowStrPtr("N1"), Status: model.FlowInstanceRunning,
		StartedAt: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
	}
}

// flowEnv 装配一个带模板 + 实例的 handler 环境
func flowEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, false, false)
	e.flowReset()
	return e
}

func (e *testEnv) flowReset() {
	e.store.flow.tpl = fixFlowTemplateRow()
	e.store.flow.createdTpl = fixFlowTemplateRow()
	e.store.flow.inst = fixFlowInstanceRow()
	e.store.flow.createdInst = fixFlowInstanceRow()
}

const flowRoleAdmin = "ROLE_ADMIN"

func hdr(role, uid string) map[string]string {
	return map[string]string{"X-Role": role, "X-User-Id": uid}
}

// ─────────────────────────────────────────────────────────────
// 2.4 模板：新建
// ─────────────────────────────────────────────────────────────

func TestT274_FlowTemplate_Create_OK(t *testing.T) {
	e := flowEnv(t)
	w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/templates",
		json.RawMessage(`{"name":"告警处置默认流程","nodes":`+fixFlowNodes+`,"edges":`+fixFlowEdges+`}`),
		hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	assert.Equal(t, "告警处置默认流程", e.store.flow.lastTplName)
	assert.Equal(t, "A0001", e.store.flow.lastTplCreator, "creator 取 gateway 注入的 X-User-Id")
	assert.JSONEq(t, fixFlowNodes, e.store.flow.lastTplNodes, "nodes 原文落库，不改写前端图数据")
	assert.JSONEq(t, fixFlowEdges, e.store.flow.lastTplEdges)

	var dto model.FlowTemplateDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "FLOW_T0A1B2C3D4", dto.TemplateID)
	assert.JSONEq(t, fixFlowNodes, string(dto.Nodes), "响应回显完整图数据供 lf.render()")
	assert.Equal(t, "运营小张", *dto.CreatorName)

	require.Len(t, e.store.auditRows, 1)
	assert.Equal(t, "config_change", e.store.auditRows[0].Action)
	assert.Equal(t, "flow_template", e.store.auditRows[0].TargetType)
	assert.Contains(t, e.store.auditRows[0].Description, "节点 3 / 连线 2")
}

func TestT274_FlowTemplate_Create_EmptyGraphAllowed(t *testing.T) {
	// 设计器「新建空白模板」：先建空模板再拖拽，故 nodes/edges 缺省合法
	e := flowEnv(t)
	w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/templates",
		map[string]string{"name": "空白流程"}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, "[]", e.store.flow.lastTplNodes)
}

func TestT274_FlowTemplate_Create_Validation400(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"缺名称", `{"nodes":[]}`, "name must be 1-64 chars"},
		{"名称超长", `{"name":"` + strings.Repeat("流", 65) + `"}`, "name must be 1-64 chars"},
		{"nodes 不是数组", `{"name":"x","nodes":{"a":1}}`, "nodes must be a JSON array"},
		{"节点 id 为空", `{"name":"x","nodes":[{"id":""}]}`, "nodes[0].id must not be empty"},
		{"节点 id 超长", `{"name":"x","nodes":[{"id":"` + strings.Repeat("N", 65) + `"}]}`, "at most 64 chars"},
		{"节点 id 重复", `{"name":"x","nodes":[{"id":"N1"},{"id":"N1"}]}`, "duplicate node id: N1"},
		{"边指向不存在节点", `{"name":"x","nodes":[{"id":"N1"}],"edges":[{"sourceNodeId":"N1","targetNodeId":"NX"}]}`,
			`edges[0].targetNodeId "NX" is not a node`},
		{"边自环", `{"name":"x","nodes":[{"id":"N1"}],"edges":[{"sourceNodeId":"N1","targetNodeId":"N1"}]}`,
			"edges[0] is a self-loop"},
		{"节点超上限", `{"name":"x","nodes":[` + strings.TrimSuffix(strings.Repeat(`{"id":"A"},`, 101), ",") + `]}`,
			"nodes must have at most 100 items"},
		{"边超上限", `{"name":"x","nodes":[{"id":"A"},{"id":"B"}],"edges":[` +
			strings.TrimSuffix(strings.Repeat(`{"sourceNodeId":"A","targetNodeId":"B"},`, 301), ",") + `]}`,
			"edges must have at most 300 items"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := flowEnv(t)
			w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/templates", json.RawMessage(c.body), hdr(flowRoleAdmin, "A0001"))
			require.Equal(t, http.StatusBadRequest, w.Code, "%s → %s", c.name, resp.Message)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			if c.want != "" {
				assert.Contains(t, resp.Message, c.want)
			}
			assert.Empty(t, e.store.auditRows, "校验失败不得留审计")
		})
	}
}

func TestT274_FlowTemplate_Create_NameConflict409(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.nameTaken = true
	w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/templates",
		map[string]string{"name": "已占用"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusConflict, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "已占用")
}

func TestT274_FlowTemplate_Create_NonAdmin403(t *testing.T) {
	for _, role := range []string{"ROLE_DOCTOR", "ROLE_CS", "technician", "patient", ""} {
		e := flowEnv(t)
		w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/templates",
			map[string]string{"name": "越权"}, hdr(role, "X1"))
		assert.Equal(t, http.StatusForbidden, w.Code, "role=%q 应 403：%s", role, resp.Message)
		assert.Empty(t, e.store.flow.lastTplName, "role=%q 不得触达写库", role)
	}
}

// ─────────────────────────────────────────────────────────────
// 2.4 模板：列表 / 详情 / 保存 / 删除
// ─────────────────────────────────────────────────────────────

func TestT274_FlowTemplate_List_Paged(t *testing.T) {
	e := flowEnv(t)
	row := *fixFlowTemplateRow()
	row.InstanceCount = 3
	// repo 层 ListFlowTemplates 用 SQL 常量把 nodes/edges 置 '[]'（见 flow.go 注释），
	// fake 按同一口径装配，本用例只验 handler 的透传与分页装配。
	row.Nodes, row.Edges = []byte("[]"), []byte("[]")
	e.store.flow.listTemplates = []repo.FlowTemplateRow{row}
	e.store.flow.listTemplateTotal = 41

	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/templates?page=2&pageSize=10&keyword=告警", nil, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	var page struct {
		List     []model.FlowTemplateDTO `json:"list"`
		Total    int64                   `json:"total"`
		Page     int                     `json:"page"`
		PageSize int                     `json:"pageSize"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	assert.Equal(t, int64(41), page.Total)
	assert.Equal(t, 2, page.Page)
	assert.Equal(t, 10, page.PageSize)

	require.Len(t, page.List, 1)
	assert.JSONEq(t, "[]", string(page.List[0].Nodes), "列表不回图数据，避免载荷膨胀")
	assert.Equal(t, 3, page.List[0].InstanceCount)
}

func TestT274_FlowTemplate_List_NonAdmin403(t *testing.T) {
	e := flowEnv(t)
	w, _ := e.do(http.MethodGet, "/api/v1/admin/flow/templates", nil, hdr("ROLE_DOCTOR", "D0001"))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestT274_FlowTemplate_Get_NotFound404(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.tplErr = repo.ErrFlowTemplateNotFound
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/templates/FLOW_T_NOPE", nil, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusNotFound, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "FLOW_T_NOPE")
}

func TestT274_FlowTemplate_Update_PartialKeepsOtherFields(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.updatedTpl = fixFlowTemplateRow()
	e.store.flow.updatedTpl.Version = 2

	w, resp := e.do(http.MethodPut, "/api/v1/admin/flow/templates/FLOW_T0A1B2C3D4",
		map[string]string{"name": "改名后的流程"}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	assert.Equal(t, "FLOW_T0A1B2C3D4", e.store.flow.lastUpdTplID)
	require.NotNil(t, e.store.flow.lastUpdName)
	assert.Equal(t, "改名后的流程", *e.store.flow.lastUpdName)
	assert.Nil(t, e.store.flow.lastUpdNodes, "未给 nodes → 传 nil，由 repo 拼 SET 时跳过（不整体清空）")
	assert.Nil(t, e.store.flow.lastUpdEdges)
	require.Len(t, e.store.auditRows, 1)
	assert.Contains(t, e.store.auditRows[0].Description, "版本→2")
}

func TestT274_FlowTemplate_Update_NewEdgesMustLandOnNewNodes(t *testing.T) {
	// 只换 nodes 删掉了 N3，旧 edges 仍指向 N3 ⇒ 图不自洽，必须 400（否则运行态推进时找不到后继）
	e := flowEnv(t)
	e.store.flow.tpl.Nodes = []byte(`[{"id":"N1"},{"id":"N2"}]`)
	w, resp := e.do(http.MethodPut, "/api/v1/admin/flow/templates/FLOW_T0A1B2C3D4",
		json.RawMessage(`{"nodes":[{"id":"N1"},{"id":"N2"}]}`), hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Contains(t, resp.Message, `.targetNodeId "N3" is not a node`)
}

func TestT274_FlowTemplate_Update_NothingToSend400(t *testing.T) {
	e := flowEnv(t)
	w, resp := e.do(http.MethodPut, "/api/v1/admin/flow/templates/FLOW_T0A1B2C3D4", map[string]any{}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "nothing to update")
}

func TestT274_FlowTemplate_Delete(t *testing.T) {
	e := flowEnv(t)
	w, resp := e.do(http.MethodDelete, "/api/v1/admin/flow/templates/FLOW_T0A1B2C3D4", nil, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	require.Len(t, e.store.auditRows, 1)
	assert.Contains(t, e.store.auditRows[0].Description, "删除流程模板 FLOW_T0A1B2C3D4")
}

func TestT274_FlowTemplate_Delete_InUse409(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.deleteEr = &repo.ErrFlowTemplateInUse{InstanceCount: 2}
	w, resp := e.do(http.MethodDelete, "/api/v1/admin/flow/templates/FLOW_T0A1B2C3D4", nil, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusConflict, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "used by 2 instance")
	assert.Empty(t, e.store.auditRows, "删除失败不留审计")
}

// ─────────────────────────────────────────────────────────────
// 2.3 实例：启动
// ─────────────────────────────────────────────────────────────

func TestT274_FlowInstance_Start_EntryNodeFromEdges(t *testing.T) {
	e := flowEnv(t)
	w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/instances",
		map[string]string{"templateId": "FLOW_T0A1B2C3D4", "alertId": "9527"}, hdr("ROLE_DOCTOR", "D0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	assert.Equal(t, int64(9527), e.store.flow.lastStartAlertID)
	assert.Equal(t, []string{"N1", "N2", "N3"}, e.store.flow.lastStartNodes, "全量节点各生成一行 todo")
	assert.Equal(t, []string{"N1"}, e.store.flow.lastStartEntries, "入边为 0 的节点才置 current")

	var dto model.FlowInstanceDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "9527", dto.AlertID, "alertId 以字符串回传，避开 JS Number 精度截断")
	require.Len(t, e.store.auditRows, 1)
	assert.Contains(t, e.store.auditRows[0].Description, "为告警 9527 启动流程实例")
}

func TestT274_FlowInstance_Start_BadAlertID400(t *testing.T) {
	for _, v := range []string{"abc", "0", "-3", "1.5", ""} {
		e := flowEnv(t)
		w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/instances",
			map[string]string{"templateId": "FLOW_T0A1B2C3D4", "alertId": v}, hdr(flowRoleAdmin, "A0001"))
		assert.Equal(t, http.StatusBadRequest, w.Code, "alertId=%q 应 400：%s", v, resp.Message)
	}
}

func TestT274_FlowInstance_Start_AlertNotFound400(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.createInstEr = repo.ErrFlowAlertNotFound
	w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/instances",
		map[string]string{"templateId": "FLOW_T0A1B2C3D4", "alertId": "999999"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "alert not found")
}

func TestT274_FlowInstance_Start_TemplateNotFound404(t *testing.T) {
	// 防回归：本端点没有 :templateId 路径参数，文案里的 ID 必须取自请求体，
	// 否则 404 会打成 "flow template not found: "（空串，前端无从排查）。
	e := flowEnv(t)
	e.store.flow.tplErr = repo.ErrFlowTemplateNotFound
	w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/instances",
		map[string]string{"templateId": "FLOW_T_NOT_EXIST", "alertId": "9527"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusNotFound, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "FLOW_T_NOT_EXIST")
}

func TestT274_FlowInstance_Start_AlreadyExists409(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.createInstEr = &repo.ErrFlowInstanceExists{Existing: fixFlowInstanceRow()}
	w, resp := e.do(http.MethodPost, "/api/v1/admin/flow/instances",
		map[string]string{"templateId": "FLOW_T0A1B2C3D4", "alertId": "9527"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusConflict, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "FLOW_I0A1B2C3D4", "409 文案带已有实例 ID，前端可直接跳详情")
}

func TestT274_FlowInstance_Start_Patient403(t *testing.T) {
	e := flowEnv(t)
	w, _ := e.do(http.MethodPost, "/api/v1/admin/flow/instances",
		map[string]string{"templateId": "T", "alertId": "1"}, hdr("patient", "P20260001"))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestT274_FlowInstance_GetByAlert(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.byAlert = []repo.FlowInstanceRow{*fixFlowInstanceRow()}
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/instances?alertId=9527", nil, hdr("technician", "T0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var body struct {
		List []model.FlowInstanceDTO `json:"list"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &body))
	require.Len(t, body.List, 1)
	assert.Equal(t, "FLOW_I0A1B2C3D4", body.List[0].InstanceID)
}

func TestT274_FlowInstance_GetByAlert_MissingAlertID400(t *testing.T) {
	e := flowEnv(t)
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/instances", nil, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "alertId query is required")
}

// ─────────────────────────────────────────────────────────────
// 2.3 节点状态（画布着色数据源）
// ─────────────────────────────────────────────────────────────

func TestT274_FlowNodeStates(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.nodes = []repo.FlowNodeStateRow{
		{NodeID: "N1", Status: model.FlowNodeDone, Operator: flowStrPtr("A0001"), OperatorName: flowStrPtr("运营小张"),
			OperatedAt: timePtr(time.Date(2026, 9, 20, 9, 5, 0, 0, time.UTC)), Remark: flowStrPtr("已核对阈值"),
			Attachments: []byte(`["FILE_A"]`)},
		{NodeID: "N2", Status: model.FlowNodeCurrent, Assignee: flowStrPtr("T0002"), AssigneeName: flowStrPtr("技师小李")},
		{NodeID: "N3", Status: model.FlowNodeTodo},
	}
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/nodes", nil, hdr("ROLE_DOCTOR", "D0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	var body struct {
		List []model.FlowNodeStateDTO `json:"list"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &body))
	require.Len(t, body.List, 3)

	assert.Equal(t, model.FlowNodeDone, body.List[0].Status)
	assert.Equal(t, "2026-09-20T09:05:00Z", *body.List[0].OperatedAt)
	assert.JSONEq(t, `["FILE_A"]`, string(body.List[0].Attachments))
	assert.Equal(t, []string{"N2"}, body.List[0].NextNodeIDs, "后继由模板 edges 现算")
	assert.Equal(t, "技师小李", *body.List[1].AssigneeName, "转派后前端显示新负责人")
	assert.JSONEq(t, "[]", string(body.List[2].Attachments), "无附件回空数组而非 null")
	assert.Equal(t, []string{}, body.List[2].NextNodeIDs, "末节点无出边回空数组")
}

func TestT274_FlowNodeStates_InstanceNotFound404(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.instErr = repo.ErrFlowInstanceNotFound
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/instances/FLOW_I_NOPE/nodes", nil, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusNotFound, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "FLOW_I_NOPE")
}

func TestT274_FlowNodeStates_CorruptedStoredGraph500(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.tpl.Nodes = []byte(`{"not":"an array"}`)
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/nodes", nil, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusInternalServerError, w.Code, resp.Message)
}

// ─────────────────────────────────────────────────────────────
// 2.3 节点操作（状态机指令）
// ─────────────────────────────────────────────────────────────

const flowActionPath = "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/nodes/"

func TestT274_FlowAction_ConfirmAdvancesSuccessor(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.applied = &repo.FlowNodeActionRow{ActionID: 77, NodeID: "N1", Action: "confirm",
		Operator: "D0001", OperatorName: flowStrPtr("张医生"), CreatedAt: time.Date(2026, 9, 20, 9, 5, 0, 0, time.UTC)}

	w, resp := e.do(http.MethodPost, flowActionPath+"N1/actions",
		map[string]any{"action": "confirm", "remark": "分诊完毕"}, hdr("ROLE_DOCTOR", "D0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	in := e.store.flow.lastAction
	assert.Equal(t, model.FlowNodeDone, in.NodeStatus)
	assert.Equal(t, []string{"N2"}, in.Successors)
	assert.Equal(t, "N2", in.CurrentNodeID, "实例指针移到后继")
	assert.False(t, in.Complete)

	var dto model.FlowNodeActionDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "77", dto.ActionID)
	assert.Equal(t, "分诊", *dto.NodeName, "nodeName 由模板 text.value 反查")
	assert.Equal(t, "确认处理", dto.ActionLabel)
	require.Len(t, e.store.auditRows, 1)
	assert.Contains(t, e.store.auditRows[0].Description, "流程节点 N1 执行确认处理，意见「分诊完毕」")
}

func TestT274_FlowAction_ConfirmLastNodeCompletesInstance(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.applied = &repo.FlowNodeActionRow{ActionID: 78, NodeID: "N3", Action: "confirm", Operator: "D0001"}
	w, resp := e.do(http.MethodPost, flowActionPath+"N3/actions",
		map[string]any{"action": "confirm"}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.True(t, e.store.flow.lastAction.Complete, "末节点确认 → 实例 completed")
	assert.Empty(t, e.store.flow.lastAction.Successors)
	assert.Equal(t, "N3", e.store.flow.lastAction.CurrentNodeID)
}

func TestT274_FlowAction_ConfirmNextNodeIDsNarrowsBranch(t *testing.T) {
	// 判断节点分叉：前端指定只走 N2
	e := flowEnv(t)
	e.store.flow.tpl.Nodes = []byte(`[{"id":"N1","text":{"value":"判断"}},{"id":"N2"},{"id":"N3"}]`)
	e.store.flow.tpl.Edges = []byte(`[{"sourceNodeId":"N1","targetNodeId":"N2"},{"sourceNodeId":"N1","targetNodeId":"N3"}]`)
	e.store.flow.applied = &repo.FlowNodeActionRow{ActionID: 79, NodeID: "N1", Action: "confirm", Operator: "A0001"}

	w, resp := e.do(http.MethodPost, flowActionPath+"N1/actions",
		json.RawMessage(`{"action":"confirm","nextNodeIds":["N3"]}`), hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, []string{"N3"}, e.store.flow.lastAction.Successors)
	assert.Equal(t, "N3", e.store.flow.lastAction.CurrentNodeID)
}

func TestT274_FlowAction_ConfirmNextNodeIDsMustBeRealEdge(t *testing.T) {
	e := flowEnv(t)
	w, resp := e.do(http.MethodPost, flowActionPath+"N1/actions",
		json.RawMessage(`{"action":"confirm","nextNodeIds":["N3"]}`), hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "is not an outgoing edge target of node N1")
}

func TestT274_FlowAction_RejectSkipsWithoutAdvance(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.applied = &repo.FlowNodeActionRow{ActionID: 80, NodeID: "N1", Action: "reject", Operator: "A0001"}
	w, resp := e.do(http.MethodPost, flowActionPath+"N1/actions",
		map[string]any{"action": "reject", "remark": "误报"}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, model.FlowNodeSkipped, e.store.flow.lastAction.NodeStatus)
	assert.Empty(t, e.store.flow.lastAction.Successors, "驳回不推进")
	assert.Empty(t, e.store.flow.lastAction.CurrentNodeID, "驳回不动实例指针")
	assert.False(t, e.store.flow.lastAction.Complete)
}

func TestT274_FlowAction_TransferNeedsTarget(t *testing.T) {
	e := flowEnv(t)
	w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions",
		map[string]any{"action": "transfer"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "targetOperator is required")

	e2 := flowEnv(t)
	e2.store.flow.applied = &repo.FlowNodeActionRow{ActionID: 81, NodeID: "N2", Action: "transfer", Operator: "A0001"}
	w, resp = e2.do(http.MethodPost, flowActionPath+"N2/actions",
		map[string]any{"action": "transfer", "targetOperator": "T0002"}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, "T0002", e2.store.flow.lastAction.TargetOperator)
	assert.Empty(t, e2.store.flow.lastAction.NodeStatus, "转派不改节点状态")
	assert.Empty(t, e2.store.flow.lastAction.Successors)
}

func TestT274_FlowAction_UrgeOnlyLogs(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.applied = &repo.FlowNodeActionRow{ActionID: 82, NodeID: "N2", Action: "urge", Operator: "A0001"}
	w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions",
		map[string]any{"action": "urge", "remark": "家属催办"}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Empty(t, e.store.flow.lastAction.NodeStatus)
	assert.Equal(t, "加急", flowActionLabels[e.store.flow.lastAction.Action])
}

func TestT274_FlowAction_Validation(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"未知 action", `{"action":"skip"}`, "invalid action"},
		{"action 缺失", `{}`, "Action"},
		{"超长 remark", `{"action":"confirm","remark":"` + strings.Repeat("意", 513) + `"}`, "remark must be at most 512 chars"},
		{"attachments 非数组", `{"action":"confirm","attachments":"FILE_A"}`, "attachments must be an array"},
		{"attachments 元素为空", `{"action":"confirm","attachments":[""]}`, "attachments[0] must not be empty"},
		{"attachments 元素超长", `{"action":"confirm","attachments":["` + strings.Repeat("F", 65) + `"]}`, "at most 64 chars"},
		{"attachments 超上限", `{"action":"confirm","attachments":[` +
			strings.TrimSuffix(strings.Repeat(`"F1",`, 21), ",") + `]}`, "at most 20 items"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := flowEnv(t)
			w, resp := e.do(http.MethodPost, flowActionPath+"N1/actions", json.RawMessage(c.body), hdr(flowRoleAdmin, "A0001"))
			assert.Equal(t, http.StatusBadRequest, w.Code, "%s → %s", c.name, resp.Message)
			if c.want != "" {
				assert.Contains(t, resp.Message, c.want)
			}
		})
	}
}

func TestT274_FlowAction_Conflicts(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.applyErr = &repo.ErrFlowNodeNotCurrent{NodeID: "N2", Status: model.FlowNodeTodo}
	w, resp := e.do(http.MethodPost, flowActionPath+"N2/actions",
		map[string]any{"action": "confirm"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusConflict, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "only the current node")

	e2 := flowEnv(t)
	e2.store.flow.applyErr = repo.ErrFlowInstanceCompleted
	w, resp = e2.do(http.MethodPost, flowActionPath+"N1/actions",
		map[string]any{"action": "confirm"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusConflict, w.Code, resp.Message)

	e3 := flowEnv(t)
	e3.store.flow.applyErr = repo.ErrFlowNodeNotFound
	w, resp = e3.do(http.MethodPost, flowActionPath+"N1/actions",
		map[string]any{"action": "confirm"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusNotFound, w.Code, resp.Message)
}

func TestT274_FlowAction_NodeNotInTemplate404(t *testing.T) {
	e := flowEnv(t)
	w, resp := e.do(http.MethodPost, flowActionPath+"N9/actions",
		map[string]any{"action": "confirm"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusNotFound, w.Code, resp.Message)
	assert.Contains(t, resp.Message, "not found in this instance's template")
	assert.Empty(t, e.store.flow.lastAction.Action, "节点不属于该模板时不得触达写库")
}

func TestT274_FlowAction_InternalErrorPaths(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.instErr = errors.New("connection reset")
	w, resp := e.do(http.MethodPost, flowActionPath+"N1/actions",
		map[string]any{"action": "confirm"}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusInternalServerError, w.Code, resp.Message)
}

// ─────────────────────────────────────────────────────────────
// 2.3 时间线
// ─────────────────────────────────────────────────────────────

func TestT274_FlowActions_Timeline(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.actions = []repo.FlowNodeActionRow{
		{ActionID: 1, NodeID: "N1", Action: "confirm", Operator: "D0001", OperatorName: flowStrPtr("张医生"),
			Remark: flowStrPtr("已分诊"), Attachments: []byte(`["FILE_A","FILE_B"]`),
			CreatedAt: time.Date(2026, 9, 20, 9, 5, 0, 0, time.UTC)},
		{ActionID: 2, NodeID: "N9", Action: "legacy_action", Operator: "A0001"},
	}
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/actions", nil, hdr("ROLE_CS", "C0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	var body struct {
		List []model.FlowNodeActionDTO `json:"list"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &body))
	require.Len(t, body.List, 2)
	assert.Equal(t, "分诊", *body.List[0].NodeName)
	assert.Equal(t, "确认处理", body.List[0].ActionLabel)
	assert.JSONEq(t, `["FILE_A","FILE_B"]`, string(body.List[0].Attachments))

	assert.Nil(t, body.List[1].NodeName, "模板里已删掉的节点：名字回 null，流水不丢")
	assert.Equal(t, "legacy_action", body.List[1].ActionLabel, "枚举外的历史值原样回显，不显示空白")
}

// ─────────────────────────────────────────────────────────────
// 运行态角色门禁（staff 四个内部角色放行，patient / 未知角色 403）
// ─────────────────────────────────────────────────────────────

func TestT274_FlowRuntime_RoleGate(t *testing.T) {
	reads := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/flow/instances?alertId=9527"},
		{http.MethodGet, "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/nodes"},
		{http.MethodGet, "/api/v1/admin/flow/instances/FLOW_I0A1B2C3D4/actions"},
	}
	for _, role := range []string{"ROLE_ADMIN", "ROLE_DOCTOR", "ROLE_CS", "technician"} {
		for _, r := range reads {
			e := flowEnv(t)
			w, resp := e.do(r.method, r.path, nil, hdr(role, "X1"))
			assert.Equal(t, http.StatusOK, w.Code, "role=%s %s %s 不应被误伤：%s", role, r.method, r.path, resp.Message)
		}
	}
	for _, role := range []string{"patient", "ROLE_GHOST", ""} {
		for _, r := range reads {
			e := flowEnv(t)
			w, _ := e.do(r.method, r.path, nil, hdr(role, "X1"))
			assert.Equal(t, http.StatusForbidden, w.Code, "role=%q %s %s 应 403（fail-closed）", role, r.method, r.path)
		}
	}
}

// ─────────────────────────────────────────────────────────────
// 图解析纯函数（覆盖分支：无 text / 空白 text / 重复边去重）
// ─────────────────────────────────────────────────────────────

func TestT274_ParseFlowGraph(t *testing.T) {
	g, appErr := parseFlowGraph(
		json.RawMessage(`[{"id":"A"},{"id":"B","text":{"value":"  "}},{"id":" C ","text":{"value":"有名字"}}]`),
		json.RawMessage(`[{"sourceNodeId":"A","targetNodeId":"B"},{"sourceNodeId":"A","targetNodeId":"B"},
                          {"sourceNodeId":"B","targetNodeId":"C"}]`))
	require.Nil(t, appErr)
	assert.Equal(t, []string{"A", "B", "C"}, g.nodeIDs)
	assert.Equal(t, 3, g.edgeCount, "计数按入参条数，去重只作用于后继索引")
	assert.Equal(t, []string{"B"}, g.successors["A"], "后继去重保序")
	assert.NotContains(t, g.nameOf, "B", "空白 text 不作为展示名")
	assert.Equal(t, "有名字", g.nameOf["C"], "节点 id 两侧空白应被 trim 后再索引")
	assert.True(t, g.entryNodeID["A"])
	assert.False(t, g.entryNodeID["B"])

	empty, appErr := parseFlowGraph(nil, nil)
	require.Nil(t, appErr)
	assert.Empty(t, empty.nodeIDs)
}

// ── 测试辅助（本包内其它测试文件已有同名 strPtr，此处用 flow 前缀避免冲突）──

func timePtr(v time.Time) *time.Time { return &v }

func flowStrPtr(v string) *string { return &v }
