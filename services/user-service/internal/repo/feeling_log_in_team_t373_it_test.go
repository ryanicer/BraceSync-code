//go:build integration
// +build integration

// T373 D1：真库里 FeelingLogInTeam 的四格「不可见」必须同结果，且这条探测不许带写副作用。
//
// 单测只能证明 handler 在越权时没调用写方法，证明不了两件事：
//  1. 患者 team_id 为 NULL 时不会被等值比较放行（内连接 + p.team_id = $2 的 SQL 语义）；
//  2. 「日志不存在」与「日志在他团队患者名下」在 repo 层同为 false（否则 handler 又能分出 404/403）。
//
// 另证一条：探测只读。D1 的缺陷是覆盖医生原文且不可恢复，
// 所以门禁这一步若碰到 reply_content / reply_time 就等于换个姿势复现同一个缺陷。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
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
	t373ITPatientOwn  = "P-USR-IT-T373A" // 属 itTeam
	t373ITPatientFree = "P-USR-IT-T373B" // team_id 为 NULL
	t373ITForeignTeam = "TEAM-USR-IT-OTHER"
)

// seedT373Logs 两名独立患者各一条感受日志（共享种子库，测后清场，口径同 T290 IT）
func seedT373Logs(t *testing.T, ctx context.Context) (ownLog, freeLog int64) {
	t.Helper()
	seedPatient := func(pid, name, hash, teamID string) {
		t.Helper()
		var team any
		if teamID != "" {
			team = teamID
		}
		_, err := itStore.pool.Exec(ctx, `
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, team_id, status)
VALUES ($1, $2, '\x00'::bytea, $3, 'female', 14, $4, 'active')
ON CONFLICT (patient_id) DO NOTHING`, pid, name, hash, team)
		require.NoError(t, err)
	}
	seedPatient(t373ITPatientOwn, "T373本团队患者", "t373a"+strings.Repeat("0", 59), itTeam)
	seedPatient(t373ITPatientFree, "T373未分配患者", "t373b"+strings.Repeat("0", 59), "")

	log := func(pid string, day int) int64 {
		t.Helper()
		_, err := itStore.pool.Exec(ctx, `
INSERT INTO feeling_logs (patient_id, log_date, comfort_level, notes)
VALUES ($1, $2, 'discomfort', 'T373 集成样本')
ON CONFLICT (patient_id, log_date) DO UPDATE SET comfort_level = EXCLUDED.comfort_level`,
			pid, time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		var id int64
		require.NoError(t, itStore.pool.QueryRow(ctx,
			`SELECT log_id FROM feeling_logs WHERE patient_id = $1 AND log_date = $2`,
			pid, time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC)).Scan(&id))
		return id
	}
	ownLog = log(t373ITPatientOwn, 11)
	freeLog = log(t373ITPatientFree, 12)

	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM feeling_logs WHERE patient_id = ANY($1)`,
			[]string{t373ITPatientOwn, t373ITPatientFree})
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = ANY($1)`,
			[]string{t373ITPatientOwn, t373ITPatientFree})
	})
	return ownLog, freeLog
}

func TestITT373FeelingLogInTeam(t *testing.T) {
	ctx := context.Background()
	ownLog, freeLog := seedT373Logs(t, ctx)

	var maxID int64
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(log_id), 0) FROM feeling_logs`).Scan(&maxID))
	missingLog := maxID + 1000 // 真库里必然无此日志行

	// 1. 反证：本团队日志判得到，否则医护写面被整条锁死
	own, err := itStore.FeelingLogInTeam(ctx, ownLog, itTeam)
	require.NoError(t, err)
	assert.True(t, own, "本团队日志必须放行，否则收口过头")

	// 2~5. 四格不可见同形：既不报错也不返回 true（报错会变 500，那就又可分了）
	for _, tc := range []struct {
		name   string
		logID  int64
		teamID string
	}{
		{"他团队患者名下的日志", ownLog, t373ITForeignTeam},
		{"患者未分配团队（team_id 为 NULL）", freeLog, itTeam},
		{"日志不存在（存在性探测）", missingLog, itTeam},
		{"调用者无团队归属（teamID 空串）", ownLog, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, denyErr := itStore.FeelingLogInTeam(ctx, tc.logID, tc.teamID)
			require.NoError(t, denyErr, "不可见必须是「无行」而不是报错")
			assert.False(t, got)
		})
	}

	// 6. 只读：探测跑过一轮之后，医生回复位仍是被种子写下的原值（NULL / 未回复）
	var reply *string
	var replyTime *time.Time
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT reply_content, reply_time FROM feeling_logs WHERE log_id = $1`, ownLog).
		Scan(&reply, &replyTime))
	assert.Nil(t, reply, "归属探测不得写 reply_content")
	assert.Nil(t, replyTime, "归属探测不得改写 reply_time（原文与回复时间一并覆盖是 D1 的危害面）")
}
