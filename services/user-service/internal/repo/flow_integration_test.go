//go:build integration
// +build integration

// T274 真库集成测试：migration 000020 落地 + 四表读写 + 节点状态机
//
// 本机无 Docker 时由 CI 的 go-integration job 执行（harness 顺序 apply scripts/db/migrations/*.up.sql，
// 因此本文件同时是 000020 DDL 的落地验证：建不出表 = 全部用例 panic）。
// 状态机的行锁串行化、jsonb 原文保真、外键/CHECK 拦截只能在真 PG 上验，故不放单测。
// 模板名/ID 只用本文件私有的「流程夹具模板-」域；患者与设备反过来复用 seedITData 的既有行，
// 以免顶翻同包计数类用例（详见 itFlowPatient / itFlowDevice 注释）。
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

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

// itFlowFixture 建一套独占的「告警 → 模板」前置数据（设备全夹具共用一台，见 itFlowDevice）
//
// 患者/设备都不按用例新建，原因都是真库实测出来的：
//   - patients：同包 TestITListPatientsJoinAndFilter 断言全表计数，夹具自建患者会把它顶翻（2 → 14）；
//   - devices：000012 的 uk_devices_active_patient = UNIQUE devices(patient_id) WHERE patient_id IS NOT NULL，
//     一个患者只允许挂一台设备 ⇒ 都复用 P-USR-IT-1 后，每套夹具再插一台必撞 23505。
type itFlowFixture struct {
	templateID string
	alertID    int64
	alertIDs   []int64
}

const (
	itFlowPatient = "P-USR-IT-1" // seedITData 里 status=active 的既有患者
	itFlowDevice  = "DEV-FX-FLOW"
)

// itFlowAlertSeq 全夹具共用的递增序号：告警们共用同一 (patient, device, type)，
// 而 alerts 上有 uk_alerts_natural(patient_id,device_id,type,ts) ⇒ ts 必须逐条推开。
var itFlowAlertSeq atomic.Int64

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

	if _, err := itStore.pool.Exec(ctx,
		`INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		 VALUES ($1, '\x00'::bytea, $2, 'online') ON CONFLICT (device_id) DO NOTHING`,
		itFlowDevice, itFlowPatient); err != nil {
		t.Fatalf("it: flow fixture 前置数据: %v", err)
	}
	fx := &itFlowFixture{}
	fx.alertID = fx.newAlert(t, "T274 夹具告警")

	// 全用例跑在同一个库上：夹具写过的告警/实例必须撤回（设备是全夹具共用的一行，留着）
	t.Cleanup(func() {
		c := context.Background()
		for _, s := range []string{
			`DELETE FROM flow_instance WHERE alert_id = ANY($1)`, // 子行由 ON DELETE CASCADE 跟上
			`DELETE FROM alerts WHERE alert_id = ANY($1)`,
		} {
			if _, err := itStore.pool.Exec(c, s, fx.alertIDs); err != nil {
				t.Errorf("it: flow fixture 清理: %v", err)
			}
		}
	})

	tpl, err := itStore.CreateFlowTemplate(ctx, "流程夹具模板-"+suffix, itFlowNodes, itFlowEdges, itAdmin)
	require.NoError(t, err)
	fx.templateID = tpl.TemplateID
	return fx
}

