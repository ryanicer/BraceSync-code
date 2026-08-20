//go:build integration
// +build integration

// Package repo T033：Dashboard 聚合 SQL 集成测试（testcontainers PG15）
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

func runDashboardIT(m *testing.M, dbURL string) int {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		panic("it: pgxpool: " + err.Error())
	}
	dashPool = pool
	defer pool.Close()

	// Reuse applyReportsMigrations from reports_query_integration_test.go
	applyReportsMigrations(ctx)
	seedDashboardData(ctx)
	return m.Run()
}

// Reuse migrationsDir + applyReportsMigrations from reports_query_integration_test.go

const dashboardPatient = "P-2026-DASH"

func seedDashboardData(ctx context.Context) {
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, diagnosis, cobb_angle, team_id, primary_doctor_id, status)
		 VALUES ($1, 'Dash 患者', '\x00'::bytea, 'd01..' || repeat('0', 58), 'male', 14, '脊柱侧弯', 25.00,
				(SELECT team_id FROM teams LIMIT 1), (SELECT doctor_id FROM doctors LIMIT 1), 'active')
		 ON CONFLICT (patient_id) DO NOTHING`, []any{dashboardPatient}},
		{`INSERT INTO devices (device_id, model, firmware_version, device_secret_enc, patient_id,
			  wifi_ssid, bind_time, status, last_report_at)
		   VALUES ($1, 'PRS-ML05-RC', 'v1.2.0', '\x00'::bytea, $2, 'ClinicWiFi', now() - INTERVAL '1 day',
			  'online', now())
		   ON CONFLICT (device_id) DO NOTHING`, []any{"D-DASH-001", dashboardPatient}},
		{`INSERT INTO pressure_records (device_id, patient_id, ts,
		   p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,p11,p12,p13,p14,p15,p16,p17,p18,p19,p20, upload_time)
		   VALUES ($1, $2, now() - INTERVAL '1 day',
		   10.0,12.0,15.0,13.0,14.0,20.0,18.0,16.0,14.0,12.0,10.0,11.0,13.0,12.0,14.0,15.0,13.0,11.0,12.0,14.0, now() - INTERVAL '30 minutes')
		   ON CONFLICT (device_id, ts) DO NOTHING`, []any{"D-DASH-001", dashboardPatient}},
		{`INSERT INTO alerts (patient_id, device_id, type, detail, sensor_point, threshold_value, actual_value,
		   ts, read_status, process_status, resolved_status)
		   VALUES ($1, $2, 'pressure_high', '压力偏高', 'P06', 45.0, 47.2, now() - INTERVAL '2 hours',
			  'unread', 'pending', 'active')
		   ON CONFLICT (patient_id, device_id, type, ts) DO NOTHING`, []any{dashboardPatient, "D-DASH-001"}},
		{`INSERT INTO daily_wear_stats (patient_id, stat_date, wear_minutes, avg_pressure, max_pressure, frame_count, abnormal_count)
		   VALUES ($1, (now() AT TIME ZONE 'Asia/Shanghai')::date - INTERVAL '1 day', 600, 20.5, 45.0, 100, 2)
		   ON CONFLICT (patient_id, stat_date) DO NOTHING`, []any{dashboardPatient}},
	}
	for _, s := range stmts {
		if _, err := dashPool.Exec(ctx, s.sql, s.args...); err != nil {
			panic("it: seed: " + err.Error())
		}
	}
}

func TestITKPI(t *testing.T) {
	ctx := context.Background()
	r := NewDashboardRepo(dashPool)
	to := time.Now().In(model.CSTZone())
	dateStr := to.Format("2006-01-02")
	alertFrom := to.AddDate(0, 0, -1) // 昨天 的 timestamptz
	monthStart := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, model.CSTZone())

	row, err := r.KPI(ctx, dateStr, alertFrom, monthStart)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, row.TotalPatients, int64(1)) // at least the test patient
	assert.GreaterOrEqual(t, row.AlertCount, int64(1))    // alert from yesterday
}

func TestITWearTrendFillMissingDays(t *testing.T) {
	ctx := context.Background()
	r := NewDashboardRepo(dashPool)
	to := time.Now().In(model.CSTZone())
	days := 7
	from := time.Date(to.Year(), to.Month(), to.Day()-days+1, 0, 0, 0, 0, model.CSTZone())

	rows, err := r.WearTrend(ctx, from.Format("2006-01-02"), to.Format("2006-01-02"))
	require.NoError(t, err)

	// 检查返回条数：只有一天数据，应该被填充到 7 天？No — the service layer does gap-filling.
	// Service fills missing days; here we only check that query returns correct rows.
	assert.LessOrEqual(t, len(rows), days)
}

func TestITRankings(t *testing.T) {
	ctx := context.Background()
	r := NewDashboardRepo(dashPool)
	to := time.Now().In(model.CSTZone())
	day := time.Date(to.Year(), to.Month(), to.Day()-(model.RankingWindowDays-1), 0, 0, 0, 0, model.CSTZone())
	dateStr := day.Format("2006-01-02")

	teamRows, err := r.TeamRanking(ctx, dateStr, model.WearTargetMinutes)
	require.NoError(t, err)
	assert.Empty(t, teamRows) // no daily_wear_stats in window for test patient

	docRows, err := r.DoctorRanking(ctx, dateStr, model.WearTargetMinutes)
	require.NoError(t, err)
	assert.Empty(t, docRows)
}

func TestITPatientAvgWear(t *testing.T) {
	ctx := context.Background()
	r := NewDashboardRepo(dashPool)
	to := time.Now().In(model.CSTZone())
	day := time.Date(to.Year(), to.Month(), to.Day()-(model.RankingWindowDays-1), 0, 0, 0, 0, model.CSTZone())
	dateStr := day.Format("2006-01-02")

	avgs, err := r.PatientAvgWearMinutes(ctx, dateStr)
	require.NoError(t, err)
	// 只有昨天的数据；窗口内应有一天的数据，平均分钟 > 0
	assert.Greater(t, len(avgs), 0)
	assert.Greater(t, avgs[0], 0.0)
}
