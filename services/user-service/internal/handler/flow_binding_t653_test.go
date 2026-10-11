// T653 告警类型 ↔ 流程模板绑定 handler 测试：
//   - GET 四类型固定视图（未绑定补 null 行）+ adminOnly 门禁
//   - PUT 部分覆盖（绑定/解绑/无变化/词表/去重/模板缺失）+ 逐项审计 target=flow_binding/<type>
//   - POST /internal/flow/auto-start：无绑定跳过 / 有绑定建实例 / 409 幂等 / 告警不存在 400 / 模板损坏降级
//
// 状态机与 SQL 语义（事务 upsert/删除、FK）由 repo 层 testcontainers 集成测试覆盖（CI go-integration）。
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

const t653TplID = "FLOW_T0A1B2C3D4"

func t653BindEnv(t *testing.T) *testEnv {
	t.Helper()
	e := flowEnv(t) // fake 内已有可用模板 fixFlowTemplateRow（N1→N2→N3，N1 入口）
	return e
}

func t653BindingsView(t *testing.T, raw json.RawMessage) []model.FlowTypeBindingDTO {
	t.Helper()
	var view struct {
		List []model.FlowTypeBindingDTO `json:"list"`
	}
	require.NoError(t, json.Unmarshal(raw, &view))
	return view.List
}

// ───────────────────────── GET 四类型固定视图 ─────────────────────────

func TestT653_GetBindings_Empty_FourUnboundRows(t *testing.T) {
	e := t653BindEnv(t)
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/type-bindings", nil, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	list := t653BindingsView(t, resp.Data)
	require.Len(t, list, 4, "未绑定也要固定回四行")
	assert.Equal(t, model.FlowBindAlertTypes, []string{
		list[0].AlertType, list[1].AlertType, list[2].AlertType, list[3].AlertType,
	})
	for i, b := range list {
		assert.Equal(t, model.FlowBindAlertTypeNames[b.AlertType], b.AlertTypeName, "中文名后端给")
		assert.Nil(t, b.TemplateID, "第 %d 行应未绑定", i)
		assert.Nil(t, b.UpdatedAt)
	}
}

func TestT653_GetBindings_BoundRowJoined(t *testing.T) {
	e := t653BindEnv(t)
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		model.FlowBindAlertTypePressureHigh: {
			AlertType: model.FlowBindAlertTypePressureHigh, TemplateID: t653TplID,
			UpdatedBy: "A0001",
		},
	}
	w, resp := e.do(http.MethodGet, "/api/v1/admin/flow/type-bindings", nil, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	list := t653BindingsView(t, resp.Data)
	require.Len(t, list, 4)
	require.NotNil(t, list[0].TemplateID)
	assert.Equal(t, t653TplID, *list[0].TemplateID)
	assert.Nil(t, list[1].TemplateID, "其余三行仍补未绑定占位")
}

func TestT653_GetBindings_RoleGate(t *testing.T) {
	e := t653BindEnv(t)
	for _, role := range []string{"ROLE_DOCTOR", "ROLE_CS", "technician", "patient", ""} {
		w, _ := e.do(http.MethodGet, "/api/v1/admin/flow/type-bindings", nil, hdr(role, "D0001"))
		assert.Equal(t, http.StatusForbidden, w.Code, "role=%q 应 403", role)
	}
}

// ───────────────────────── PUT 绑定/解绑/校验/审计 ─────────────────────────

func TestT653_SaveBindings_Bind_OK_AndAudit(t *testing.T) {
	e := t653BindEnv(t)
	w, resp := e.do(http.MethodPut, "/api/v1/admin/flow/type-bindings",
		map[string]any{"bindings": []map[string]string{
			{"alertType": "pressure_high", "templateId": t653TplID},
		}}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.flow.replacedItems, 1)
	assert.Equal(t, "pressure_high", e.store.flow.replacedItems[0].AlertType)
	assert.Equal(t, t653TplID, e.store.flow.replacedItems[0].TemplateID)
	assert.Equal(t, "A0001", e.store.flow.replacedItems[0].UpdatedBy, "updatedBy 取 X-User-Id")

	require.Len(t, e.store.auditRows, 1)
	a := e.store.auditRows[0]
	assert.Equal(t, auditActionDataModify, a.Action)
	assert.Equal(t, "flow_binding", a.TargetType)
	assert.Equal(t, "pressure_high", a.TargetID)
	assert.Contains(t, a.Description, "压力偏高")

	list := t653BindingsView(t, resp.Data)
	require.NotNil(t, list[0].TemplateID)
	assert.Equal(t, t653TplID, *list[0].TemplateID)
}

