// Package repo — daily_wear_stats 只读访问（T257 2.6）
//
// 表 owner 是 data-service（rollup 落库方），本服务只读，且只读 rollup 层不扫压力明细
// （架构 §5；msg-service 佩戴提醒读同一张表 ⇒ 达标口径全仓唯一）。
package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGWearRepo daily_wear_stats 仓储（实现 scanner.WearStore）
type PGWearRepo struct {
	pool *pgxpool.Pool
}

// NewWearRepo 创建 PGWearRepo
func NewWearRepo(pool *pgxpool.Pool) *PGWearRepo { return &PGWearRepo{pool: pool} }

// DailyWearMinutes 业务日累计佩戴分钟数。
// 无 rollup 行（该日一次上报都没有）= 0 分钟，与 msg-service TodayWearMinutes 同语义；
// 真·查询失败才返回 error（调用方跳过该患者，不中断整轮）。
func (r *PGWearRepo) DailyWearMinutes(ctx context.Context, patientID string, bizDay time.Time) (int, error) {
	var minutes int
	err := r.pool.QueryRow(ctx,
		`SELECT wear_minutes FROM daily_wear_stats WHERE patient_id = $1 AND stat_date = $2::date`,
		patientID, bizDay.Format("2006-01-02")).Scan(&minutes)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("daily wear minutes: %w", err)
	}
	return minutes, nil
}
