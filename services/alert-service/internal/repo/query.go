// Package repo — alerts 公开查询/处理数据访问（T028）
//
// 支撑公开端点 GET /api/v1/alerts 与 POST /api/v1/alerts/:alertId/process
// （契约 docs/ getAlerts / processAlert）。
//
// 查询性能：筛选条件均走已有索引（架构 §4.4）：
//
//	patient_id → idx_alerts_patient_ts (patient_id, ts DESC)
//	type       → idx_alerts_type
//	分页排序   → ORDER BY ts DESC, alert_id DESC（patientId 场景命中复合索引序）
package repo

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// 分页默认值与上限（防止大页扫描）
const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

// AlertQueryFilter 公开查询筛选条件（空值 = 不过滤）
type AlertQueryFilter struct {
	PatientID string
	Type      string // pressure_high / pressure_fluctuation / wear_interrupt / sensor_drift
	Status    string // process_status：pending / processed
	Page      int
	PageSize  int
}

// NormalizePage 补齐/钳制分页参数：缺省 page=1 / pageSize=20，pageSize 上限 100。
// 调用方（handler）已保证入参合法（page≥1、pageSize≥1），此处只做兜底。
func (f *AlertQueryFilter) NormalizePage() {
	if f.Page < 1 {
		f.Page = defaultPage
	}
	if f.PageSize < 1 {
		f.PageSize = defaultPageSize
	}
	if f.PageSize > maxPageSize {
		f.PageSize = maxPageSize
	}
}

// Offset 分页偏移量
func (f AlertQueryFilter) Offset() int { return (f.Page - 1) * f.PageSize }

// buildAlertWhere 构造筛选 WHERE 子句（纯函数，可单测）；
// 条件顺序固定，参数位与 args 一一对应
func buildAlertWhere(f AlertQueryFilter) (string, []any) {
	var conds []string
	var args []any
	if f.PatientID != "" {
		args = append(args, f.PatientID)
		conds = append(conds, "patient_id = $"+strconv.Itoa(len(args)))
	}
	if f.Type != "" {
		args = append(args, f.Type)
		conds = append(conds, "type = $"+strconv.Itoa(len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		conds = append(conds, "process_status = $"+strconv.Itoa(len(args)))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// AlertRow alerts 公开查询投影（字段对齐 shared-types Alert）
type AlertRow struct {
	AlertID        int64
	PatientID      string
	DeviceID       string
	Type           string
	Detail         string
	SensorPoint    string
	ThresholdValue float64
	ActualValue    float64
	Ts             time.Time
	ReadStatus     string
	ProcessStatus  string
	ResolvedStatus string
	ResolvedAt     *time.Time
	ProcessedBy    *string
	ProcessedAt    *time.Time
	ProcessNote    *string
}

// alertSelectColumns 查询列（可空列 COALESCE 兜底，避免 NULL 扫描错误）
const alertSelectColumns = `alert_id, patient_id, device_id, type,
	COALESCE(detail, ''), COALESCE(sensor_point, ''),
	COALESCE(threshold_value, 0), COALESCE(actual_value, 0),
	ts, read_status, process_status, resolved_status,
	resolved_at, processed_by, processed_at, process_note`

// rowScanner pgx.Row / pgx.Rows 的最小公共接口（便于单测扫描逻辑）
type rowScanner interface{ Scan(dest ...any) error }

// scanAlertRow 扫描单行 alerts 投影（列序 = alertSelectColumns）
func scanAlertRow(s rowScanner, r *AlertRow) error {
	return s.Scan(&r.AlertID, &r.PatientID, &r.DeviceID, &r.Type,
		&r.Detail, &r.SensorPoint, &r.ThresholdValue, &r.ActualValue,
		&r.Ts, &r.ReadStatus, &r.ProcessStatus, &r.ResolvedStatus,
		&r.ResolvedAt, &r.ProcessedBy, &r.ProcessedAt, &r.ProcessNote)
}

// ListAlerts 分页查询告警（返回当页记录 + 筛选总数）。
// 先 NormalizePage 兜底分页参数；COUNT 与分页查询共享同一 WHERE。
func (r *PGAlertRepo) ListAlerts(ctx context.Context, f AlertQueryFilter) ([]AlertRow, int64, error) {
	f.NormalizePage()
	where, args := buildAlertWhere(f)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	pageArgs := append(append([]any{}, args...), f.PageSize, f.Offset())
	rows, err := r.pool.Query(ctx,
		`SELECT `+alertSelectColumns+` FROM alerts`+where+
			` ORDER BY ts DESC, alert_id DESC LIMIT $`+strconv.Itoa(len(args)+1)+` OFFSET $`+strconv.Itoa(len(args)+2),
		pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]AlertRow, 0, f.PageSize)
	for rows.Next() {
		var row AlertRow
		if err := scanAlertRow(rows, &row); err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// ProcessAlert 标记告警已处理（幂等）：
//   - 存在且 pending → 置 processed + processed_at，返回 exists=true
//   - 存在且已 processed → 不重写处理时间，返回 exists=true（幂等）
//   - 不存在 → 返回 exists=false
func (r *PGAlertRepo) ProcessAlert(ctx context.Context, alertID int64) (exists bool, err error) {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE alerts SET process_status = 'processed', processed_at = now()
		 WHERE alert_id = $1 AND process_status = 'pending'`, alertID)
	if err != nil {
		return false, err
	}
	if cmd.RowsAffected() == 1 {
		return true, nil
	}
	// 未更新：可能已处理（幂等成功）或不存在（404）
	err = r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM alerts WHERE alert_id = $1)`, alertID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
