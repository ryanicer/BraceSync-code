// T498 扫描器派生告警的来源：刻意留空（落 NULL）。
//
// 为什么钉这一格：ingest_source 的含义是「这一帧是谁产生的」（real / mock）。
// wear_interrupt 读的是 Redis dev:lastseen，wear_duration_short 读的是 daily_wear_stats
// 的日聚合 wear_minutes —— 两条都不是逐帧通路，没有「一帧」可指认。
// 此时若顺手填 real，库里就把「来源未表态」和「已证实是真数据」压成同一个值，
// 而清理注入数据（DELETE ... WHERE ingest_source='mock'）这条唯一可执行的判据会误伤。
//
// 本用例是行为断言（跑两条扫描、看落库入参），不是注释断言：
// 一旦有人给 NewAlert 补上 IngestSource: model.IngestReal 之类的默认值，这里必判红。
package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

// TestT498_ScanInterruptAlertStaysUnstamped lastseen 派生的 wear_interrupt：不带来源。
func TestT498_ScanInterruptAlertStaysUnstamped(t *testing.T) {
	now := time.Now()
	devs := newFakeDevices(defaultDevice())
	alerts := newFakeAlerts()
	ls := newFakeLastSeen()
	ls.values["DEV001"] = now.Add(-90 * time.Minute) // 超 60min 阈值 → 必命中

	_, err := newTestScanner(devs, alerts, ls, now).Scan(context.Background())
	require.NoError(t, err)

	require.Len(t, alerts.created, 1, "前提：这条扫描确实产出了告警")
	a := alerts.created[0]
	assert.Equal(t, engine.TypeWearInterrupt, a.Type)
	assert.Empty(t, a.IngestSource,
		"扫描器无逐帧来源可证，不得盖章：空串在 repo 侧转 NULL，与 real 是两种含义")
}

// TestT498_ScanDailyWearAlertStaysUnstamped daily_wear_stats 派生的 wear_duration_short：同样不盖。
func TestT498_ScanDailyWearAlertStaysUnstamped(t *testing.T) {
	loc := cstDailyFixture(t)
	day := time.Date(2026, 9, 19, 0, 0, 0, 0, loc)
	devs := newFakeDevices(Device{DeviceID: "DEV-A", PatientID: "P001"})
	alerts := newFakeAlerts()
	wear := newFakeWear()
	wear.minutes["P001|2026-09-19"] = 390 // 远低于 18h 目标 → 必命中

	s := New(devs, alerts, newFakeLastSeen(), engine.NewDefaultRuleEvaluator())
	s.SetWearStore(wear)
	_, err := s.ScanDailyWear(context.Background(), day, 18)
	require.NoError(t, err)

	require.Len(t, alerts.created, 1, "前提：这条扫描确实产出了告警")
	a := alerts.created[0]
	assert.Equal(t, engine.TypeWearDurationShort, a.Type)
	assert.Empty(t, a.IngestSource,
		"日聚合派生的告警一旦盖成 real，注入帧产出的告警与它在库里就无法区分")
}

// TestT498_NewAlertSourceIsPlainString 钉字段形状：来源是值类型 string，不是 *string。
// 与 repo 侧 NULLIF($9,”) 配套 —— 调用方不判空、空串即「未表态」，
// 换成指针会让两条扫描通路必须显式传 nil，口径分叉就从这里开始。
func TestT498_NewAlertSourceIsPlainString(t *testing.T) {
	a := NewAlert{IngestSource: "mock"}
	assert.Equal(t, "mock", a.IngestSource)
	assert.Empty(t, NewAlert{}.IngestSource, "零值必须是空串（落 NULL），不能带任何默认来源")
}
