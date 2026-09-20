//go:build integration
// +build integration

// T274 真库集成测试：migration 000020 落地 + 四表读写 + 节点状态机
//
// 本机无 Docker 时由 CI 的 go-integration job 执行（harness 顺序 apply scripts/db/migrations/*.up.sql，
// 因此本文件同时是 000020 DDL 的落地验证：建不出表 = 全部用例 panic）。
// 状态机的行锁串行化、jsonb 原文保真、外键/CHECK 拦截只能在真 PG 上验，故不放单测。
// 只使用本文件私有的 ID/名称域，避免与 seedITData 及既有用例互污染。
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// itFlowNodes / itFlowEdges 三节点线性流程：分诊 → 工程师处理 → 归档确认
const (
	itFlowNodes = `[{"id":"N1","type":"rect","text":{"value":"分诊"},"properties":{"godl":{"fill":"#EAF3FF"}}},` +
		`{"id":"N2","type":"rect","text":{"value":"矫形工程师处理"}},` +
		`{"id":"N3","type":"circle","text":{"value":"归档确认"}}]`
	itFlowEdges = `[{"id":"E1","type":"polyline","sourceNodeId":"N1","targetNodeId":"N2"},` +
		`{"id":"E2","type":"polyline","sourceNodeId":"N2","targetNodeId":"N3"}]`
)

// itFlowFixture 建一套独占的「设备 → 告警 → 模板」前置数据；alertID 由 IDENTITY 生成后回读
type itFlowFixture struct {
	templateID string
	alertID    int64
}

