// Package repo T021：daily_wear_stats 与 health_reports 数据访问层
package repo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// DailyWearStatsStore daily_wear_stats 读写契约（service 层消费）
type DailyWearStatsStore interface {
	// Upsert 幂等 UPSERT（ON CONFLICT (patient_id, stat_date) DO UPDATE）
	Upsert(ctx context.Context, stats []model.DailyWearStats) error
	// AggregateDate 聚合指定 UTC 时间窗口内所有患者的明细 → 返回 DailyWearStats 切片。
	// from/to 为 UTC 时间（由调用方传入 Asia/Shanghai 切日转换后的 UTC 范围），
	// wearingThresholdN 为佩戴帧判定阈值（单帧 20 点峰值高于该值视为佩戴帧）。
	// 口径（T352）：佩戴分钟 = 佩戴帧时间跨度 + 一个实测帧间隔；日均压力 = 佩戴帧 20 点全点均值；
	// 异常数 = 同窗口 alerts 行数（与告警落库口径一致）。
	AggregateDate(ctx context.Context, from, to time.Time, wearingThresholdN float64) ([]model.DailyWearStats, error)
	// QueryRange 查询日期范围内的日聚合数据（报告生成用），按 stat_date ASC 排列。
	QueryRange(ctx context.Context, patientID string, from, to time.Time) ([]model.DailyWearStats, error)
	// ListPatientsWithStats 列出指定日期范围内有聚合数据的所有患者 ID（报告生成时批量遍历）。
	ListPatientsWithStats(ctx context.Context, from, to time.Time) ([]string, error)
}

// HealthReportStore health_reports 读写契约
type HealthReportStore interface {
	// InsertReport 插入报告（ON CONFLICT DO NOTHING 幂等）；返回是否实际插入。
	InsertReport(ctx context.Context, report *model.HealthReport) (inserted bool, err error)
	// QueryPreviousReport 查询上一周期同类型报告（趋势对比用）；无记录返回 nil, nil。
	QueryPreviousReport(ctx context.Context, patientID, reportType string, currentStart time.Time) (*model.HealthReport, error)
	// LatestSuggestion 查询患者最新矫形方案的建议文本（orthosis_plans.content）；无方案返回空串。
	LatestSuggestion(ctx context.Context, patientID string) (string, error)
}

// ─────────────────────────────────────────────────────────────
// RollupRepo DailyWearStatsStore 的 pgx 实现
// ─────────────────────────────────────────────────────────────

// RollupRepo 聚合表数据访问
type RollupRepo struct {
	pool *pgxpool.Pool
}

// NewRollupRepo 创建 RollupRepo
func NewRollupRepo(pool *pgxpool.Pool) *RollupRepo { return &RollupRepo{pool: pool} }

const upsertDailyWearStatsSQL = `
INSERT INTO daily_wear_stats
  (patient_id, stat_date, wear_minutes, avg_pressure, max_pressure, max_point, frame_count, abnormal_count, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
ON CONFLICT (patient_id, stat_date) DO UPDATE SET
  wear_minutes   = EXCLUDED.wear_minutes,
  avg_pressure   = EXCLUDED.avg_pressure,
  max_pressure   = EXCLUDED.max_pressure,
  max_point      = EXCLUDED.max_point,
  frame_count    = EXCLUDED.frame_count,
  abnormal_count = EXCLUDED.abnormal_count,
  updated_at     = now()`

// Upsert 批量幂等 UPSERT
func (r *RollupRepo) Upsert(ctx context.Context, stats []model.DailyWearStats) error {
	if len(stats) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, s := range stats {
		batch.Queue(upsertDailyWearStatsSQL,
			s.PatientID, s.StatDate, s.WearMinutes, s.AvgPressure,
			s.MaxPressure, s.MaxPoint, s.FrameCount, s.AbnormalCount,
		)
	}
	br := r.pool.SendBatch(ctx, batch)
	for i := 0; i < len(stats); i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("upsert daily_wear_stats [%d]: %w", i, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("close batch: %w", err)
	}
	return nil
}

