// Package repo T033：Dashboard 聚合查询数据访问层（daily_wear_stats + alerts + 身份域 join）
//
// 数据源约定（架构 §4.4）：Dashboard 全部读 daily_wear_stats 聚合表与身份域小表，
// 不扫 pressure_records 明细分区。日期参数以 'YYYY-MM-DD' 文本传参并显式 ::date，
// 规避容器 session timezone 对 DATE/timestamptz 隐式转换的影响（业务切日 = Asia/Shanghai）。
package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// KPIRow KPI 六指标单趟查询投影（avg_wear_minutes 由 service 换算小时）
type KPIRow struct {
	TotalPatients    int64
	ActiveWear       int64   // 窗口内有佩戴（wear_minutes > 0）的患者数
	AlertCount       int64   // 窗口内告警数
	AvgWearMinutes   float64 // 窗口内日聚合平均佩戴分钟
	DeviceOnlineRate float64 // online / 已绑定设备 × 100（无绑定设备为 0）
	MonthNewPatients int64   // 本自然月新增患者
}

// TrendRow 日趋势投影（wear：平均佩戴分钟；alert：告警条数）
type TrendRow struct {
	Date  time.Time
	Value float64
}

// RankingRow 团队/医生排行投影（complianceRate：wear_minutes >= 目标 的天数占比 × 100）
type RankingRow struct {
	Name         string
	TeamName     string // 仅医生排行使用（团队名 join，无团队为空串）
	PatientCount int64
	AvgWearMin   float64
	Compliance   float64
}

// DashboardStore Dashboard 聚合查询契约（service 层消费）
type DashboardStore interface {
	// KPI 六指标单趟查询；fromDate 为窗口起始日（YYYY-MM-DD），alertFrom 为告警时间窗起点
	KPI(ctx context.Context, fromDate string, alertFrom, monthStart time.Time) (*KPIRow, error)
	// WearTrend 按日平均佩戴分钟（fromDate/toDate 闭区间，YYYY-MM-DD）
	WearTrend(ctx context.Context, fromDate, toDate string) ([]TrendRow, error)
	// AlertTrend 按业务时区（Asia/Shanghai）切日的告警日计数
	AlertTrend(ctx context.Context, from time.Time) ([]TrendRow, error)
	// TeamRanking 团队排行（fromDate 起窗口；wearTargetMin 为达标判定分钟数）
	TeamRanking(ctx context.Context, fromDate string, wearTargetMin int) ([]RankingRow, error)
	// DoctorRanking 医生排行（fromDate 起窗口；wearTargetMin 为达标判定分钟数）
	DoctorRanking(ctx context.Context, fromDate string, wearTargetMin int) ([]RankingRow, error)
	// PatientAvgWearMinutes 每位患者窗口内日均佩戴分钟（佩戴分布输入）
	PatientAvgWearMinutes(ctx context.Context, fromDate string) ([]float64, error)
}

// DashboardRepo DashboardStore 的 pgx 实现
type DashboardRepo struct {
	pool *pgxpool.Pool
}

// NewDashboardRepo 创建 DashboardRepo
func NewDashboardRepo(pool *pgxpool.Pool) *DashboardRepo { return &DashboardRepo{pool: pool} }

// kpiSQL 六指标单趟查询（全部走聚合表/身份域小表，无明细扫描）
const kpiSQL = `
SELECT
  (SELECT COUNT(*) FROM patients)                                                     AS total_patients,
  (SELECT COUNT(DISTINCT patient_id) FROM daily_wear_stats
     WHERE stat_date >= $1::date AND wear_minutes > 0)                                AS active_wear,
  (SELECT COUNT(*) FROM alerts WHERE ts >= $2)                                        AS alert_count,
  (SELECT COALESCE(AVG(wear_minutes), 0) FROM daily_wear_stats
     WHERE stat_date >= $1::date)                                                     AS avg_wear_minutes,
  (SELECT CASE WHEN COUNT(*) FILTER (WHERE status <> 'unbound') = 0 THEN 0
               ELSE COUNT(*) FILTER (WHERE status = 'online') * 100.0 /
                    COUNT(*) FILTER (WHERE status <> 'unbound') END
     FROM devices)                                                                    AS device_online_rate,
  (SELECT COUNT(*) FROM patients WHERE created_at >= $3)                              AS month_new_patients`

