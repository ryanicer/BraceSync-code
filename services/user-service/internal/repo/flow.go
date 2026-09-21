// T274 告警流程画布数据访问层（flow_template / flow_instance / flow_node_state / flow_node_action）
//
// 表归属：四表均 user-service owner（migration 000020）。跨 owner 只有 flow_instance.alert_id
// 指向 alerts（alert-service）—— 不建外键（000020 有因），本层只写该列值、不读其列，
// 存在性由 CreateFlowInstance 事务内校验。
//
// 🔴 单一状态权威在后端：flow_node_state 只由本文件的 CreateFlowInstance / ApplyFlowNodeAction
// 两处写入，读端点不改状态。前端不得自行推算节点色（契约 submitFlowNodeAction 已写同一条）。
package repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ─────────────────────────────────────────────────────────────
// sentinel 错误（handler 据此映射 HTTP code）
// ─────────────────────────────────────────────────────────────

// ErrFlowTemplateNotFound 模板 ID 不存在 → 404。
var ErrFlowTemplateNotFound = errors.New("flow template not found")

// ErrFlowTemplateInUse 模板仍被实例引用，不可删除。handler 据 InstanceCount 拼 409 文案。
type ErrFlowTemplateInUse struct{ InstanceCount int }

func (e *ErrFlowTemplateInUse) Error() string {
	return fmt.Sprintf("flow template in use by %d instance(s)", e.InstanceCount)
}

// ErrFlowInstanceNotFound 实例 ID 不存在 → 404。
var ErrFlowInstanceNotFound = errors.New("flow instance not found")

// ErrFlowInstanceExists 该告警已有流程实例（flow_instance.alert_id UNIQUE）→ 409。
// Existing 供 handler 幂等跳转（409 的 data 回已存在实例）。
type ErrFlowInstanceExists struct{ Existing *FlowInstanceRow }

func (e *ErrFlowInstanceExists) Error() string {
	return "flow instance already exists for this alert"
}

// ErrFlowAlertNotFound 告警不存在（CreateFlowInstance 事务内校验）→ 400。
var ErrFlowAlertNotFound = errors.New("alert not found for flow instance")

// ErrFlowNodeNotFound 节点不属于该实例（flow_node_state 无该行）→ 404。
var ErrFlowNodeNotFound = errors.New("flow node state not found")

// ErrFlowNodeNotCurrent 对非 current 节点执行 confirm/reject → 409。
type ErrFlowNodeNotCurrent struct{ NodeID, Status string }

func (e *ErrFlowNodeNotCurrent) Error() string {
	return fmt.Sprintf("node %s is not current (status=%s)", e.NodeID, e.Status)
}

// ErrFlowInstanceCompleted 实例已 completed 仍提交操作 → 409。
var ErrFlowInstanceCompleted = errors.New("flow instance already completed")

// newFlowID 生成业务 ID：前缀 + 10 位大写 hex（口径同 T252 newRoleID 的 ROLE_C）。
func newFlowID(prefix string) (string, error) {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + strings.ToUpper(hex.EncodeToString(buf)), nil
}

// ─────────────────────────────────────────────────────────────
// 投影行
// ─────────────────────────────────────────────────────────────

// FlowTemplateRow 模板行。Nodes/Edges 为 JSONB 原文（后端不解析业务属性，原样回传前端）。
type FlowTemplateRow struct {
	TemplateID    string
	Name          string
	Nodes         []byte
	Edges         []byte
	Creator       string
	CreatorName   *string
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	InstanceCount int
}

// FlowInstanceRow 实例行（TemplateName 由 LEFT JOIN 带出，前端免二次查）。
type FlowInstanceRow struct {
	InstanceID    string
	TemplateID    string
	TemplateName  string
	AlertID       int64
	CurrentNodeID *string
	Status        string
	StartedAt     time.Time
	EndedAt       *time.Time
}

// FlowNodeStateRow 节点运行状态行。
type FlowNodeStateRow struct {
	NodeID       string
	Status       string
	Operator     *string
	OperatorName *string
	OperatedAt   *time.Time
	Remark       *string
	Attachments  []byte // JSON 数组原文，无附件为 "[]"
	Assignee     *string
	AssigneeName *string
}

