// Package service T248 1.1：数据概览 KPI「对比基准」字段测试（等长前窗口径）
//
// 沿用同包 mockDashboardStore，经包级 spy 记录 KPICompare 入参（不改既有结构体字段）。
package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// t248Args KPICompare 实收入参快照
type t248Args struct {
	prevFromDate   string
	fromDate       string
	prevAlertFrom  time.Time
	alertFrom      time.Time
	monthStart     time.Time
	prevMonthStart time.Time
}

var (
	t248CmpRow  *repo.KPICompareRow // nil → 返回零值行（既有 KPI 用例不因新增查询改变断言）
	t248CmpErr  error
	t248CmpCall int
	t248CmpArgs t248Args
)

func t248ResetCompareSpy() {
	t248CmpRow, t248CmpErr, t248CmpCall = nil, nil, 0
	t248CmpArgs = t248Args{}
}

func (m *mockDashboardStore) KPICompare(ctx context.Context, prevFromDate, fromDate string,
	prevAlertFrom, alertFrom, monthStart, prevMonthStart time.Time) (*repo.KPICompareRow, error) {
	t248CmpCall++
	t248CmpArgs = t248Args{prevFromDate, fromDate, prevAlertFrom, alertFrom, monthStart, prevMonthStart}
	if t248CmpErr != nil {
		return nil, t248CmpErr
	}
	if t248CmpRow != nil {
		return t248CmpRow, nil
	}
	return &repo.KPICompareRow{}, nil
}

// t248FixedNow 固定为 CST 2026-09-19 14:30（避开零点，验证切日取窗口起点而非 now）
func t248FixedNow() time.Time {
	return time.Date(2026, 9, 19, 14, 30, 0, 0, model.CSTZone())
}

func TestT248_KPI_ComparisonValues(t *testing.T) {
	t248ResetCompareSpy()
	store := &mockDashboardStore{kpiRow: &repo.KPIRow{
		TotalPatients: 100, ActiveWear: 100, AlertCount: 30,
		AvgWearMinutes: 660, DeviceOnlineRate: 62.5, MonthNewPatients: 8,
	}}
	t248CmpRow = &repo.KPICompareRow{
		ActiveWear: 80, AlertCount: 20, AvgWearMinutes: 600,
		TotalPatientsAtMonth: 90, PrevMonthNewPatients: 5,
	}
	svc := NewDashboardService(store, nil)
	svc.now = t248FixedNow

	dto, appErr := svc.GetKPI(context.Background(), "today")
	require.Nil(t, appErr)
	require.Equal(t, 1, t248CmpCall)

	require.NotNil(t, dto.PrevTodayActiveWear)
	assert.Equal(t, int64(80), *dto.PrevTodayActiveWear)
	require.NotNil(t, dto.PrevTodayAlerts)
	assert.Equal(t, int64(20), *dto.PrevTodayAlerts)
	require.NotNil(t, dto.PrevAvgWearHours)
	assert.Equal(t, 10.0, *dto.PrevAvgWearHours) // 600min
	require.NotNil(t, dto.PrevTotalPatients)
	assert.Equal(t, int64(90), *dto.PrevTotalPatients)
	require.NotNil(t, dto.PrevMonthNewPatients)
	assert.Equal(t, int64(5), *dto.PrevMonthNewPatients)

	require.NotNil(t, dto.ActiveWearChangePct)
	assert.Equal(t, 25.0, *dto.ActiveWearChangePct) // (100-80)/80
	require.NotNil(t, dto.AlertsChangePct)
	assert.Equal(t, 50.0, *dto.AlertsChangePct)
	require.NotNil(t, dto.TotalPatientsChangePct)
	assert.Equal(t, 11.11, *dto.TotalPatientsChangePct) // (100-90)/90
	require.NotNil(t, dto.MonthNewPatientsChangePct)
	assert.Equal(t, 60.0, *dto.MonthNewPatientsChangePct)
	require.NotNil(t, dto.AvgWearHoursDelta)
	assert.Equal(t, 1.0, *dto.AvgWearHoursDelta) // 11h - 10h，设计稿「0.3h 较昨日」绝对差口径

	// 下降为负值（前端据此渲染红/绿箭头）
	t248CmpRow.AlertCount = 60
	dto2, appErr := svc.GetKPI(context.Background(), "today")
	require.Nil(t, appErr)
	require.NotNil(t, dto2.AlertsChangePct)
	assert.Equal(t, -50.0, *dto2.AlertsChangePct)
}

