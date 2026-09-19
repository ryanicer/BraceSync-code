// T252 2.2 逐采集点告警阈值读写（alert_point_rules，owner: user-service）
//
// 表**稀疏存放**：只有被显式保存过的点才有行；服务层对前端恒呈现 P01–P20 共 20 条，
// 未落库的点回默认（monitored=true、上下限跟随统一值）⇒「恢复默认」只需清表。
//
// 统一上下限与全局规则是标量 ⇒ 落 sys_configs KV（与 /admin/settings 同一张表、同一写通道）。
// 「保存规则」一次提交同时改 sys_configs 与逐点表，必须同一事务：否则统一值改了、
// 逐点没改，alert-service 会按错位的组合判定告警。
package repo

import (
	"context"
)

// AlertPointRuleRow alert_point_rules 行投影
type AlertPointRuleRow struct {
	PointID   string
	Monitored bool
	UpperN    *float64 // nil = 跟随统一上限
	LowerN    *float64 // nil = 跟随统一下限
}

// ListAlertPointRules 已落库的逐点阈值（按 point_id 升序；未落库点不出现）
func (s *PGStore) ListAlertPointRules(ctx context.Context) ([]AlertPointRuleRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT point_id, monitored, upper_n::float8, lower_n::float8
		   FROM alert_point_rules ORDER BY point_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []AlertPointRuleRow
	for rows.Next() {
		var r AlertPointRuleRow
		if scanErr := rows.Scan(&r.PointID, &r.Monitored, &r.UpperN, &r.LowerN); scanErr != nil {
			return nil, scanErr
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// SaveAlertRules 单事务写统一上下限/全局规则（kvs → sys_configs UPSERT）+ 逐点阈值 UPSERT。
// UpperN/LowerN 为 nil 落 NULL 列 = 跟随统一值。
func (s *PGStore) SaveAlertRules(ctx context.Context, kvs []ConfigKV, rules []AlertPointRuleRow, updatedBy string) error {
	return s.saveAlertRules(ctx, kvs, rules, false, updatedBy)
}

// ResetAlertRules 恢复默认（同一事务）：清空逐点表 + 统一/全局键写回默认值。
func (s *PGStore) ResetAlertRules(ctx context.Context, kvs []ConfigKV, updatedBy string) error {
	return s.saveAlertRules(ctx, kvs, nil, true, updatedBy)
}

func (s *PGStore) saveAlertRules(ctx context.Context, kvs []ConfigKV, rules []AlertPointRuleRow, clearPoints bool, updatedBy string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if clearPoints {
		if _, execErr := tx.Exec(ctx, `DELETE FROM alert_point_rules`); execErr != nil {
			return execErr
		}
	}
	for _, kv := range kvs {
		if _, execErr := tx.Exec(ctx,
			`INSERT INTO sys_configs (config_key, config_value, updated_by, updated_at)
			 VALUES ($1, $2, $3, now())
			 ON CONFLICT (config_key) DO UPDATE
			   SET config_value = EXCLUDED.config_value,
			       updated_by = EXCLUDED.updated_by,
			       updated_at = now()`,
			kv.Key, kv.Value, updatedBy); execErr != nil {
			return execErr
		}
	}
	for _, r := range rules {
		if _, execErr := tx.Exec(ctx,
			`INSERT INTO alert_point_rules (point_id, monitored, upper_n, lower_n, updated_by, updated_at)
			 VALUES ($1, $2, $3, $4, $5, now())
			 ON CONFLICT (point_id) DO UPDATE
			   SET monitored  = EXCLUDED.monitored,
			       upper_n    = EXCLUDED.upper_n,
			       lower_n    = EXCLUDED.lower_n,
			       updated_by = EXCLUDED.updated_by,
			       updated_at = now()`,
			r.PointID, r.Monitored, r.UpperN, r.LowerN, updatedBy); execErr != nil {
			return execErr
		}
	}
	return tx.Commit(ctx)
}
