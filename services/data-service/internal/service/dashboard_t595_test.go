// T595 甲案：Dashboard「今日」KPI 两格（活跃佩戴 / 平均佩戴时长）改从当日明细
// 与 rollup 同源聚合。日切表每日 00:10 只聚合昨日，白天无当日行 ⇒ 修复前两格恒 0。
//
// 本文件覆盖 service 编排：today 才查 live、两格被 live 行覆盖、其余指标不变；
// live 故障降级保留日切值；week/month 不触发 live；阈值源走 WithThresholdStore。
// SQL 同式（COUNT 佩戴帧 + 首末帧跨度 → WearMinutesFromSpan）的真库回归在
// repo 包 dashboard_t595_integration_test.go。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// t595Svc 组装无缓存、时钟固定（fixedNow）的 DashboardService，today 窗断言不受真实午夜影响
func t595Svc(store *mockDashboardStore) *DashboardService {
	s := NewDashboardService(store, nil)
	s.now = func() time.Time { return fixedNow }
	return s
}

// t595TodayWindow fixedNow 落在 CST 的当日 [00:00, 次日00:00)（半开区间）
func t595TodayWindow() (time.Time, time.Time) {
	cst := model.CSTZone()
	nowCST := fixedNow.In(cst)
	start := time.Date(nowCST.Year(), nowCST.Month(), nowCST.Day(), 0, 0, 0, 0, cst)
	return start, start.AddDate(0, 0, 1)
}

// t595Thresholds 可控阈值源（只实现 repo.ThresholdStore）
// 🔴 T733 方案A：字段量纲 = mN（存储空间），与帧值同量纲，进 SQL 前零换算。
type t595Thresholds struct{ wearingMn float64 }

func (t t595Thresholds) GetPressureThresholds(context.Context) (model.PressureThresholds, error) {
	return model.PressureThresholds{WearingMn: t.wearingMn}, nil
}

// 主路径：period=today 时两格取 live 聚合值，日切行的其余四格不动。
func TestT595GetKPI_TodayOverridesTwoFieldsFromLive(t *testing.T) {
	t248ResetCompareSpy()
	store := &mockDashboardStore{
		// 日切行：修复前白天直接把这两个陈旧/为 0 的值显示出去
		kpiRow: &repo.KPIRow{
			TotalPatients: 10, ActiveWear: 100, AlertCount: 3,
			AvgWearMinutes: float64Val(660), MonthNewPatients: 2,
		},
		liveRow: &repo.LiveTodayRow{ActiveWear: 3, AvgWearMinutes: float64Val(180)},
	}
	svc := t595Svc(store)

	dto, appErr := svc.GetKPI(context.Background(), "today", model.ScopeAll())
	require.Nil(t, appErr)

	// 两格 = live（180 分钟 ⇒ 3.0 小时）
	assert.Equal(t, int64(3), dto.TodayActiveWear)
	require.NotNil(t, dto.AvgWearHours)
	assert.InDelta(t, 3.0, *dto.AvgWearHours, 1e-9)
	// 其余指标仍来自日切行，不被 live 通道波及
	assert.Equal(t, int64(10), dto.TotalPatients)
	assert.Equal(t, int64(3), dto.TodayAlerts)
	assert.Equal(t, int64(2), dto.MonthNewPatients)

	// live 只在 today 调一次；窗口 = CST 当日半开区间；阈值未注入配置源 ⇒ 用 model 默认 50 mN（与 rollup 同）
	require.Equal(t, 1, store.liveCalls)
	wantFrom, wantTo := t595TodayWindow()
	assert.True(t, store.liveFrom.Equal(wantFrom), "live from=%v want %v", store.liveFrom, wantFrom)
	assert.True(t, store.liveTo.Equal(wantTo), "live to=%v want %v", store.liveTo, wantTo)
	assert.Equal(t, float32(model.WearingThresholdMn), store.liveThMn, "默认佩戴阈值 50 mN 原样进 SQL（mN 存储空间）")
	assert.Equal(t, model.ScopeAll(), store.scopes["KPILiveToday"], "scope 必须与日切 KPI 同范围")
}

