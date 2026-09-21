//go:build integration
// +build integration

// Package repo 集成测试：T256 #1/#2 两条端点的真库口径（此前 services 侧零覆盖）。
//
// 重点守 T290-A 修的字段链路：ListFeelingLogsAdmin 必须把 patients.name 扫进
// FeelingLogRow.PatientName（handler 侧 DTO 回填由 fake 层用例覆盖，但 fake 会跟着
// 实现一起错，故这里打真库）。
//
// T290-F（comfort_level 是 VARCHAR(8)、装不下 10 字符的 'discomfort'）已由 T302 迁移
// 000022 加宽到 VARCHAR(16) 修掉；本用例因此两档都能落库，样本覆盖
// fitted / discomfort / NULL（未评）三态 —— 筛掉 NULL 与跨档筛选
// 恰证 feeling 是 comfort_level 列比对，而不是 comfort_score 派生。
package repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t290PatientA = "P-USR-IT-T290A"
	t290PatientB = "P-USR-IT-T290B"
)

// seedT290Logs 两条独立患者 + 四条日志（共享种子库，测后清场，口径同 T248 IT）。
// A：09-05 fitted、09-03 未评（NULL）、09-02 discomfort；B：09-04 fitted。
// 三条都刻意不带 comfort_score ⇒ feeling 只能来自 comfort_level 列。
func seedT290Logs(t *testing.T, ctx context.Context) {
	t.Helper()
	seedPatient := func(pid, name, phoneHash string) {
		t.Helper()
		_, err := itStore.pool.Exec(ctx, `
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, status)
VALUES ($1, $2, '\x00'::bytea, $3, 'female', 15, 'active')
ON CONFLICT (patient_id) DO NOTHING`, pid, name, phoneHash)
		require.NoError(t, err)
	}
	seedPatient(t290PatientA, "T290甲患者", "t290a"+strings.Repeat("0", 59))
	seedPatient(t290PatientB, "T290乙患者", "t290b"+strings.Repeat("0", 59))

	log := func(pid string, day int, level any) {
		t.Helper()
		_, err := itStore.pool.Exec(ctx, `
INSERT INTO feeling_logs (patient_id, log_date, comfort_level, notes)
VALUES ($1, $2, $3, 'T290 集成样本')
ON CONFLICT (patient_id, log_date) DO UPDATE SET comfort_level = EXCLUDED.comfort_level`,
			pid, time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC), level)
		require.NoError(t, err)
	}
	log(t290PatientA, 5, "fitted")
	log(t290PatientA, 3, nil) // 未评
	log(t290PatientB, 4, "fitted")
	// T302：discomfort 10 字符，000022 加宽前列宽 8 直接 22001 落不了库
	log(t290PatientA, 2, "discomfort")

	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM feeling_logs WHERE patient_id = ANY($1)`,
			[]string{t290PatientA, t290PatientB})
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = ANY($1)`,
			[]string{t290PatientA, t290PatientB})
	})
}

func TestITListFeelingLogsAdmin_T290(t *testing.T) {
	ctx := context.Background()
	seedT290Logs(t, ctx)

	// 全量（keyword 命中两条患者的共同姓名前缀）：四条都在，且每行带出患者姓名
	rows, total, err := itStore.ListFeelingLogsAdmin(ctx, FeelingLogAdminFilter{
		Keyword: "T290", Page: 1, PageSize: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	require.Len(t, rows, 4)
	// 倒序：09-05 → 09-04 → 09-03 → 09-02
	assert.Equal(t, "2026-09-05", rows[0].LogDate.Format("2006-01-02"))
	assert.Equal(t, "2026-09-03", rows[2].LogDate.Format("2006-01-02"))
	assert.Equal(t, "2026-09-02", rows[3].LogDate.Format("2006-01-02"))

	byDate := map[string]FeelingLogRow{}
	for _, r := range rows {
		byDate[r.LogDate.Format("2006-01-02")] = r
	}
	require.Contains(t, byDate, "2026-09-05")
	assert.Equal(t, "T290甲患者", byDate["2026-09-05"].PatientName, "T290-A：join 出的姓名必须落到行投影")
	assert.Equal(t, t290PatientA, byDate["2026-09-05"].PatientID, "姓名与编号并存，前端患者列两行展示")
	require.Contains(t, byDate, "2026-09-04")
	assert.Equal(t, "T290乙患者", byDate["2026-09-04"].PatientName, "跨患者流要按各行各自的患者取名，不能整批同值")

	// keyword 收窄到单人
	rows, total, err = itStore.ListFeelingLogsAdmin(ctx, FeelingLogAdminFilter{
		Keyword: "T290乙", Page: 1, PageSize: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	assert.Equal(t, t290PatientB, rows[0].PatientID)
	assert.Equal(t, "T290乙患者", rows[0].PatientName)

	// feeling=fitted 命中 2 条，未评（comfort_level IS NULL）与 discomfort 那条都被筛掉
	// ⇒ 证明确实按 comfort_level 列比对，而非按 comfort_score 派生
	rows, total, err = itStore.ListFeelingLogsAdmin(ctx, FeelingLogAdminFilter{
		Keyword: "T290", Feeling: "fitted", Page: 1, PageSize: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.NotNil(t, r.ComfortLevel)
		assert.Equal(t, "fitted", *r.ComfortLevel)
		assert.NotEmpty(t, r.PatientName, "筛选路径上姓名同样不得丢")
	}

	// T302：feeling=discomfort 必须查得到（加宽前这条 INSERT 就落不了库，筛选恒空）
	rows, total, err = itStore.ListFeelingLogsAdmin(ctx, FeelingLogAdminFilter{
		Keyword: "T290", Feeling: "discomfort", Page: 1, PageSize: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	assert.Equal(t, t290PatientA, rows[0].PatientID)
	require.NotNil(t, rows[0].ComfortLevel)
	assert.Equal(t, "discomfort", *rows[0].ComfortLevel)

	// 日期闭区间：09-04..09-05 命中两条，边界当天含入
	_, total, err = itStore.ListFeelingLogsAdmin(ctx, FeelingLogAdminFilter{
		Keyword: "T290", StartDate: "2026-09-04", EndDate: "2026-09-05", Page: 1, PageSize: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)

	// 分页切片：pageSize=3 时第二页只剩 1 条（尾页不满），total 仍是全量
	rows, total, err = itStore.ListFeelingLogsAdmin(ctx, FeelingLogAdminFilter{
		Keyword: "T290", Page: 2, PageSize: 3,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(4), total, "total 不受分页影响")
	require.Len(t, rows, 1)
	assert.Equal(t, "2026-09-02", rows[0].LogDate.Format("2006-01-02"))
}

func TestITGetTeamStats_T256(t *testing.T) {
	ctx := context.Background()
	seedT290Logs(t, ctx)

	teamCount, memberCount, managed, unassigned, err := itStore.GetTeamStats(ctx)
	require.NoError(t, err)
	assert.Greater(t, teamCount, 0, "种子含团队")
	assert.GreaterOrEqual(t, memberCount, 0)
	// 两条 T290 样本患者未分配团队 ⇒ 待分配至少含这两条
	assert.GreaterOrEqual(t, unassigned, 2, "team_id IS NULL 的患者计入待分配")
	assert.Equal(t, managed+unassigned, countITPatients(ctx, t),
		"管理患者 + 待分配 = 患者总数（四张卡按 team_id 二分为不重不漏）")
}

func countITPatients(ctx context.Context, t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, itStore.pool.QueryRow(ctx, `SELECT COUNT(*) FROM patients`).Scan(&n))
	return n
}
