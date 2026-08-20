// Package service T033：Dashboard 聚合查询服务（6 端点编排）
//
// 周期窗口口径（业务切日 Asia/Shanghai，架构 §3.5）：
//   - period=today  当日 00:00 起
//   - period=week   近 7 个自然日（含今日）
//   - period=month  近 30 个自然日（含今日）
//
// KPI 缓存语义（架构 §4.7）：kpi:dashboard:{period} 查询回填，TTL 60s；
// Redis 故障降级直查 DB（Dashboard 可用性优先，不阻塞主流程）。
// 排行/趋势/分布窗口固定近 7 日（admin-web 契约调用口径）。
package service

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// KPI 周期枚举白名单（契约 getDashboardKPI period 参数）
var validKPIPeriods = map[string]struct{}{
	"today": {},
	"week":  {},
	"month": {},
}

const (
	// kpiCacheTTL 架构 §4.7：kpi:dashboard TTL 60s
	kpiCacheTTL = 60 * time.Second
	// maxTrendDays 趋势查询天数上限（防大范围聚合扫描）
	maxTrendDays = 90
)

// ─────────────────────────────────────────────────────────────
// DTO（字段对齐 shared-types DashboardKPI / TeamRanking / DoctorRanking）
// ─────────────────────────────────────────────────────────────

// DashboardKPIDTO 对齐 shared-types DashboardKPI
type DashboardKPIDTO struct {
	TotalPatients    int64   `json:"totalPatients"`
	TodayActiveWear  int64   `json:"todayActiveWear"`
	TodayAlerts      int64   `json:"todayAlerts"`
	AvgWearHours     float64 `json:"avgWearHours"`
	DeviceOnlineRate float64 `json:"deviceOnlineRate"`
	MonthNewPatients int64   `json:"monthNewPatients"`
}

// WearTrendPoint 佩戴趋势点（date 为 MM-DD，对齐 admin-web mock 口径）
type WearTrendPoint struct {
	Date     string  `json:"date"`
	AvgHours float64 `json:"avgHours"`
}

// AlertTrendPoint 告警趋势点
type AlertTrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// TeamRankingDTO 对齐 shared-types TeamRanking
type TeamRankingDTO struct {
	Rank           int     `json:"rank"`
	TeamName       string  `json:"teamName"`
	PatientCount   int64   `json:"patientCount"`
	AvgDailyWear   float64 `json:"avgDailyWear"`
	ComplianceRate float64 `json:"complianceRate"`
}

// DoctorRankingDTO 对齐 shared-types DoctorRanking
type DoctorRankingDTO struct {
	Rank           int     `json:"rank"`
	DoctorName     string  `json:"doctorName"`
	TeamName       string  `json:"teamName"`
	PatientCount   int64   `json:"patientCount"`
	ComplianceRate float64 `json:"complianceRate"`
}

// WearDistributionBucket 佩戴时长分布桶（range 文案对齐 admin-web mock）
type WearDistributionBucket struct {
	Range string `json:"range"`
	Count int64  `json:"count"`
}

// ─────────────────────────────────────────────────────────────
// DashboardService 编排：参数校验 + 周期窗口 + KPI 缓存
// ─────────────────────────────────────────────────────────────

// DashboardCache KPI 缓存依赖（repo.DashboardCache 实现）
type DashboardCache interface {
	GetKPI(ctx context.Context, period string) (string, error)
	SetKPI(ctx context.Context, period, valueJSON string, ttl time.Duration) error
}

// DashboardService Dashboard 查询编排（store 由 main 注入 repo.DashboardRepo）
type DashboardService struct {
	store repo.DashboardStore
	cache DashboardCache
	now   func() time.Time
}

// NewDashboardService 组装 DashboardService
func NewDashboardService(store repo.DashboardStore, cache DashboardCache) *DashboardService {
	return &DashboardService{store: store, cache: cache, now: time.Now}
}

// periodWindow period 枚举 → 窗口起点（CST 切日；today=当日，week=近7日，month=近30日）
func (s *DashboardService) periodWindow(period string) (fromDate string, fromTime time.Time, appErr *model.AppError) {
	if _, ok := validKPIPeriods[period]; !ok {
		return "", time.Time{}, model.ErrQueryParam("invalid period %q (want today|week|month)", period)
	}
	now := s.now().In(model.CSTZone())
	var days int
	switch period {
	case "week":
		days = 6
	case "month":
		days = 29
	}
	start := time.Date(now.Year(), now.Month(), now.Day()-days, 0, 0, 0, 0, model.CSTZone())
	return start.Format("2006-01-02"), start, nil
}

