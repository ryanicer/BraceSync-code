// Package repo msg-service 数据访问层（pgx 实现）
//
// 写归属（架构 §4.2 一期偏离声明，见 api-contracts.ts updateWearReminder/updateNotifyRule 注释）：
//   - notification_records / notification_retry_queue / quota_grants 写归 msg-service（000003 migration 新建）；
//   - patient_preferences（owner: user-service）仅 reminder_*/subscription_quota/updated_at 字段经本层写，
//     msg-service 是这些字段的唯一写入路径，无跨服务写冲突；
//   - alert_notify_rules（owner: alert-service）仅经本层后台接口写（写入路径唯一）。
//
// 其余表（patients / daily_wear_stats）只读。
package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
)

// ErrNotFound 行不存在（service 层映射为 50404）
var ErrNotFound = errors.New("repo: row not found")

// RecordFilter 通知记录过滤条件（后台日志查询；零值字段不参与过滤）
type RecordFilter struct {
	PatientID string
	AlertType string
	Channel   string
	Status    string
	Page      int
	PageSize  int
}

// ReminderCandidate 佩戴提醒扫描候选（reminder_enabled=true 且 reminder_time 已到）
type ReminderCandidate struct {
	PatientID    string
	ReminderTime string // HH:mm
}

// Store msg-service 存储契约（单测可用内存实现替换）
type Store interface {
	// ── 通知规则（alert_notify_rules）──
	FindRules(ctx context.Context) ([]model.NotifyRule, error)
	// FindRuleByType 查规则；未配置返回 (nil, nil)（未知 type 不发送）
	FindRuleByType(ctx context.Context, alertType model.AlertType) (*model.NotifyRule, error)
	UpsertRule(ctx context.Context, rule *model.NotifyRule) error

	// ── 订阅额度（patient_preferences.subscription_quota + quota_grants 台账）──
	// GetQuota 无偏好行时返回默认额度 3（patient_preferences DEFAULT 3）
	GetQuota(ctx context.Context, patientID string) (*model.SubscriptionQuota, error)
	// GrantQuota 幂等增额：同 (patient_id, idempotency_key) 仅增额一次（quota_grants UNIQUE 兜底）
	GrantQuota(ctx context.Context, patientID, idempotencyKey string, increment int) (*model.SubscriptionQuota, error)
	// ConsumeQuota 实际发送订阅消息时内部扣减（下限 0，无对外扣减接口，架构 §2.5）
	ConsumeQuota(ctx context.Context, patientID string) (*model.SubscriptionQuota, error)

	// ── 佩戴提醒（patient_preferences.reminder_*，一期偏离直写）──
	GetWearReminder(ctx context.Context, patientID string) (*model.WearReminderSettings, error)
	UpdateWearReminder(ctx context.Context, patientID string, enabled bool, reminderTime *string) (*model.WearReminderSettings, error)
	// ListReminderCandidates 扫描 reminder_enabled=true 且 reminder_time ≤ nowHM 的患者（架构 §7 每 15min）
	ListReminderCandidates(ctx context.Context, nowHM string) ([]ReminderCandidate, error)
	// ReminderSentToday 当日（业务时区切日）是否已推送过佩戴提醒（去重，防 15min 窗口重复推送）
	ReminderSentToday(ctx context.Context, patientID string, dayStart time.Time) (bool, error)

	// ── 发送记录（notification_records）──
	// CreateNotificationRecord 落库并回填 RecordID
	CreateNotificationRecord(ctx context.Context, rec *model.NotificationRecord) error
	GetNotificationRecord(ctx context.Context, recordID int64) (*model.NotificationRecord, error)
	// UpdateNotificationStatus 状态流转（成功置 sent+sentAt；失败置 failed）
	UpdateNotificationStatus(ctx context.Context, recordID int64, status string, sentAt *time.Time) error
	IncrementRetryCount(ctx context.Context, recordID int64) error
	// ListNotificationRecords 分页（created_at DESC 时间倒序）+ 总数
	ListNotificationRecords(ctx context.Context, f RecordFilter) ([]model.NotificationRecord, int, error)

	// ── 重试队列（notification_retry_queue，对齐 T010 降级队列模式）──
	EnqueueRetry(ctx context.Context, recordID int64, nextRetryAt time.Time) error
	// ListDueRetries 拉取到期的 pending 队列项（next_retry_at ≤ now）
	ListDueRetries(ctx context.Context, now time.Time, limit int) ([]model.RetryQueueItem, error)
	// FinishRetry 终态：done=重试成功 / failed=放弃（达到最大重试次数）
	FinishRetry(ctx context.Context, queueID int64, status string) error
	RescheduleRetry(ctx context.Context, queueID int64, retryCount int, nextRetryAt time.Time) error

	// ── 佩戴达标只读（daily_wear_stats rollup 层，禁扫明细，架构 §5）──
	// TodayWearMinutes 当日佩戴分钟数；无 rollup 行返回 0（视为未达标）
	TodayWearMinutes(ctx context.Context, patientID string, bizDate string) (int, error)
}

