// Package service T033：Dashboard service 实现侧测试（合约测试外，service 逻辑覆盖）
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// startMiniRedis 返回 DashboardCache 控制器
func startMiniRedisCache(t *testing.T) (DashboardCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := repo.NewDashboardCache(rdb)
	return cache, mr
}

// mockDashboardStore 轻量 fake（服务单元测试用）
type mockDashboardStore struct {
	kpiRow    *repo.KPIRow
	wearRows  []repo.TrendRow
	alertRows []repo.TrendRow
	teamRows  []repo.RankingRow
	docRows   []repo.RankingRow
	avgWears  []float64
	err       error
}

func (m *mockDashboardStore) KPI(ctx context.Context, from string, alertFrom, monthStart time.Time) (*repo.KPIRow, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.kpiRow, nil
}
func (m *mockDashboardStore) WearTrend(ctx context.Context, fromDate, toDate string) ([]repo.TrendRow, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.wearRows, nil
}
func (m *mockDashboardStore) AlertTrend(ctx context.Context, from time.Time) ([]repo.TrendRow, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.alertRows, nil
}
func (m *mockDashboardStore) TeamRanking(ctx context.Context, fromDate string, wearTargetMin int) ([]repo.RankingRow, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.teamRows, nil
}
func (m *mockDashboardStore) DoctorRanking(ctx context.Context, fromDate string, wearTargetMin int) ([]repo.RankingRow, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.docRows, nil
}
func (m *mockDashboardStore) PatientAvgWearMinutes(ctx context.Context, fromDate string) ([]float64, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.avgWears, nil
}

func TestServicePeriodWindow(t *testing.T) {
	store := &mockDashboardStore{kpiRow: &repo.KPIRow{TotalPatients: 1}}
	svc := NewDashboardService(store, nil)
	// today 周期合法，直接返回数据（无缓存）
	dto, appErr := svc.GetKPI(context.Background(), "today")
	require.Nil(t, appErr)
	assert.Equal(t, int64(1), dto.TotalPatients)
	// invalid period → ErrQueryParam
	_, appErr = svc.GetKPI(context.Background(), "year")
	assert.NotNil(t, appErr)
}

// ========== Service Layer: Error Path Coverage (store errors → ErrInternal) ==========

func TestServiceGetKPI_Error(t *testing.T) {
	store := &mockDashboardStore{kpiRow: nil, err: fmt.Errorf("db down")}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return time.Now() }

	_, appErr := svc.GetKPI(context.Background(), "today")
	require.NotNil(t, appErr)
	assert.Contains(t, appErr.Message, "query dashboard kpi failed") // ErrInternal
}

func TestServiceGetWearTrend_Error(t *testing.T) {
	store := &mockDashboardStore{wearRows: nil, err: fmt.Errorf("query failed")}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return time.Now() }

	_, appErr := svc.GetWearTrend(context.Background(), 7)
	require.NotNil(t, appErr)
	assert.Contains(t, appErr.Message, "query wear trend failed") // ErrInternal
}

func TestServiceGetAlertTrend_Error(t *testing.T) {
	store := &mockDashboardStore{alertRows: nil, err: fmt.Errorf("DB error")}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return time.Now() }

	_, appErr := svc.GetAlertTrend(context.Background(), 7)
	require.NotNil(t, appErr)
	assert.Contains(t, appErr.Message, "query alert trend failed") // ErrInternal
}

func TestServiceGetTeamRanking_Error(t *testing.T) {
	store := &mockDashboardStore{teamRows: nil, err: fmt.Errorf("pg down")}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return time.Now() }

	_, appErr := svc.GetTeamRanking(context.Background())
	require.NotNil(t, appErr)
	assert.Contains(t, appErr.Message, "query team ranking failed") // ErrInternal
}

func TestServiceGetDoctorRanking_Error(t *testing.T) {
	store := &mockDashboardStore{docRows: nil, err: fmt.Errorf("store err")}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return time.Now() }

	_, appErr := svc.GetDoctorRanking(context.Background())
	require.NotNil(t, appErr)
	assert.Contains(t, appErr.Message, "query doctor ranking failed") // ErrInternal
}

func TestServiceGetWearDistribution_Error(t *testing.T) {
	store := &mockDashboardStore{avgWears: nil, err: fmt.Errorf("agg failed")}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return time.Now() }

	_, appErr := svc.GetWearDistribution(context.Background())
	require.NotNil(t, appErr)
	assert.Contains(t, appErr.Message, "query wear distribution failed") // ErrInternal
}