func TestT653_SaveBindings_Unbind_OK(t *testing.T) {
	e := t653BindEnv(t)
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"wear_interrupt": {AlertType: "wear_interrupt", TemplateID: t653TplID, UpdatedBy: "A0001"},
	}
	w, resp := e.do(http.MethodPut, "/api/v1/admin/flow/type-bindings",
		map[string]any{"bindings": []map[string]string{
			{"alertType": "wear_interrupt", "templateId": ""},
		}}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.flow.replacedItems, 1)
	assert.Empty(t, e.store.flow.replacedItems[0].TemplateID, "空串 = 解绑（DELETE）")
	require.Len(t, e.store.auditRows, 1)
	assert.Equal(t, "wear_interrupt", e.store.auditRows[0].TargetID)
	assert.Contains(t, e.store.auditRows[0].Description, "解绑")
}

func TestT653_SaveBindings_NoChange_NoWriteNoAudit(t *testing.T) {
	e := t653BindEnv(t)
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: t653TplID, UpdatedBy: "A0001"},
	}
	// 绑同一模板 + 解绑一个本就没绑定的类型 ⇒ 零变化
	w, resp := e.do(http.MethodPut, "/api/v1/admin/flow/type-bindings",
		map[string]any{"bindings": []map[string]string{
			{"alertType": "pressure_high", "templateId": t653TplID},
			{"alertType": "sensor_drift", "templateId": ""},
		}}, hdr(flowRoleAdmin, "A0001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Empty(t, e.store.flow.replacedItems, "无变化不落库")
	assert.Empty(t, e.store.auditRows, "无变化不刷审计")
}

func TestT653_SaveBindings_Validation400(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"空 bindings", map[string]any{"bindings": []any{}}},
		{"类型不在词表", map[string]any{"bindings": []map[string]string{
			{"alertType": "pressure_fluctuation", "templateId": t653TplID},
		}}},
		{"同类型重复", map[string]any{"bindings": []map[string]string{
			{"alertType": "pressure_high", "templateId": t653TplID},
			{"alertType": "pressure_high", "templateId": ""},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := t653BindEnv(t)
			w, resp := e.do(http.MethodPut, "/api/v1/admin/flow/type-bindings", tc.body, hdr(flowRoleAdmin, "A0001"))
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			assert.Empty(t, e.store.auditRows, "被拒的写不留痕")
		})
	}
}

