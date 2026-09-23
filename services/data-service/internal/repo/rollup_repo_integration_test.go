//go:build integration
// +build integration

// Package repo T352：daily rollup 聚合 SQL 真库回归（testcontainers PG15）
//
// 覆盖三件历史上没算对的事：
//  1. 佩戴分钟来自佩戴帧的真实时间跨度，不再按「帧数 × 配置采集间隔」（配置 30 分钟对实际约 31 秒）
//  2. 日均压力取佩戴帧 20 个采集点的全点均值，不是帧峰值均值，未佩戴帧不参与
//  3. abnormal_count 由同窗口 alerts 行数真回填（旧实现只 SELECT 六列，该列恒 0）
package repo

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// CST 2026-08-15 全天 → AggregateDate 的 UTC 窗口（与 RollupService 切日口径一致）
var (
	t352From = time.Date(2026, 8, 14, 16, 0, 0, 0, time.UTC)
	t352To   = time.Date(2026, 8, 15, 16, 0, 0, 0, time.UTC)
)

// t352Frame 一帧明细：ts（CST 字面量）+ p01 + 其余 19 点的统一值
type t352Frame struct {
	ts     string
	p01    float64
	others float64
}

// pointSum 该帧 20 点之和（日均压力 = 佩戴帧的 (和/20) 均值）
func (f t352Frame) pointSum() float64 { return f.p01 + 19*f.others }

// TestITT352RollupSpanAvgAndAbnormal 主用例：跨度折算 / 全点均值 / alerts 计数 / 阈值驱动
func TestITT352RollupSpanAvgAndAbnormal(t *testing.T) {
	ctx := context.Background()
	pool := dashPool
	require.NotNil(t, pool, "集成测试池未初始化")

	const (
		patient = "P-T352-IT1"
		device  = "D-T352-IT1"
	)
	seedT352Patient(ctx, t, pool, patient, device)

	// 3 帧佩戴（01:00/03:00/05:00，跨度 4h）+ 1 帧未佩戴（峰值 0.01 < 0.05）+ 1 帧窗口外
	frames := []t352Frame{
		{"2026-08-15 01:00:00+08", 3.0, 1.0},
		{"2026-08-15 03:00:00+08", 2.0, 1.0},
		{"2026-08-15 05:00:00+08", 1.5, 0.5},
		{"2026-08-15 20:00:00+08", 0.01, 0.0},
		{"2026-08-16 00:30:00+08", 9.0, 0.0}, // 窗口外，不得参与任何字段
	}
	seedT352Frames(ctx, t, pool, patient, device, frames)

	// 窗口内 2 条告警；1 条落在次日 00:30（UTC 16:30）→ 窗口外
	seedT352Alerts(ctx, t, pool, patient, device, []string{
		"2026-08-15 09:00:00+08",
		"2026-08-15 23:00:00+08",
		"2026-08-16 00:30:00+08",
	})

	r := NewRollupRepo(pool)
	stats, err := r.AggregateDate(ctx, t352From, t352To, model.WearingThresholdN)
	require.NoError(t, err)
	got := findT352Stat(t, stats, patient)

	assert.Equal(t, 4, got.FrameCount, "frame_count = 窗口内帧总数（与佩戴与否无关）")

	// 佩戴分钟 = 跨度 4h × 3/2（补一个实测间隔 2h）= 360，而非 4 帧或 3 帧 × 配置 30 分钟
	assert.Equal(t, 360, got.WearMinutes, "佩戴分钟须等于佩戴帧跨度折算")
	assert.LessOrEqual(t, got.WearMinutes, model.MaxWearMinutesPerDay)
	assert.NotEqual(t, 120, got.WearMinutes, "不得按帧数×collect_interval(30 分钟)折算")
	assert.NotEqual(t, 90, got.WearMinutes, "不得按佩戴帧数×collect_interval 折算")

	// 日均压力 = 佩戴帧全点均值 (1.10+1.05+0.55)/3 = 0.90；帧峰值均值会是 3.0/2.0/1.5 量级
	wantAvg := (frames[0].pointSum() + frames[1].pointSum() + frames[2].pointSum()) / 20.0 / 3.0
	assert.InDelta(t, wantAvg, float64(got.AvgPressure), 1e-4, "日均压力 = 佩戴帧 20 点全点均值")
	assert.InDelta(t, 3.0, float64(got.MaxPressure), 1e-4, "窗口外那帧 9.0 不得参与")
	assert.Equal(t, "P01", got.MaxPoint)

	assert.Equal(t, 2, got.AbnormalCount, "abnormal_count = 同窗口 alerts 行数")

	// 阈值同源：抬高到 1.6 后只剩 2 帧佩戴 → 跨度 2h、均值取这两帧
	stats2, err := r.AggregateDate(ctx, t352From, t352To, 1.6)
	require.NoError(t, err)
	got2 := findT352Stat(t, stats2, patient)
	assert.Equal(t, 240, got2.WearMinutes, "佩戴帧集合随配置阈值变化（阈值进 SQL，不在 Go 侧写死）")
	assert.InDelta(t, (frames[0].pointSum()+frames[1].pointSum())/20.0/2.0,
		float64(got2.AvgPressure), 1e-4, "日均压力只统计阈值以上帧")

	// 写读闭环：按 RollupService 的窗口口径落库再读回，abnormal_count 不再恒 0。
	// 容器 session timezone 为 UTC（见下方 Log），而 StatDate 按生产口径写 CST 日界——
	// 读端若依赖 session timezone 隐式转换 DATE 就会整日丢行（run 35891822503 实测 0 行），
	// 故 queryRangeSQL 显式换算 Asia/Shanghai 后再截 date，本用例即该修复的回归守卫。
	var sessTZ string
	require.NoError(t, pool.QueryRow(ctx, `SHOW timezone`).Scan(&sessTZ))
	t.Logf("容器 session timezone = %s（读端日期边界不得依赖它）", sessTZ)

	got.StatDate = time.Date(2026, 8, 15, 0, 0, 0, 0, model.CSTZone())
	require.NoError(t, r.Upsert(ctx, []model.DailyWearStats{*got}))
	rows, err := r.QueryRange(ctx, patient, t352From, t352To)
	require.NoError(t, err)
	require.Len(t, rows, 1, "CST 2026-08-15 的聚合行必须落在该 UTC 窗口内")
	assert.Equal(t, 360, rows[0].WearMinutes)
	assert.Equal(t, 2, rows[0].AbnormalCount, "落库后读回应保持告警口径计数")
	assert.InDelta(t, wantAvg, float64(rows[0].AvgPressure), 1e-4)
	assert.Equal(t, "2026-08-15", rows[0].StatDate.In(model.CSTZone()).Format("2006-01-02"))

	// 边界：整段窗口前移一天应为空（读端不漏日也不越日）
	prev, err := r.QueryRange(ctx, patient, t352From.AddDate(0, 0, -1), t352To.AddDate(0, 0, -1))
	require.NoError(t, err)
	assert.Empty(t, prev, "相邻日不得串数据")

	// 报告链路同口径：按 CST 周窗口应枚举出本患者
	patients, err := r.ListPatientsWithStats(ctx, t352From, t352To.AddDate(0, 0, 7))
	require.NoError(t, err)
	assert.Contains(t, patients, patient, "listPatientsSQL 的日期边界同样不依赖 session timezone")
}

