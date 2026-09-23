//go:build integration
// +build integration

// Package repo T350 集成测试：团队范围收口在真库（PG15）上的口径。
//
// 这条链路只能靠真 SQL 证明三件事，单测里 stub 不掉：
//  1. $scoped=FALSE（运营/客服/技师）时行集与本卡改造前逐字相同 —— 只收紧不放宽；
//  2. 医生 scope 只计本科室患者的行，且「当前窗」与 T248「对比窗」同一范围；
//  3. 有医生身份但 doctors.team_id 为 NULL（TeamArg 落成 NULL）⇒ 谓词恒非真，
//     返回空集/全零，绝不退化成全院（fail-closed）。
//
// fixture 只在本用例内插、结束即删：teams 表一旦被永久插行会让同包
// TestITRankings 的 assert.Empty 失去意义（排行按团队分组，有团队就有行）。
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

const (
	t350TeamA  = "TEAM-T350-IT-A"
	t350TeamB  = "TEAM-T350-IT-B"
	t350PatA   = "P-T350-IT-A1"
	t350PatB   = "P-T350-IT-B1"
	t350DevA   = "D-T350-IT-A1"
	t350DevB   = "D-T350-IT-B1"
	t350DocA   = "DR-T350-IT-A"
	t350DocB   = "DR-T350-IT-B"
	t350DocNoT = "DR-T350-IT-NOTTEAM"
	t350AdmA   = "ADM-T350-IT-A"
	t350AdmB   = "ADM-T350-IT-B"
	t350AdmNoT = "ADM-T350-IT-NOTTEAM"
	t350Role   = "ROLE-T350-IT"
)