// aggregateDateSQL 按日聚合 pressure_records 明细：
// 佩戴帧判定 max_pressure > $3（配置 wearing_pressure_threshold，读不到回退默认）。
// 佩戴分钟不走「帧数 × 配置采集间隔」（配置 30 分钟对实际约 31 秒上报，放大近 58 倍），
// 改为回传佩戴帧时间跨度秒数，由 Go 侧按「跨度 + 一个实测间隔」折算（T352）。
// 日均压力取佩戴帧 20 个采集点的全点均值（旧口径 AVG(max_pressure) 是帧峰值均值，
// 与压力上限线不同层）。abnormal_count 取同窗口 alerts 行数 —— 与告警落库同源
// （alert-service 评估命中才写行、命中时同步给实时 stat:today 的 abnormal_count 加一）。
// 时间窗口：[$1, $2) UTC（由调用方传入 Asia/Shanghai 切日转换后的 UTC 范围）。
const aggregateDateSQL = `
SELECT patient_id,
       COUNT(*)                                          AS frame_count,
       COUNT(*) FILTER (WHERE max_pressure > $3)         AS wearing_frames,
       COALESCE(AVG((p01+p02+p03+p04+p05+p06+p07+p08+p09+p10+
                     p11+p12+p13+p14+p15+p16+p17+p18+p19+p20) / 20.0)
                FILTER (WHERE max_pressure > $3), 0)::real AS avg_pressure,
       MAX(max_pressure)                                 AS max_pressure,
       COALESCE((EXTRACT(EPOCH FROM (
         MAX(ts) FILTER (WHERE max_pressure > $3) - MIN(ts) FILTER (WHERE max_pressure > $3)
       )))::float8, 0)                                   AS wear_span_seconds,
       (ARRAY_AGG(
         CASE WHEN max_pressure > 0 THEN
           'P' || LPAD((
             CASE
               WHEN p01 = max_pressure THEN '01'
               WHEN p02 = max_pressure THEN '02'
               WHEN p03 = max_pressure THEN '03'
               WHEN p04 = max_pressure THEN '04'
               WHEN p05 = max_pressure THEN '05'
               WHEN p06 = max_pressure THEN '06'
               WHEN p07 = max_pressure THEN '07'
               WHEN p08 = max_pressure THEN '08'
               WHEN p09 = max_pressure THEN '09'
               WHEN p10 = max_pressure THEN '10'
               WHEN p11 = max_pressure THEN '11'
               WHEN p12 = max_pressure THEN '12'
               WHEN p13 = max_pressure THEN '13'
               WHEN p14 = max_pressure THEN '14'
               WHEN p15 = max_pressure THEN '15'
               WHEN p16 = max_pressure THEN '16'
               WHEN p17 = max_pressure THEN '17'
               WHEN p18 = max_pressure THEN '18'
               WHEN p19 = max_pressure THEN '19'
               WHEN p20 = max_pressure THEN '20'
               ELSE '01'
             END
           )::text, 2, '0')
         END
         ORDER BY max_pressure DESC
       ) FILTER (WHERE max_pressure > 0))[1]              AS max_point,
       (SELECT COUNT(*) FROM alerts a
         WHERE a.patient_id = pressure_records.patient_id
           AND a.ts >= $1 AND a.ts < $2)                  AS abnormal_count
FROM pressure_records
WHERE ts >= $1 AND ts < $2
GROUP BY patient_id`

