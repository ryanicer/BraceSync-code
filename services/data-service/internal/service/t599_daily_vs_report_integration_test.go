//go:build integration
// +build integration

// T599：患者日报（daily-wear）与异常报告面逐日对拍 —— 卡面建议修法里
// 「同一患者同一区间，两面逐日等值」的断言落地。
//
// 真值取数与 alert-service 报告面完全同式（summarizeSQL 的分桶 + buildAlertWhere 的
// 半开窗口）：同一张 alerts 表、同一 to_char(ts AT TIME ZONE 'Asia/Shanghai') 分桶、
// 同一 [start, end) 窗口。两面各自独立取数后按日期并集逐日比对，
// 甲（零帧日无行）、乙（表内 0）、丙（表内 3）三形在真实 PG 上一次钉死。
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

const (
	t599Patient = "P-T599-IT1"
	t599Device  = "D-T599-IT1"
)

// seedT599Data 患者 + 设备 + 逐日告警 + 带错值的聚合行，用例结束清理
// （alerts 唯一键 = patient/device/type/ts，同日多条按分钟错开）。
func seedT599Data(ctx context.Context, t *testing.T, days []time.Time) {
	t.Helper()

	_, err := itPool.Exec(ctx, `INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		VALUES ($1, 'T599 患者', '\x00'::bytea, 't599' || repeat('0', 60), 'active')
		ON CONFLICT (patient_id) DO NOTHING`, t599Patient)
	require.NoError(t, err)
	_, err = itPool.Exec(ctx, `INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		VALUES ($1, '\x00'::bytea, $2, 'online')
		ON CONFLICT (device_id) DO NOTHING`, t599Device, t599Patient)
	require.NoError(t, err)

	// 逐日告警：day0=2 条（甲形零帧日）、day1=4 条（乙形）、day2=0、day3=1 条（丙形）、day4=0
	perDay := []int{2, 4, 0, 1, 0}
	for i, n := range perDay {
		for k := 0; k < n; k++ {
			ts := days[i].Add(time.Duration(9+k) * time.Hour)
			_, err := itPool.Exec(ctx,
				`INSERT INTO alerts (patient_id, device_id, type, detail, sensor_point,
					threshold_value, actual_value, ts, read_status, process_status, resolved_status)
				 VALUES ($1, $2, 'pressure_high', 'T599 对拍告警', 'P01', 5.0, 6.0, $3,
				         'unread', 'pending', 'active')
				 ON CONFLICT (patient_id, device_id, type, ts) DO NOTHING`,
				t599Patient, t599Device, ts)
			require.NoError(t, err)
		}
	}

	// 聚合行（错值复刻卡面）：day1 表内 0（乙形）、day3 表内 3（丙形）、day2 表内 0 正确；
	// day0/day4 不出行 —— 写入腿 GROUP BY pressure_records，零帧日无行（甲形根源）
	for _, r := range []struct {
		day      time.Time
		abnormal int
		frames   int
		wearMin  int
	}{
		{days[1], 0, 100, 600},
		{days[2], 0, 80, 500},
		{days[3], 3, 90, 550},
	} {
		_, err := itPool.Exec(ctx,
			`INSERT INTO daily_wear_stats (patient_id, stat_date, wear_minutes, avg_pressure,
				max_pressure, max_point, frame_count, abnormal_count, updated_at)
			 VALUES ($1, $2::date, $3, 10.0, 30.0, 'P01', $4, $5, now())
			 ON CONFLICT (patient_id, stat_date) DO UPDATE SET
			   wear_minutes = EXCLUDED.wear_minutes, frame_count = EXCLUDED.frame_count,
			   abnormal_count = EXCLUDED.abnormal_count, updated_at = now()`,
			t599Patient, r.day.Format("2006-01-02"), r.wearMin, r.frames, r.abnormal)
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM alerts WHERE patient_id = $1`,
			`DELETE FROM daily_wear_stats WHERE patient_id = $1`,
		} {
			if _, err := itPool.Exec(ctx, sql, t599Patient); err != nil {
				t.Errorf("t599 cleanup: %v", err)
			}
		}
		if _, err := itPool.Exec(ctx, `DELETE FROM devices WHERE device_id = $1`, t599Device); err != nil {
			t.Errorf("t599 cleanup device: %v", err)
		}
		if _, err := itPool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, t599Patient); err != nil {
			t.Errorf("t599 cleanup patient: %v", err)
		}
	})
}

// reportFaceByDay 模拟异常报告面取数（alert-service summarizeSQL + buildAlertWhere 同式）：
// 北京日分桶 × 患者 × 半开窗口，返回 每日条数。
func reportFaceByDay(ctx context.Context, t *testing.T, from, to time.Time) map[string]int {
	t.Helper()
	rows, err := itPool.Query(ctx,
		`SELECT to_char(a.ts AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') AS day, COUNT(*)::int AS n
		   FROM alerts AS a
		  WHERE a.patient_id = $1 AND a.ts >= $2 AND a.ts < $3
		  GROUP BY 1`, t599Patient, from, to)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		require.NoError(t, rows.Scan(&d, &n))
		out[d] = n
	}
	require.NoError(t, rows.Err())
	return out
}

// TestIT_T599DailyWearMatchesReportByDay 两面逐日等值（卡面第六节建议的断言）：
// 患者日报的 abnormalCount 逐日 = 异常报告面 byDay 逐日（缺行按 0 比对）。
func TestIT_T599DailyWearMatchesReportByDay(t *testing.T) {
	ctx := context.Background()

	// 窗口 = 前 5 日起（全为已过日，避开「今日不补行」的时钟分支，断言与跑表时刻无关）
	local := time.Now().In(model.CSTZone())
	day0 := time.Date(local.Year(), local.Month(), local.Day()-5, 0, 0, 0, 0, model.CSTZone())
	days := make([]time.Time, 5)
	for i := range days {
		days[i] = day0.AddDate(0, 0, i)
	}
	fromUTC := day0.UTC()
	toUTC := day0.AddDate(0, 0, 5).UTC()

	seedT599Data(ctx, t, days)

	// 患者日报读路（生产装配形态：store/detail/configs/abnormal 全注入真池）
	svc := NewDailyWearService(
		repo.NewRollupRepo(itPool),
		repo.NewRecordRepo(itPool),
		repo.NewConfigRepo(itPool),
		repo.NewRollupRepo(itPool),
	)
	// now = 窗口末日次日：相对本用例窗口「今日」恒在未来，补行分支确定性成立
	svc.now = func() time.Time { return day0.AddDate(0, 0, 6).Add(10 * time.Hour) }

	startStr := days[0].Format("2006-01-02")
	endStr := days[4].Format("2006-01-02")
	list, appErr := svc.GetDailyWear(ctx, t599Patient, startStr, endStr)
	require.Nil(t, appErr)

	daily := map[string]int{}
	for _, d := range list {
		daily[d.Date] = d.AbnormalCount
	}
	report := reportFaceByDay(ctx, t, fromUTC, toUTC)

	// 逐日等值（日期并集；缺行按 0）
	seen := map[string]bool{}
	for i := range days {
		ds := days[i].Format("2006-01-02")
		seen[ds] = true
		assert.Equal(t, report[ds], daily[ds],
			"两面逐日不等：date=%s report=%d daily=%d", ds, report[ds], daily[ds])
	}
	for ds := range report {
		assert.True(t, seen[ds], "报告面日期 %s 落在窗口外，seed 或分桶口径有问题", ds)
	}

	// 三形逐形确认（防「都算 0」式的假等值）
	assert.Equal(t, 2, daily[days[0].Format("2006-01-02")], "甲形：零帧日补出行，异常数=报告面 2")
	assert.Equal(t, 4, daily[days[1].Format("2006-01-02")], "乙形：表内 0 被现算 4 覆盖")
	assert.Equal(t, 1, daily[days[3].Format("2006-01-02")], "丙形：表内 3 被现算 1 覆盖")
	assert.Equal(t, 0, daily[days[4].Format("2006-01-02")], "零告警零行日不补行，对拍按 0 等值")
	assert.Zero(t, daily[days[2].Format("2006-01-02")], "零告警有行日为 0")

	// 总量闭合：日报逐日之和 = 报告面 total（同窗 alerts 行数）
	var sumDaily, sumReport int
	for _, v := range daily {
		sumDaily += v
	}
	for _, v := range report {
		sumReport += v
	}
	assert.Equal(t, sumReport, sumDaily, "两面总量须相等")
}