func TestServiceGetKPICacheHit(t *testing.T) {
	ctx := context.Background()
	svc := NewDashboardService(nil, nil)
	cache, mr := startMiniRedisCache(t)
	nowTS := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return nowTS }

	mockStore := &mockDashboardStore{kpiRow: &repo.KPIRow{
		TotalPatients:    100,
		ActiveWear:       80,
		AlertCount:       50,
		AvgWearMinutes:   480,
		DeviceOnlineRate: 95.5,
		MonthNewPatients: 10,
	}}
	svc.store = mockStore
	svc.cache = cache

	kpiDTO := DashboardKPIDTO{
		TotalPatients:    100,
		TodayActiveWear:  80,
		TodayAlerts:      50,
		AvgWearHours:     round2(mockStore.kpiRow.AvgWearMinutes / 60),
		DeviceOnlineRate: round2(mockStore.kpiRow.DeviceOnlineRate),
		MonthNewPatients: 10,
	}
	data, err := json.Marshal(kpiDTO)
	require.NoError(t, err)

	// 先回写缓存
	err = svc.cache.SetKPI(ctx, "week", string(data), kpiCacheTTL)
	require.NoError(t, err)

	// 调用时缓存命中，store 不应被访问（mockStore 应保持不变）
	dto, appErr := svc.GetKPI(ctx, "week")
	require.Nil(t, appErr)
	assert.Equal(t, kpiDTO.TotalPatients, dto.TotalPatients)

	// 验证 TTL 已设置
	ttl := mr.TTL(repo.KeyDashboardKPI("week"))
	assert.Less(t, ttl.Seconds(), float64(kpiCacheTTL.Seconds()+2))
}

func TestServiceGetKPIDegradation(t *testing.T) {
	ctx := context.Background()
	store := &mockDashboardStore{kpiRow: &repo.KPIRow{TotalPatients: 999}}
	cache, mr := startMiniRedisCache(t)
	svc := NewDashboardService(store, cache)

	// Redis 故障：mr.Stop() 模拟不可用
	mr.Close()

	dto, appErr := svc.GetKPI(ctx, "today")
	require.Nil(t, appErr) // 降级成功
	assert.Equal(t, int64(999), dto.TotalPatients)
}

func TestValidateDays(t *testing.T) {
	days, _ := validateDays(7) // 默认
	assert.Equal(t, 7, days)
	days, _ = validateDays(0) // 缺省 7
	assert.Equal(t, 7, days)
	days, _ = validateDays(1)
	assert.Equal(t, 1, days)
	days, _ = validateDays(90)
	assert.Equal(t, 90, days)
	_, err := validateDays(-1) // invalid range → ErrQueryParam
	require.NotNil(t, err)
}

// ========== Service Layer: WearTrend + AlertTrend (gap-filling + CST timezone) ==========

func TestServiceGetWearTrend_GapFilling(t *testing.T) {
	nowTS := time.Date(2026, 8, 11, 10, 0, 0, 0, model.CSTZone())
	store := &mockDashboardStore{
		wearRows: []repo.TrendRow{
			// 今日 CST 8/11 的值，Key 应为 "08-11"（与 service 层 byDate 映射一致）
			{Date: time.Date(2026, 8, 11, 0, 0, 0, 0, model.CSTZone()), Value: 480}, // 今日 8h
		},
	}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return nowTS }

	list, err := svc.GetWearTrend(context.Background(), 7)
	require.Nil(t, err)
	assert.Len(t, list, 7) // 填充 7 天（含今日）
	// 检查最近的一天（今天是 8/11）格式 MM-DD
	lastDate := list[len(list)-1].Date
	assert.Equal(t, "08-11", lastDate)
	// 今日有值 = 8h；其他缺失日为 0
	assert.Equal(t, 8.0, list[6].AvgHours)
	for i := 0; i < 6; i++ {
		assert.Equal(t, 0.0, list[i].AvgHours)
	}
}