// FlowNodeActionRow 节点操作流水行。
type FlowNodeActionRow struct {
	ActionID       int64
	NodeID         string
	Action         string
	Operator       string
	OperatorName   *string
	Remark         *string
	Attachments    []byte
	TargetOperator *string
	CreatedAt      time.Time
}

// flowAccountName 账号显示名反查（跨 admins/doctors/technicians/patients 四表，
// 查不到回 NULL 不编造）—— 与 T252 auditOperatorName 同口径，列名由调用方硬编码传入。
//
// 四个内层别名统一带 fa/fd/ft/fp 前缀：调用方外层常见别名正是 a/d/t/p/i
// （flow_template 用 t），若内层复用 t，`t.creator` 会被内层表遮蔽解析成
// technicians.creator → SQLSTATE 42703。
func flowAccountName(col string) string {
	return fmt.Sprintf(`COALESCE(
            (SELECT fa.name FROM admins fa WHERE fa.admin_id = %s),
            (SELECT fd.name FROM doctors fd WHERE fd.doctor_id = %s),
            (SELECT ft.name FROM technicians ft WHERE ft.tech_id = %s),
            (SELECT fp.name FROM patients fp WHERE fp.patient_id = %s))`, col, col, col, col)
}

// ─────────────────────────────────────────────────────────────
// 模板 CRUD（2.4 设计器）
// ─────────────────────────────────────────────────────────────

const flowTemplateCols = `t.template_id, t.name, t.nodes::text, t.edges::text, t.creator,
       %s, t.version, t.created_at, t.updated_at,
       (SELECT COUNT(*) FROM flow_instance i WHERE i.template_id = t.template_id)`

// scanFlowTemplate 扫描一行模板（列顺序须与 flowTemplateCols 一致）。
func scanFlowTemplate(row pgx.Row) (*FlowTemplateRow, error) {
	var r FlowTemplateRow
	err := row.Scan(&r.TemplateID, &r.Name, &r.Nodes, &r.Edges, &r.Creator, &r.CreatorName,
		&r.Version, &r.CreatedAt, &r.UpdatedAt, &r.InstanceCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFlowTemplateNotFound
	}
	return &r, err
}

// ListFlowTemplates 模板分页列表（keyword 匹配名称 ILIKE，updatedAt 倒序）。
// 列表不回图数据：nodes/edges 置 "[]"，避免载荷膨胀（契约 listFlowTemplates 已写明）。
func (s *PGStore) ListFlowTemplates(ctx context.Context, keyword string, page, pageSize int) ([]FlowTemplateRow, int64, error) {
	var total int64
	countSQL := `SELECT COUNT(*) FROM flow_template t`
	var args []any
	if keyword != "" {
		countSQL += ` WHERE t.name ILIKE $1`
		args = append(args, "%"+keyword+"%")
	}
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	sql := fmt.Sprintf(
		`SELECT `+`t.template_id, t.name, '[]'::text, '[]'::text, t.creator, %s,
		        t.version, t.created_at, t.updated_at, 0
		   FROM flow_template t%s
		  ORDER BY t.updated_at DESC, t.template_id DESC
		  LIMIT $%d OFFSET $%d`,
		flowAccountName("t.creator"),
		func() string {
			if keyword == "" {
				return ""
			}
			return " WHERE t.name ILIKE $1"
		}(),
		len(args)+1, len(args)+2)

	rows, err := s.pool.Query(ctx, sql, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]FlowTemplateRow, 0, pageSize)
	for rows.Next() {
		var r FlowTemplateRow
		if err := rows.Scan(&r.TemplateID, &r.Name, &r.Nodes, &r.Edges, &r.Creator, &r.CreatorName,
			&r.Version, &r.CreatedAt, &r.UpdatedAt, &r.InstanceCount); err != nil {
			return nil, 0, err
		}
		list = append(list, r)
	}
	return list, total, rows.Err()
}