// seedT350Scope 建一套两团队对照现场：A 科 1 名患者佩戴 600 分钟 / 1 条告警，
// B 科 1 名患者 120 分钟 / 1 条告警，再加一名无团队医生。返回清理函数。
func seedT350Scope(ctx context.Context, t *testing.T, today time.Time) {
	t.Helper()
	dateStr := today.Format("2006-01-02")
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO roles (role_id, name, permissions_json) VALUES ($1, 'T350 测试角色', '{}')
		   ON CONFLICT (role_id) DO NOTHING`, []any{t350Role}},
		{`INSERT INTO admins (admin_id, username, name, password_hash, role_id)
		   VALUES ($1, 't350_it_a', 'A 科医生', 'x', $3),
		          ($2, 't350_it_b', 'B 科医生', 'x', $3)
		   ON CONFLICT (admin_id) DO NOTHING`, []any{t350AdmA, t350AdmB, t350Role}},
		{`INSERT INTO admins (admin_id, username, name, password_hash, role_id)
		   VALUES ($1, 't350_it_n', '无团队医生', 'x', $2)
		   ON CONFLICT (admin_id) DO NOTHING`, []any{t350AdmNoT, t350Role}},
		{`INSERT INTO teams (team_id, name) VALUES ($1, 'T350 A 科'), ($2, 'T350 B 科')
		   ON CONFLICT (team_id) DO NOTHING`, []any{t350TeamA, t350TeamB}},
		{`INSERT INTO doctors (doctor_id, name, team_id, admin_id)
		   VALUES ($1, 'A 科医生', $3, $4), ($2, 'B 科医生', $5, $6)
		   ON CONFLICT (doctor_id) DO NOTHING`,
			[]any{t350DocA, t350DocB, t350TeamA, t350AdmA, t350TeamB, t350AdmB}},
		{`INSERT INTO doctors (doctor_id, name, team_id, admin_id) VALUES ($1, '无团队医生', NULL, $2)
		   ON CONFLICT (doctor_id) DO NOTHING`, []any{t350DocNoT, t350AdmNoT}},
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, diagnosis,
		   cobb_angle, team_id, primary_doctor_id, status, created_at)
		   VALUES ($1, 'A 科患者', '\x00'::bytea, 't350a' || repeat('0', 59), 'male', 14, '脊柱侧弯', 25.00,
		           $3, $4, 'active', $5),
		          ($2, 'B 科患者', '\x00'::bytea, 't350b' || repeat('0', 59), 'female', 15, '脊柱侧弯', 30.00,
		           $6, $7, 'active', $5)
		   ON CONFLICT (patient_id) DO NOTHING`,
			[]any{t350PatA, t350PatB, t350TeamA, t350DocA, today, t350TeamB, t350DocB}},
		{`INSERT INTO devices (device_id, model, firmware_version, device_secret_enc, patient_id,
		   wifi_ssid, bind_time, status, last_report_at)
		   VALUES ($1, 'PRS-ML05-RC', 'v1.2.0', '\x00'::bytea, $3, 'ClinicWiFi', $2, 'online', $2),
		          ($4, 'PRS-ML05-RC', 'v1.2.0', '\x00'::bytea, $5, 'ClinicWiFi', $2, 'online', $2)
		   ON CONFLICT (device_id) DO NOTHING`,
			[]any{t350DevA, today, t350PatA, t350DevB, t350PatB}},
		{`INSERT INTO daily_wear_stats (patient_id, stat_date, wear_minutes, avg_pressure, max_pressure, frame_count, abnormal_count)
		   VALUES ($1, $3::date, 600, 20.5, 45.0, 100, 2), ($2, $3::date, 120, 10.0, 20.0, 50, 0)
		   ON CONFLICT (patient_id, stat_date) DO NOTHING`, []any{t350PatA, t350PatB, dateStr}},
		{`INSERT INTO alerts (patient_id, device_id, type, detail, sensor_point, threshold_value, actual_value,
		   ts, read_status, process_status, resolved_status)
		   VALUES ($1, $2, 'pressure_high', 'A 科告警', 'P06', 45.0, 47.2, $3, 'unread', 'pending', 'active'),
		          ($4, $5, 'pressure_high', 'B 科告警', 'P06', 45.0, 47.2, $3, 'unread', 'pending', 'active')
		   ON CONFLICT (patient_id, device_id, type, ts) DO NOTHING`,
			[]any{t350PatA, t350DevA, today, t350PatB, t350DevB}},
	}
	for _, s := range stmts {
		if _, err := dashPool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("t350 it: seed: %v", err)
		}
	}
	t.Cleanup(func() {
		clean := []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM alerts WHERE patient_id IN ($1, $2)`, []any{t350PatA, t350PatB}},
			{`DELETE FROM daily_wear_stats WHERE patient_id IN ($1, $2)`, []any{t350PatA, t350PatB}},
			{`DELETE FROM devices WHERE device_id IN ($1, $2)`, []any{t350DevA, t350DevB}},
			{`DELETE FROM patients WHERE patient_id IN ($1, $2)`, []any{t350PatA, t350PatB}},
			{`DELETE FROM doctors WHERE doctor_id IN ($1, $2, $3)`, []any{t350DocA, t350DocB, t350DocNoT}},
			{`DELETE FROM teams WHERE team_id IN ($1, $2)`, []any{t350TeamA, t350TeamB}},
			{`DELETE FROM admins WHERE admin_id IN ($1, $2, $3)`, []any{t350AdmA, t350AdmB, t350AdmNoT}},
			{`DELETE FROM roles WHERE role_id = $1`, []any{t350Role}},
		}
		for _, c := range clean {
			if _, err := dashPool.Exec(context.Background(), c.sql, c.args...); err != nil {
				t.Errorf("t350 it: cleanup %s: %v", c.sql, err)
			}
		}
	})
}

func TestT350ITKPITeamScope(t *testing.T) {
	ctx := context.Background()
	r := NewDashboardRepo(dashPool)
	now := time.Now().In(model.CSTZone())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, model.CSTZone())
	dateStr := today.Format("2006-01-02")
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, model.CSTZone())
	seedT350Scope(ctx, t, now)

	scoped := func(scope model.TeamScope) *KPIRow {
		row, err := r.KPI(ctx, dateStr, today, monthStart, scope)
		require.NoError(t, err)
		return row
	}

	// 同窗、同月份起点，只换范围 ⇒ 差异必然来自团队谓词
	a := scoped(model.ScopeAll())
	ta := scoped(model.ScopeTeam(t350TeamA))
	tb := scoped(model.ScopeTeam(t350TeamB))
	none := scoped(model.ScopeTeam(""))

	assert.GreaterOrEqual(t, a.TotalPatients, ta.TotalPatients+tb.TotalPatients,
		"全院 ≥ 两团队之和（同窗内还有 seed 的无团队患者）")
	assert.Equal(t, int64(1), ta.TotalPatients, "A 科医生只看得到 1 名患者")
	assert.Equal(t, int64(1), ta.ActiveWear)
	assert.Equal(t, int64(1), ta.AlertCount, "A 科告警不得混入 B 科那条")
	assert.InDelta(t, 600.0, ta.AvgWearMinutes, 0.01, "均值只在本科室患者上算，不被 B 科的 120 拉低")
	assert.Equal(t, int64(1), ta.MonthNewPatients)
	assert.InDelta(t, 100.0, ta.DeviceOnlineRate, 0.01)
	assert.Equal(t, int64(1), tb.AlertCount)
	assert.InDelta(t, 120.0, tb.AvgWearMinutes, 0.01)

	// fail-closed：team_id 为 NULL 的医生 ⇒ 零值，不是全院
	assert.Zero(t, none.TotalPatients)
	assert.Zero(t, none.ActiveWear)
	assert.Zero(t, none.AlertCount)
	assert.Zero(t, none.AvgWearMinutes)
	assert.Zero(t, none.MonthNewPatients)
	assert.Zero(t, none.DeviceOnlineRate)
}

// TestT350ITKPICompareSameScope 对比窗必须与当前窗同一范围，否则「较昨日」跨团队比歪。
func TestT350ITKPICompareSameScope(t *testing.T) {
	ctx := context.Background()
	r := NewDashboardRepo(dashPool)
	now := time.Now().In(model.CSTZone())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, model.CSTZone())
	tomorrow := today.AddDate(0, 0, 1)
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, model.CSTZone())
	prevMonthStart := monthStart.AddDate(0, -1, 0)
	seedT350Scope(ctx, t, now)

	// 前窗取 [今天, 明天)：正好罩住 fixture 那两行
	cmpAll, err := r.KPICompare(ctx, dateStrOf(today), dateStrOf(tomorrow),
		today, tomorrow, monthStart, prevMonthStart, model.ScopeAll())
	require.NoError(t, err)
	cmpA, err := r.KPICompare(ctx, dateStrOf(today), dateStrOf(tomorrow),
		today, tomorrow, monthStart, prevMonthStart, model.ScopeTeam(t350TeamA))
	require.NoError(t, err)
	cmpNone, err := r.KPICompare(ctx, dateStrOf(today), dateStrOf(tomorrow),
		today, tomorrow, monthStart, prevMonthStart, model.ScopeTeam(""))
	require.NoError(t, err)

	assert.GreaterOrEqual(t, cmpAll.ActiveWear, int64(2))
	assert.Equal(t, int64(1), cmpA.ActiveWear)
	assert.Equal(t, int64(1), cmpA.AlertCount)
	assert.InDelta(t, 600.0, cmpA.AvgWearMinutes, 0.01)
	assert.Zero(t, cmpNone.ActiveWear)
	assert.Zero(t, cmpNone.AlertCount)
	assert.Zero(t, cmpNone.AvgWearMinutes)
}

func TestT350ITTrendAndDistributionScope(t *testing.T) {
	ctx := context.Background()
	r := NewDashboardRepo(dashPool)
	now := time.Now().In(model.CSTZone())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, model.CSTZone())
	seedT350Scope(ctx, t, now)
	dateStr := dateStrOf(today)

	wearAll, err := r.WearTrend(ctx, dateStr, dateStr, model.ScopeAll())
	require.NoError(t, err)
	wearA, err := r.WearTrend(ctx, dateStr, dateStr, model.ScopeTeam(t350TeamA))
	require.NoError(t, err)
	wearNone, err := r.WearTrend(ctx, dateStr, dateStr, model.ScopeTeam(""))
	require.NoError(t, err)
	require.Len(t, wearAll, 1)
	require.Len(t, wearA, 1)
	assert.Empty(t, wearNone, "无团队医生的趋势必须是空行集（service 侧补 0，不是全院）")
	assert.InDelta(t, 600.0, wearA[0].Value, 0.01)
	assert.InDelta(t, 360.0, wearAll[0].Value, 0.01, "全院均值 = 两科室患者合算，与本科室均值不同")

	alertAll, err := r.AlertTrend(ctx, today, model.ScopeAll())
	require.NoError(t, err)
	alertB, err := r.AlertTrend(ctx, today, model.ScopeTeam(t350TeamB))
	require.NoError(t, err)
	alertNone, err := r.AlertTrend(ctx, today, model.ScopeTeam(""))
	require.NoError(t, err)
	require.Len(t, alertAll, 1)
	require.Len(t, alertB, 1)
	assert.GreaterOrEqual(t, alertAll[0].Value, int64(2))
	assert.Equal(t, int64(1), alertB[0].Value)
	assert.Empty(t, alertNone)

	avgAll, err := r.PatientAvgWearMinutes(ctx, dateStr, model.ScopeAll())
	require.NoError(t, err)
	avgA, err := r.PatientAvgWearMinutes(ctx, dateStr, model.ScopeTeam(t350TeamA))
	require.NoError(t, err)
	avgNone, err := r.PatientAvgWearMinutes(ctx, dateStr, model.ScopeTeam(""))
	require.NoError(t, err)
	assert.Equal(t, []float64{600}, avgA, "佩戴分布按患者出桶：只含本科室那一名患者")
	assert.Empty(t, avgNone)
	assert.Len(t, avgAll, 2, "全院 = A、B 两名患者各自一行")
}

func TestT350ITRankingsScope(t *testing.T) {
	ctx := context.Background()
	r := NewDashboardRepo(dashPool)
	now := time.Now().In(model.CSTZone())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, model.CSTZone())
	seedT350Scope(ctx, t, now)
	dateStr := dateStrOf(today)

	teamAll, err := r.TeamRanking(ctx, dateStr, model.WearTargetMinutes, model.ScopeAll())
	require.NoError(t, err)
	teamA, err := r.TeamRanking(ctx, dateStr, model.WearTargetMinutes, model.ScopeTeam(t350TeamA))
	require.NoError(t, err)
	teamNone, err := r.TeamRanking(ctx, dateStr, model.WearTargetMinutes, model.ScopeTeam(""))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(teamAll), 2, "两科都要出现在全院排行里")
	require.Len(t, teamA, 1, "医生只看得到本科室那一行")
	assert.Equal(t, "T350 A 科", teamA[0].Name)
	assert.Equal(t, int64(1), teamA[0].PatientCount)
	assert.Empty(t, teamNone)

	docAll, err := r.DoctorRanking(ctx, dateStr, model.WearTargetMinutes, model.ScopeAll())
	require.NoError(t, err)
	docA, err := r.DoctorRanking(ctx, dateStr, model.WearTargetMinutes, model.ScopeTeam(t350TeamA))
	require.NoError(t, err)
	docNone, err := r.DoctorRanking(ctx, dateStr, model.WearTargetMinutes, model.ScopeTeam(""))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(docAll), 3)
	require.Len(t, docA, 1, "医生排行不含跨科同事")
	assert.Equal(t, "A 科医生", docA[0].Name)
	assert.Equal(t, int64(1), docA[0].PatientCount)
	assert.Empty(t, docNone)
}

// TestT350ITPatientRepoScope 患者域单资源端点的两条推导 SQL（handler 侧靠它们判 403）。
func TestT350ITPatientRepoScope(t *testing.T) {
	ctx := context.Background()
	p := NewPatientRepo(dashPool)
	now := time.Now().In(model.CSTZone())
	seedT350Scope(ctx, t, now)

	cases := []struct {
		name        string
		patient     string
		admin       string
		wantAllowed bool
	}{
		{"同团队放行", t350PatA, t350AdmA, true},
		{"跨团队拒绝", t350PatB, t350AdmA, false},
		{"患者侧无团队拒绝", dashboardPatient, t350AdmA, false}, // dashboard seed 患者 team_id 为 NULL
		{"医生侧无团队拒绝", t350PatA, t350AdmNoT, false},
		{"患者档案不存在拒绝", "P-T350-IT-NOTEXIST", t350AdmA, false},
		{"非医护账号拒绝", t350PatA, "ADM-T350-IT-NOSUCH", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.PatientInAdminTeam(ctx, tc.patient, tc.admin)
			require.NoError(t, err)
			assert.Equal(t, tc.wantAllowed, got)
		})
	}

	team, ok, err := p.DoctorTeamByAdmin(ctx, t350AdmA)
	require.NoError(t, err)
	assert.Equal(t, t350TeamA, team)
	assert.True(t, ok)

	team, ok, err = p.DoctorTeamByAdmin(ctx, t350AdmNoT)
	require.NoError(t, err)
	assert.Equal(t, "", team)
	assert.False(t, ok, "无团队医生不得拿到一个可用的 teamID")

	team, ok, err = p.DoctorTeamByAdmin(ctx, "ADM-T350-IT-NOSUCH")
	require.NoError(t, err)
	assert.Empty(t, team)
	assert.False(t, ok)
}

func dateStrOf(day time.Time) string { return day.Format("2006-01-02") }