// GetKPI 契约 getDashboardKPI：缓存命中直返；未命中查库并回填（TTL 60s，§4.7）。
// Redis 故障降级直查 DB（可用性优先）。
func (s *DashboardService) GetKPI(ctx context.Context, period string) (*DashboardKPIDTO, *model.AppError) {
	fromDate, fromTime, appErr := s.periodWindow(period)
	if appErr != nil {
		return nil, appErr
	}

	if s.cache != nil {
		if cached, err := s.cache.GetKPI(ctx, period); err != nil {
			log.Warn().Err(err).Str("period", period).Msg("dashboard kpi cache read failed, fallback to DB")
		} else if cached != "" {
			var dto DashboardKPIDTO
			if json.Unmarshal([]byte(cached), &dto) == nil {
				return &dto, nil
			}
			log.Warn().Str("period", period).Msg("dashboard kpi cache corrupted, fallback to DB")
		}
	}

	monthStart := time.Date(fromTime.Year(), fromTime.Month(), 1, 0, 0, 0, 0, model.CSTZone())
	row, err := s.store.KPI(ctx, fromDate, fromTime, monthStart)
	if err != nil {
		log.Error().Err(err).Str("period", period).Msg("query dashboard kpi failed")
		return nil, model.ErrInternal("query dashboard kpi failed")
	}
	dto := &DashboardKPIDTO{
		TotalPatients:    row.TotalPatients,
		TodayActiveWear:  row.ActiveWear,
		TodayAlerts:      row.AlertCount,
		AvgWearHours:     round2(row.AvgWearMinutes / 60),
		DeviceOnlineRate: round2(row.DeviceOnlineRate),
		MonthNewPatients: row.MonthNewPatients,
	}
	if data, mErr := json.Marshal(dto); mErr == nil && s.cache != nil {
		if sErr := s.cache.SetKPI(ctx, period, string(data), kpiCacheTTL); sErr != nil {
			log.Warn().Err(sErr).Str("period", period).Msg("dashboard kpi cache backfill failed")
		}
	}
	return dto, nil
}

// validateDays 趋势天数参数校验（缺省 7，范围 [1, 90]）
func validateDays(days int) (int, *model.AppError) {
	if days == 0 {
		days = model.RankingWindowDays
	}
	if days < 1 || days > maxTrendDays {
		return 0, model.ErrQueryParam("invalid days %d (want 1-%d)", days, maxTrendDays)
	}
	return days, nil
}

// GetWearTrend 契约 getWearTrend：近 days 日平均佩戴小时（缺失日补 0）
func (s *DashboardService) GetWearTrend(ctx context.Context, days int) ([]WearTrendPoint, *model.AppError) {
	days, appErr := validateDays(days)
	if appErr != nil {
		return nil, appErr
	}
	to := s.now().In(model.CSTZone())
	from := time.Date(to.Year(), to.Month(), to.Day()-(days-1), 0, 0, 0, 0, model.CSTZone())

	rows, err := s.store.WearTrend(ctx, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		log.Error().Err(err).Msg("query wear trend failed")
		return nil, model.ErrInternal("query wear trend failed")
	}
	byDate := make(map[string]float64, len(rows))
	for _, r := range rows {
		byDate[r.Date.Format("01-02")] = round2(r.Value / 60)
	}
	out := make([]WearTrendPoint, 0, days)
	for d := 0; d < days; d++ {
		day := from.AddDate(0, 0, d)
		out = append(out, WearTrendPoint{Date: day.Format("01-02"), AvgHours: byDate[day.Format("01-02")]})
	}
	return out, nil
}

