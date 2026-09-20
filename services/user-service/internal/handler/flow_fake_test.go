// T274 流程画布 handler 单测的 fake 状态（由 handler_impl_test.go 的 fakeStore 嵌入字段 flow 持有）
//
// 口径：这里只做「返回值 / 错误注入 / 入参记录」，不复制 repo 层状态机 ——
// 状态机（节点推进、行锁、唯一冲突）由 repo/flow_test.go 的 testcontainers 集成测试覆盖（CI 跑）。
package handler

import (
	"context"

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
