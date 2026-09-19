package repo

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bracesync/bracesync/services/alert-service/internal/config"
	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

// PGConfigRepo sys_configs 阈值配置读写 + alert_point_rules 逐点规则读取
// （实现 config.Store（T009）与 config.PointRuleStore（T252 2.2））
//
// 表归属：sys_configs 为共享 KV 配置表（架构 §4.2）；本服务经 config.Manager.Update
// （§7D.12 系统配置变更路径，写入前联动校验）维护告警阈值相关键（config.Keys()）。
// 其余键（佩戴目标/WiFi 预设等）归属各自服务，本服务不写。
type PGConfigRepo struct {
	pool *pgxpool.Pool
}

// NewConfigRepo 创建 PGConfigRepo
func NewConfigRepo(pool *pgxpool.Pool) *PGConfigRepo { return &PGConfigRepo{pool: pool} }

// FetchAll 读取本包管理的全部配置键（缺失行不返回，解析层回退默认）
func (r *PGConfigRepo) FetchAll(ctx context.Context) (map[string]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT config_key, config_value FROM sys_configs WHERE config_key = ANY($1)`, config.Keys())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

// FetchPointRules 读取逐采集点规则（实现 config.PointRuleStore，T252 2.2）。
// 表为稀疏存储：只有运营在网格上改过的点有行，缺失点位由引擎回退统一阈值。
// 表尚未迁移（42P01）时按「无逐点规则」返回，不让迁移顺序卡住整条阈值热更新链路。
func (r *PGConfigRepo) FetchPointRules(ctx context.Context) (map[string]engine.PointRule, error) {
	rows, err := r.pool.Query(ctx, `SELECT point_id, monitored, upper_n::float8 FROM alert_point_rules`)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]engine.PointRule)
	for rows.Next() {
		var pointID string
		var monitored bool
		var upperN *float64
		if err := rows.Scan(&pointID, &monitored, &upperN); err != nil {
			return nil, err
		}
		rule := engine.PointRule{Monitored: monitored}
		if upperN != nil {
			rule.UpperN = *upperN
		}
		out[pointID] = rule
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// Upsert 单事务批量写入（INSERT ON CONFLICT DO UPDATE）；写失败整体回滚
func (r *PGConfigRepo) Upsert(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Commit 后 Rollback 为 no-op

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 确定性写入顺序，便于排查与回放
	for _, k := range keys {
		if _, err := tx.Exec(ctx, `
			INSERT INTO sys_configs (config_key, config_value, updated_at)
			VALUES ($1, $2, now())
			ON CONFLICT (config_key) DO UPDATE
			SET config_value = EXCLUDED.config_value, updated_at = now()`,
			k, values[k]); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
