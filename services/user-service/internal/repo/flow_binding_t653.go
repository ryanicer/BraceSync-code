// T653 告警类型 ↔ 流程模板绑定数据访问层（flow_type_bindings，migration 000037）
//
// 口径（PRD V3.44 §8.2，R8 甲）：显式绑定无兜底 —— 无行 = 不自动建实例；
// 一类型一行（alert_type PK），多对一指向 flow_template；不绑在模板行加列。
package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// FlowTypeBindingRow 绑定行（左连模板取名；未绑定不由本层产出——GET 缺行由 handler 按四类型补 null 行）。
type FlowTypeBindingRow struct {
	AlertType     string
	TemplateID    string
	TemplateName  *string
	UpdatedBy     string
	UpdatedByName *string
	UpdatedAt     time.Time
}

// FlowTypeBindingSet 单项保存：TemplateID 空串 = 解绑（DELETE 该行）。
type FlowTypeBindingSet struct {
	AlertType  string
	TemplateID string
	UpdatedBy  string
}

var flowBindingCols = `b.alert_type, b.template_id, t.name, b.updated_by, ` +
	flowAccountName("b.updated_by") + `, b.updated_at`

// ListFlowTypeBindings 取现存绑定行（可能 0-4 行；顺序由 handler 按规范四类型重排）。
func (s *PGStore) ListFlowTypeBindings(ctx context.Context) ([]FlowTypeBindingRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+flowBindingCols+`
		  FROM flow_type_bindings b
		  LEFT JOIN flow_template t ON t.template_id = b.template_id
		 ORDER BY b.alert_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]FlowTypeBindingRow, 0, 4)
	for rows.Next() {
		var r FlowTypeBindingRow
		if err := rows.Scan(&r.AlertType, &r.TemplateID, &r.TemplateName,
			&r.UpdatedBy, &r.UpdatedByName, &r.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// GetFlowTypeBinding 按类型取绑定；无行返回 (nil, nil)（= 未绑定，不视为错误）。
func (s *PGStore) GetFlowTypeBinding(ctx context.Context, alertType string) (*FlowTypeBindingRow, error) {
	var r FlowTypeBindingRow
	err := s.pool.QueryRow(ctx, `
		SELECT `+flowBindingCols+`
		  FROM flow_type_bindings b
		  LEFT JOIN flow_template t ON t.template_id = b.template_id
		 WHERE b.alert_type = $1`, alertType).
		Scan(&r.AlertType, &r.TemplateID, &r.TemplateName,
			&r.UpdatedBy, &r.UpdatedByName, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ReplaceFlowTypeBindings 单事务按 items 逐项 upsert / 删除：
// TemplateID 空串 → DELETE（解绑，不存在也不报错）；非空 → 先校验模板存在再 upsert。
// 类型词表与请求形状由 handler 层校验；模板不存在 → ErrFlowTemplateNotFound。
func (s *PGStore) ReplaceFlowTypeBindings(ctx context.Context, items []FlowTypeBindingSet) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, it := range items {
		if it.TemplateID == "" {
			if _, err := tx.Exec(ctx,
				`DELETE FROM flow_type_bindings WHERE alert_type = $1`, it.AlertType); err != nil {
				return err
			}
			continue
		}
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM flow_template WHERE template_id = $1)`,
			it.TemplateID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrFlowTemplateNotFound
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO flow_type_bindings (alert_type, template_id, updated_by)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (alert_type) DO UPDATE
			   SET template_id = EXCLUDED.template_id,
			       updated_by  = EXCLUDED.updated_by,
			       updated_at  = now()`,
			it.AlertType, it.TemplateID, it.UpdatedBy); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