// KPI 六指标单趟查询
func (r *DashboardRepo) KPI(ctx context.Context, fromDate string, alertFrom, monthStart time.Time) (*KPIRow, error) {
	var row KPIRow
	err := r.pool.QueryRow(ctx, kpiSQL, fromDate, alertFrom, monthStart).Scan(
		&row.TotalPatients, &row.ActiveWear, &row.AlertCount,
		&row.AvgWearMinutes, &row.DeviceOnlineRate, &row.MonthNewPatients,
	)
	if err != nil {
		return nil, fmt.Errorf("query dashboard kpi: %w", err)
	}
	return &row, nil
}

const wearTrendSQL = `
SELECT stat_date, AVG(wear_minutes)
FROM daily_wear_stats
WHERE stat_date >= $1::date AND stat_date <= $2::date
GROUP BY stat_date
ORDER BY stat_date`

// WearTrend 按日平均佩戴分钟
func (r *DashboardRepo) WearTrend(ctx context.Context, fromDate, toDate string) ([]TrendRow, error) {
	rows, err := r.pool.Query(ctx, wearTrendSQL, fromDate, toDate)
	if err != nil {
		return nil, fmt.Errorf("query wear trend: %w", err)
	}
	defer rows.Close()
	return scanTrendRows(rows)
}

const alertTrendSQL = `
SELECT date_trunc('day', ts AT TIME ZONE 'Asia/Shanghai')::date AS d, COUNT(*)
FROM alerts
WHERE ts >= $1
GROUP BY 1
ORDER BY 1`

// AlertTrend 告警日计数（业务时区切日）
func (r *DashboardRepo) AlertTrend(ctx context.Context, from time.Time) ([]TrendRow, error) {
	rows, err := r.pool.Query(ctx, alertTrendSQL, from)
	if err != nil {
		return nil, fmt.Errorf("query alert trend: %w", err)
	}
	defer rows.Close()
	return scanTrendRows(rows)
}

func scanTrendRows(rows pgx.Rows) ([]TrendRow, error) {
	var out []TrendRow
	for rows.Next() {
		var tr TrendRow
		if err := rows.Scan(&tr.Date, &tr.Value); err != nil {
			return nil, fmt.Errorf("scan trend row: %w", err)
		}
		out = append(out, tr)
	}
	return out, rows.Err()
}

const teamRankingSQL = `
SELECT t.name,
       COUNT(DISTINCT p.patient_id) AS patient_count,
       COALESCE(AVG(s.wear_minutes), 0) AS avg_wear_minutes,
       CASE WHEN COUNT(s.stat_date) = 0 THEN 0
            ELSE COUNT(s.stat_date) FILTER (WHERE s.wear_minutes >= $2) * 100.0 /
                 COUNT(s.stat_date) END AS compliance_rate
FROM teams t
LEFT JOIN patients p ON p.team_id = t.team_id
LEFT JOIN daily_wear_stats s ON s.patient_id = p.patient_id AND s.stat_date >= $1::date
GROUP BY t.team_id, t.name
ORDER BY avg_wear_minutes DESC, patient_count DESC, t.name
LIMIT 10`

// TeamRanking 团队排行（按窗口日均佩戴降序，Top 10）
func (r *DashboardRepo) TeamRanking(ctx context.Context, fromDate string, wearTargetMin int) ([]RankingRow, error) {
	rows, err := r.pool.Query(ctx, teamRankingSQL, fromDate, wearTargetMin)
	if err != nil {
		return nil, fmt.Errorf("query team ranking: %w", err)
	}
	defer rows.Close()
	return scanRankingRows(rows, false)
}

