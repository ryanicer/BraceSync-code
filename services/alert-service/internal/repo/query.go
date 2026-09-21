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
	Type      string // pressure_high / wear_interrupt / sensor_drift / wear_duration_short（pressure_fluctuation 只读历史行）
	Status    string // process_status：pending / processing / processed（T257 2.7 三态）
	// StartTs/EndTs T300：采集时间范围，半开区间 [StartTs, EndTs)。
	// 由 handler 把「北京日历日」换算成时刻（末日次日 00:00 为 EndTs），
	// nil = 该端不限。与 ts 的索引序 (patient_id, ts DESC) 同向，不额外扫表。
	StartTs  *time.Time
	EndTs    *time.Time
	Page     int
	PageSize int
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
		conds = append(conds, "a.patient_id = $"+strconv.Itoa(len(args)))
	}
	if f.Type != "" {
		args = append(args, f.Type)
		conds = append(conds, "a.type = $"+strconv.Itoa(len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		conds = append(conds, "a.process_status = $"+strconv.Itoa(len(args)))
	}
	// T300：日期范围为半开区间 [start, end)，end 落在「末日次日 00:00」，
	// 用 < 而非 <= 才能既含末日 23:59:59 又不依赖 ts 的小数秒精度。
	if f.StartTs != nil {
		args = append(args, *f.StartTs)
		conds = append(conds, "a.ts >= $"+strconv.Itoa(len(args)))
	}
	if f.EndTs != nil {
		args = append(args, *f.EndTs)
		conds = append(conds, "a.ts < $"+strconv.Itoa(len(args)))
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
	PatientName    string
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
	InProgressAt   *time.Time // T257 2.7：NULL = 从未进入过「处理中」（含迁移前历史行）
	ProcessedBy    *string
	ProcessedAt    *time.Time
	ProcessNote    *string
}

// alertSelectColumns 查询列（可空列 COALESCE 兜底，避免 NULL 扫描错误）
// LEFT JOIN 后统一加表别名前缀：a=alerts, p=patients，防止列引用歧义
const alertSelectColumns = `a.alert_id, a.patient_id, COALESCE(p.name, ''), a.device_id, a.type,
	COALESCE(a.detail, ''), COALESCE(a.sensor_point, ''),
	COALESCE(a.threshold_value, 0), COALESCE(a.actual_value, 0),
	a.ts, a.read_status, a.process_status, a.resolved_status,
	a.resolved_at, a.in_progress_at, a.processed_by, a.processed_at, a.process_note`

// rowScanner pgx.Row / pgx.Rows 的最小公共接口（便于单测扫描逻辑）
type rowScanner interface{ Scan(dest ...any) error }

// scanAlertRow 扫描单行 alerts 投影（列序 = alertSelectColumns）
func scanAlertRow(s rowScanner, r *AlertRow) error {
	return s.Scan(&r.AlertID, &r.PatientID, &r.PatientName, &r.DeviceID, &r.Type,
		&r.Detail, &r.SensorPoint, &r.ThresholdValue, &r.ActualValue,
		&r.Ts, &r.ReadStatus, &r.ProcessStatus, &r.ResolvedStatus,
		&r.ResolvedAt, &r.InProgressAt, &r.ProcessedBy, &r.ProcessedAt, &r.ProcessNote)
}

// ListAlerts 分页查询告警（返回当页记录 + 筛选总数）。
// 先 NormalizePage 兜底分页参数；COUNT 与分页查询共享同一 WHERE。
func (r *PGAlertRepo) ListAlerts(ctx context.Context, f AlertQueryFilter) ([]AlertRow, int64, error) {
	f.NormalizePage()
	where, args := buildAlertWhere(f)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts AS a`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	pageArgs := append(append([]any{}, args...), f.PageSize, f.Offset())
	rows, err := r.pool.Query(ctx,
		`SELECT `+alertSelectColumns+` FROM alerts AS a LEFT JOIN patients AS p ON a.patient_id = p.patient_id`+where+
			` ORDER BY a.ts DESC, a.alert_id DESC LIMIT $`+strconv.Itoa(len(args)+1)+` OFFSET $`+strconv.Itoa(len(args)+2),
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

// ProcessState StartProcessing 的返回值（handler 据此映射 404/409/200）。
type ProcessState struct {
	Exists       bool
	Status       string // 本次调用后的 process_status
	InProgressAt *time.Time
}

// StartProcessing T257 2.7：pending → processing，写 in_progress_at = now()。
//   - pending → 置 processing，回读新时刻，Exists=true
//   - 已是 processing → **不刷新** in_progress_at（反复点「开始处理」不应把耗时清零），幂等返回原时刻
//   - 已 processed → 不回退（处理完的记录不能被重新打开），Status 原样返回供 handler 出 409
//   - 不存在 → Exists=false（handler 映射 404）
//
// 并发安全：UPDATE 只针对 pending；未命中时再单读一次当前状态，天然覆盖上面四种结局。
func (r *PGAlertRepo) StartProcessing(ctx context.Context, alertID int64) (ProcessState, error) {
	var st ProcessState
	cmd, err := r.pool.Exec(ctx,
		`UPDATE alerts SET process_status = 'processing', in_progress_at = now()
		 WHERE alert_id = $1 AND process_status = 'pending'`, alertID)
	if err != nil {
		return st, err
	}
	if cmd.RowsAffected() == 1 {
		st.Exists = true
		st.Status = "processing"
		return r.readState(ctx, alertID, &st)
	}
	err = r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM alerts WHERE alert_id = $1)`, alertID).Scan(&st.Exists)
	if err != nil || !st.Exists {
		return st, err
	}
	return r.readState(ctx, alertID, &st)
}

// readState 读回 alert_id 的当前处理态（Exists 由调用方保证为 true）。
func (r *PGAlertRepo) readState(ctx context.Context, alertID int64, st *ProcessState) (ProcessState, error) {
	err := r.pool.QueryRow(ctx,
		`SELECT process_status, in_progress_at FROM alerts WHERE alert_id = $1`,
		alertID).Scan(&st.Status, &st.InProgressAt)
	return *st, err
}

// ProcessAlert 标记告警已处理（幂等）：
//   - 存在且 pending / processing → 置 processed + processed_at + processed_by，返回 exists=true
//   - 存在且已 processed → 不重写处理时间与处理人（首次置为 processed 者为准），返回 exists=true（幂等）
//   - 不存在 → 返回 exists=false
//
// T257 2.7：守卫由「仅 pending 可改」放宽为「非 processed 即可改」——否则三态下
// 「处理中」的记录永远点不完成（跳级与逐级都走本端点，老调用方行为不变）。
// operatorID 来自 gateway 注入的 X-User-Id（可为空，空则不写 processed_by，保持旧行为）。
//
// T278-①：note = 处理备注，写入 process_note。口径与 operatorID 一致 ——
// 空串一律「不写、保留原值」（COALESCE(NULLIF(...))），所以无备注的老调用方
// （技师端 uni.request 不带 body）与「不带 body 的前端」行为完全不变。
// 与 processed_at / processed_by 同规则：已 processed 时整条 UPDATE 不命中，备注同样首次为准。
func (r *PGAlertRepo) ProcessAlert(ctx context.Context, alertID int64, operatorID, note string) (exists bool, err error) {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE alerts
		    SET process_status = 'processed',
		        processed_at   = now(),
		        processed_by   = COALESCE(NULLIF($2, ''), processed_by),
		        process_note   = COALESCE(NULLIF($3, ''), process_note)
		 WHERE alert_id = $1 AND process_status <> 'processed'`, alertID, operatorID, note)
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
