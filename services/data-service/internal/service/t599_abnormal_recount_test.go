// T599：daily-wear 异常数读时现算 —— 与异常报告面同源同窗同分桶，
// 卡面三形（甲：零帧日无行；乙：无印章行 0；丙：有印章行少 1）逐形钉死，
// 外加降级路径（源挂 ⇒ 回退表内值，接口不 500）。
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

// fakeAbnormalSource T599 内存实现：预置逐日告警计数 / 注入错误
type fakeAbnormalSource struct {
	counts map[string]int
	err    error
	calls  int
	gotPID string
	gotTo  time.Time
}

func (f *fakeAbnormalSource) AbnormalCountByCSTDay(_ context.Context, pid string, _, to time.Time) (map[string]int, error) {
	f.calls++
	f.gotPID = pid
	f.gotTo = to
	if f.err != nil {
		return nil, f.err
	}
	return f.counts, nil
}

var _ repo.AbnormalCountSource = (*fakeAbnormalSource)(nil)

// newT599Svc 装配「注入了现算源」的 DailyWearService（detail/configs 不注入，
// 本组断言只押异常数，不碰来源档与代次）
func newT599Svc(store repo.DailyWearStatsStore, abn repo.AbnormalCountSource, now time.Time) *DailyWearService {
	svc := NewDailyWearService(store, nil, nil, abn)
	svc.now = func() time.Time { return now }
	return svc
}

// findT599Day 按日期取输出行；不存在返回 nil
func findT599Day(list []*model.DailyWearDayDTO, date string) *model.DailyWearDayDTO {
	for _, d := range list {
		if d.Date == date {
			return d
		}
	}
	return nil
}

// fakeNowT599 = 2026-09-02 10:00 CST：今日为 09-02，08-27..09-01 全为已过日
var fakeNowT599 = time.Date(2026, 9, 2, 10, 0, 0, 0, model.CSTZone())

// TestT599RecountOverridesStoredValue 乙/丙形：有行天的异常数以现算为准，
// 表内旧值（0 或 3）一律被报告面同源值覆盖；该日无告警则覆盖为 0
func TestT599RecountOverridesStoredValue(t *testing.T) {
	store := &fakeDailyWearStore{rows: []model.DailyWearStats{
		newDailyWearStatsTestRow("P1", "2026-08-28", 600, 100, 0, 10, 30, "P01"), // 乙形：表 0，现算 4
		newDailyWearStatsTestRow("P1", "2026-08-31", 700, 110, 3, 11, 31, "P02"), // 丙形：表 3，现算 4
		newDailyWearStatsTestRow("P1", "2026-09-01", 800, 120, 5, 12, 32, "P03"), // 表 5，现算 0（报告面该日无告警）
	}}
	abn := &fakeAbnormalSource{counts: map[string]int{
		"2026-08-28": 4,
		"2026-08-31": 4,
	}}
	svc := newT599Svc(store, abn, fakeNowT599)

	list, appErr := svc.GetDailyWear(context.Background(), "P1", "2026-08-27", "2026-09-01")
	require.Nil(t, appErr)

	assert.Equal(t, 4, findT599Day(list, "2026-08-28").AbnormalCount, "乙形：表内 0 须被现算 4 覆盖")
	assert.Equal(t, 4, findT599Day(list, "2026-08-31").AbnormalCount, "丙形：表内 3 须被现算 4 覆盖")
	assert.Equal(t, 0, findT599Day(list, "2026-09-01").AbnormalCount, "现算为 0 时旧表值 5 不得残留")
	assert.Equal(t, 1, abn.calls, "现算源一趟查询")
	assert.Equal(t, "P1", abn.gotPID)
}