func newITFlow(t *testing.T, suffix string) *itFlowFixture {
	t.Helper()
	ctx := context.Background()

	// 000020 落地核对：harness 的 apply 若失败会 panic，这里再显式确认四张表都在
	for _, tbl := range []string{"flow_template", "flow_instance", "flow_node_state", "flow_node_action"} {
		var exists bool
		require.NoError(t, itStore.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_tables WHERE tablename = $1)`, tbl).Scan(&exists),
			"pg_tables 查询失败")
		require.True(t, exists, "migration 000020 未建出表 %s", tbl)
	}

	fx := &itFlowFixture{}
	patient := "P-FX-" + suffix
	device := "DEV-FX-" + suffix
	team := "TEAM-FX-" + suffix

	stmts := []string{
		fmt.Sprintf(`INSERT INTO teams (team_id, name, member_count, patient_count) VALUES ('%s', '流程夹具团队', 0, 1)
		 ON CONFLICT (team_id) DO NOTHING`, team),
		fmt.Sprintf(`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, team_id, status) VALUES
		   ('%s', '流程夹具患者', '\x00'::bytea, 'ff%s' || repeat('0', 58), 'male', 12, '%s', 'active')
		 ON CONFLICT (patient_id) DO NOTHING`, patient, suffix, team),
		fmt.Sprintf(`INSERT INTO devices (device_id, device_secret_enc, patient_id, status) VALUES
		   ('%s', '\x00'::bytea, '%s', 'online') ON CONFLICT (device_id) DO NOTHING`, device, patient),
	}
	for _, s := range stmts {
		if _, err := itStore.pool.Exec(ctx, s); err != nil {
			t.Fatalf("it: flow fixture 前置数据: %v", err)
		}
	}
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`INSERT INTO alerts (patient_id, device_id, type, detail, ts)
		 VALUES ($1, $2, 'pressure_high', 'T274 夹具告警', now()) RETURNING alert_id`,
		patient, device).Scan(&fx.alertID), "it: 夹具告警插入失败")

	tpl, err := itStore.CreateFlowTemplate(ctx, "流程夹具模板-"+suffix, itFlowNodes, itFlowEdges, itAdmin)
	require.NoError(t, err)
	fx.templateID = tpl.TemplateID
	return fx
}

// nodeStateOf 取某节点状态行（不存在即 require 失败）
func nodeStateOf(t *testing.T, instanceID, nodeID string) FlowNodeStateRow {
	t.Helper()
	rows, err := itStore.ListNodeStates(context.Background(), instanceID)
	require.NoError(t, err)
	for _, r := range rows {
		if r.NodeID == nodeID {
			return r
		}
	}
	t.Fatalf("instance %s 无节点 %s 的状态行", instanceID, nodeID)
	return FlowNodeStateRow{}
}

func statusOf(t *testing.T, instanceID string) string {
	t.Helper()
	inst, err := itStore.GetFlowInstance(context.Background(), instanceID)
	require.NoError(t, err)
	return inst.Status
}

// ─────────────────────────────────────────────────────────────
// 模板 CRUD
// ─────────────────────────────────────────────────────────────

func TestITT274TemplateRoundTrip(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "T001")

	got, err := itStore.GetFlowTemplate(ctx, fx.templateID)
	require.NoError(t, err)
	assert.Equal(t, "流程夹具模板-T001", got.Name)
	assert.Equal(t, 1, got.Version)
	assert.Equal(t, itAdmin, got.Creator)
	require.NotNil(t, got.CreatorName, "creator 反查 admins.name")
	assert.Equal(t, "集成账号", *got.CreatorName)
	assert.Equal(t, 0, got.InstanceCount)

	// JSONB 原文保真：前端存什么取什么，后端不改写坐标/自定义属性
	var nodes []map[string]any
	require.NoError(t, json.Unmarshal(got.Nodes, &nodes))
	require.Len(t, nodes, 3)
	props, ok := nodes[0]["properties"].(map[string]any)
	require.True(t, ok, "LogicFlow 的 properties 字段必须原样保留")
	assert.Contains(t, props, "godl", "自定义样式属性不被剥离")

	taken, err := itStore.FlowTemplateNameTaken(ctx, "流程夹具模板-T001", "")
	require.NoError(t, err)
	assert.True(t, taken)
	taken, err = itStore.FlowTemplateNameTaken(ctx, "流程夹具模板-T001", fx.templateID)
	require.NoError(t, err)
	assert.False(t, taken, "排除自身后不应判重名")

	// 保存：只改 nodes → version+1，edges 保持原值
	newNodes := `[{"id":"N1","text":{"value":"分诊"}},{"id":"N2","text":{"value":"工程师处理"}}]`
	upd, err := itStore.UpdateFlowTemplate(ctx, fx.templateID, nil, &newNodes, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, upd.Version)
	assert.JSONEq(t, newNodes, string(upd.Nodes))
	assert.JSONEq(t, itFlowEdges, string(upd.Edges), "nil 入参不改该列")

	// 改名
	name := "改名后的模板"
	upd, err = itStore.UpdateFlowTemplate(ctx, fx.templateID, &name, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, name, upd.Name)
	assert.Equal(t, 3, upd.Version)

	_, err = itStore.UpdateFlowTemplate(ctx, "FLOW_T_NOT_EXIST", &name, nil, nil)
	assert.True(t, errors.Is(err, ErrFlowTemplateNotFound), "不存在 → ErrFlowTemplateNotFound，实际 %v", err)
}

func TestITT274TemplateListPagingAndKeyword(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "T002")
	fx2 := newITFlow(t, "T003")

	rows, total, err := itStore.ListFlowTemplates(ctx, "流程夹具模板-T00", 1, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, int64(2))
	for _, r := range rows {
		assert.Contains(t, r.Name, "流程夹具模板-T00")
		assert.JSONEq(t, "[]", string(r.Nodes), "列表不回图数据")
	}

	// 命中集里两条都在
	_, total2, err := itStore.ListFlowTemplates(ctx, "流程夹具模板-T00", 1, 100)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total2, int64(2))
	assert.NotEqual(t, fx.templateID, fx2.templateID, "ID 生成应唯一")

	// 不存在的关键词 → 0 条（且不是错误）
	rows, total, err = itStore.ListFlowTemplates(ctx, "绝不可能命中的关键词", 1, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, rows)
}

func TestITT274TemplateDeleteGuards(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "T004")

	// 无实例引用 → 可删
	require.NoError(t, itStore.DeleteFlowTemplate(ctx, fx.templateID))
	_, err := itStore.GetFlowTemplate(ctx, fx.templateID)
	assert.True(t, errors.Is(err, ErrFlowTemplateNotFound))
	assert.True(t, errors.Is(itStore.DeleteFlowTemplate(ctx, fx.templateID), ErrFlowTemplateNotFound),
		"重复删除 → ErrFlowTemplateNotFound（幂等由 handler 转 404）")

	// 有实例引用 → 在用不可删
	fx2 := newITFlow(t, "T005")
	_, err = itStore.CreateFlowInstance(ctx, fx2.templateID, fx2.alertID, []string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)
	delErr := itStore.DeleteFlowTemplate(ctx, fx2.templateID)
	var inUse *ErrFlowTemplateInUse
	require.True(t, errors.As(delErr, &inUse), "应返回 *ErrFlowTemplateInUse，实际 %v", delErr)
	assert.Equal(t, 1, inUse.InstanceCount)
}

// ─────────────────────────────────────────────────────────────
// 实例启动
// ─────────────────────────────────────────────────────────────

func TestITT274InstanceStartGeneratesStates(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "I001")

	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID, []string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)
	assert.Equal(t, "running", inst.Status)
	assert.Equal(t, fx.templateID, inst.TemplateID)
	assert.Equal(t, "流程夹具模板-I001", inst.TemplateName, "LEFT JOIN 带出模板名")
	require.NotNil(t, inst.CurrentNodeID)
	assert.Equal(t, "N1", *inst.CurrentNodeID)
	assert.Nil(t, inst.EndedAt)

	states, err := itStore.ListNodeStates(ctx, inst.InstanceID)
	require.NoError(t, err)
	require.Len(t, states, 3, "全量节点各生成一行")
	assert.Equal(t, "current", states[0].Status, "起始节点置 current")
	assert.Equal(t, "todo", states[1].Status)
	assert.Equal(t, "todo", states[2].Status)
	assert.Nil(t, states[1].Operator, "未操作节点无操作人")

	// 同告警重复启动 → ErrFlowInstanceExists（alert_id UNIQUE）
	_, err = itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID, []string{"N1"}, []string{"N1"})
	var exists *ErrFlowInstanceExists
	require.True(t, errors.As(err, &exists), "应返回 *ErrFlowInstanceExists，实际 %v", err)
	assert.Equal(t, inst.InstanceID, exists.Existing.InstanceID, "409 要带回已有实例供前端跳转")

	// 不存在的告警 → 外键拦截
	_, err = itStore.CreateFlowInstance(ctx, fx.templateID, 999999999, []string{"N1"}, []string{"N1"})
	assert.True(t, errors.Is(err, ErrFlowAlertNotFound), "应返回 ErrFlowAlertNotFound，实际 %v", err)

	// 按告警反查
	list, err := itStore.ListFlowInstancesByAlert(ctx, fx.alertID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, inst.InstanceID, list[0].InstanceID)
}

// ─────────────────────────────────────────────────────────────
// 状态机：confirm 推进 / reject 跳过 / transfer 换人 / urge 留痕
// ─────────────────────────────────────────────────────────────

func TestITT274ConfirmAdvancesAndCompletes(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "S001")
	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID, []string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)

	remark := "分诊完毕，转工程师"
	attaches := `["FILE_A","FILE_B"]`
	act, err := itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N1", Action: "confirm",
		Operator: itDoctor, Remark: remark, Attachments: attaches,
		NodeStatus: "done", Successors: []string{"N2"}, CurrentNodeID: "N2",
	})
	require.NoError(t, err)
	assert.Greater(t, act.ActionID, int64(0))
	assert.Equal(t, itDoctor, act.Operator)
	require.NotNil(t, act.OperatorName)
	assert.Equal(t, "集成医生", *act.OperatorName)
	assert.JSONEq(t, attaches, string(act.Attachments), "attachments 原文往返")

	assert.Equal(t, "done", nodeStateOf(t, inst.InstanceID, "N1").Status)
	assert.Equal(t, "current", nodeStateOf(t, inst.InstanceID, "N2").Status)
	assert.Equal(t, "todo", nodeStateOf(t, inst.InstanceID, "N3").Status, "后继只推进一层")
	n1 := nodeStateOf(t, inst.InstanceID, "N1")
	require.NotNil(t, n1.Remark)
	assert.Equal(t, remark, *n1.Remark)
	require.NotNil(t, n1.OperatedAt)

	cur, err := itStore.GetFlowInstance(ctx, inst.InstanceID)
	require.NoError(t, err)
	assert.Equal(t, "N2", *cur.CurrentNodeID)

	// N2 → N3
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N2", Action: "confirm", Operator: itAdmin,
		NodeStatus: "done", Successors: []string{"N3"}, CurrentNodeID: "N3",
	})
	require.NoError(t, err)

	// 末节点：无后继 → Complete
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N3", Action: "confirm", Operator: itAdmin,
		NodeStatus: "done", CurrentNodeID: "N3", Complete: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "completed", statusOf(t, inst.InstanceID))
	done, err := itStore.GetFlowInstance(ctx, inst.InstanceID)
	require.NoError(t, err)
	require.NotNil(t, done.EndedAt, "completed 必须落 ended_at")

	// 已完结实例再操作 → ErrFlowInstanceCompleted（流水不得增加）
	before := len(itFlowActions(t, inst.InstanceID))
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N1", Action: "confirm", Operator: itAdmin, NodeStatus: "done",
	})
	assert.True(t, errors.Is(err, ErrFlowInstanceCompleted), "应返回 ErrFlowInstanceCompleted，实际 %v", err)
	assert.Len(t, itFlowActions(t, inst.InstanceID), before, "事务回滚：失败操作不留流水")
}

func itFlowActions(t *testing.T, instanceID string) []FlowNodeActionRow {
	t.Helper()
	rows, err := itStore.ListFlowNodeActions(context.Background(), instanceID)
	require.NoError(t, err)
	return rows
}

func TestITT274RejectSkipsWithoutAdvance(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "S002")
	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID, []string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)

	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N1", Action: "reject",
		Operator: itAdmin, Remark: "误报", NodeStatus: "skipped",
	})
	require.NoError(t, err)
	assert.Equal(t, "skipped", nodeStateOf(t, inst.InstanceID, "N1").Status)
	assert.Equal(t, "todo", nodeStateOf(t, inst.InstanceID, "N2").Status, "驳回不推进后继")
	assert.Equal(t, "running", statusOf(t, inst.InstanceID))
}

func TestITT274OnlyCurrentNodeCanBeConfirmed(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "S003")
	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID, []string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)

	for _, action := range []string{"confirm", "reject"} {
		_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
			InstanceID: inst.InstanceID, NodeID: "N3", Action: action,
			Operator: itAdmin, NodeStatus: "done",
		})
		var notCur *ErrFlowNodeNotCurrent
		require.True(t, errors.As(err, &notCur), "%s 非 current 节点应返回 *ErrFlowNodeNotCurrent，实际 %v", action, err)
		assert.Equal(t, "N3", notCur.NodeID)
		assert.Equal(t, "todo", notCur.Status)
	}

	// 不存在的节点
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N9", Action: "confirm", Operator: itAdmin, NodeStatus: "done",
	})
	assert.True(t, errors.Is(err, ErrFlowNodeNotFound), "应返回 ErrFlowNodeNotFound，实际 %v", err)

	// 不存在的实例
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: "FLOW_I_NOPE", NodeID: "N1", Action: "confirm", Operator: itAdmin, NodeStatus: "done",
	})
	assert.True(t, errors.Is(err, ErrFlowInstanceNotFound), "应返回 ErrFlowInstanceNotFound，实际 %v", err)
}

func TestITT274TransferAndUrgeDoNotMoveState(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "S004")
	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID, []string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)

	// 转派：只换 assignee，状态与操作人都不动
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N1", Action: "transfer",
		Operator: itAdmin, Remark: "转给技师", TargetOperator: itDoctor,
	})
	require.NoError(t, err)
	st := nodeStateOf(t, inst.InstanceID, "N1")
	assert.Equal(t, "current", st.Status)
	require.NotNil(t, st.Assignee)
	assert.Equal(t, itDoctor, *st.Assignee)
	require.NotNil(t, st.AssigneeName, "assignee 反查 doctors.name")
	assert.Equal(t, "集成医生", *st.AssigneeName)
	assert.Nil(t, st.Operator, "转派不产生处理人")

	// 加急：只留痕
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N1", Action: "urge", Operator: itAdmin, Remark: "家属催办",
	})
	require.NoError(t, err)
	st2 := nodeStateOf(t, inst.InstanceID, "N1")
	assert.Equal(t, "current", st2.Status)
	assert.Nil(t, st2.OperatedAt, "加急不记操作时间")
	assert.Equal(t, itDoctor, *st2.Assignee, "加急不会冲掉此前的转派")

	// 时间线：三次操作按序可查，transfer 行带 target_operator
	rows := itFlowActions(t, inst.InstanceID)
	require.Len(t, rows, 2, "transfer + urge 各一行，confirm 未发生")
	assert.Equal(t, "transfer", rows[0].Action)
	require.NotNil(t, rows[0].TargetOperator)
	assert.Equal(t, itDoctor, *rows[0].TargetOperator)
	assert.Equal(t, "urge", rows[1].Action)
	assert.JSONEq(t, "[]", string(rows[1].Attachments), "未给附件 → 空数组而非 null")
	assert.Greater(t, rows[1].ActionID, rows[0].ActionID, "action_id 递增即时间线顺序")
}

func TestITT274ParallelEntryNodes(t *testing.T) {
	// 并行分叉：两个起始节点同时 current，指针取字典序首个（可重现，不乱跳）
	ctx := context.Background()
	fx := newITFlow(t, "S005")
	branchNodes := `[{"id":"B1"},{"id":"B2"},{"id":"B3"}]`
	branchEdges := `[{"sourceNodeId":"B1","targetNodeId":"B3"},{"sourceNodeId":"B2","targetNodeId":"B3"}]`
	_, err := itStore.UpdateFlowTemplate(ctx, fx.templateID, nil, &branchNodes, &branchEdges)
	require.NoError(t, err)

	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID, []string{"B1", "B2", "B3"}, []string{"B1", "B2"})
	require.NoError(t, err)
	assert.Equal(t, "B1", *inst.CurrentNodeID)
	assert.Equal(t, "current", nodeStateOf(t, inst.InstanceID, "B1").Status)
	assert.Equal(t, "current", nodeStateOf(t, inst.InstanceID, "B2").Status)

	// 一条分支走完，另一条仍是 current：后继已是 current 则不被改回
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "B1", Action: "confirm", Operator: itAdmin,
		NodeStatus: "done", Successors: []string{"B3"}, CurrentNodeID: "B2",
	})
	require.NoError(t, err)
	assert.Equal(t, "current", nodeStateOf(t, inst.InstanceID, "B2").Status)
	assert.Equal(t, "current", nodeStateOf(t, inst.InstanceID, "B3").Status)
}

func TestITT274CheckConstraintsRejectBadEnums(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "C001")
	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID, []string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)

	// 越过 handler 直写脏枚举：CHECK 必须拦下（枚举是 DB 侧契约，不只在前端）
	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO flow_node_action (instance_id, node_id, action, operator) VALUES ($1, 'N1', 'approve', $2)`,
		inst.InstanceID, itAdmin)
	require.Error(t, err, "action CHECK 应拒绝枚举外的值")
	assert.Contains(t, err.Error(), "flow_node_action_action_check")

	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO flow_node_state (instance_id, node_id, status) VALUES ($1, 'NX', 'in_progress')`,
		inst.InstanceID)
	require.Error(t, err, "status CHECK 应拒绝枚举外的值")
	assert.Contains(t, err.Error(), "flow_node_state_status_check")

	// 复合主键：同实例同节点不得两行
	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO flow_node_state (instance_id, node_id, status) VALUES ($1, 'N1', 'todo')`, inst.InstanceID)
	require.Error(t, err, "PK(instance_id,node_id) 应拒绝重复节点行")

	// 节点状态行随实例级联删除（ON DELETE CASCADE）
	_, err = itStore.pool.Exec(ctx, `DELETE FROM flow_instance WHERE instance_id = $1`, inst.InstanceID)
	require.NoError(t, err)
	remaining := itCountRows(t, `SELECT COUNT(*) FROM flow_node_state WHERE instance_id = $1`, inst.InstanceID)
	assert.Zero(t, remaining, "删实例应级联清掉节点状态，避免脏 current")
}

func itCountRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, itStore.pool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}