// TestITT352RollupSingleWearingFrameIsZero 单帧日：无跨度可确立，按 0 计而不用配置间隔臆造 30 分钟
func TestITT352RollupSingleWearingFrameIsZero(t *testing.T) {
	ctx := context.Background()
	pool := dashPool
	require.NotNil(t, pool)

	const (
		patient = "P-T352-IT2"
		device  = "D-T352-IT2"
	)
	seedT352Patient(ctx, t, pool, patient, device)
	seedT352Frames(ctx, t, pool, patient, device, []t352Frame{
		{"2026-08-15 01:00:00+08", 3.0, 1.0},
		{"2026-08-15 20:00:00+08", 0.01, 0.0},
	})

	stats, err := NewRollupRepo(pool).AggregateDate(ctx, t352From, t352To, model.WearingThresholdN)
	require.NoError(t, err)
	got := findT352Stat(t, stats, patient)

	assert.Equal(t, 2, got.FrameCount)
	assert.Zero(t, got.WearMinutes, "单帧不成立跨度，不折算分钟")
	assert.Zero(t, got.AbnormalCount, "当日无告警则为 0（非缺列零值）")
	assert.InDelta(t, 22.0/20.0, float64(got.AvgPressure), 1e-4, "均值只算那 1 帧佩戴帧")
}

// TestITT352RollupWearMinutesClampedToPhysicalDay 跨度撑过物理日时仍夹到 1440
func TestITT352RollupWearMinutesClampedToPhysicalDay(t *testing.T) {
	ctx := context.Background()
	pool := dashPool
	require.NotNil(t, pool)

	const (
		patient = "P-T352-IT3"
		device  = "D-T352-IT3"
	)
	seedT352Patient(ctx, t, pool, patient, device)
	seedT352Frames(ctx, t, pool, patient, device, []t352Frame{
		{"2026-08-15 00:15:00+08", 3.0, 1.0},
		{"2026-08-15 23:45:00+08", 2.0, 1.0},
	})

	stats, err := NewRollupRepo(pool).AggregateDate(ctx, t352From, t352To, model.WearingThresholdN)
	require.NoError(t, err)
	got := findT352Stat(t, stats, patient)

	assert.Equal(t, model.MaxWearMinutesPerDay, got.WearMinutes, "2 帧×23.5h 跨度折算 2820 分钟须夹到 1440")
}