// TestT599BackfillsPastDayWithoutRow 甲形：已过日无聚合行但现算有告警 ⇒ 补出行；
// 零告警的无行日保持不出行；日期升序
func TestT599BackfillsPastDayWithoutRow(t *testing.T) {
	store := &fakeDailyWearStore{rows: []model.DailyWearStats{
		newDailyWearStatsTestRow("P1", "2026-08-30", 600, 100, 0, 10, 30, ""),
	}}
	abn := &fakeAbnormalSource{counts: map[string]int{
		"2026-08-27": 2, // 甲形：无行有告警 → 补行
		"2026-08-30": 1, // 有行有告警 → 覆盖
		"2026-09-01": 3, // 无行有告警 → 补行
	}}
	svc := newT599Svc(store, abn, fakeNowT599)

	list, appErr := svc.GetDailyWear(context.Background(), "P1", "2026-08-27", "2026-09-01")
	require.Nil(t, appErr)

	require.NotNil(t, findT599Day(list, "2026-08-27"), "甲形：已过日有告警必须补出行")
	assert.Equal(t, 2, findT599Day(list, "2026-08-27").AbnormalCount)
	assert.Zero(t, findT599Day(list, "2026-08-27").WearMinutes, "补行佩戴各列为 0（该日明细确为零帧）")
	require.NotNil(t, findT599Day(list, "2026-09-01"), "甲形（区间末日）：无行有告警同样补出行")
	assert.Equal(t, 3, findT599Day(list, "2026-09-01").AbnormalCount)
	assert.Nil(t, findT599Day(list, "2026-08-28"), "零告警的无行日不得补行")
	assert.Nil(t, findT599Day(list, "2026-08-29"), "零告警的无行日不得补行")
	assert.Nil(t, findT599Day(list, "2026-08-31"), "零告警的无行日不得补行")
	assert.Equal(t, 1, findT599Day(list, "2026-08-30").AbnormalCount, "有行天照常现算覆盖")

	// 日期升序：补行与表行混合后仍须按日升序
	for i := 1; i < len(list); i++ {
		assert.Less(t, list[i-1].Date, list[i].Date, "输出须按日期升序")
	}
}

// TestT599TodayNotBackfilled 今日/未来日即使现算有告警也不补行：
// 聚合任务可能还没跑，补 0 帧行会把「没聚合」说成「佩戴 0 小时」（前端 null 语义红线）
func TestT599TodayNotBackfilled(t *testing.T) {
	store := &fakeDailyWearStore{rows: []model.DailyWearStats{
		newDailyWearStatsTestRow("P1", "2026-09-02", 600, 100, 0, 10, 30, ""), // 今日有行
	}}
	abn := &fakeAbnormalSource{counts: map[string]int{
		"2026-09-02": 5, // 今日有告警（今日有行则照常覆盖，不涉及补行）
	}}
	svc := newT599Svc(store, abn, fakeNowT599)

	list, appErr := svc.GetDailyWear(context.Background(), "P1", "2026-09-01", "2026-09-02")
	require.Nil(t, appErr)
	assert.Equal(t, 5, findT599Day(list, "2026-09-02").AbnormalCount, "今日有行天照常现算覆盖")
	assert.Nil(t, findT599Day(list, "2026-09-01"), "今日之前的零告警无行日不补行（该日确无告警）")

	// 今日无行 + 有告警：不补
	store2 := &fakeDailyWearStore{}
	svc2 := newT599Svc(store2, abn, fakeNowT599)
	list2, appErr2 := svc2.GetDailyWear(context.Background(), "P1", "2026-09-02", "2026-09-02")
	require.Nil(t, appErr2)
	assert.Nil(t, findT599Day(list2, "2026-09-02"), "今日无行即使有告警也不补（聚合可能未跑，缺行语义保留）")
}

// TestT599SourceFailureFallsBackToStored 现算源挂掉：回退表内值、接口不 500（可用性优先）
func TestT599SourceFailureFallsBackToStored(t *testing.T) {
	store := &fakeDailyWearStore{rows: []model.DailyWearStats{
		newDailyWearStatsTestRow("P1", "2026-08-28", 600, 100, 7, 10, 30, ""),
	}}
	abn := &fakeAbnormalSource{err: errors.New("alerts db down")}
	svc := newT599Svc(store, abn, fakeNowT599)

	list, appErr := svc.GetDailyWear(context.Background(), "P1", "2026-08-28", "2026-08-28")
	require.Nil(t, appErr, "现算失败不得把 daily-wear 打成 500")
	require.Len(t, list, 1)
	assert.Equal(t, 7, list[0].AbnormalCount, "失败回退表内值（T599 之前的行为）")
}

// TestT599NilSourceKeepsStoredBehavior 未注入现算源（既有装配形态）：行为与 T599 之前完全一致
func TestT599NilSourceKeepsStoredBehavior(t *testing.T) {
	store := &fakeDailyWearStore{rows: []model.DailyWearStats{
		newDailyWearStatsTestRow("P1", "2026-08-28", 600, 100, 3, 10, 30, ""),
	}}
	svc := newT599Svc(store, nil, fakeNowT599)

	list, appErr := svc.GetDailyWear(context.Background(), "P1", "2026-08-28", "2026-08-28")
	require.Nil(t, appErr)
	require.Len(t, list, 1)
	assert.Equal(t, 3, list[0].AbnormalCount, "nil 源 = 原样透出表内值")
}
