// T252 12.3 操作日志（audit_logs，owner: user-service；PRD §8.2 / §9.2a 等保二级留痕）
//
// 表在 000001:301-313 已建，T252 之前全仓零读写。PRD §8.2 的 AuditLog **没有**描述列
// ⇒ 操作描述固定落 detail JSONB 的 description 键（不擅自加列，加列要改 PRD 数据模型）。
package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AuditInput 审计写入参。空字符串字段落 NULL（等保口径：内部动作可无操作人）。
type AuditInput struct {
	OperatorID   string
	OperatorRole string
	Action       string // login | data_modify | config_change | permission_change（列无 CHECK，可扩展）
	TargetType   string // patient | team | doctor | technician | role | sys_config | alert_rule ...
	TargetID     string
	Description  string
	Detail       map[string]any // 与 Description 合并落 detail；Description 优先级更高
	IP           string
}

// auditOperatorName 操作人显示名反查（跨 4 张账号表，按 operator_id 逐表试；查不到 → NULL 不编造）
const auditOperatorName = `COALESCE(
            (SELECT a.name FROM admins a WHERE a.admin_id = l.operator_id),
            (SELECT d.name FROM doctors d WHERE d.doctor_id = l.operator_id),
            (SELECT t.name FROM technicians t WHERE t.tech_id = l.operator_id),
            (SELECT p.name FROM patients p WHERE p.patient_id = l.operator_id))`

// AuditLogRow 审计流水查询投影
type AuditLogRow struct {
	LogID        int64
	OperatorID   *string
	OperatorRole *string
	OperatorName *string
	Action       string
	TargetType   *string
	TargetID     *string
	Detail       *string // JSON 文本（可空）
	IP           *string
	Ts           time.Time
}

// AuditFilter 查询筛选。From/To 为半开区间 [From, To)，nil = 不限；
// 单日切分由 handler 按 Asia/Shanghai 换算后下发，repo 只收 UTC 时刻。
type AuditFilter struct {
	From       *time.Time
	To         *time.Time
	Action     string
	Operator   string // ILIKE 匹配 operator_id 或反查出的显示名
	TargetType string
	TargetID   string
	Page       int
	PageSize   int
}

// WriteAuditLog 写入一条审计流水。调用方不得因审计失败中断主流程（handler 侧记 WARN 日志）。
func (s *PGStore) WriteAuditLog(ctx context.Context, in AuditInput) error {
	detail := make(map[string]any, len(in.Detail)+1)
	for k, v := range in.Detail {
		detail[k] = v
	}
	detail["description"] = in.Description
	payload, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO audit_logs (operator_id, operator_role, action, target_type, target_id, detail, ip)
		 VALUES (NULLIF($1, ''), NULLIF($2, ''), $3, NULLIF($4, ''), NULLIF($5, ''), $6::jsonb, NULLIF($7, ''))`,
		in.OperatorID, in.OperatorRole, in.Action, in.TargetType, in.TargetID, string(payload), in.IP)
	return err
}

// auditWhere 组装筛选条件（仅拼占位符序号，不拼用户输入）
func auditWhere(f AuditFilter) (string, []any) {
	var conds []string
	var args []any
	// 每条筛选恰好占一个序号；序号由已有条件数推导，不再手工维护计数器
	next := func() int { return len(conds) + 1 }
	add := func(cond string, arg any) {
		conds = append(conds, cond)
		args = append(args, arg)
	}

	if f.From != nil {
		add(fmt.Sprintf("l.ts >= $%d", next()), *f.From)
	}
	if f.To != nil {
		add(fmt.Sprintf("l.ts < $%d", next()), *f.To)
	}
	if f.Action != "" {
		add(fmt.Sprintf("l.action = $%d", next()), f.Action)
	}
	if f.Operator != "" {
		n := next()
		add(fmt.Sprintf("(l.operator_id ILIKE $%d OR %s ILIKE $%d)", n, auditOperatorName, n), "%"+f.Operator+"%")
	}
	if f.TargetType != "" {
		add(fmt.Sprintf("l.target_type = $%d", next()), f.TargetType)
	}
	if f.TargetID != "" {
		add(fmt.Sprintf("l.target_id = $%d", next()), f.TargetID)
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// QueryAuditLogs 审计分页查询（ts 倒序，同秒按 log_id 倒序保证稳定）
func (s *PGStore) QueryAuditLogs(ctx context.Context, f AuditFilter) ([]AuditLogRow, int64, error) {
	where, args := auditWhere(f)

	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs l`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listArgs := append(append([]any{}, args...), f.PageSize, (f.Page-1)*f.PageSize)
	sql := fmt.Sprintf(`
		SELECT l.log_id, l.operator_id, l.operator_role, %s, l.action, l.target_type, l.target_id,
		       l.detail::text, l.ip, l.ts
		  FROM audit_logs l%s
		 ORDER BY l.ts DESC, l.log_id DESC
		 LIMIT $%d OFFSET $%d`, auditOperatorName, where, len(args)+1, len(args)+2)

	rows, err := s.pool.Query(ctx, sql, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]AuditLogRow, 0, f.PageSize)
	for rows.Next() {
		var r AuditLogRow
		if scanErr := rows.Scan(&r.LogID, &r.OperatorID, &r.OperatorRole, &r.OperatorName, &r.Action,
			&r.TargetType, &r.TargetID, &r.Detail, &r.IP, &r.Ts); scanErr != nil {
			return nil, 0, scanErr
		}
		list = append(list, r)
	}
	return list, total, rows.Err()
}