// GetFlowTemplate 模板详情（含完整 nodes/edges）；不存在 → ErrFlowTemplateNotFound。
func (s *PGStore) GetFlowTemplate(ctx context.Context, templateID string) (*FlowTemplateRow, error) {
	sql := fmt.Sprintf(`SELECT `+flowTemplateCols+` FROM flow_template t WHERE t.template_id = $1`,
		flowAccountName("t.creator"))
	r, err := scanFlowTemplate(s.pool.QueryRow(ctx, sql, templateID))
	if err != nil {
		return nil, err
	}
	return r, nil
}

// FlowTemplateNameTaken 模板名是否被占用（excludeID 空串表示不排除）——应用层查重，
// flow_template.name 无唯一索引（同 roles 先例，见 000020 注释）。
func (s *PGStore) FlowTemplateNameTaken(ctx context.Context, name, excludeID string) (bool, error) {
	var taken bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM flow_template WHERE name = $1 AND template_id <> $2)`,
		name, excludeID).Scan(&taken)
	return taken, err
}

// CreateFlowTemplate 新建模板（templateId 应用层生成）；成功回读完整行。
// nodes/edges 为空串时落 '[]'::jsonb（由调用方保证是合法 JSON 数组文本）。
func (s *PGStore) CreateFlowTemplate(ctx context.Context, name, nodesJSON, edgesJSON, creator string) (*FlowTemplateRow, error) {
	id, err := newFlowID("FLOW_T")
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO flow_template (template_id, name, nodes, edges, creator)
		 VALUES ($1, $2, COALESCE(NULLIF($3, '')::jsonb, '[]'::jsonb),
                 COALESCE(NULLIF($4, '')::jsonb, '[]'::jsonb), $5)`,
		id, name, nodesJSON, edgesJSON, creator); err != nil {
		return nil, err
	}
	return s.GetFlowTemplate(ctx, id)
}

// UpdateFlowTemplate 保存模板（nil 字段不改）；name/nodes/edges 任一给出即 updated_at=now()、version+1。
// 不存在 → ErrFlowTemplateNotFound。
func (s *PGStore) UpdateFlowTemplate(ctx context.Context, templateID string, name, nodesJSON, edgesJSON *string) (*FlowTemplateRow, error) {
	var sets []string
	args := []any{templateID}
	add := func(cond string, arg any) {
		args = append(args, arg)
		sets = append(sets, fmt.Sprintf(cond, len(args)))
	}
	if name != nil {
		add("name = $%d", *name)
	}
	if nodesJSON != nil {
		add("nodes = COALESCE(NULLIF($%d, '')::jsonb, '[]'::jsonb)", *nodesJSON)
	}
	if edgesJSON != nil {
		add("edges = COALESCE(NULLIF($%d, '')::jsonb, '[]'::jsonb)", *edgesJSON)
	}
	if len(sets) == 0 {
		return s.GetFlowTemplate(ctx, templateID)
	}
	sets = append(sets, "version = version + 1", "updated_at = now()")
	sql := "UPDATE flow_template t SET " + strings.Join(sets, ", ") + " WHERE t.template_id = $1"
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrFlowTemplateNotFound
	}
	return s.GetFlowTemplate(ctx, templateID)
}

