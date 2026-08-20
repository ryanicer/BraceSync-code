// Package repo — alert-service 数据访问层（T008）
//
// 表级写归属（架构 §4.2/§4.3）：alerts 归 alert-service；
// devices 表本服务仅写 status 列（§3.6 扫描落库），其余列归 device-service。
// dev:lastseen:* 由 data-service 写入，本服务只读。
package repo

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

// ─────────────────────────────────────────────────────────────
// PGAlertRepo alerts 表仓储（实现 scanner.AlertStore）
// ─────────────────────────────────────────────────────────────

// PGAlertRepo alerts 表访问
type PGAlertRepo struct {
	pool *pgxpool.Pool
}

// NewAlertRepo 创建 PGAlertRepo
func NewAlertRepo(pool *pgxpool.Pool) *PGAlertRepo { return &PGAlertRepo{pool: pool} }

// CreateAlert 落库告警；自然唯一约束 uk_alerts_natural 冲突时 created=false（DB 层保底去重，A7）。
// 返回 alertID（BIGINT 字符串化）用于通知链路；冲突时 alertID 为空。
func (r *PGAlertRepo) CreateAlert(ctx context.Context, a scanner.NewAlert) (alertID string, created bool, err error) {
	var id int64
	err = r.pool.QueryRow(ctx, `
		INSERT INTO alerts (patient_id, device_id, type, sensor_point, detail, threshold_value, actual_value, ts)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT ON CONSTRAINT uk_alerts_natural DO NOTHING
		RETURNING alert_id`,
		a.PatientID, a.DeviceID, string(a.Type), a.SensorPoint, a.Detail, a.ThresholdValue, a.ActualValue, a.Ts).Scan(&id)
	if err != nil {
		// ON CONFLICT DO NOTHING 无 RETURNING 行 → Scan 返回 ErrNoRows，视为去重命中
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return strconv.FormatInt(id, 10), true, nil
}

// HasAlertSince 同设备同类型告警在 since 之后是否已存在（去重窗口 = 1×中断阈值）
func (r *PGAlertRepo) HasAlertSince(ctx context.Context, deviceID string, alertType engine.AlertType, since time.Time) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM alerts WHERE device_id = $1 AND type = $2 AND ts >= $3)`,
		deviceID, string(alertType), since).Scan(&exists)
	return exists, err
}

// HasActiveInterrupt 是否存在未 resolve 的佩戴中断告警
func (r *PGAlertRepo) HasActiveInterrupt(ctx context.Context, deviceID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM alerts
		 WHERE device_id = $1 AND type = $2 AND resolved_status = 'active')`,
		deviceID, string(engine.TypeWearInterrupt)).Scan(&exists)
	return exists, err
}

// ResolveActiveInterrupts 恢复上报自动 resolve（架构 §4.3）：
// active 佩戴中断告警置 resolved + resolved_at；与 process_status 正交。返回影响行数。
func (r *PGAlertRepo) ResolveActiveInterrupts(ctx context.Context, deviceID string, resolvedAt time.Time) (int64, error) {
	cmd, err := r.pool.Exec(ctx, `
		UPDATE alerts
		SET resolved_status = 'resolved', resolved_at = $2
		WHERE device_id = $1 AND type = $3 AND resolved_status = 'active'`,
		deviceID, resolvedAt, string(engine.TypeWearInterrupt))
	if err != nil {
		return 0, err
	}
	return cmd.RowsAffected(), nil
}

// ─────────────────────────────────────────────────────────────
// PGDeviceRepo devices 表仓储（实现 scanner.DeviceStore）
// ─────────────────────────────────────────────────────────────

// PGDeviceRepo devices 表访问（本服务仅读清单 + 写 status）
type PGDeviceRepo struct {
	pool *pgxpool.Pool
}

// NewDeviceRepo 创建 PGDeviceRepo
func NewDeviceRepo(pool *pgxpool.Pool) *PGDeviceRepo { return &PGDeviceRepo{pool: pool} }

// ListBoundDevices 全部已绑定患者的设备（扫描范围；未绑定设备不参与中断扫描）
func (r *PGDeviceRepo) ListBoundDevices(ctx context.Context) ([]scanner.Device, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT device_id, patient_id, status FROM devices WHERE patient_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []scanner.Device
	for rows.Next() {
		var d scanner.Device
		if err := rows.Scan(&d.DeviceID, &d.PatientID, &d.Status); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateStatus 状态落库（PRD §8.1 扫描落库）；status 未变化不写库（changed=false），
// 避免每 5min 全量 UPDATE 放大写量。updated_at 为状态变更行级时间戳（schema 约定应用层刷新）。
func (r *PGDeviceRepo) UpdateStatus(ctx context.Context, deviceID, status string) (bool, error) {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE devices SET status = $2, updated_at = now() WHERE device_id = $1 AND status <> $2`,
		deviceID, status)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() == 1, nil
}

// ─────────────────────────────────────────────────────────────
// RedisLastSeen dev:lastseen:{device_id} 只读（实现 scanner.LastSeenReader）
// ─────────────────────────────────────────────────────────────

// lastSeenKeyPrefix 与 data-service repo.KeyLastSeen 契约一致（架构 §4.7）。
// data-service 侧为写入方（Lua 单调推进），alert-service 只读。
const lastSeenKeyPrefix = "dev:lastseen:"

// RedisLastSeen Redis lastseen 读取器
type RedisLastSeen struct {
	rdb redis.Cmdable
}

// NewRedisLastSeen 创建 RedisLastSeen
func NewRedisLastSeen(rdb redis.Cmdable) *RedisLastSeen { return &RedisLastSeen{rdb: rdb} }

// GetLastSeen 读 dev:lastseen:{device_id}（Unix 秒字符串）；无记录返回 ok=false
func (r *RedisLastSeen) GetLastSeen(ctx context.Context, deviceID string) (time.Time, bool, error) {
	v, err := r.rdb.Get(ctx, lastSeenKeyPrefix+deviceID).Result()
	if errors.Is(err, redis.Nil) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, false, err
	}
	return time.Unix(sec, 0), true, nil
}