// newAlert 在共用设备下追加一条告警，ts 按全局序号推开以躲开 uk_alerts_natural
func (fx *itFlowFixture) newAlert(t *testing.T, detail string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`INSERT INTO alerts (patient_id, device_id, type, detail, ts)
		 VALUES ($1, $2, 'pressure_high', $3, now() + make_interval(secs => $4::int)) RETURNING alert_id`,
		itFlowPatient, itFlowDevice, detail, int(itFlowAlertSeq.Add(1))).Scan(&id), "it: 夹具告警插入失败")
	fx.alertIDs = append(fx.alertIDs, id)
	return id
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

	// 不存在的告警 → repo 层存在性校验拦截（alerts 上无外键，见 000020）
	_, err = itStore.CreateFlowInstance(ctx, fx.templateID, 999999999, []string{"N1"}, []string{"N1"})
	assert.True(t, errors.Is(err, ErrFlowAlertNotFound), "应返回 ErrFlowAlertNotFound，实际 %v", err)

	// 不存在的模板（告警合法）→ 唯一的 template_id 外键，必须回 ErrFlowTemplateNotFound，
	// 否则「模板刚被并发删除」会被报成「告警不存在」，前端排查方向整个错掉。
	// 🔴 要用一条没有实例的干净告警：复用 fx.alertID 时 alert_id UNIQUE(23505) 先触发，只会回 409。
	_, err = itStore.CreateFlowInstance(ctx, "FLOW_T_NOT_EXIST",
		fx.newAlert(t, "T274 模板外键分支告警"), []string{"N1"}, []string{"N1"})
	assert.True(t, errors.Is(err, ErrFlowTemplateNotFound), "应返回 ErrFlowTemplateNotFound，实际 %v", err)
	assert.NotErrorIs(t, err, ErrFlowAlertNotFound)

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

	// 'doing' 而非 'in_progress'：status 是 VARCHAR(8)，超长先撞 22001 长度错，
	// 就测不到「枚举外取值被 CHECK 拦下」这条契约了。
	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO flow_node_state (instance_id, node_id, status) VALUES ($1, 'NX', 'doing')`,
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

// ─────────────────────────────────────────────────────────────
// 677 甲案：末节点完结同事务回写 alerts（正例一致性 + 负例回滚两侧都不落）
// ─────────────────────────────────────────────────────────────

// itAlertProcessRow alerts 处理态投影（677 回写断言用；可空列按 NULL 扫指针）
type itAlertProcessRow struct {
	Status       string
	ProcessedBy  *string
	ProcessedAt  *time.Time
	ProcessNote  *string
	InProgressAt *time.Time
}

func itAlertProcess(t *testing.T, alertID int64) itAlertProcessRow {
	t.Helper()
	var r itAlertProcessRow
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT process_status, processed_by, processed_at, process_note, in_progress_at
		   FROM alerts WHERE alert_id = $1`, alertID).Scan(
		&r.Status, &r.ProcessedBy, &r.ProcessedAt, &r.ProcessNote, &r.InProgressAt), "读告警处理态失败")
	return r
}

// walkITFlowToLast 按序确认 confirms 各节点（链条内逐个后继推进），
// 最后一个确认的后继置为 finalCurrent——即把 current 推到 finalCurrent 但不完结。
func walkITFlowToLast(t *testing.T, instanceID string, confirms []string, finalCurrent, operator string) {
	t.Helper()
	ctx := context.Background()
	for i, node := range confirms {
		next := finalCurrent
		if i+1 < len(confirms) {
			next = confirms[i+1]
		}
		_, err := itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
			InstanceID: instanceID, NodeID: node, Action: "confirm", Operator: operator,
			NodeStatus: "done", Successors: []string{next}, CurrentNodeID: next,
		})
		require.NoError(t, err, "走 %s→%s 失败", node, next)
	}
}

