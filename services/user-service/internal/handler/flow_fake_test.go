// T274 流程画布 handler 单测的 fake 状态（由 handler_impl_test.go 的 fakeStore 嵌入字段 flow 持有）
//
// 口径：这里只做「返回值 / 错误注入 / 入参记录」，不复制 repo 层状态机 ——
// 状态机（节点推进、行锁、唯一冲突）由 repo/flow_test.go 的 testcontainers 集成测试覆盖（CI 跑）。
package handler

import (
	"context"
	"time"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

type fakeFlowState struct {
	listTemplates     []repo.FlowTemplateRow
	listTemplateTotal int64
	listTemplateErr   error

	tpl         *repo.FlowTemplateRow
	tplErr      error
	nameTaken   bool
	nameTakenEr error
	createdTpl  *repo.FlowTemplateRow
	createEr    error
	updatedTpl  *repo.FlowTemplateRow
	updateEr    error
	deleteEr    error

	lastTplName    string
	lastTplNodes   string
	lastTplEdges   string
	lastTplCreator string
	lastUpdTplID   string
	lastUpdName    *string
	lastUpdNodes   *string
	lastUpdEdges   *string

	inst         *repo.FlowInstanceRow
	instErr      error
	byAlert      []repo.FlowInstanceRow
	byAlertErr   error
	createdInst  *repo.FlowInstanceRow
	createInstEr error

	lastStartTplID   string
	lastStartAlertID int64
	lastStartNodes   []string
	lastStartEntries []string

	nodes    []repo.FlowNodeStateRow
	nodesErr error

	applied    *repo.FlowNodeActionRow
	applyErr   error
	lastAction repo.FlowActionWrite

	actions    []repo.FlowNodeActionRow
	actionsErr error

	// T653 类型绑定：bindings 为现存绑定行（key=alertType），Get 缺行回 (nil,nil)；
	// Replace 按项写 map（空 templateId 删除），failReplace=true 时返回 ErrFlowTemplateNotFound。
	bindings      map[string]repo.FlowTypeBindingRow
	failReplace   bool
	replacedItems []repo.FlowTypeBindingSet

	// 677 快处路由：AlertRouteInfo 替身（零值 = 告警不存在；routeErr 注入读失败）。
	routeTypeName string
	routeStatus   string
	routeExists   bool
	routeErr      error
	routeAlertID  int64 // 最近一次入参记录
	routeHitCount int

	// T681 夹具清理：cleanupRes/cleanupErr 为返回值注入，lastCleanup* 记录入参。
	cleanupRes        *repo.FlowFixtureCleanupResult
	cleanupErr        error
	lastCleanupPrefix string
	lastCleanupAfter  time.Time
	// T679 医护待办读侧：todos 为待办行（含详情单行）；记录 filter 供断言团队/实例谓词。
	todos          []repo.FlowTodoRow
	todosErr       error
	lastTodoFilter repo.FlowTodoFilter

	// T687 账号形态归一：accountIDs 为 raw→canonical 映射（缺项原样透传，同 PGStore 三级解析语义）；
	// resolveErr 注入解析失败（handler 侧应回退原值，不得把失败放大成新 403/500）。
	accountIDs map[string]string
	resolveErr error
	resolveHit []string // 入参记录（断言归一发生在正确的时机）
}

// ResolveFlowAccountID T687 账号形态归一替身：命中映射回 canonical，缺项/出错按 PGStore 语义
// 分别为原样透传 / 由调用方回退（此处返回空串+err，handler 回退原值）。
func (f *fakeStore) ResolveFlowAccountID(_ context.Context, raw string) (string, error) {
	f.flow.resolveHit = append(f.flow.resolveHit, raw)
	if f.flow.resolveErr != nil {
		return "", f.flow.resolveErr
	}
	if v, ok := f.flow.accountIDs[raw]; ok {
		return v, nil
	}
	return raw, nil
}

// AlertRouteInfo 677 快处路由判定的跨 owner 只读替身。
func (f *fakeStore) AlertRouteInfo(_ context.Context, alertID int64) (string, string, bool, error) {
	f.flow.routeAlertID = alertID
	f.flow.routeHitCount++
	return f.flow.routeTypeName, f.flow.routeStatus, f.flow.routeExists, f.flow.routeErr
}

func (f *fakeStore) ListFlowTemplates(_ context.Context, _ string, _, _ int) ([]repo.FlowTemplateRow, int64, error) {
	return f.flow.listTemplates, f.flow.listTemplateTotal, f.flow.listTemplateErr
}

func (f *fakeStore) GetFlowTemplate(_ context.Context, _ string) (*repo.FlowTemplateRow, error) {
	return f.flow.tpl, f.flow.tplErr
}

func (f *fakeStore) FlowTemplateNameTaken(_ context.Context, _, _ string) (bool, error) {
	return f.flow.nameTaken, f.flow.nameTakenEr
}

func (f *fakeStore) CreateFlowTemplate(_ context.Context, name, nodesJSON, edgesJSON, creator string) (*repo.FlowTemplateRow, error) {
	f.flow.lastTplName, f.flow.lastTplNodes, f.flow.lastTplEdges, f.flow.lastTplCreator = name, nodesJSON, edgesJSON, creator
	return f.flow.createdTpl, f.flow.createEr
}

func (f *fakeStore) UpdateFlowTemplate(_ context.Context, templateID string, name, nodesJSON, edgesJSON *string) (*repo.FlowTemplateRow, error) {
	f.flow.lastUpdTplID, f.flow.lastUpdName, f.flow.lastUpdNodes, f.flow.lastUpdEdges = templateID, name, nodesJSON, edgesJSON
	return f.flow.updatedTpl, f.flow.updateEr
}

func (f *fakeStore) DeleteFlowTemplate(_ context.Context, _ string) error {
	return f.flow.deleteEr
}

func (f *fakeStore) CreateFlowInstance(_ context.Context, templateID string, alertID int64, nodeIDs, entryNodeIDs []string) (*repo.FlowInstanceRow, error) {
	f.flow.lastStartTplID, f.flow.lastStartAlertID = templateID, alertID
	f.flow.lastStartNodes, f.flow.lastStartEntries = nodeIDs, entryNodeIDs
	return f.flow.createdInst, f.flow.createInstEr
}

func (f *fakeStore) GetFlowInstance(_ context.Context, _ string) (*repo.FlowInstanceRow, error) {
	return f.flow.inst, f.flow.instErr
}

func (f *fakeStore) ListFlowInstancesByAlert(_ context.Context, _ int64) ([]repo.FlowInstanceRow, error) {
	return f.flow.byAlert, f.flow.byAlertErr
}

func (f *fakeStore) ListNodeStates(_ context.Context, _ string) ([]repo.FlowNodeStateRow, error) {
	return f.flow.nodes, f.flow.nodesErr
}

func (f *fakeStore) ApplyFlowNodeAction(_ context.Context, in repo.FlowActionWrite) (*repo.FlowNodeActionRow, error) {
	f.flow.lastAction = in
	return f.flow.applied, f.flow.applyErr
}

func (f *fakeStore) ListFlowNodeActions(_ context.Context, _ string) ([]repo.FlowNodeActionRow, error) {
	return f.flow.actions, f.flow.actionsErr
}

func (f *fakeStore) CleanupFlowFixtures(_ context.Context, templatePrefix string, createdAfter time.Time) (*repo.FlowFixtureCleanupResult, error) {
	f.flow.lastCleanupPrefix, f.flow.lastCleanupAfter = templatePrefix, createdAfter
	return f.flow.cleanupRes, f.flow.cleanupErr
}

func (f *fakeStore) ListFlowTodos(_ context.Context, in repo.FlowTodoFilter) ([]repo.FlowTodoRow, error) {
	f.flow.lastTodoFilter = in
	return f.flow.todos, f.flow.todosErr
}
