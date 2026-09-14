package repo

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bracesync/bracesync/services/data-service/internal/calibration"
)

// BaselineRepo calibration.Store 的 pgx 实现（baselines 表只读，写归 device-service，D3）
type BaselineRepo struct {
	pool *pgxpool.Pool
}

// NewBaselineRepo 创建 BaselineRepo
func NewBaselineRepo(pool *pgxpool.Pool) *BaselineRepo { return &BaselineRepo{pool: pool} }

// GetLatestBaseline 返回设备当前生效基线（baseline_id 最新一条）。
// 规矩 A（单次权威校准，T174）：一安装一基线，一机多装按安装时序推进，最新 baseline_id 即当前。
// T173 000013 迁移为本查询补 baselines(device_id) 读取索引。
func (r *BaselineRepo) GetLatestBaseline(ctx context.Context, deviceID string) (calibration.Baseline, bool, error) {
	var bl calibration.Baseline
	var offsets []float32
	err := r.pool.QueryRow(ctx,
		`SELECT baseline_id, offset_values FROM baselines WHERE device_id = $1 ORDER BY baseline_id DESC LIMIT 1`,
		deviceID,
	).Scan(&bl.BaselineID, &offsets)
	if err == pgx.ErrNoRows {
		return calibration.Baseline{}, false, nil
	}
	if err != nil {
		return calibration.Baseline{}, false, err
	}
	if len(offsets) != 20 {
		return calibration.Baseline{}, false, nil
	}
	copy(bl.Offsets[:], offsets)
	return bl, true, nil
}