// TestIT677FlowCompleteWritesBackAlert 正例：流程终态与 alert 处理态一致。
// 中间节点确认不碰 alerts；末节点完结同事务落 processed（对齐 ProcessAlert first-wins 字段语义）。
func TestIT677FlowCompleteWritesBackAlert(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "T677W")
	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID,
		[]string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)

	walkITFlowToLast(t, inst.InstanceID, []string{"N1", "N2"}, "N3", itDoctor)
	mid := itAlertProcess(t, fx.alertID)
	assert.Equal(t, "pending", mid.Status, "中间节点确认不得回写告警（只在末节点完结分支）")

	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N3", Action: "confirm", Operator: itAdmin,
		NodeStatus: "done", CurrentNodeID: "N3", Complete: true,
	})
	require.NoError(t, err)

	assert.Equal(t, "completed", statusOf(t, inst.InstanceID))
	done, err := itStore.GetFlowInstance(ctx, inst.InstanceID)
	require.NoError(t, err)
	require.NotNil(t, done.EndedAt)

	got := itAlertProcess(t, fx.alertID)
	assert.Equal(t, "processed", got.Status, "流程终态必须回写告警处理态")
	require.NotNil(t, got.ProcessedAt, "processed_at 必须落")
	assert.Equal(t, itAdmin, *got.ProcessedBy, "回写取末节点确认人")
	assert.Nil(t, got.ProcessNote, "process_note 不归回写管（处置语义在流程时间线）")
	assert.Nil(t, got.InProgressAt, "回写不冒充「开始处理」时刻")
}

// TestIT677WriteBackKeepsFirstProcessor 正例旁支：告警已被快处先标 processed 的，
// 完结回写不得改写首次处置者/时刻/备注（与 alert-service ProcessAlert 幂等语义同规则）。
func TestIT677WriteBackKeepsFirstProcessor(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "T677F")
	_, err := itStore.pool.Exec(ctx,
		`UPDATE alerts SET process_status = 'processed',
		        processed_at = now() - interval '1 hour',
		        processed_by = 'QK-QUICK', process_note = '快处先行备注'
		  WHERE alert_id = $1`, fx.alertID)
	require.NoError(t, err, "预置快处已处理失败")
	before := itAlertProcess(t, fx.alertID)
	require.Equal(t, "processed", before.Status)

	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID,
		[]string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)
	walkITFlowToLast(t, inst.InstanceID, []string{"N1", "N2"}, "N3", itDoctor)
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N3", Action: "confirm", Operator: itAdmin,
		NodeStatus: "done", CurrentNodeID: "N3", Complete: true,
	})
	require.NoError(t, err)

	after := itAlertProcess(t, fx.alertID)
	assert.Equal(t, "processed", after.Status)
	require.NotNil(t, after.ProcessedBy)
	assert.Equal(t, "QK-QUICK", *after.ProcessedBy, "first-wins：首次处置者不被回写覆盖")
	require.NotNil(t, after.ProcessedAt)
	assert.True(t, after.ProcessedAt.Equal(*before.ProcessedAt), "first-wins：首次处置时刻不被回写刷新")
	require.NotNil(t, after.ProcessNote)
	assert.Equal(t, "快处先行备注", *after.ProcessNote)
}

