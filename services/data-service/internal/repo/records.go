// Package repo data-service 数据访问层（PostgreSQL + Redis）
//
// 写归属（架构 §4.2）：pressure_records、daily_wear_stats 归 data-service。
// devices / sys_configs 仅读（跨服务只读不写）。
package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// PendingFrame 待落库帧（已通过业务校验）
type PendingFrame struct {
	Ts        time.Time
	Points    [model.PointCount]float32
	Battery   int
	FaultCode int
}

// RecordStore pressure_records 读写契约（service 层消费）
type RecordStore interface {
	// InsertRecord 单帧幂等落库：ON CONFLICT (device_id, ts) DO NOTHING。
	// 返回 record_id 与是否实际插入（false=幂等命中重复帧）。
	InsertRecord(ctx context.Context, deviceID, patientID string, f PendingFrame) (recordID int64, inserted bool, err error)
	// BatchInsert 单事务批量幂等落库，返回实际插入帧的采集时间列表。
	BatchInsert(ctx context.Context, deviceID, patientID string, frames []PendingFrame) (acceptedTS []time.Time, err error)
	// QueryHistory 按 patient_id + 时间范围分页读分区表，返回行与总数。
	QueryHistory(ctx context.Context, patientID string, from, to time.Time, page, pageSize int) ([]model.PressureRecord, int64, error)
}

// RecordRepo RecordStore 的 pgx 实现
type RecordRepo struct {
	pool *pgxpool.Pool
}

// NewRecordRepo 创建 RecordRepo
func NewRecordRepo(pool *pgxpool.Pool) *RecordRepo { return &RecordRepo{pool: pool} }

const insertRecordSQL = `
INSERT INTO pressure_records (device_id, patient_id, ts,
  p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
  p11,p12,p13,p14,p15,p16,p17,p18,p19,p20)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
ON CONFLICT (device_id, ts) DO NOTHING
RETURNING record_id`

// InsertRecord 实现幂等单帧落库（幂等键 (device_id, ts)，架构 §3.5）
func (r *RecordRepo) InsertRecord(ctx context.Context, deviceID, patientID string, f PendingFrame) (int64, bool, error) {
	args := frameArgs(deviceID, patientID, f)
	var recordID int64
	err := r.pool.QueryRow(ctx, insertRecordSQL, args...).Scan(&recordID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil // 幂等命中：重复帧
	}
	if err != nil {
		return 0, false, mapPGError(err)
	}
	return recordID, true, nil
}

// batchInsertColumns 批量插入列数：device_id, patient_id, ts, p01..p20
const batchInsertColumns = 23

// buildBatchInsertSQL 构造 n 帧的多行幂等 INSERT 语句（占位符 $1..$n*23）
func buildBatchInsertSQL(n int) string {
	var sb strings.Builder
	sb.WriteString(`INSERT INTO pressure_records (device_id, patient_id, ts,
  p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
  p11,p12,p13,p14,p15,p16,p17,p18,p19,p20) VALUES `)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('(')
		base := i * batchInsertColumns
		for j := 0; j < batchInsertColumns; j++ {
			if j > 0 {
				sb.WriteByte(',')
			}
			fmt.Fprintf(&sb, "$%d", base+j+1)
		}
		sb.WriteByte(')')
	}
	sb.WriteString(` ON CONFLICT (device_id, ts) DO NOTHING RETURNING ts`)
	return sb.String()
}

// BatchInsert 单事务多行 INSERT + ON CONFLICT DO NOTHING（协议 §4.2）
func (r *RecordRepo) BatchInsert(ctx context.Context, deviceID, patientID string, frames []PendingFrame) ([]time.Time, error) {
	if len(frames) == 0 {
		return nil, nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	args := make([]any, 0, len(frames)*batchInsertColumns)
	for _, f := range frames {
		args = append(args, frameArgs(deviceID, patientID, f)...)
	}

	rows, err := tx.Query(ctx, buildBatchInsertSQL(len(frames)), args...)
	if err != nil {
		return nil, mapPGError(err)
	}
	var accepted []time.Time
	for rows.Next() {
		var ts time.Time
		if err := rows.Scan(&ts); err != nil {
			rows.Close()
			return nil, err
		}
		accepted = append(accepted, ts)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return accepted, nil
}

const historySQL = `
SELECT record_id, device_id, patient_id, ts,
  p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
  p11,p12,p13,p14,p15,p16,p17,p18,p19,p20,
  max_pressure, upload_time
FROM pressure_records
WHERE patient_id = $1 AND ts >= $2 AND ts < $3
ORDER BY ts DESC
LIMIT $4 OFFSET $5`

const historyCountSQL = `SELECT count(*) FROM pressure_records WHERE patient_id = $1 AND ts >= $2 AND ts < $3`

// QueryHistory 分区表历史查询（命中 (patient_id, ts DESC) 分区内索引）
func (r *RecordRepo) QueryHistory(ctx context.Context, patientID string, from, to time.Time, page, pageSize int) ([]model.PressureRecord, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, historyCountSQL, patientID, from, to).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx, historySQL, patientID, from, to, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []model.PressureRecord
	for rows.Next() {
		rec := model.PressureRecord{}
		dest := make([]any, 0, model.PointCount+6)
		dest = append(dest, &rec.RecordID, &rec.DeviceID, &rec.PatientID, &rec.Ts)
		for i := 0; i < model.PointCount; i++ {
			dest = append(dest, &rec.Points[i])
		}
		dest = append(dest, &rec.MaxPressure, &rec.UploadTime)
		if err := rows.Scan(dest...); err != nil {
			return nil, 0, err
		}
		list = append(list, rec)
	}
	return list, total, rows.Err()
}

// frameArgs 组装 23 个插入参数
func frameArgs(deviceID, patientID string, f PendingFrame) []any {
	args := make([]any, 0, 23)
	args = append(args, deviceID, patientID, f.Ts)
	for i := 0; i < model.PointCount; i++ {
		args = append(args, f.Points[i])
	}
	return args
}

// mapPGError 将可识别的 PG 错误映射为业务错误（其余原样返回）
func mapPGError(err error) error {
	// 帧时间落在未预建分区范围（早于 202607 或晚于已建分区）
	if strings.Contains(err.Error(), "no partition of relation") {
		return fmt.Errorf("%w: %v", ErrNoPartition, err)
	}
	return err
}

// ErrNoPartition 分区缺失哨兵错误（service 层映射为 20402）
var ErrNoPartition = errors.New("timestamp outside existing partitions")

// IsNoPartitionError 判定是否为分区缺失错误
func IsNoPartitionError(err error) bool { return errors.Is(err, ErrNoPartition) }
