// Package service T350：Dashboard 数据范围（scope）在编排层与缓存层的落点用例。
//
// 要守的两件事：
//  1. handler 推导出的 scope 必须原样透到 store 的全部 7 条查询（含 T248 的对比窗，
//     否则医生看到的「较昨日」是拿全院前窗比本团队当前窗）；
//  2. 缓存键必须含 scope —— 运营先查过一次 KPI 后，同 period 的医生请求若命中那份
//     全院 JSON，SQL 层刚收紧的范围会被缓存原样放宽回去。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

var t350Scope = model.ScopeTeam("TEAM-T350-A")

// TestT350_ScopeReachesEveryStoreQuery 6 个编排方法 + 对比窗，实收 scope 一字不差。
func TestT350_ScopeReachesEveryStoreQuery(t *testing.T) {
	store := &mockDashboardStore{
		kpiRow:    &repo.KPIRow{TotalPatients: 3},
		wearRows:  []repo.TrendRow{},
		alertRows: []repo.TrendRow{},
		teamRows:  []repo.RankingRow{{Name: "TEAM-T350-A"}},
		docRows:   []repo.RankingRow{{Name: "DR-A"}},
		avgWears:  []float64{600},
	}
	svc := NewDashboardService(store, nil)
	svc.now = t248FixedNow
	ctx := context.Background()

	_, appErr := svc.GetKPI(ctx, "today", t350Scope)
	require.Nil(t, appErr)
	_, appErr = svc.GetWearTrend(ctx, 7, t350Scope)
	require.Nil(t, appErr)
	_, appErr = svc.GetAlertTrend(ctx, 7, t350Scope)
	require.Nil(t, appErr)
	_, appErr = svc.GetTeamRanking(ctx, t350Scope)
	require.Nil(t, appErr)
	_, appErr = svc.GetDoctorRanking(ctx, t350Scope)
	require.Nil(t, appErr)
	_, appErr = svc.GetWearDistribution(ctx, t350Scope)
	require.Nil(t, appErr)

	for _, op := range []string{"KPI", "KPICompare", "WearTrend", "AlertTrend",
		"TeamRanking", "DoctorRanking", "PatientAvgWear"} {
		got, ok := store.scopes[op]
		require.True(t, ok, "store.%s 未被调用，scope 无处可验", op)
		assert.Equal(t, t350Scope, got, "store.%s 实收 scope", op)
	}
}

// TestT350_KPICacheIsolatedByScope 缓存按范围分片：三条路径各读各的键，互不串味。
func TestT350_KPICacheIsolatedByScope(t *testing.T) {
	ctx := context.Background()
	nowTS := time.Date(2026, 9, 19, 14, 30, 0, 0, model.CSTZone())
	cache, _ := startMiniRedisCache(t)
	newSvc := func(store *mockDashboardStore) *DashboardService {
		s := NewDashboardService(store, cache)
		s.now = func() time.Time { return nowTS }
		return s
	}

	// 三态键两两不同名，否则后写的一方覆盖前一方
	kAll := repo.KeyDashboardKPI("today", model.ScopeAll())
	kTeam := repo.KeyDashboardKPI("today", t350Scope)
	kNone := repo.KeyDashboardKPI("today", model.ScopeTeam(""))
	assert.NotEqual(t, kAll, kTeam)
	assert.NotEqual(t, kAll, kNone)
	assert.NotEqual(t, kTeam, kNone)

	// 运营先查一次 today（全院 8 人），结果回填全院键
	_, appErr := newSvc(&mockDashboardStore{kpiRow: &repo.KPIRow{TotalPatients: 8}}).GetKPI(ctx, "today", model.ScopeAll())
	require.Nil(t, appErr)

	// 同 period 的医生请求必须绕过那份全院数字、重新查库
	docStore := &mockDashboardStore{kpiRow: &repo.KPIRow{TotalPatients: 2}}
	docDTO, appErr := newSvc(docStore).GetKPI(ctx, "today", t350Scope)
	require.Nil(t, appErr)
	require.Equal(t, int64(2), docDTO.TotalPatients, "医生命中了运营回填的全院缓存")
	assert.Equal(t, t350Scope, docStore.scopes["KPI"])

	// 无团队医生（空集）回填后，运营与本科室医生仍各读各的键：
	// 以「查库必报错」的 store 证明这两条路径命中的是自己那份缓存。
	_, appErr = newSvc(&mockDashboardStore{kpiRow: &repo.KPIRow{}}).GetKPI(ctx, "today", model.ScopeTeam(""))
	require.Nil(t, appErr)
	poison := &mockDashboardStore{err: assert.AnError}
	opsAgain, appErr := newSvc(poison).GetKPI(ctx, "today", model.ScopeAll())
	require.Nil(t, appErr, "全院路径被其他 scope 的回填污染后会查库报错")
	assert.Equal(t, int64(8), opsAgain.TotalPatients)

	teamAgain, appErr := newSvc(poison).GetKPI(ctx, "today", t350Scope)
	require.Nil(t, appErr)
	assert.Equal(t, int64(2), teamAgain.TotalPatients)
}