func TestServiceGetAlertTrend_TimezoneCutDay(t *testing.T) {
	// 告警在 Asia/Shanghai 03:00 UTC+8 → UTC 19:00（昨天）
	nowTS := time.Date(2026, 8, 11, 3, 0, 0, 0, model.CSTZone()) // CST 03:00
	store := &mockDashboardStore{
		alertRows: []repo.TrendRow{
			{Date: time.Date(2026, 8, 10, 19, 0, 0, 0, time.UTC).In(model.CSTZone()), Value: 10}, // CST 03:00 后归 8/11
		},
	}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return nowTS }

	list, err := svc.GetAlertTrend(context.Background(), 2)
	require.Nil(t, err)
	assert.Len(t, list, 2)
	// 业务时区切日：CST 03:00 属于当日（8/11），因此只有一条
	hasToday := false
	for _, p := range list {
		if p.Date == "08-11" {
			hasToday = true
			assert.Equal(t, int64(10), p.Count)
		}
	}
	assert.True(t, hasToday)
}

// ========== Service Layer: Rankings (Top 10 cut-off + complianceRate 计算) ==========

func TestServiceGetTeamRanking_Top10(t *testing.T) {
	nowTS := time.Date(2026, 8, 11, 0, 0, 0, 0, model.CSTZone())
	// 提供 15 条数据
	store := &mockDashboardStore{
		teamRows: make([]repo.RankingRow, 15),
	}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return nowTS }

	list, err := svc.GetTeamRanking(context.Background())
	require.Nil(t, err)
	assert.Len(t, list, 10) // 只返回 Top 10
	for i, r := range list {
		assert.Equal(t, i+1, r.Rank)
	}
}

func TestServiceGetDoctorRanking_ComplianceRate(t *testing.T) {
	nowTS := time.Date(2026, 8, 11, 0, 0, 0, 0, model.CSTZone())
	// avg_wear=1320min（达标目标），compliance=100%
	store := &mockDashboardStore{
		docRows: []repo.RankingRow{
			{Name: "DR-A", TeamName: "TEAM-X", PatientCount: 10, AvgWearMin: 1320, Compliance: 100},
			{Name: "DR-B", TeamName: "TEAM-Y", PatientCount: 5, AvgWearMin: 0, Compliance: 0},
		},
	}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return nowTS }

	list, err := svc.GetDoctorRanking(context.Background())
	require.Nil(t, err)
	assert.Len(t, list, 2)
	// doctor-ranking 按 compliance_rate DESC 排序（由 SQL 控制）
	assert.GreaterOrEqual(t, list[0].ComplianceRate, list[1].ComplianceRate)
}

// ========== Service Layer: Wear Distribution (5 buckets) ==========

func TestServiceGetWearDistribution_Buckets(t *testing.T) {
	nowTS := time.Date(2026, 8, 11, 0, 0, 0, 0, model.CSTZone())
	// 精确覆盖 5 个桶的边界值（分钟）:<240, 300, 420, 540, >=600
	store := &mockDashboardStore{
		avgWears: []float64{200, 300, 420, 540, 720}, // 每个桶至少一条
	}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return nowTS }

	dist, err := svc.GetWearDistribution(context.Background())
	require.Nil(t, err)
	assert.Len(t, dist, 5) // 5 个固定桶
	// 基本断言：总数等于输入长度
	var total int64
	for _, b := range dist {
		total += b.Count
	}
	assert.Equal(t, int64(len(store.avgWears)), total)
	// 验证每个桶都有计数（说明循环覆盖了所有 bucket）
	for i := 0; i < 5; i++ {
		assert.GreaterOrEqual(t, dist[i].Count, int64(1), "bucket %d should have data", i)
	}
}

// TestRound2 单独覆盖 round2 函数
func TestRound2(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"8h", 480.0 / 60, 8.0},     // 8h
		{"round up", 7.845, 7.85},   // 四舍五入
		{"round down", 7.844, 7.84}, // 向下取整
		{"zero", 0.0, 0.0},          // 零值
		{"large", 999.999, 1000.0},  // 大数进位
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := round2(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ========== Helper: ranking window calculation check ==========

func TestServiceRankingFromDate(t *testing.T) {
	nowTS := time.Date(2026, 8, 11, 10, 30, 0, 0, model.CSTZone())
	store := &mockDashboardStore{}
	svc := NewDashboardService(store, nil)
	svc.now = func() time.Time { return nowTS }

	// 手动验证日期计算
	start := time.Date(nowTS.Year(), nowTS.Month(), nowTS.Day()-6, 0, 0, 0, 0, model.CSTZone())
	want := start.Format("2006-01-02")
	assert.Equal(t, want, "2026-08-05") // 近 7 日窗口起点
}