func TestT653_SaveBindings_TemplateMissing_400(t *testing.T) {
	e := t653BindEnv(t)
	e.store.flow.tpl = nil
	e.store.flow.tplErr = repo.ErrFlowTemplateNotFound
	w, _ := e.do(http.MethodPut, "/api/v1/admin/flow/type-bindings",
		map[string]any{"bindings": []map[string]string{
			{"alertType": "pressure_high", "templateId": "FLOW_TNOPE"},
		}}, hdr(flowRoleAdmin, "A0001"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, e.store.flow.replacedItems, "模板校验先于落库")
	assert.Empty(t, e.store.auditRows)
}

func TestT653_SaveBindings_RoleGate(t *testing.T) {
	e := t653BindEnv(t)
	for _, role := range []string{"ROLE_DOCTOR", "ROLE_CS", "technician", "patient", ""} {
		w, _ := e.do(http.MethodPut, "/api/v1/admin/flow/type-bindings",
			map[string]any{"bindings": []map[string]string{{"alertType": "pressure_high", "templateId": t653TplID}}},
			hdr(role, "D0001"))
		assert.Equal(t, http.StatusForbidden, w.Code, "role=%q 应 403", role)
	}
	assert.Empty(t, e.store.auditRows)
}

// ───────────────────────── POST /internal/flow/auto-start ─────────────────────────

func t653AutoBody(alertID, alertType string) map[string]string {
	return map[string]string{"alertId": alertID, "alertType": alertType}
}

func TestT653_AutoStart_Unbound_Skip(t *testing.T) {
	e := t653BindEnv(t) // 无任何绑定行
	w, resp := e.do(http.MethodPost, "/internal/flow/auto-start", t653AutoBody("9527", "pressure_high"), nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var data model.AutoStartFlowResponse
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.False(t, data.Started)
	assert.Equal(t, "unbound", data.Reason)
	assert.Equal(t, int64(0), e.store.flow.lastStartAlertID, "无绑定不得建实例")
}

func TestT653_AutoStart_Bound_StartInstance(t *testing.T) {
	e := t653BindEnv(t)
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: t653TplID, UpdatedBy: "A0001"},
	}
	w, resp := e.do(http.MethodPost, "/internal/flow/auto-start", t653AutoBody("9527", "pressure_high"), nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	var data model.AutoStartFlowResponse
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.True(t, data.Started)
	require.NotNil(t, data.InstanceID)
	assert.Equal(t, "FLOW_I0A1B2C3D4", *data.InstanceID)
	assert.Equal(t, t653TplID, e.store.flow.lastStartTplID)
	assert.Equal(t, int64(9527), e.store.flow.lastStartAlertID)
	assert.ElementsMatch(t, []string{"N1", "N2", "N3"}, e.store.flow.lastStartNodes)
	assert.ElementsMatch(t, []string{"N1"}, e.store.flow.lastStartEntries, "N1 无入边 = 唯一入口")
}

func TestT653_AutoStart_AlreadyExists_Idempotent(t *testing.T) {
	e := t653BindEnv(t)
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: t653TplID},
	}
	e.store.flow.createInstEr = &repo.ErrFlowInstanceExists{Existing: fixFlowInstanceRow()}
	w, resp := e.do(http.MethodPost, "/internal/flow/auto-start", t653AutoBody("9527", "pressure_high"), nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message, "重复触发幂等回 200 而非错误")
	var data model.AutoStartFlowResponse
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.False(t, data.Started)
	assert.Equal(t, "exists", data.Reason)
	require.NotNil(t, data.InstanceID)
	assert.Equal(t, "FLOW_I0A1B2C3D4", *data.InstanceID)
}

func TestT653_AutoStart_AlertNotFound_400(t *testing.T) {
	e := t653BindEnv(t)
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: t653TplID},
	}
	e.store.flow.createInstEr = repo.ErrFlowAlertNotFound
	w, _ := e.do(http.MethodPost, "/internal/flow/auto-start", t653AutoBody("40404", "pressure_high"), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestT653_AutoStart_BoundTemplateDeleted_DegradeUnbound(t *testing.T) {
	e := t653BindEnv(t)
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: "FLOW_TGONE"},
	}
	e.store.flow.tpl = nil
	e.store.flow.tplErr = repo.ErrFlowTemplateNotFound // FK 双保险场景：绑定指向的模板没了
	w, resp := e.do(http.MethodPost, "/internal/flow/auto-start", t653AutoBody("9527", "pressure_high"), nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var data model.AutoStartFlowResponse
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.False(t, data.Started)
	assert.Equal(t, "unbound", data.Reason, "模板损坏按未绑定降级，不向告警链路抛错")
}

func TestT653_AutoStart_InvalidRequest_400(t *testing.T) {
	e := t653BindEnv(t)
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: t653TplID},
	}
	cases := []struct {
		name string
		body map[string]string
	}{
		{"alertId 非数字", t653AutoBody("abc", "pressure_high")},
		{"alertId 非正", t653AutoBody("0", "pressure_high")},
		{"alertType 非法", t653AutoBody("9527", "data_timeout")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := e.do(http.MethodPost, "/internal/flow/auto-start", tc.body, nil)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}