// GetAlertTrend 契约 getAlertTrend：近 days 日告警数（业务时区切日，缺失日补 0）
func (s *DashboardService) GetAlertTrend(ctx context.Context, days int) ([]AlertTrendPoint, *model.AppError) {
	days, appErr := validateDays(days)
	if appErr != nil {
		return nil, appErr
	}
	to := s.now().In(model.CSTZone())
	from := time.Date(to.Year(), to.Month(), to.Day()-(days-1), 0, 0, 0, 0, model.CSTZone())

	rows, err := s.store.AlertTrend(ctx, from)
	if err != nil {
		log.Error().Err(err).Msg("query alert trend failed")
		return nil, model.ErrInternal("query alert trend failed")
	}
	byDate := make(map[string]int64, len(rows))
	for _, r := range rows {
		byDate[r.Date.Format("01-02")] = int64(r.Value)
	}
	out := make([]AlertTrendPoint, 0, days)
	for d := 0; d < days; d++ {
		day := from.AddDate(0, 0, d)
		out = append(out, AlertTrendPoint{Date: day.Format("01-02"), Count: byDate[day.Format("01-02")]})
	}
	return out, nil
}

// rankingFromDate 排行窗口起点（近 7 日含今日，YYYY-MM-DD）
func (s *DashboardService) rankingFromDate() string {
	now := s.now().In(model.CSTZone())
	return time.Date(now.Year(), now.Month(), now.Day()-(model.RankingWindowDays-1), 0, 0, 0, 0, model.CSTZone()).Format("2006-01-02")
}

// GetTeamRanking 契约 getTeamRanking:团队排行 Top 10(近 7 日窗口)
func (s *DashboardService) GetTeamRanking(ctx context.Context) ([]TeamRankingDTO, *model.AppError) {
	rows, err := s.store.TeamRanking(ctx, s.rankingFromDate(), model.WearTargetMinutes)
	if err != nil {
		log.Error().Err(err).Msg("query team ranking failed")
		return nil, model.ErrInternal("query team ranking failed")
	}
	out := make([]TeamRankingDTO, 0, len(rows))
	for i, r := range rows {
		if i >= 10 {
			break // Top 10
		}
		out = append(out, TeamRankingDTO{
			Rank:           i + 1,
			TeamName:       r.Name,
			PatientCount:   r.PatientCount,
			AvgDailyWear:   round2(r.AvgWearMin / 60),
			ComplianceRate: round2(r.Compliance),
		})
	}
	return out, nil
}

// GetDoctorRanking 契约 getDoctorRanking:医生排行 Top 10(近 7 日窗口)
func (s *DashboardService) GetDoctorRanking(ctx context.Context) ([]DoctorRankingDTO, *model.AppError) {
	rows, err := s.store.DoctorRanking(ctx, s.rankingFromDate(), model.WearTargetMinutes)
	if err != nil {
		log.Error().Err(err).Msg("query doctor ranking failed")
		return nil, model.ErrInternal("query doctor ranking failed")
	}
	out := make([]DoctorRankingDTO, 0, len(rows))
	for i, r := range rows {
		if i >= 10 {
			break // Top 10
		}
		out = append(out, DoctorRankingDTO{
			Rank:           i + 1,
			DoctorName:     r.Name,
			TeamName:       r.TeamName,
			PatientCount:   r.PatientCount,
			ComplianceRate: round2(r.Compliance),
		})
	}
	return out, nil
}

// wearDistributionRanges 佩戴时长分布桶上界（小时，末桶无上界）；文案对齐 admin-web mock
var wearDistributionRanges = []struct {
	label  string
	maxHrs float64
}{
	{"< 4小时", 4},
	{"4-6小时", 6},
	{"6-8小时", 8},
	{"8-10小时", 10},
	{"≥ 10小时", math.Inf(1)},
}

// GetWearDistribution 契约 getWearDistribution：按患者近 7 日日均佩戴时长分桶计数
func (s *DashboardService) GetWearDistribution(ctx context.Context) ([]WearDistributionBucket, *model.AppError) {
	avgs, err := s.store.PatientAvgWearMinutes(ctx, s.rankingFromDate())
	if err != nil {
		log.Error().Err(err).Msg("query wear distribution failed")
		return nil, model.ErrInternal("query wear distribution failed")
	}
	counts := make([]int64, len(wearDistributionRanges))
	for _, minutes := range avgs {
		hours := minutes / 60
		for i, b := range wearDistributionRanges {
			if hours < b.maxHrs {
				counts[i]++
				break
			}
		}
	}
	out := make([]WearDistributionBucket, 0, len(wearDistributionRanges))
	for i, b := range wearDistributionRanges {
		out = append(out, WearDistributionBucket{Range: b.label, Count: counts[i]})
	}
	return out, nil
}

// round2 保留两位小数（Dashboard 展示口径）
func round2(v float64) float64 { return math.Round(v*100) / 100 }