// DeleteFlowTemplate 删除模板。被实例引用 → ErrFlowTemplateInUse（先查后删；
// 并发下 flow_instance 外键 23503 兜底为同口径 409，参照 DeleteRole）。
func (s *PGStore) DeleteFlowTemplate(ctx context.Context, templateID string) error {
	var cnt int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM flow_instance WHERE template_id = $1`, templateID).Scan(&cnt); err != nil {
		return err
	}
	if cnt > 0 {
		return &ErrFlowTemplateInUse{InstanceCount: cnt}
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM flow_template WHERE template_id = $1`, templateID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return &ErrFlowTemplateInUse{InstanceCount: 1}
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFlowTemplateNotFound
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// 实例与节点状态（2.3 运行态）
// ─────────────────────────────────────────────────────────────

const flowInstanceColumns = `i.instance_id, i.template_id, t.name, i.alert_id, i.current_node_id,
       i.status, i.started_at, i.ended_at`

func scanFlowInstance(row pgx.Row) (*FlowInstanceRow, error) {
	var r FlowInstanceRow
	err := row.Scan(&r.InstanceID, &r.TemplateID, &r.TemplateName, &r.AlertID, &r.CurrentNodeID,
		&r.Status, &r.StartedAt, &r.EndedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFlowInstanceNotFound
	}
	return &r, err
}

// CreateFlowInstance 启动流程实例：一条事务内写 flow_instance + 按 nodeIDs 全量生成
// flow_node_state（status='todo'），并把 entryNodeIDs（模板入边为 0 的节点）置 'current'。
//
// 图数据不在本方法解析：nodeIDs / entryNodeIDs 由 handler 依模板 edges 算好后传入，
// 与 ApplyFlowNodeAction 同口径（数据层不做图遍历）。启动人只落审计，不入实例行。
//
// 告警不存在 → ErrFlowAlertNotFound（事务内存在性校验）；该告警已有实例 → ErrFlowInstanceExists（23505）。
func (s *PGStore) CreateFlowInstance(ctx context.Context, templateID string, alertID int64, nodeIDs, entryNodeIDs []string) (*FlowInstanceRow, error) {
	id, err := newFlowID("FLOW_I")
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// alerts 上不带外键（见 000020：alert-service 用 TRUNCATE alerts RESTART IDENTITY 做用例隔离），
	// 存在性只在这里校，同一事务内校完即插，不给并发窗口留空档。
	var existsAlert bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM alerts WHERE alert_id = $1)`, alertID).Scan(&existsAlert); err != nil {
		return nil, err
	}
	if !existsAlert {
		return nil, ErrFlowAlertNotFound
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO flow_instance (instance_id, template_id, alert_id, current_node_id)
		 VALUES ($1, $2, $3, NULL)`, id, templateID, alertID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" { // 仅剩 template_id 一个外键
			return nil, ErrFlowTemplateNotFound
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // alert_id UNIQUE：已有实例
			// 🔴 语句失败后 PG 事务进入 aborted 态（25P02），必须先用 Rollback 结束它，
			// 再用池连接读既有实例；在同一个 tx 里查会拿到 25P02 而不是 ErrFlowInstanceExists。
			_ = tx.Rollback(ctx)
			existing, qErr := s.getFlowInstanceByAlert(ctx, alertID)
			if qErr != nil {
				return nil, qErr
			}
			return nil, &ErrFlowInstanceExists{Existing: existing}
		}
		return nil, err
	}

	for _, nodeID := range nodeIDs {
		status := "todo"
		if containsStr(entryNodeIDs, nodeID) {
			status = "current"
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO flow_node_state (instance_id, node_id, status) VALUES ($1, $2, $3)`,
			id, nodeID, status); err != nil {
			return nil, err
		}
	}
	// current_node_id 指针：多起始节点时取排序后首个（并行时以 node_state 为准，见 000020 注释）
	if len(entryNodeIDs) > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE flow_instance SET current_node_id = $2 WHERE instance_id = $1`,
			id, sortedFirst(entryNodeIDs)); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetFlowInstance(ctx, id)
}

// getFlowInstanceByAlert 冲突分支下取既有实例（按 alert_id，池连接读）。
// 只在 CreateFlowInstance 回滚 aborted 事务之后调用。
func (s *PGStore) getFlowInstanceByAlert(ctx context.Context, alertID int64) (*FlowInstanceRow, error) {
	return scanFlowInstance(s.pool.QueryRow(ctx,
		`SELECT `+flowInstanceColumns+`
		   FROM flow_instance i JOIN flow_template t ON t.template_id = i.template_id
		  WHERE i.alert_id = $1`, alertID))
}

// GetFlowInstance 实例详情；不存在 → ErrFlowInstanceNotFound。
func (s *PGStore) GetFlowInstance(ctx context.Context, instanceID string) (*FlowInstanceRow, error) {
	return scanFlowInstance(s.pool.QueryRow(ctx,
		`SELECT `+flowInstanceColumns+`
		   FROM flow_instance i JOIN flow_template t ON t.template_id = i.template_id
		  WHERE i.instance_id = $1`, instanceID))
}