// TestIT677CompleteRollbackLeavesBothSides 负例：完结回写途中失败 → 整事务回滚，
// 流程不落终态、告警不落 processed（证明回写与流程推进同事务，不是事外补写）。
// 注入手段：仅对本夹具告警的 processed UPDATE 抛错的测试域触发器，t.Cleanup 必拆。
func TestIT677CompleteRollbackLeavesBothSides(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "T677R")
	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID,
		[]string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)
	walkITFlowToLast(t, inst.InstanceID, []string{"N1", "N2"}, "N3", itDoctor)

	_, err = itStore.pool.Exec(ctx,
		`CREATE FUNCTION it677_fail_processed() RETURNS trigger LANGUAGE plpgsql AS
		 $$ BEGIN RAISE EXCEPTION 'it677 injected failure'; END $$`)
	require.NoError(t, err, "建注入函数失败")
	_, err = itStore.pool.Exec(ctx,
		`CREATE TRIGGER it677_fail BEFORE UPDATE ON alerts FOR EACH ROW
		 WHEN (NEW.alert_id = `+fmt.Sprintf("%d", fx.alertID)+` AND NEW.process_status = 'processed')
		 EXECUTE FUNCTION it677_fail_processed()`)
	require.NoError(t, err, "建注入触发器失败")
	t.Cleanup(func() {
		c := context.Background()
		if _, e := itStore.pool.Exec(c, `DROP TRIGGER IF EXISTS it677_fail ON alerts`); e != nil {
			t.Errorf("it: 拆注入触发器: %v", e)
		}
		if _, e := itStore.pool.Exec(c, `DROP FUNCTION IF EXISTS it677_fail_processed()`); e != nil {
			t.Errorf("it: 拆注入函数: %v", e)
		}
	})

	actionsBefore := len(itFlowActions(t, inst.InstanceID))
	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N3", Action: "confirm", Operator: itAdmin,
		NodeStatus: "done", CurrentNodeID: "N3", Complete: true,
	})
	require.Error(t, err, "回写被注入失败时完结必须报错")
	assert.Contains(t, err.Error(), "it677 injected failure")

	// 侧一：流程不落终态
	assert.Equal(t, "running", statusOf(t, inst.InstanceID), "回滚后实例不得是 completed")
	rollback, err := itStore.GetFlowInstance(ctx, inst.InstanceID)
	require.NoError(t, err)
	assert.Nil(t, rollback.EndedAt, "回滚后 ended_at 不得落")
	assert.Equal(t, "current", nodeStateOf(t, inst.InstanceID, "N3").Status, "末节点状态随事务回滚")
	assert.Len(t, itFlowActions(t, inst.InstanceID), actionsBefore, "回滚后不得留 N3 流水")

	// 侧二：告警不落 processed
	got := itAlertProcess(t, fx.alertID)
	assert.Equal(t, "pending", got.Status, "回滚后告警不得是 processed")
	assert.Nil(t, got.ProcessedAt, "回滚后 processed_at 不得落")
	assert.Nil(t, got.ProcessedBy, "回滚后 processed_by 不得落")
}

// TestIT677CompleteMissingAlertRollsBack 负例支：完结时告警行缺失（数据漂移）→
// 返回 ErrFlowAlertNotFound，流程同样不落终态——宁可报错回滚，不许流程走了告警没跟上。
func TestIT677CompleteMissingAlertRollsBack(t *testing.T) {
	ctx := context.Background()
	fx := newITFlow(t, "T677M")
	inst, err := itStore.CreateFlowInstance(ctx, fx.templateID, fx.alertID,
		[]string{"N1", "N2", "N3"}, []string{"N1"})
	require.NoError(t, err)
	walkITFlowToLast(t, inst.InstanceID, []string{"N1", "N2"}, "N3", itDoctor)

	_, err = itStore.pool.Exec(ctx, `DELETE FROM alerts WHERE alert_id = $1`, fx.alertID)
	require.NoError(t, err)

	_, err = itStore.ApplyFlowNodeAction(ctx, FlowActionWrite{
		InstanceID: inst.InstanceID, NodeID: "N3", Action: "confirm", Operator: itAdmin,
		NodeStatus: "done", CurrentNodeID: "N3", Complete: true,
	})
	require.Error(t, err, "告警行缺失时完结必须报错")
	assert.True(t, errors.Is(err, ErrFlowAlertNotFound), "应返回 ErrFlowAlertNotFound，实际 %v", err)
	assert.Equal(t, "running", statusOf(t, inst.InstanceID), "缺告警时流程不得落终态")
	assert.Equal(t, "current", nodeStateOf(t, inst.InstanceID, "N3").Status, "末节点状态随事务回滚")
}

func itCountRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, itStore.pool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

// ─────────────────────────────────────────────────────────────
// T681 E2E 夹具清理：按「前缀 + 时间窗」单事务回收
// 真库验 SQL 语义（starts_with 前缀 / started_at+created_at 双窗 / DELETE…USING）、
// FK ON DELETE CASCADE 级联、删实例释放告警池位、空窗口幂等。
// 该用例独占 "T053流程" 前缀域（与 itFlowFixture 的「流程夹具模板-」域不重叠）。
// ─────────────────────────────────────────────────────────────

const it681Prefix = "T053流程"