// prev=0 ⇒ 变化率无定义：指针为 null，不以 0 冒充「持平」
func TestT248_KPI_ZeroPrevWindow_YieldsNullChangePct(t *testing.T) {
	t248ResetCompareSpy()
	store := &mockDashboardStore{kpiRow: &repo.KPIRow{TotalPatients: 12, ActiveWear: 9, AlertCount: 4}}
	svc := NewDashboardService(store, nil)
	svc.now = t248FixedNow

	dto, appErr := svc.GetKPI(context.Background(), "today")
	require.Nil(t, appErr)

	require.NotNil(t, dto.PrevTodayActiveWear)
	assert.Equal(t, int64(0), *dto.PrevTodayActiveWear, "前窗原值仍返回 0（是真值，不是无数据）")
	assert.Nil(t, dto.ActiveWearChangePct)
	assert.Nil(t, dto.AlertsChangePct)
	assert.Nil(t, dto.TotalPatientsChangePct)
	assert.Nil(t, dto.MonthNewPatientsChangePct)
	require.NotNil(t, dto.AvgWearHoursDelta, "小时差是绝对差，前窗为 0 仍有定义")
	assert.Equal(t, 0.0, *dto.AvgWearHoursDelta)
}

// 🔴 对比查询失败只降级：主指标照常 200，对比字段留空，不让整块看板 500
func TestT248_KPI_CompareFailure_Degrades(t *testing.T) {
	t248ResetCompareSpy()
	store := &mockDashboardStore{kpiRow: &repo.KPIRow{TotalPatients: 7, ActiveWear: 3, AlertCount: 1}}
	t248CmpErr = fmt.Errorf("connection reset")
	svc := NewDashboardService(store, nil)
	svc.now = t248FixedNow

	dto, appErr := svc.GetKPI(context.Background(), "week")
	require.Nil(t, appErr, "对比基准失败不得升级为整端点失败")
	assert.Equal(t, int64(7), dto.TotalPatients)
	assert.Equal(t, 1, t248CmpCall)

	assert.Nil(t, dto.PrevTodayActiveWear)
	assert.Nil(t, dto.PrevTodayAlerts)
	assert.Nil(t, dto.PrevAvgWearHours)
	assert.Nil(t, dto.ActiveWearChangePct)
	assert.Nil(t, dto.AlertsChangePct)
	assert.Nil(t, dto.AvgWearHoursDelta)
}

// 等长前窗口径：today→昨日 1 日 / week→前一 7 日 / month→前一 30 日（半开区间）
func TestT248_KPI_PrevWindowArgs(t *testing.T) {
	cases := []struct {
		period   string
		fromDate string
		prevDate string
		days     int
	}{
		{"today", "2026-09-19", "2026-09-18", 1},
		{"week", "2026-09-13", "2026-09-06", 7},
		{"month", "2026-08-21", "2026-07-22", 30},
	}
	for _, tc := range cases {
		t248ResetCompareSpy()
		store := &mockDashboardStore{kpiRow: &repo.KPIRow{}}
		svc := NewDashboardService(store, nil)
		svc.now = t248FixedNow

		_, appErr := svc.GetKPI(context.Background(), tc.period)
		require.Nil(t, appErr, tc.period)
		require.Equal(t, 1, t248CmpCall, tc.period)

		assert.Equal(t, tc.fromDate, t248CmpArgs.fromDate, tc.period)
		assert.Equal(t, tc.prevDate, t248CmpArgs.prevFromDate, "%s 前窗起点", tc.period)
		assert.Equal(t, tc.days, int(t248CmpArgs.alertFrom.Sub(t248CmpArgs.prevAlertFrom)/(24*time.Hour)),
			"%s 告警窗口与当前周期等长", tc.period)
		assert.Equal(t, t248CmpArgs.fromDate, t248CmpArgs.alertFrom.Format("2006-01-02"),
			"%s 前窗右端 = 当前窗左端（不重不漏）", tc.period)

		wantMonthStart := time.Date(2026, 9, 1, 0, 0, 0, 0, model.CSTZone())
		if tc.period == "month" {
			wantMonthStart = time.Date(2026, 8, 1, 0, 0, 0, 0, model.CSTZone())
		}
		assert.True(t, wantMonthStart.Equal(t248CmpArgs.monthStart), "%s 自然月起点 CST 零点", tc.period)
		assert.Equal(t, wantMonthStart.AddDate(0, -1, 0).Format("2006-01-02"),
			t248CmpArgs.prevMonthStart.Format("2006-01-02"), "%s 上月起点（累计患者/上月新增口径）", tc.period)
	}
}

// 设备在线率无历史快照 ⇒ 两项对比字段恒 null（不伪造基准）
func TestT248_KPI_DeviceOnlineRate_HasNoBaseline(t *testing.T) {
	t248ResetCompareSpy()
	store := &mockDashboardStore{kpiRow: &repo.KPIRow{DeviceOnlineRate: 66.67}}
	t248CmpRow = &repo.KPICompareRow{ActiveWear: 1, AvgWearMinutes: 60}
	svc := NewDashboardService(store, nil)
	svc.now = t248FixedNow

	dto, appErr := svc.GetKPI(context.Background(), "today")
	require.Nil(t, appErr)
	assert.Equal(t, 66.67, dto.DeviceOnlineRate)
	assert.Nil(t, dto.PrevDeviceOnlineRate)
	assert.Nil(t, dto.DeviceOnlineRateDelta)
}