const doctorRankingSQL = `
SELECT d.name,
       COALESCE(t.name, '') AS team_name,
       COUNT(DISTINCT p.patient_id) AS patient_count,
       COALESCE(AVG(s.wear_minutes), 0) AS avg_wear_minutes,
       CASE WHEN COUNT(s.stat_date) = 0 THEN 0
            ELSE COUNT(s.stat_date) FILTER (WHERE s.wear_minutes >= $2) * 100.0 /
                 COUNT(s.stat_date) END AS compliance_rate
FROM doctors d
LEFT JOIN teams t ON t.team_id = d.team_id
LEFT JOIN patients p ON p.primary_doctor_id = d.doctor_id
LEFT JOIN daily_wear_stats s ON s.patient_id = p.patient_id AND s.stat_date >= $1::date
GROUP BY d.doctor_id, d.name, t.name
ORDER BY compliance_rate DESC, patient_count DESC, d.name
LIMIT 10`

// DoctorRanking 医生排行（按窗口达标率降序，Top 10）
func (r *DashboardRepo) DoctorRanking(ctx context.Context, fromDate string, wearTargetMin int) ([]RankingRow, error) {
	rows, err := r.pool.Query(ctx, doctorRankingSQL, fromDate, wearTargetMin)
	if err != nil {
		return nil, fmt.Errorf("query doctor ranking: %w", err)
	}
	defer rows.Close()
	return scanRankingRows(rows, true)
}

func scanRankingRows(rows pgx.Rows, withTeamName bool) ([]RankingRow, error) {
	var out []RankingRow
	for rows.Next() {
		var rr RankingRow
		var err error
		if withTeamName {
			err = rows.Scan(&rr.Name, &rr.TeamName, &rr.PatientCount, &rr.AvgWearMin, &rr.Compliance)
		} else {
			err = rows.Scan(&rr.Name, &rr.PatientCount, &rr.AvgWearMin, &rr.Compliance)
		}
		if err != nil {
			return nil, fmt.Errorf("scan ranking row: %w", err)
		}
		out = append(out, rr)
	}
	return out, rows.Err()
}

const patientAvgWearSQL = `
SELECT AVG(wear_minutes)
FROM daily_wear_stats
WHERE stat_date >= $1::date
GROUP BY patient_id`

// PatientAvgWearMinutes 每位患者窗口内日均佩戴分钟
func (r *DashboardRepo) PatientAvgWearMinutes(ctx context.Context, fromDate string) ([]float64, error) {
	rows, err := r.pool.Query(ctx, patientAvgWearSQL, fromDate)
	if err != nil {
		return nil, fmt.Errorf("query patient avg wear: %w", err)
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var minutes float64
		if err := rows.Scan(&minutes); err != nil {
			return nil, fmt.Errorf("scan patient avg wear: %w", err)
		}
		out = append(out, minutes)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────
// DashboardCache kpi:dashboard:{period} 查询回填缓存（架构 §4.7，TTL 60s）
// ─────────────────────────────────────────────────────────────

// KeyDashboardKPI kpi:dashboard:{period}
func KeyDashboardKPI(period string) string { return "kpi:dashboard:" + period }

// DashboardCache KPI 缓存读写（防 Dashboard 高频查询打库）
type DashboardCache struct {
	rdb *redis.Client
}

// NewDashboardCache 创建 DashboardCache
func NewDashboardCache(rdb *redis.Client) *DashboardCache { return &DashboardCache{rdb: rdb} }

// GetKPI 读缓存 JSON；未命中返回空串
func (c *DashboardCache) GetKPI(ctx context.Context, period string) (string, error) {
	v, err := c.rdb.Get(ctx, KeyDashboardKPI(period)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return v, err
}

// SetKPI 回填缓存 JSON（TTL 见架构 §4.7）
func (c *DashboardCache) SetKPI(ctx context.Context, period, valueJSON string, ttl time.Duration) error {
	return c.rdb.Set(ctx, KeyDashboardKPI(period), valueJSON, ttl).Err()
}