// AggregateDate 聚合指定 UTC 时间窗口内的所有患者明细
func (r *RollupRepo) AggregateDate(ctx context.Context, from, to time.Time, wearingThresholdN float64) ([]model.DailyWearStats, error) {
	rows, err := r.pool.Query(ctx, aggregateDateSQL, from, to, float32(wearingThresholdN))
	if err != nil {
		return nil, fmt.Errorf("aggregate pressure_records: %w", err)
	}
	defer rows.Close()

	var stats []model.DailyWearStats
	for rows.Next() {
		var s model.DailyWearStats
		var wearingFrames int
		var avgP, maxP float32
		var wearSpanSeconds float64
		var maxPoint *string
		if err := rows.Scan(&s.PatientID, &s.FrameCount, &wearingFrames, &avgP, &maxP,
			&wearSpanSeconds, &maxPoint, &s.AbnormalCount); err != nil {
			return nil, fmt.Errorf("scan aggregate row: %w", err)
		}
		s.WearMinutes = wearMinutesFromSpan(wearingFrames, wearSpanSeconds)
		s.AvgPressure = avgP
		s.MaxPressure = maxP
		if maxPoint != nil {
			s.MaxPoint = *maxPoint
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// wearMinutesFromSpan 佩戴分钟 = 佩戴帧时间跨度 + 一个实测帧间隔（跨度 /(帧数-1)），
// 等价于「佩戴帧数 × 实测间隔」，与跨度口径在 ±1 帧内自洽（T352 验收判据）。
// 不足两帧无法确立跨度，按 0 计（单个瞬时样本不构成时长，不用配置间隔臆造）。
// 上限仍夹到物理日，防跨月/时区异常窗口把单日撑爆。
func wearMinutesFromSpan(wearingFrames int, spanSeconds float64) int {
	if wearingFrames < 2 || spanSeconds <= 0 {
		return 0
	}
	minutes := int(math.Round(spanSeconds * float64(wearingFrames) / float64(wearingFrames-1) / 60))
	return min(minutes, model.MaxWearMinutesPerDay)
}

// queryRangeSQL 的日期边界先换算到 Asia/Shanghai 再截 date：调用方传的是「CST 日界 UTC 化」的
// time.Time，而 stat_date 是 DATE。直接比较会让窗口整体早一天——问 [X,X] 只回 X-1 的行、
// 区间最后一天永远取不到（T352：CI 集成测试实测 0 行；staging 只读探针实测同样偏一天）。
// 切日口径与 dashboard_repo 一致：业务时区固定 Asia/Shanghai，不依赖会话时区/参数隐式转换。
const queryRangeSQL = `
SELECT patient_id, stat_date, wear_minutes, avg_pressure, max_pressure,
       COALESCE(max_point, ''), frame_count, abnormal_count, updated_at
FROM daily_wear_stats
WHERE patient_id = $1
  AND stat_date >= ($2::timestamptz AT TIME ZONE 'Asia/Shanghai')::date
  AND stat_date <  ($3::timestamptz AT TIME ZONE 'Asia/Shanghai')::date
ORDER BY stat_date ASC`

// QueryRange 查询日期范围内的日聚合数据
func (r *RollupRepo) QueryRange(ctx context.Context, patientID string, from, to time.Time) ([]model.DailyWearStats, error) {
	rows, err := r.pool.Query(ctx, queryRangeSQL, patientID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query daily_wear_stats: %w", err)
	}
	defer rows.Close()

	var stats []model.DailyWearStats
	for rows.Next() {
		var s model.DailyWearStats
		if err := rows.Scan(&s.PatientID, &s.StatDate, &s.WearMinutes, &s.AvgPressure,
			&s.MaxPressure, &s.MaxPoint, &s.FrameCount, &s.AbnormalCount, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan daily_wear_stats: %w", err)
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// listPatientsSQL 同 queryRangeSQL：日期边界显式换算 Asia/Shanghai 后再截 date，不整体早一天
const listPatientsSQL = `
SELECT DISTINCT patient_id FROM daily_wear_stats
WHERE stat_date >= ($1::timestamptz AT TIME ZONE 'Asia/Shanghai')::date
  AND stat_date <  ($2::timestamptz AT TIME ZONE 'Asia/Shanghai')::date
ORDER BY patient_id`

// ListPatientsWithStats 列出日期范围内有聚合数据的患者
func (r *RollupRepo) ListPatientsWithStats(ctx context.Context, from, to time.Time) ([]string, error) {
	rows, err := r.pool.Query(ctx, listPatientsSQL, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ─────────────────────────────────────────────────────────────
// ReportRepo HealthReportStore 的 pgx 实现
// ─────────────────────────────────────────────────────────────

// ReportRepo 健康报告数据访问
type ReportRepo struct {
	pool *pgxpool.Pool
}

// NewReportRepo 创建 ReportRepo
func NewReportRepo(pool *pgxpool.Pool) *ReportRepo { return &ReportRepo{pool: pool} }

const insertReportSQL = `
INSERT INTO health_reports
  (patient_id, report_type, period_start, period_end, wear_compliance_rate,
   avg_pressure, trend_judgment, suggestion, generate_time)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (patient_id, report_type, period_start) DO NOTHING
RETURNING report_id`

// InsertReport 幂等插入健康报告
func (r *ReportRepo) InsertReport(ctx context.Context, report *model.HealthReport) (bool, error) {
	var id int64
	err := r.pool.QueryRow(ctx, insertReportSQL,
		report.PatientID, report.ReportType, report.PeriodStart, report.PeriodEnd,
		report.WearComplianceRate, report.AvgPressure, report.TrendJudgment,
		report.Suggestion, report.GenerateTime,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // 幂等命中
	}
	if err != nil {
		return false, fmt.Errorf("insert health_report: %w", err)
	}
	report.ReportID = id
	return true, nil
}

const queryPrevReportSQL = `
SELECT report_id, patient_id, report_type, period_start, period_end,
       COALESCE(wear_compliance_rate, 0), COALESCE(avg_pressure, 0),
       COALESCE(trend_judgment, 'flat'), COALESCE(suggestion, ''), generate_time
FROM health_reports
WHERE patient_id = $1 AND report_type = $2 AND period_start < $3
ORDER BY period_start DESC
LIMIT 1`

// QueryPreviousReport 查询上一周期同类型报告（趋势对比）
func (r *ReportRepo) QueryPreviousReport(ctx context.Context, patientID, reportType string, currentStart time.Time) (*model.HealthReport, error) {
	var rpt model.HealthReport
	err := r.pool.QueryRow(ctx, queryPrevReportSQL, patientID, reportType, currentStart).Scan(
		&rpt.ReportID, &rpt.PatientID, &rpt.ReportType, &rpt.PeriodStart, &rpt.PeriodEnd,
		&rpt.WearComplianceRate, &rpt.AvgPressure, &rpt.TrendJudgment, &rpt.Suggestion,
		&rpt.GenerateTime,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query previous report: %w", err)
	}
	return &rpt, nil
}

const latestSuggestionSQL = `
SELECT content FROM orthosis_plans
WHERE patient_id = $1
ORDER BY created_at DESC
LIMIT 1`

// LatestSuggestion 查询患者最新矫形方案建议
func (r *ReportRepo) LatestSuggestion(ctx context.Context, patientID string) (string, error) {
	var content string
	err := r.pool.QueryRow(ctx, latestSuggestionSQL, patientID).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query latest suggestion: %w", err)
	}
	return content, nil
}