// 当日全平台无帧：live 行 active=0、avg=nil（T636：无帧群体不涂成 0 小时）。
func TestT595GetKPI_TodayNoFramesKeepsNilHours(t *testing.T) {
	t248ResetCompareSpy()
	store := &mockDashboardStore{
		kpiRow:  &repo.KPIRow{TotalPatients: 5, AvgWearMinutes: float64Val(660)},
		liveRow: &repo.LiveTodayRow{ActiveWear: 0, AvgWearMinutes: nil},
	}
	dto, appErr := t595Svc(store).GetKPI(context.Background(), "today", model.ScopeAll())
	require.Nil(t, appErr)
	assert.Zero(t, dto.TodayActiveWear)
	assert.Nil(t, dto.AvgWearHours, "无帧患者群体 ⇒ 平均佩戴 nil，不得被 COALESCE 成 0")
}

// live 通道故障：整块 KPI 不 500，两格保留日切值（看板可用性优先）。
func TestT595GetKPI_TodayLiveErrorFallsBackToRollup(t *testing.T) {
	t248ResetCompareSpy()
	store := &mockDashboardStore{
		kpiRow: &repo.KPIRow{
			TotalPatients: 10, ActiveWear: 7, AvgWearMinutes: float64Val(660),
		},
		liveErr: errors.New("live detail scan unavailable"),
	}
	dto, appErr := t595Svc(store).GetKPI(context.Background(), "today", model.ScopeAll())
	require.Nil(t, appErr, "live 失败只降级，不返回 AppError")
	assert.Equal(t, int64(7), dto.TodayActiveWear, "保留日切行活跃数")
	require.NotNil(t, dto.AvgWearHours)
	assert.InDelta(t, 11.0, *dto.AvgWearHours, 1e-9, "保留日切行 660min ⇒ 11h")
	require.Equal(t, 1, store.liveCalls)
}

// week/month 仍完全走日切表，不得扫当日明细（性能与口径双不变）。
func TestT595GetKPI_NonTodayPeriodsSkipLive(t *testing.T) {
	for _, period := range []string{"week", "month"} {
		t.Run(period, func(t *testing.T) {
			t248ResetCompareSpy()
			store := &mockDashboardStore{
				kpiRow:  &repo.KPIRow{TotalPatients: 10, ActiveWear: 6, AvgWearMinutes: float64Val(300)},
				liveRow: &repo.LiveTodayRow{ActiveWear: 99, AvgWearMinutes: float64Val(999)},
			}
			dto, appErr := t595Svc(store).GetKPI(context.Background(), period, model.ScopeAll())
			require.Nil(t, appErr)
			assert.Zero(t, store.liveCalls, "%s 不得触发当日 live 聚合", period)
			assert.Equal(t, int64(6), dto.TodayActiveWear)
		})
	}
}

// WithThresholdStore：main 装配注入 sys_configs 后，live SQL 收配置阈值；读不到回退默认。
func TestT595GetKPI_LiveThresholdFromStore(t *testing.T) {
	t248ResetCompareSpy()
	store := &mockDashboardStore{
		kpiRow:  &repo.KPIRow{TotalPatients: 1},
		liveRow: &repo.LiveTodayRow{ActiveWear: 0, AvgWearMinutes: nil},
	}
	svc := t595Svc(store).WithThresholdStore(t595Thresholds{wearingMn: 1600}) // 1.6 N（mN 口径）

	_, appErr := svc.GetKPI(context.Background(), "today", model.ScopeAll())
	require.Nil(t, appErr)
	require.Equal(t, 1, store.liveCalls)
	assert.Equal(t, float32(1600), store.liveThMn, "阈值必须来自注入的 sys_configs 源（mN，与 rollup 同源）")

	// 未链式注入（nil）⇒ 默认阈值 50 mN，主路径用例已断言，这里再确认不 panic
	plain := t595Svc(&mockDashboardStore{
		kpiRow:  &repo.KPIRow{TotalPatients: 1},
		liveRow: &repo.LiveTodayRow{},
	})
	_, appErr = plain.GetKPI(context.Background(), "today", model.ScopeAll())
	require.Nil(t, appErr)
}