type it681Fixture struct {
	templateID string
	instanceID string // withInstance=false 时为空
	alertID    int64  // withInstance=false 时为 0
}

// newIT681Fixture 造一套 "T053流程-" 前缀夹具（可选挂一条实例），并登记自清。
// 设备/患者复用全夹具共用的既有行（同 newITFlow：自建设备会撞 uk_devices_active_patient）。
func newIT681Fixture(t *testing.T, suffix string, withInstance bool) it681Fixture {
	t.Helper()
	ctx := context.Background()
	if _, err := itStore.pool.Exec(ctx,
		`INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		 VALUES ($1, '\x00'::bytea, $2, 'online') ON CONFLICT (device_id) DO NOTHING`,
		itFlowDevice, itFlowPatient); err != nil {
		t.Fatalf("it: T681 夹具设备前置: %v", err)
	}
	tpl, err := itStore.CreateFlowTemplate(ctx, it681Prefix+"-"+suffix, itFlowNodes, itFlowEdges, itAdmin)
	require.NoError(t, err)
	fx := it681Fixture{templateID: tpl.TemplateID}
	if withInstance {
		var id int64
		require.NoError(t, itStore.pool.QueryRow(ctx,
			`INSERT INTO alerts (patient_id, device_id, type, detail, ts)
			 VALUES ($1, $2, 'pressure_high', $3, now() + make_interval(secs => $4::int)) RETURNING alert_id`,
			itFlowPatient, itFlowDevice, "T681 夹具告警-"+suffix, int(itFlowAlertSeq.Add(1))).Scan(&id))
		fx.alertID = id
		inst, err := itStore.CreateFlowInstance(ctx, tpl.TemplateID, id, []string{"N1", "N2", "N3"}, []string{"N1"})
		require.NoError(t, err)
		fx.instanceID = inst.InstanceID
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = itStore.pool.Exec(c, `DELETE FROM flow_instance WHERE template_id = $1`, fx.templateID)
		_, _ = itStore.pool.Exec(c, `DELETE FROM flow_template WHERE template_id = $1`, fx.templateID)
		if fx.alertID != 0 {
			_, _ = itStore.pool.Exec(c, `DELETE FROM alerts WHERE alert_id = $1`, fx.alertID)
		}
	})
	return fx
}