// ListFlowInstancesByAlert 按告警取实例（startedAt 倒序；正常 0 或 1 条，alert_id 唯一）。
func (s *PGStore) ListFlowInstancesByAlert(ctx context.Context, alertID int64) ([]FlowInstanceRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+flowInstanceColumns+`
		   FROM flow_instance i JOIN flow_template t ON t.template_id = i.template_id
		  WHERE i.alert_id = $1
		  ORDER BY i.started_at DESC, i.instance_id DESC`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]FlowInstanceRow, 0, 1)
	for rows.Next() {
		var r FlowInstanceRow
		if err := rows.Scan(&r.InstanceID, &r.TemplateID, &r.TemplateName, &r.AlertID, &r.CurrentNodeID,
			&r.Status, &r.StartedAt, &r.EndedAt); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// ListNodeStates 实例的全部节点状态（按 node_id 升序稳定输出；顺序与渲染无关，前端按 nodeId join）。
func (s *PGStore) ListNodeStates(ctx context.Context, instanceID string) ([]FlowNodeStateRow, error) {
	if err := s.flowInstanceExists(ctx, instanceID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT node_id, status, operator, `+flowAccountName("operator")+`,
		        operated_at, remark, attachments::text, assignee, `+flowAccountName("assignee")+`
		   FROM flow_node_state WHERE instance_id = $1 ORDER BY node_id`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]FlowNodeStateRow, 0, 8)
	for rows.Next() {
		var r FlowNodeStateRow
		if err := rows.Scan(&r.NodeID, &r.Status, &r.Operator, &r.OperatorName, &r.OperatedAt,
			&r.Remark, &r.Attachments, &r.Assignee, &r.AssigneeName); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// flowInstanceExists 读端点的前置存在性检查（不存在 → ErrFlowInstanceNotFound → 404）。
func (s *PGStore) flowInstanceExists(ctx context.Context, instanceID string) error {
	var ok bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM flow_instance WHERE instance_id = $1)`, instanceID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrFlowInstanceNotFound
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// 节点操作（状态机唯一写入点）
// ─────────────────────────────────────────────────────────────

// FlowActionWrite 一次节点操作的写入参数。后继集合与目标状态由 handler 依模板 edges 算好，
// repo 不读模板 JSON（图遍历是业务逻辑，不放数据层）。
type FlowActionWrite struct {
	InstanceID     string
	NodeID         string
	Action         string
	Operator       string
	Remark         string
	Attachments    string // JSON 数组文本，空 → "[]"
	TargetOperator string // 仅 transfer

	NodeStatus    string   // 本节点操作后置为
	Successors    []string // 置 current 的后继节点（confirm 且有出边时非空）
	CurrentNodeID string   // 实例 current_node_id 新指针；空串表示不改
	Complete      bool     // true → 实例置 completed、ended_at=now()
}

// ApplyFlowNodeAction 事务内完成「写流水 + 更新节点状态 + 推进流程」。
//
// 行锁顺序：先 flow_instance FOR UPDATE 再 flow_node_state FOR UPDATE —— 同一实例的并发操作
// 因此串行化，避免两个 confirm 同时把同一 current 节点推进两次。
func (s *PGStore) ApplyFlowNodeAction(ctx context.Context, in FlowActionWrite) (*FlowNodeActionRow, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM flow_instance WHERE instance_id = $1 FOR UPDATE`, in.InstanceID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFlowInstanceNotFound
		}
		return nil, err
	}
	if status != "running" {
		return nil, ErrFlowInstanceCompleted
	}

	var cur string
	err = tx.QueryRow(ctx,
		`SELECT status FROM flow_node_state WHERE instance_id = $1 AND node_id = $2 FOR UPDATE`,
		in.InstanceID, in.NodeID).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFlowNodeNotFound
	}
	if err != nil {
		return nil, err
	}

	// confirm / reject 只能作用在 current 节点上；transfer / urge 不校验位置
	if (in.Action == "confirm" || in.Action == "reject") && cur != "current" {
		return nil, &ErrFlowNodeNotCurrent{NodeID: in.NodeID, Status: cur}
	}

	var actionID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO flow_node_action (instance_id, node_id, action, operator, remark, attachments, target_operator)
		 VALUES ($1, $2, $3, $4, NULLIF($5, ''), COALESCE(NULLIF($6, '')::jsonb, '[]'::jsonb), NULLIF($7, ''))
		 RETURNING action_id`,
		in.InstanceID, in.NodeID, in.Action, in.Operator, in.Remark, in.Attachments, in.TargetOperator,
	).Scan(&actionID); err != nil {
		return nil, err
	}

	switch in.Action {
	case "transfer":
		if _, err := tx.Exec(ctx,
			`UPDATE flow_node_state SET assignee = NULLIF($3, '')
			  WHERE instance_id = $1 AND node_id = $2`,
			in.InstanceID, in.NodeID, in.TargetOperator); err != nil {
			return nil, err
		}
	case "urge":
		// 仅留痕：状态与指派都不动（流水已在上面写入）
	default: // confirm / reject
		if _, err := tx.Exec(ctx,
			`UPDATE flow_node_state
			    SET status = $3, operator = $4, operated_at = now(), remark = NULLIF($5, ''),
			        attachments = COALESCE(NULLIF($6, '')::jsonb, '[]'::jsonb)
			  WHERE instance_id = $1 AND node_id = $2`,
			in.InstanceID, in.NodeID, in.NodeStatus, in.Operator, in.Remark, in.Attachments); err != nil {
			return nil, err
		}
		for _, next := range in.Successors {
			if _, err := tx.Exec(ctx,
				`UPDATE flow_node_state SET status = 'current'
				  WHERE instance_id = $1 AND node_id = $2 AND status = 'todo'`,
				in.InstanceID, next); err != nil {
				return nil, err
			}
		}
		if in.CurrentNodeID != "" {
			if _, err := tx.Exec(ctx,
				`UPDATE flow_instance SET current_node_id = $2 WHERE instance_id = $1`,
				in.InstanceID, in.CurrentNodeID); err != nil {
				return nil, err
			}
		}
		if in.Complete {
			if _, err := tx.Exec(ctx,
				`UPDATE flow_instance SET status = 'completed', ended_at = now() WHERE instance_id = $1`,
				in.InstanceID); err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	row := &FlowNodeActionRow{ActionID: actionID, NodeID: in.NodeID, Action: in.Action,
		Operator: in.Operator, Remark: nilIfEmpty(in.Remark), Attachments: attachmentsOrEmpty(in.Attachments),
		TargetOperator: nilIfEmpty(in.TargetOperator), CreatedAt: time.Now()}
	if name, err := s.accountName(ctx, in.Operator); err == nil {
		row.OperatorName = name
	}
	return row, nil
}

// ListFlowNodeActions 时间线（createdAt 正序 = action_id 正序，不分页，见契约）。
func (s *PGStore) ListFlowNodeActions(ctx context.Context, instanceID string) ([]FlowNodeActionRow, error) {
	if err := s.flowInstanceExists(ctx, instanceID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT action_id, node_id, action, operator, `+flowAccountName("operator")+`,
		        remark, attachments::text, target_operator, created_at
		   FROM flow_node_action WHERE instance_id = $1 ORDER BY action_id`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]FlowNodeActionRow, 0, 8)
	for rows.Next() {
		var r FlowNodeActionRow
		if err := rows.Scan(&r.ActionID, &r.NodeID, &r.Action, &r.Operator, &r.OperatorName,
			&r.Remark, &r.Attachments, &r.TargetOperator, &r.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// accountName 单账号显示名反查（写操作成功后回填 DTO，查不到回 nil 不编造）。
func (s *PGStore) accountName(ctx context.Context, id string) (*string, error) {
	var name *string
	err := s.pool.QueryRow(ctx, `SELECT `+flowAccountName("$1")+``, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return name, err
}

func nilIfEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func attachmentsOrEmpty(v string) []byte {
	if v == "" {
		return []byte("[]")
	}
	return []byte(v)
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// sortedFirst 多起始节点时取字典序首个，保证 current_node_id 可重现（不乱跳）。
func sortedFirst(list []string) string {
	first := list[0]
	for _, v := range list[1:] {
		if v < first {
			first = v
		}
	}
	return first
}
