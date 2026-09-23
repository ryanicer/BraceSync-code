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

	"github.com/bracesync/bracesync/services/data-service/internal/model"
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

// KPICompareRow 上一周期对比基准投影（T248 1.1 · PRD §7D.1 KPI 表「对比基准」列）。
// 窗口 = 与当前 period 等长、紧邻在前的一段（[prevFromDate, fromDate)）。
// 🔴 不含设备在线率：devices.status 为当前态快照、无历史表，昨日在线率无从取（见 service 层注释）。
type KPICompareRow struct {
	ActiveWear           int64   // 上一周期有佩戴的去重患者数
	AlertCount           int64   // 上一周期告警数
	AvgWearMinutes       float64 // 上一周期平均佩戴分钟
	TotalPatientsAtMonth int64   // 上月末累计患者（created_at < 本月起点）
	PrevMonthNewPatients int64   // 上月新增患者
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
//
// 末位 scope 为 T350 数据范围：ScopeAll() = 全院（admin/客服/技师，SQL 与改造前逐字等价）；
// ScopeTeam(id) = 只统计该团队患者的行。
type DashboardStore interface {
	// KPI 六指标单趟查询；fromDate 为窗口起始日（YYYY-MM-DD），alertFrom 为告警时间窗起点
	KPI(ctx context.Context, fromDate string, alertFrom, monthStart time.Time, scope model.TeamScope) (*KPIRow, error)
	// KPICompare 上一等长周期的对比基准（T248 1.1）。prevFromDate/fromDate 为半开区间两端（YYYY-MM-DD），
	// prevAlertFrom/alertFrom 为同一区间的 timestamptz 表达，monthStart/prevMonthStart 为自然月起点。
	KPICompare(ctx context.Context, prevFromDate, fromDate string, prevAlertFrom, alertFrom, monthStart, prevMonthStart time.Time, scope model.TeamScope) (*KPICompareRow, error)
	// WearTrend 按日平均佩戴分钟（fromDate/toDate 闭区间，YYYY-MM-DD）
	WearTrend(ctx context.Context, fromDate, toDate string, scope model.TeamScope) ([]TrendRow, error)
	// AlertTrend 按业务时区（Asia/Shanghai）切日的告警日计数
	AlertTrend(ctx context.Context, from time.Time, scope model.TeamScope) ([]TrendRow, error)
	// TeamRanking 团队排行（fromDate 起窗口；wearTargetMin 为达标判定分钟数）
	TeamRanking(ctx context.Context, fromDate string, wearTargetMin int, scope model.TeamScope) ([]RankingRow, error)
	// DoctorRanking 医生排行（fromDate 起窗口；wearTargetMin 为达标判定分钟数）
	DoctorRanking(ctx context.Context, fromDate string, wearTargetMin int, scope model.TeamScope) ([]RankingRow, error)
	// PatientAvgWearMinutes 每位患者窗口内日均佩戴分钟（佩戴分布输入）
	PatientAvgWearMinutes(ctx context.Context, fromDate string, scope model.TeamScope) ([]float64, error)
}

// DashboardRepo DashboardStore 的 pgx 实现
type DashboardRepo struct {
	pool *pgxpool.Pool
}

// NewDashboardRepo 创建 DashboardRepo
func NewDashboardRepo(pool *pgxpool.Pool) *DashboardRepo { return &DashboardRepo{pool: pool} }

// kpiSQL 六指标单趟查询（全部走聚合表/身份域小表，无明细扫描）
//
// T350 团队范围：$4=$scoped(bool)、$5=$team(text)，经 CTE sp 收口一次 patients。
// $scoped=FALSE ⇒ 每个谓词短路为真，行集与本卡改造前逐字相同（非医生路径零影响）；
// $scoped=TRUE 且 $team 为 NULL（医生有身份但 doctors.team_id 未分配）⇒ sp 为空集，
// 六项自然归零，不退化成全院（fail-closed）。$team 恒为 NULL 或 patients.team_id 域内的值，
// 由 service 层从 doctors 表推导，客户端无法注入。
const kpiSQL = `
WITH sp AS (
  SELECT patient_id FROM patients WHERE $4::bool = FALSE OR team_id = $5::text
)
SELECT
  (SELECT COUNT(*) FROM patients
     WHERE $4::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp))            AS total_patients,
  (SELECT COUNT(DISTINCT patient_id) FROM daily_wear_stats
     WHERE stat_date >= $1::date AND wear_minutes > 0
       AND ($4::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp)))          AS active_wear,
  (SELECT COUNT(*) FROM alerts WHERE ts >= $2
       AND ($4::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp)))          AS alert_count,
  (SELECT COALESCE(AVG(wear_minutes), 0) FROM daily_wear_stats
     WHERE stat_date >= $1::date
       AND ($4::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp)))          AS avg_wear_minutes,
  (SELECT CASE WHEN COUNT(*) FILTER (WHERE status <> 'unbound') = 0 THEN 0
               ELSE COUNT(*) FILTER (WHERE status = 'online') * 100.0 /
                    COUNT(*) FILTER (WHERE status <> 'unbound') END
     FROM devices
     WHERE $4::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp))            AS device_online_rate,
  (SELECT COUNT(*) FROM patients WHERE created_at >= $3
       AND ($4::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp)))          AS month_new_patients`

// KPI 六指标单趟查询
func (r *DashboardRepo) KPI(ctx context.Context, fromDate string, alertFrom, monthStart time.Time,
	scope model.TeamScope) (*KPIRow, error) {
	var row KPIRow
	err := r.pool.QueryRow(ctx, kpiSQL, fromDate, alertFrom, monthStart, scope.Scoped(), scope.TeamArg()).Scan(
		&row.TotalPatients, &row.ActiveWear, &row.AlertCount,
		&row.AvgWearMinutes, &row.DeviceOnlineRate, &row.MonthNewPatients,
	)
	if err != nil {
		return nil, fmt.Errorf("query dashboard kpi: %w", err)
	}
	return &row, nil
}

// kpiCompareSQL 上一等长周期的对比基准（T248 1.1）。日期口径与 kpiSQL 一致：
// 文本传参 + 显式 ::date，规避容器 timezone 隐式转换（业务切日 = Asia/Shanghai）。
// T350：scope = $7/$8，收口方式与 kpiSQL 同（当前窗与前窗必须同一范围，否则「较昨日」跨团队比歪）。
const kpiCompareSQL = `
WITH sp AS (
  SELECT patient_id FROM patients WHERE $7::bool = FALSE OR team_id = $8::text
)
SELECT
  (SELECT COUNT(DISTINCT patient_id) FROM daily_wear_stats
     WHERE stat_date >= $1::date AND stat_date < $2::date AND wear_minutes > 0
       AND ($7::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp)))          AS active_wear,
  (SELECT COUNT(*) FROM alerts WHERE ts >= $3 AND ts < $4
       AND ($7::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp)))          AS alert_count,
  (SELECT COALESCE(AVG(wear_minutes), 0) FROM daily_wear_stats
     WHERE stat_date >= $1::date AND stat_date < $2::date
       AND ($7::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp)))          AS avg_wear_minutes,
  (SELECT COUNT(*) FROM patients WHERE created_at < $5
       AND ($7::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp)))         AS total_patients_at_month,
  (SELECT COUNT(*) FROM patients WHERE created_at >= $6 AND created_at < $5
       AND ($7::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp)))         AS prev_month_new_patients`

// KPICompare 上一等长周期对比基准查询
func (r *DashboardRepo) KPICompare(ctx context.Context, prevFromDate, fromDate string,
	prevAlertFrom, alertFrom, monthStart, prevMonthStart time.Time, scope model.TeamScope) (*KPICompareRow, error) {
	row := &KPICompareRow{}
	err := r.pool.QueryRow(ctx, kpiCompareSQL,
		prevFromDate, fromDate, prevAlertFrom, alertFrom, monthStart, prevMonthStart,
		scope.Scoped(), scope.TeamArg(),
	).Scan(&row.ActiveWear, &row.AlertCount, &row.AvgWearMinutes,
		&row.TotalPatientsAtMonth, &row.PrevMonthNewPatients)
	if err != nil {
		return nil, fmt.Errorf("query dashboard kpi compare: %w", err)
	}
	return row, nil
}

const wearTrendSQL = `
WITH sp AS (
  SELECT patient_id FROM patients WHERE $3::bool = FALSE OR team_id = $4::text
)
SELECT stat_date, AVG(wear_minutes)
FROM daily_wear_stats
WHERE stat_date >= $1::date AND stat_date <= $2::date
  AND ($3::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp))
GROUP BY stat_date
ORDER BY stat_date`

// WearTrend 按日平均佩戴分钟
func (r *DashboardRepo) WearTrend(ctx context.Context, fromDate, toDate string,
	scope model.TeamScope) ([]TrendRow, error) {
	rows, err := r.pool.Query(ctx, wearTrendSQL, fromDate, toDate, scope.Scoped(), scope.TeamArg())
	if err != nil {
		return nil, fmt.Errorf("query wear trend: %w", err)
	}
	defer rows.Close()
	return scanTrendRows(rows)
}

const alertTrendSQL = `
WITH sp AS (
  SELECT patient_id FROM patients WHERE $2::bool = FALSE OR team_id = $3::text
)
SELECT date_trunc('day', ts AT TIME ZONE 'Asia/Shanghai')::date AS d, COUNT(*)
FROM alerts
WHERE ts >= $1
  AND ($2::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp))
GROUP BY 1
ORDER BY 1`

// AlertTrend 告警日计数（业务时区切日）
func (r *DashboardRepo) AlertTrend(ctx context.Context, from time.Time,
	scope model.TeamScope) ([]TrendRow, error) {
	rows, err := r.pool.Query(ctx, alertTrendSQL, from, scope.Scoped(), scope.TeamArg())
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

// teamRankingSQL T350：scope = $3/$4，直接落在维度列 t.team_id 上（排行本身按团队分组，
// 医生只看自己那一行）；$team 为 NULL ⇒ 谓词恒非真 ⇒ 空结果，不放行全院排行。
const teamRankingSQL = `
SELECT t.name,
       COUNT(DISTINCT p.patient_id) AS patient_count,
       COALESCE(AVG(LEAST(s.wear_minutes, 1440)), 0) AS avg_wear_minutes,
       CASE WHEN COUNT(s.stat_date) = 0 THEN 0
            ELSE COUNT(s.stat_date) FILTER (WHERE s.wear_minutes >= $2) * 100.0 /
                 COUNT(s.stat_date) END AS compliance_rate
FROM teams t
LEFT JOIN patients p ON p.team_id = t.team_id
LEFT JOIN daily_wear_stats s ON s.patient_id = p.patient_id AND s.stat_date >= $1::date
WHERE ($3::bool = FALSE OR t.team_id = $4::text)
GROUP BY t.team_id, t.name
ORDER BY avg_wear_minutes DESC, patient_count DESC, t.name
LIMIT 10`

// TeamRanking 团队排行（按窗口日均佩戴降序，Top 10）
func (r *DashboardRepo) TeamRanking(ctx context.Context, fromDate string, wearTargetMin int,
	scope model.TeamScope) ([]RankingRow, error) {
	rows, err := r.pool.Query(ctx, teamRankingSQL, fromDate, wearTargetMin, scope.Scoped(), scope.TeamArg())
	if err != nil {
		return nil, fmt.Errorf("query team ranking: %w", err)
	}
	defer rows.Close()
	return scanRankingRows(rows, false)
}

// doctorRankingSQL T350：scope = $3/$4 落在 d.team_id（医生所属团队），
// 故医生看到的「医生排行」只含本科室医生；跨团队同院对比只对运营开放。
// 患者侧再加一条 p.team_id 同团队约束（挂在 LEFT JOIN 上，不动分组）：
// 医生若跨科带组外患者，那些患者的佩戴数据也不进他的统计口径 —— 与「只看本团队患者」一致。
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
                     AND ($3::bool = FALSE OR p.team_id = $4::text)
LEFT JOIN daily_wear_stats s ON s.patient_id = p.patient_id AND s.stat_date >= $1::date
WHERE ($3::bool = FALSE OR d.team_id = $4::text)
GROUP BY d.doctor_id, d.name, t.name
ORDER BY compliance_rate DESC, patient_count DESC, d.name
LIMIT 10`

// DoctorRanking 医生排行（按窗口达标率降序，Top 10）
func (r *DashboardRepo) DoctorRanking(ctx context.Context, fromDate string, wearTargetMin int,
	scope model.TeamScope) ([]RankingRow, error) {
	rows, err := r.pool.Query(ctx, doctorRankingSQL, fromDate, wearTargetMin, scope.Scoped(), scope.TeamArg())
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

// patientAvgWearSQL T350：scope = $2/$3（佩戴分布按患者出桶，医生只看本团队患者的分布）。
const patientAvgWearSQL = `
WITH sp AS (
  SELECT patient_id FROM patients WHERE $2::bool = FALSE OR team_id = $3::text
)
SELECT AVG(LEAST(wear_minutes, 1440))
FROM daily_wear_stats
WHERE stat_date >= $1::date
  AND ($2::bool = FALSE OR patient_id IN (SELECT patient_id FROM sp))
GROUP BY patient_id`

// PatientAvgWearMinutes 每位患者窗口内日均佩戴分钟
func (r *DashboardRepo) PatientAvgWearMinutes(ctx context.Context, fromDate string,
	scope model.TeamScope) ([]float64, error) {
	rows, err := r.pool.Query(ctx, patientAvgWearSQL, fromDate, scope.Scoped(), scope.TeamArg())
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
// DashboardCache kpi:dashboard:{period}:{scope} 查询回填缓存（架构 §4.7，TTL 60s）
// ─────────────────────────────────────────────────────────────

// KeyDashboardKPI kpi:dashboard:{period}:{scope}
//
// T350：范围必须进键。旧键只按 period 分片，一旦运营先查过一次 KPI，
// 同窗口内医生就会命中那份全院数字 —— 缓存层把刚收紧的 SQL 又放宽回去。
// scope 段取值：all（全院）/ 团队 ID（TEAM 前缀短码，无空格无冒号）/ none（医生无团队 ⇒ 空集）。
// 旧格式 kpi:dashboard:{period} 不再有读写方，TTL 60s 内自然过期，无需清理动作。
func KeyDashboardKPI(period string, scope model.TeamScope) string {
	return "kpi:dashboard:" + period + ":" + scopeCacheSeg(scope)
}

// scopeCacheSeg 范围 → 键段（三态必须两两不同名，否则串味）
func scopeCacheSeg(scope model.TeamScope) string {
	switch {
	case !scope.Scoped():
		return "all"
	case scope.TeamID == "":
		return "none"
	default:
		return scope.TeamID
	}
}

// DashboardCache KPI 缓存读写（防 Dashboard 高频查询打库）
type DashboardCache struct {
	rdb *redis.Client
}

// NewDashboardCache 创建 DashboardCache
func NewDashboardCache(rdb *redis.Client) *DashboardCache { return &DashboardCache{rdb: rdb} }

// GetKPI 读缓存 JSON；未命中返回空串
func (c *DashboardCache) GetKPI(ctx context.Context, period string, scope model.TeamScope) (string, error) {
	v, err := c.rdb.Get(ctx, KeyDashboardKPI(period, scope)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return v, err
}

// SetKPI 回填缓存 JSON（TTL 见架构 §4.7）
func (c *DashboardCache) SetKPI(ctx context.Context, period string, scope model.TeamScope, valueJSON string, ttl time.Duration) error {
	return c.rdb.Set(ctx, KeyDashboardKPI(period, scope), valueJSON, ttl).Err()
}