// PGStore Store 的 pgxpool 实现
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore 创建 PGStore
func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

// ─────────────────────────────────────────────────────────────
// 通知规则
// ─────────────────────────────────────────────────────────────

func scanRule(row pgx.Row) (*model.NotifyRule, error) {
	r := &model.NotifyRule{}
	err := row.Scan(&r.Type, &r.Channels, &r.NotifyTargets, &r.UpdatedBy, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// FindRules 全量规则（type 排序稳定输出）
func (r *PGStore) FindRules(ctx context.Context) ([]model.NotifyRule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT type, channels, notify_targets, COALESCE(updated_by, ''), updated_at
		 FROM alert_notify_rules ORDER BY type`)
	if err != nil {
		return nil, fmt.Errorf("find rules: %w", err)
	}
	defer rows.Close()

	out := []model.NotifyRule{}
	for rows.Next() {
		var rule model.NotifyRule
		if err = rows.Scan(&rule.Type, &rule.Channels, &rule.NotifyTargets, &rule.UpdatedBy, &rule.UpdatedAt); err != nil {
			return nil, fmt.Errorf("find rules scan: %w", err)
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

// FindRuleByType 按类型查规则；未配置返回 nil（service 层据此"未知 type 不发送"）
func (r *PGStore) FindRuleByType(ctx context.Context, alertType model.AlertType) (*model.NotifyRule, error) {
	rule, err := scanRule(r.pool.QueryRow(ctx,
		`SELECT type, channels, notify_targets, COALESCE(updated_by, ''), updated_at
		 FROM alert_notify_rules WHERE type = $1`, string(alertType)))
	if err != nil {
		return nil, fmt.Errorf("find rule %s: %w", alertType, err)
	}
	return rule, nil
}

// UpsertRule 规则 upsert（后台管理接口，写入路径唯一）
func (r *PGStore) UpsertRule(ctx context.Context, rule *model.NotifyRule) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO alert_notify_rules (type, channels, notify_targets, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (type) DO UPDATE
		   SET channels = EXCLUDED.channels,
		       notify_targets = EXCLUDED.notify_targets,
		       updated_by = EXCLUDED.updated_by,
		       updated_at = now()`,
		string(rule.Type), rule.Channels, rule.NotifyTargets, rule.UpdatedBy)
	if err != nil {
		return fmt.Errorf("upsert rule %s: %w", rule.Type, err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// 订阅额度
// ─────────────────────────────────────────────────────────────

// grantedTotal 累计授予额度 = 默认 3 + quota_grants 台账增量合计
func (r *PGStore) grantedTotal(ctx context.Context, tx pgx.Tx, patientID string) (int, error) {
	var sum int
	q := `SELECT $2 + COALESCE(SUM(increment), 0) FROM quota_grants WHERE patient_id = $1`
	var err error
	if tx != nil {
		err = tx.QueryRow(ctx, q, patientID, model.DefaultQuota).Scan(&sum)
	} else {
		err = r.pool.QueryRow(ctx, q, patientID, model.DefaultQuota).Scan(&sum)
	}
	if err != nil {
		return 0, fmt.Errorf("granted total: %w", err)
	}
	return sum, nil
}

func buildQuota(patientID string, remaining, total int, updatedAt *time.Time) *model.SubscriptionQuota {
	return &model.SubscriptionQuota{
		PatientID: patientID,
		Remaining: remaining,
		Total:     total,
		IsLow:     remaining <= model.QuotaLowThreshold,
		UpdatedAt: updatedAt,
	}
}

// GetQuota 读取额度快照；无偏好行返回默认额度 3
func (r *PGStore) GetQuota(ctx context.Context, patientID string) (*model.SubscriptionQuota, error) {
	var remaining int
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT subscription_quota, updated_at FROM patient_preferences WHERE patient_id = $1`,
		patientID).Scan(&remaining, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return buildQuota(patientID, model.DefaultQuota, model.DefaultQuota, nil), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get quota: %w", err)
	}
	total, err := r.grantedTotal(ctx, nil, patientID)
	if err != nil {
		return nil, err
	}
	return buildQuota(patientID, remaining, total, &updatedAt), nil
}

// GrantQuota 幂等增额（同 Idempotency-Key 不重复增额）：
// 事务内先写 quota_grants 台账（UNIQUE 冲突 → 已授予，直接返回当前额度），
// 再 upsert patient_preferences.subscription_quota。
func (r *PGStore) GrantQuota(ctx context.Context, patientID, idempotencyKey string, increment int) (*model.SubscriptionQuota, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("grant quota: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`INSERT INTO quota_grants (patient_id, idempotency_key, increment)
		 VALUES ($1, $2, $3) ON CONFLICT (patient_id, idempotency_key) DO NOTHING`,
		patientID, idempotencyKey, increment)
	if err != nil {
		return nil, fmt.Errorf("grant quota: ledger: %w", err)
	}

	var remaining int
	var updatedAt time.Time
	if tag.RowsAffected() == 1 {
		// 首次授予：增额（无偏好行时建行 = 默认 3 + increment）
		// 参数显式 ::int 转换：防 pgx 参数类型推断歧义（operator is not unique，SQLSTATE 42725）
		err = tx.QueryRow(ctx,
			`INSERT INTO patient_preferences (patient_id, subscription_quota, updated_at)
			 VALUES ($1, $2::int + $3::int, now())
			 ON CONFLICT (patient_id) DO UPDATE
			   SET subscription_quota = patient_preferences.subscription_quota + $3::int,
			       updated_at = now()
			 RETURNING subscription_quota, updated_at`,
			patientID, model.DefaultQuota, increment).Scan(&remaining, &updatedAt)
		if err != nil {
			return nil, fmt.Errorf("grant quota: upsert prefs: %w", err)
		}
	} else {
		// 幂等命中：同 Idempotency-Key 重复回报，不重复增额
		err = tx.QueryRow(ctx,
			`SELECT subscription_quota, updated_at FROM patient_preferences WHERE patient_id = $1`,
			patientID).Scan(&remaining, &updatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			remaining, updatedAt = model.DefaultQuota, time.Now()
		} else if err != nil {
			return nil, fmt.Errorf("grant quota: read prefs: %w", err)
		}
	}

	total, err := r.grantedTotal(ctx, tx, patientID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("grant quota: commit: %w", err)
	}
	return buildQuota(patientID, remaining, total, &updatedAt), nil
}

// ConsumeQuota 发送时内部扣减（下限 0）；无偏好行按默认 3 扣减后建行
func (r *PGStore) ConsumeQuota(ctx context.Context, patientID string) (*model.SubscriptionQuota, error) {
	var remaining int
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx,
		`INSERT INTO patient_preferences (patient_id, subscription_quota, updated_at)
		 VALUES ($1, $2::int - 1, now())
		 ON CONFLICT (patient_id) DO UPDATE
		   SET subscription_quota = GREATEST(patient_preferences.subscription_quota - 1, 0),
		       updated_at = now()
		 RETURNING subscription_quota, updated_at`,
		patientID, model.DefaultQuota).Scan(&remaining, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("consume quota: %w", err)
	}
	total, err := r.grantedTotal(ctx, nil, patientID)
	if err != nil {
		return nil, err
	}
	return buildQuota(patientID, remaining, total, &updatedAt), nil
}

// ─────────────────────────────────────────────────────────────
// 佩戴提醒（patient_preferences 直写，一期偏离声明见包注释）
// ─────────────────────────────────────────────────────────────

// GetWearReminder 读取提醒设置；无偏好行返回默认关闭
func (r *PGStore) GetWearReminder(ctx context.Context, patientID string) (*model.WearReminderSettings, error) {
	var enabled bool
	var reminderTime *string
	err := r.pool.QueryRow(ctx,
		`SELECT reminder_enabled,
		        TO_CHAR(reminder_time, 'HH24:MI')
		 FROM patient_preferences WHERE patient_id = $1`, patientID,
	).Scan(&enabled, &reminderTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return &model.WearReminderSettings{ReminderEnabled: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get wear reminder: %w", err)
	}
	return &model.WearReminderSettings{ReminderEnabled: enabled, ReminderTime: reminderTime}, nil
}

// UpdateWearReminder upsert 提醒设置（reminder_time 传 nil 清空）
func (r *PGStore) UpdateWearReminder(ctx context.Context, patientID string, enabled bool, reminderTime *string) (*model.WearReminderSettings, error) {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO patient_preferences (patient_id, reminder_enabled, reminder_time, updated_at)
		 VALUES ($1, $2, $3::time, now())
		 ON CONFLICT (patient_id) DO UPDATE
		   SET reminder_enabled = EXCLUDED.reminder_enabled,
		       reminder_time = EXCLUDED.reminder_time,
		       updated_at = now()`,
		patientID, enabled, reminderTime)
	if err != nil {
		return nil, fmt.Errorf("update wear reminder: %w", err)
	}
	return &model.WearReminderSettings{ReminderEnabled: enabled, ReminderTime: reminderTime}, nil
}

// ListReminderCandidates 到点候选扫描（每 15min 定时任务，架构 §7）
func (r *PGStore) ListReminderCandidates(ctx context.Context, nowHM string) ([]ReminderCandidate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT patient_id, TO_CHAR(reminder_time, 'HH24:MI')
		 FROM patient_preferences
		 WHERE reminder_enabled = true
		   AND reminder_time IS NOT NULL
		   AND reminder_time <= $1::time
		 ORDER BY patient_id`, nowHM)
	if err != nil {
		return nil, fmt.Errorf("list reminder candidates: %w", err)
	}
	defer rows.Close()

	out := []ReminderCandidate{}
	for rows.Next() {
		var c ReminderCandidate
		if err = rows.Scan(&c.PatientID, &c.ReminderTime); err != nil {
			return nil, fmt.Errorf("list reminder candidates scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReminderSentToday 当日是否已推送（kind=wear_reminder 且非 failed，created_at ≥ 业务日零点 UTC 时刻）
func (r *PGStore) ReminderSentToday(ctx context.Context, patientID string, dayStart time.Time) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM notification_records
		   WHERE patient_id = $1 AND kind = $2 AND status <> $3 AND created_at >= $4
		 )`, patientID, model.KindWearReminder, model.StatusFailed, dayStart).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("reminder sent today: %w", err)
	}
	return exists, nil
}

// ─────────────────────────────────────────────────────────────
// 发送记录
// ─────────────────────────────────────────────────────────────

// CreateNotificationRecord 落库并回填 record_id / created_at
func (r *PGStore) CreateNotificationRecord(ctx context.Context, rec *model.NotificationRecord) error {
	var alertID any
	if rec.AlertID != nil {
		alertID = *rec.AlertID
	}
	var alertType any
	if rec.AlertType != nil {
		alertType = string(*rec.AlertType)
	}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO notification_records
		   (patient_id, alert_id, alert_type, kind, channel, status, content, retry_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING record_id, created_at`,
		rec.PatientID, alertID, alertType, rec.Kind, rec.Channel, rec.Status, rec.Content, rec.RetryCount,
	).Scan(&rec.RecordID, &rec.CreatedAt)
	if err != nil {
		return fmt.Errorf("create notification record: %w", err)
	}
	return nil
}

func scanRecord(row pgx.Row) (*model.NotificationRecord, error) {
	rec := &model.NotificationRecord{}
	var alertID *string
	var alertType *string
	err := row.Scan(&rec.RecordID, &rec.PatientID, &alertID, &alertType, &rec.Kind,
		&rec.Channel, &rec.Status, &rec.Content, &rec.RetryCount, &rec.SentAt, &rec.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.AlertID = alertID
	if alertType != nil {
		at := model.AlertType(*alertType)
		rec.AlertType = &at
	}
	return rec, nil
}

// GetNotificationRecord 按 ID 查记录；不存在返回 ErrNotFound
func (r *PGStore) GetNotificationRecord(ctx context.Context, recordID int64) (*model.NotificationRecord, error) {
	rec, err := scanRecord(r.pool.QueryRow(ctx,
		`SELECT record_id, patient_id, alert_id, alert_type, kind, channel, status,
		        content, retry_count, sent_at, created_at
		 FROM notification_records WHERE record_id = $1`, recordID))
	if err != nil {
		return nil, fmt.Errorf("get notification record: %w", err)
	}
	return rec, nil
}

// UpdateNotificationStatus 状态流转（sent+sentAt / failed / degraded）
func (r *PGStore) UpdateNotificationStatus(ctx context.Context, recordID int64, status string, sentAt *time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE notification_records SET status = $2, sent_at = $3 WHERE record_id = $1`,
		recordID, status, sentAt)
	if err != nil {
		return fmt.Errorf("update notification status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IncrementRetryCount 重试失败递增计数
func (r *PGStore) IncrementRetryCount(ctx context.Context, recordID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notification_records SET retry_count = retry_count + 1 WHERE record_id = $1`, recordID)
	if err != nil {
		return fmt.Errorf("increment retry count: %w", err)
	}
	return nil
}

// ListNotificationRecords 分页过滤查询（created_at DESC 时间倒序，架构 §3.5 分页约定）
func (r *PGStore) ListNotificationRecords(ctx context.Context, f RecordFilter) ([]model.NotificationRecord, int, error) {
	var conds []string
	var args []any
	add := func(cond string, v any) {
		conds = append(conds, fmt.Sprintf(cond, len(args)+1))
		args = append(args, v)
	}
	if f.PatientID != "" {
		add(`patient_id = $%d`, f.PatientID)
	}
	if f.AlertType != "" {
		add(`alert_type = $%d`, f.AlertType)
	}
	if f.Channel != "" {
		add(`channel = $%d`, f.Channel)
	}
	if f.Status != "" {
		add(`status = $%d`, f.Status)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notification_records `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notification records: %w", err)
	}

	page, pageSize := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	q := `SELECT record_id, patient_id, alert_id, alert_type, kind, channel, status,
	             content, retry_count, sent_at, created_at
	      FROM notification_records ` + where +
		` ORDER BY created_at DESC, record_id DESC LIMIT $` + fmt.Sprint(len(args)+1) +
		` OFFSET $` + fmt.Sprint(len(args)+2)
	rows, err := r.pool.Query(ctx, q, append(args, pageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list notification records: %w", err)
	}
	defer rows.Close()

	out := []model.NotificationRecord{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("list notification records scan: %w", err)
		}
		out = append(out, *rec)
	}
	return out, total, rows.Err()
}

// ─────────────────────────────────────────────────────────────
// 重试队列
// ─────────────────────────────────────────────────────────────

// EnqueueRetry 失败记录入队（同记录已存在 pending 项时仅刷新重试时刻，幂等）
func (r *PGStore) EnqueueRetry(ctx context.Context, recordID int64, nextRetryAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO notification_retry_queue (record_id, retry_count, next_retry_at)
		 SELECT $1, retry_count, $2 FROM notification_records WHERE record_id = $1
		 ON CONFLICT DO NOTHING`, recordID, nextRetryAt)
	if err != nil {
		return fmt.Errorf("enqueue retry: %w", err)
	}
	return nil
}

// ListDueRetries 到期 pending 队列项
func (r *PGStore) ListDueRetries(ctx context.Context, now time.Time, limit int) ([]model.RetryQueueItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT queue_id, record_id, retry_count, next_retry_at, status
		 FROM notification_retry_queue
		 WHERE status = 'pending' AND next_retry_at <= $1
		 ORDER BY next_retry_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list due retries: %w", err)
	}
	defer rows.Close()

	out := []model.RetryQueueItem{}
	for rows.Next() {
		var item model.RetryQueueItem
		if err = rows.Scan(&item.QueueID, &item.RecordID, &item.RetryCount, &item.NextRetryAt, &item.Status); err != nil {
			return nil, fmt.Errorf("list due retries scan: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// FinishRetry 队列项终态（done / failed）
func (r *PGStore) FinishRetry(ctx context.Context, queueID int64, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notification_retry_queue SET status = $2 WHERE queue_id = $1`, queueID, status)
	if err != nil {
		return fmt.Errorf("finish retry: %w", err)
	}
	return nil
}

// RescheduleRetry 重试失败后更新计数与下次重试时刻（保持 pending）
func (r *PGStore) RescheduleRetry(ctx context.Context, queueID int64, retryCount int, nextRetryAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notification_retry_queue SET retry_count = $2, next_retry_at = $3
		 WHERE queue_id = $1`, queueID, retryCount, nextRetryAt)
	if err != nil {
		return fmt.Errorf("reschedule retry: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// 佩戴达标只读（daily_wear_stats rollup）
// ─────────────────────────────────────────────────────────────

// TodayWearMinutes 业务日佩戴分钟数（无 rollup 行 = 0，视为未达标 → 推送提醒）
func (r *PGStore) TodayWearMinutes(ctx context.Context, patientID string, bizDate string) (int, error) {
	var minutes int
	err := r.pool.QueryRow(ctx,
		`SELECT wear_minutes FROM daily_wear_stats WHERE patient_id = $1 AND stat_date = $2::date`,
		patientID, bizDate).Scan(&minutes)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("today wear minutes: %w", err)
	}
	return minutes, nil
}