func findT352Stat(t *testing.T, stats []model.DailyWearStats, patient string) *model.DailyWearStats {
	t.Helper()
	for i := range stats {
		if stats[i].PatientID == patient {
			return &stats[i]
		}
	}
	t.Fatalf("聚合结果缺患者 %s（共 %d 行）", patient, len(stats))
	return nil
}

// seedT352Patient 造患者+设备，并注册 t.Cleanup 清掉本用例写的所有行
func seedT352Patient(ctx context.Context, t *testing.T, pool *pgxpool.Pool, patient, device string) {
	t.Helper()
	stmts := []string{
		fmt.Sprintf(`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		 VALUES ('%s', 'T352 患者', '\x00'::bytea, md5('%s') || repeat('0', 32), 'active')
		 ON CONFLICT (patient_id) DO NOTHING`, patient, patient),
		fmt.Sprintf(`INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		 VALUES ('%s', '\x00'::bytea, '%s', 'online')
		 ON CONFLICT (device_id) DO NOTHING`, device, patient),
	}
	for _, s := range stmts {
		_, err := pool.Exec(ctx, s)
		require.NoError(t, err, "seed patient/device")
	}
	t.Cleanup(func() {
		for _, s := range []string{
			fmt.Sprintf(`DELETE FROM pressure_records WHERE device_id = '%s'`, device),
			fmt.Sprintf(`DELETE FROM alerts WHERE patient_id = '%s'`, patient),
			fmt.Sprintf(`DELETE FROM daily_wear_stats WHERE patient_id = '%s'`, patient),
			fmt.Sprintf(`DELETE FROM devices WHERE device_id = '%s'`, device),
			fmt.Sprintf(`DELETE FROM patients WHERE patient_id = '%s'`, patient),
		} {
			if _, err := pool.Exec(context.Background(), s); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
}

// ctsLayout 带时区偏移的时间字面量格式（'2026-08-15 01:00:00+08'）
const ctsLayout = "2006-01-02 15:04:05-07"

func mustT352Time(tb testing.TB, literal string) time.Time {
	tb.Helper()
	ts, err := time.Parse(ctsLayout, literal)
	require.NoError(tb, err, "bad ts literal %q", literal)
	return ts
}

func seedT352Frames(ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	patient, device string, frames []t352Frame) {
	t.Helper()
	cols := make([]string, 20)
	for i := range cols {
		cols[i] = fmt.Sprintf("p%02d", i+1)
	}
	sql := fmt.Sprintf(`INSERT INTO pressure_records (device_id, patient_id, ts, %s, upload_time)
		VALUES ($1, $2, $3, %s, $24)
		ON CONFLICT (device_id, ts) DO NOTHING`,
		strings.Join(cols, ", "),
		strings.Join(tildeSlots(4, 23), ", "))
	for _, f := range frames {
		ts := mustT352Time(t, f.ts)
		vals := make([]any, 0, 24)
		vals = append(vals, device, patient, ts)
		for i := 0; i < 20; i++ {
			if i == 0 {
				vals = append(vals, float32(f.p01))
			} else {
				vals = append(vals, float32(f.others))
			}
		}
		vals = append(vals, ts)
		_, err := pool.Exec(ctx, sql, vals...)
		require.NoError(t, err, "seed frame %s", f.ts)
	}
}

func seedT352Alerts(ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	patient, device string, tsList []string) {
	t.Helper()
	const sql = `INSERT INTO alerts (patient_id, device_id, type, detail, sensor_point,
	    threshold_value, actual_value, ts, read_status, process_status, resolved_status)
		VALUES ($1, $2, 'pressure_high', 'T352 集成测试告警', 'P01', 5.0, 6.0, $3,
		        'unread', 'pending', 'active')
		ON CONFLICT (patient_id, device_id, type, ts) DO NOTHING`
	for _, tsLiteral := range tsList {
		_, err := pool.Exec(ctx, sql, patient, device, mustT352Time(t, tsLiteral))
		require.NoError(t, err, "seed alert %s", tsLiteral)
	}
}

// tildeSlots 生成 $from..$to 的占位符列表（Real 列逐位写出）
func tildeSlots(from, to int) []string {
	out := make([]string, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("$%d", i))
	}
	return out
}