func it681TemplateExists(t *testing.T, templateID string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM flow_template WHERE template_id = $1)`, templateID).Scan(&exists))
	return exists
}

func TestITT681CleanupFlowFixturesByPrefixAndWindow(t *testing.T) {
	ctx := context.Background()
	windowStart := time.Now().Add(-time.Minute) // 窗口下界：本用例造数之前

	fxIn1 := newIT681Fixture(t, "IN1", true)  // 窗口内：模板 + 实例
	fxIn2 := newIT681Fixture(t, "IN2", true)  // 窗口内：模板 + 实例
	fxIn3 := newIT681Fixture(t, "IN3", false) // 窗口内：仅模板

	// 窗口外：造好后把模板 created_at 与实例 started_at 改到窗口之前 → 不得被清
	fxOut := newIT681Fixture(t, "OUT", true)
	old := windowStart.Add(-time.Hour)
	_, err := itStore.pool.Exec(ctx,
		`UPDATE flow_template SET created_at = $2 WHERE template_id = $1`, fxOut.templateID, old)
	require.NoError(t, err)
	_, err = itStore.pool.Exec(ctx,
		`UPDATE flow_instance SET started_at = $2 WHERE template_id = $1`, fxOut.templateID, old)
	require.NoError(t, err)

	// 窗口内但被类型绑定引用的模板：其实例应被删，模板须留存（flow_type_bindings FK 无级联，
	// 硬删会抛违例回滚整事务 → 反把实例删除一起撤销）——校验新增的 NOT EXISTS 护栏。
	fxBound := newIT681Fixture(t, "BND", true)
	require.NoError(t, itStore.ReplaceFlowTypeBindings(ctx,
		[]FlowTypeBindingSet{{AlertType: "pressure_high", TemplateID: fxBound.templateID, UpdatedBy: itAdmin}}))
	t.Cleanup(func() { // LIFO：先解绑，fixture 自清才能删模板
		_ = itStore.ReplaceFlowTypeBindings(context.Background(),
			[]FlowTypeBindingSet{{AlertType: "pressure_high", TemplateID: ""}})
	})

	res, err := itStore.CleanupFlowFixtures(ctx, it681Prefix, windowStart, time.Time{})
	require.NoError(t, err)

	// 删除计数：窗口内 3 实例（IN1/IN2/BND）+ 3 模板（IN1/IN2/IN3；BND 因绑定被留存）
	assert.Equal(t, it681Prefix, res.TemplatePrefix)
	assert.Equal(t, int64(3), res.DeletedInstances, "窗口内实例全删（含被绑定模板 BND 的实例）")
	assert.Equal(t, int64(3), res.DeletedTemplates, "窗口内无引用模板全删（含无实例的 fxIn3；被绑定的 BND 留存）")

	// 前后两拍读数：模板 -3 / 实例 -3；freePool 净增 = 删实例数（删实例即释放告警池位）
	assert.Equal(t, res.Before.TemplateCount-3, res.After.TemplateCount, "前缀模板数 -3")
	assert.Equal(t, res.Before.InstanceCount-3, res.After.InstanceCount, "前缀实例数 -3")
	assert.Equal(t, res.Before.TemplateCountTotal-3, res.After.TemplateCountTotal, "全量模板数同样 -3（前缀外不动）")
	assert.Equal(t, res.DeletedInstances, res.After.FreePool-res.Before.FreePool, "freePool 净增 = 删实例数")

	// 窗口内模板已删；窗口外模板留存（时间窗隔离）；被绑定模板留存且绑定行完好
	assert.False(t, it681TemplateExists(t, fxIn1.templateID))
	assert.False(t, it681TemplateExists(t, fxIn2.templateID))
	assert.False(t, it681TemplateExists(t, fxIn3.templateID))
	assert.True(t, it681TemplateExists(t, fxOut.templateID), "窗口外模板不得被清")
	assert.True(t, it681TemplateExists(t, fxBound.templateID), "被类型绑定引用的模板不得被清")
	assert.Equal(t, 1, itCountRows(t, `SELECT COUNT(*) FROM flow_type_bindings WHERE template_id = $1`, fxBound.templateID),
		"绑定行完好（模板留存则绑定不被破坏）")
	assert.Zero(t, itCountRows(t, `SELECT COUNT(*) FROM flow_instance WHERE template_id = $1`, fxBound.templateID),
		"被绑定模板的窗口内实例仍被删除（保池优先）")

	// 级联：窗口内实例的节点状态/流水随实例删除（不留脏 current）
	assert.Zero(t, itCountRows(t, `SELECT COUNT(*) FROM flow_node_state WHERE instance_id = $1`, fxIn1.instanceID))
	assert.Zero(t, itCountRows(t, `SELECT COUNT(*) FROM flow_node_action WHERE instance_id = $1`, fxIn1.instanceID))
	// 窗口外实例留存
	assert.Equal(t, 1, itCountRows(t, `SELECT COUNT(*) FROM flow_instance WHERE template_id = $1`, fxOut.templateID))

	// 只回收流程侧，不动告警源数据：被删实例的告警行仍在
	assert.Equal(t, 1, itCountRows(t, `SELECT COUNT(*) FROM alerts WHERE alert_id = $1`, fxIn1.alertID))

	// 幂等：同窗口再调 → 删 0，前后两拍读数完全相同（收尾钩子可无脑调用）
	res2, err := itStore.CleanupFlowFixtures(ctx, it681Prefix, windowStart, time.Time{})
	require.NoError(t, err)
	assert.Zero(t, res2.DeletedInstances)
	assert.Zero(t, res2.DeletedTemplates)
	assert.Equal(t, res2.Before, res2.After, "空窗口幂等：前后读数一致")
}

// T701：createdBefore 闭窗上界（残留守卫清往轮存量、不吞并发 run 正在造的夹具）
func TestITT701CleanupFlowFixturesClosedWindowUpperBound(t *testing.T) {
	ctx := context.Background()
	windowStart := time.Now().Add(-time.Minute)

	// 往轮存量：started_at/created_at 落在闭窗 [windowStart, windowBefore) 内 ⇒ 应被清
	fxOld := newIT681Fixture(t, "OLD", true)
	// 并发 run 在用：started_at 晚于闭窗上界 ⇒ 必须留存
	fxNew := newIT681Fixture(t, "NEW", true)

	cut := time.Now().Add(-time.Second) // 上界：把 NEW 排除在窗外
	_, err := itStore.pool.Exec(ctx,
		`UPDATE flow_template SET created_at = $2 WHERE template_id = $1`, fxOld.templateID, windowStart.Add(30*time.Second))
	require.NoError(t, err)
	_, err = itStore.pool.Exec(ctx,
		`UPDATE flow_instance SET started_at = $2 WHERE template_id = $1`, fxOld.templateID, windowStart.Add(30*time.Second))
	require.NoError(t, err)

	res, err := itStore.CleanupFlowFixtures(ctx, it681Prefix, windowStart, cut)
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.DeletedInstances, "只删闭窗内实例（NEW 在窗外留存）")
	assert.Equal(t, int64(1), res.DeletedTemplates, "只删闭窗内模板")
	assert.False(t, it681TemplateExists(t, fxOld.templateID), "往轮存量（窗内）被回收")
	assert.True(t, it681TemplateExists(t, fxNew.templateID), "并发 run 在用夹具（窗外）不得被清")
	assert.Equal(t, 1, itCountRows(t, `SELECT COUNT(*) FROM flow_instance WHERE template_id = $1`, fxNew.templateID),
		"窗外实例留存（残留守卫的防误伤口）")

	// 幂等：同闭窗再调 → 0
	res2, err := itStore.CleanupFlowFixtures(ctx, it681Prefix, windowStart, cut)
	require.NoError(t, err)
	assert.Zero(t, res2.DeletedInstances)
	assert.Zero(t, res2.DeletedTemplates)

	// 零值上界 = 仅下界旧行为：窗内 NEW 现在应被清（与 T681 完全一致）
	res3, err := itStore.CleanupFlowFixtures(ctx, it681Prefix, windowStart, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res3.DeletedInstances, "零值上界回落仅下界（旧客户端兼容）")
	assert.False(t, it681TemplateExists(t, fxNew.templateID))
}

// ─────────────────────────────────────────────────────────────
// T687 账号形态归一（ResolveFlowAccountID 三级解析，真库验 SQL）
// ─────────────────────────────────────────────────────────────

// TestITT687ResolveFlowAccountID 真库三级解析读数：
// admin_id 直配 / username 形 / doctor_id 形 → 全部回 admins.admin_id；
// 非 admins 域账号（技师 tech_id）与未知串原样透传；空串短路。
// 种子复用 seedITData：itAdmin=ADM-USR-IT（username=it_admin）、itDoctor=DOC-USR-IT（admin_id=ADM-USR-IT）。
func TestITT687ResolveFlowAccountID(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"admin_id直配", itAdmin, itAdmin},
		{"username形→admin_id", "it_admin", itAdmin},
		{"doctor_id形→admin_id", itDoctor, itAdmin},
		{"技师tech_id透传", "T-IT-UNKNOWN", "T-IT-UNKNOWN"},
		{"未知串透传", "no_such_account", "no_such_account"},
		{"空串短路", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := itStore.ResolveFlowAccountID(ctx, tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
