// 677 快处路由端点测试（POST /internal/flow/quick-route）：
//   - 不可绑定类型（存量 pressure_fluctuation）/ 无绑定行 → direct（最小闭环：无绑定保持现状）
//   - 有绑定 → 建实例（幂等回既有 id）→ flow + instanceId
//   - alreadyProcessed 透传；告警不存在 400；读库失败 500；模板被删按未绑定降级 direct
//
// 建实例/绑定 SQL 语义由 repo 层 testcontainers 集成测试覆盖（CI go-integration）。
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// quickRouteData 与 quickRouteResponse 同形的解析壳。
type quickRouteData struct {
	Route            string  `json:"route"`
	InstanceID       *string `json:"instanceId,omitempty"`
	AlreadyProcessed bool    `json:"alreadyProcessed"`
}

func t677Route(t *testing.T, e *testEnv, alertID any) (int, quickRouteData, string) {
	t.Helper()
	w, resp := e.do(http.MethodPost, "/internal/flow/quick-route", map[string]any{"alertId": alertID}, nil)
	var data quickRouteData
	if len(resp.Data) > 0 {
		require.NoError(t, json.Unmarshal(resp.Data, &data))
	}
	return w.Code, data, resp.Message
}

// t677SeenAlert 预置「告警存在且可读」。
func t677SeenAlert(e *testEnv, typeName, status string) {
	e.store.flow.routeExists = true
	e.store.flow.routeTypeName = typeName
	e.store.flow.routeStatus = status
}

func TestT677_QuickRoute_UnbindableType_Direct(t *testing.T) {
	e := flowEnv(t)
	// 存量 pressure_fluctuation：不在 FlowBindAlertTypes 可绑定集 → 永远进不了绑定表。
	t677SeenAlert(e, "pressure_fluctuation", "pending")
	code, data, msg := t677Route(t, e, 9527)
	require.Equal(t, http.StatusOK, code, msg)
	assert.Equal(t, "direct", data.Route)
	assert.False(t, data.AlreadyProcessed)
	assert.Equal(t, int64(0), e.store.flow.lastStartAlertID, "不可绑定类型不得建实例")
	assert.Equal(t, 1, e.store.flow.routeHitCount, "应读过告警一次")
	assert.Equal(t, int64(9527), e.store.flow.routeAlertID, "入参透传")
}

func TestT677_QuickRoute_BindableButUnbound_Direct(t *testing.T) {
	e := flowEnv(t)
	t677SeenAlert(e, "pressure_high", "pending") // bindings 空 = 无绑定行
	code, data, msg := t677Route(t, e, 9527)
	require.Equal(t, http.StatusOK, code, msg)
	assert.Equal(t, "direct", data.Route)
	assert.Equal(t, int64(0), e.store.flow.lastStartAlertID, "无绑定不得建实例（保持现状）")
}

func TestT677_QuickRoute_Bound_CreatesInstance_Flow(t *testing.T) {
	e := flowEnv(t)
	t677SeenAlert(e, "pressure_high", "pending")
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: t653TplID, UpdatedBy: "A0001"},
	}
	code, data, msg := t677Route(t, e, 9527)
	require.Equal(t, http.StatusOK, code, msg)
	assert.Equal(t, "flow", data.Route)
	require.NotNil(t, data.InstanceID, "flow 必须带 instanceId 供调用方转流程")
	assert.Equal(t, "FLOW_I0A1B2C3D4", *data.InstanceID)
	assert.False(t, data.AlreadyProcessed)
	assert.Equal(t, int64(9527), e.store.flow.lastStartAlertID, "绑定未建实例时此处补建（同 auto-start 幂等）")
}

func TestT677_QuickRoute_BoundInstanceExists_IdempotentFlow(t *testing.T) {
	e := flowEnv(t)
	t677SeenAlert(e, "pressure_high", "processing")
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: t653TplID},
	}
	e.store.flow.createInstEr = &repo.ErrFlowInstanceExists{Existing: fixFlowInstanceRow()}
	code, data, msg := t677Route(t, e, 9527)
	require.Equal(t, http.StatusOK, code, msg)
	assert.Equal(t, "flow", data.Route)
	require.NotNil(t, data.InstanceID)
	assert.Equal(t, "FLOW_I0A1B2C3D4", *data.InstanceID, "已有实例幂等回既有 id")
	assert.False(t, data.AlreadyProcessed, "processing 不算已办结")
}

func TestT677_QuickRoute_AlreadyProcessed_Flag(t *testing.T) {
	e := flowEnv(t)
	t677SeenAlert(e, "pressure_high", "processed")
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: t653TplID},
	}
	code, data, msg := t677Route(t, e, 9527)
	require.Equal(t, http.StatusOK, code, msg)
	assert.Equal(t, "flow", data.Route, "路由判定照回；是否 409 由调用方按 alreadyProcessed 决定")
	assert.True(t, data.AlreadyProcessed, "已 processed 必须透传，保住快处幂等语义")
}

func TestT677_QuickRoute_AlertNotFound_400(t *testing.T) {
	e := flowEnv(t)
	// routeExists 零值 = 告警不存在
	code, _, _ := t677Route(t, e, 40404)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestT677_QuickRoute_RouteInfoReadError_500(t *testing.T) {
	e := flowEnv(t)
	e.store.flow.routeExists = true
	e.store.flow.routeTypeName = "pressure_high"
	e.store.flow.routeErr = repo.ErrFlowTemplateNotFound // 任意非 nil 错即可
	code, _, _ := t677Route(t, e, 9527)
	assert.Equal(t, http.StatusInternalServerError, code)
}

func TestT677_QuickRoute_TemplateDeleted_DegradeDirect(t *testing.T) {
	e := flowEnv(t)
	t677SeenAlert(e, "pressure_high", "pending")
	e.store.flow.bindings = map[string]repo.FlowTypeBindingRow{
		"pressure_high": {AlertType: "pressure_high", TemplateID: "FLOW_TGONE"},
	}
	e.store.flow.tpl = nil
	e.store.flow.tplErr = repo.ErrFlowTemplateNotFound
	code, data, msg := t677Route(t, e, 9527)
	require.Equal(t, http.StatusOK, code, msg)
	assert.Equal(t, "direct", data.Route, "模板损坏按未绑定降级，与 auto-start 同口径")
}

func TestT677_QuickRoute_InvalidBody_400(t *testing.T) {
	e := flowEnv(t)
	w, _ := e.do(http.MethodPost, "/internal/flow/quick-route", map[string]any{"alertId": "not-a-number"}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = e.do(http.MethodPost, "/internal/flow/quick-route", map[string]any{"alertId": -7}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, "非正 alertId 拒 400")
	assert.Zero(t, e.store.flow.routeHitCount, "入参非法不得打到库")
}
