//go:build integration
// +build integration

// T371-B1（PM 2026-09-24 20:08 闸门回退后落地的实际修复）：团队「患者数」必须是实时计数。
//
// 修前的现网事实：teams.patient_count 是建库那一刻写死的快照 —— 全仓运行期无一处更新该列
// （写侧只有迁移/seed，唯一现场数过的团队删除守卫数完不写回）。于是同一页上两个数互相打脸：
// 团队列表读到「0 患者」，点删除却得 409（守卫按 patients.team_id 实时判在用）。
//
// 本文件跑真库（testcontainers PG15）守四件事，单测（fakeStore）一条都守不住：
//  1. 反证：teams.patient_count 里故意留着与真值不同的旧值，列表/详情都不许读到它；
//  2. 实时：患者换团队后，列表与详情的患者数当场跟随，且与删除守卫报的数同源；
//  3. 归零：患者全部移走后列表读 0，删除随即放行（快照列还留着假数，证明没人读它）；
//  4. 不换源：member_count 仍走 teams 维护列（只换患者一列，别把整行读空当成修好了）。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t371TeamA     = "TEAM-USR-T371-A"
	t371TeamB     = "TEAM-USR-T371-B"
	t371Patient1  = "P-USR-T371-1"
	t371Patient2  = "P-USR-T371-2"
	t371Patient3  = "P-USR-T371-3"
	t371StaleColA = 9 // teams.patient_count 里写死的假快照；A 真名下 2 名患者
)

// t371Hash 64 字符且各行互不相同（patients.phone_hash 是 CHAR(64) + 唯一索引）
func t371Hash(seq string) string {
	return "bf71" + seq + strings.Repeat("0", 59)
}

// t371TeamRow 从 ListTeams 结果里取一行（找不到即失败，不许静默给零值）
func t371TeamRow(t *testing.T, teamID string) TeamRow {
	t.Helper()
	list, err := itStore.ListTeams(context.Background())
	require.NoError(t, err)
	for _, r := range list {
		if r.TeamID == teamID {
			return r
		}
	}
	t.Fatalf("%s 不在 ListTeams 结果中", teamID)
	return TeamRow{}
}

func TestITT371TeamPatientCountIsRealtime(t *testing.T) {
	ctx := context.Background()
	pool := itStore.pool

	// 现场：甲团队快照列 9 / 成员列 5，真实归属患者 2 名；乙团队快照列 0、患者 1 名
	seedTeam := func(teamID, name string, stalePatients, staleMembers int) {
		_, err := pool.Exec(ctx, `INSERT INTO teams (team_id, name, member_count, patient_count)
			VALUES ($1, $2, $3, $4) ON CONFLICT (team_id) DO NOTHING`,
			teamID, name, staleMembers, stalePatients)
		require.NoError(t, err)
	}
	seedTeam(t371TeamA, "T371甲团队", t371StaleColA, 5)
	seedTeam(t371TeamB, "T371乙团队", 0, 0)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM patients WHERE patient_id IN ($1, $2, $3)`,
			t371Patient1, t371Patient2, t371Patient3)
		_, _ = pool.Exec(ctx, `DELETE FROM teams WHERE team_id IN ($1, $2)`, t371TeamA, t371TeamB)
	})

	for _, p := range []struct{ id, hash, teamID string }{
		{t371Patient1, t371Hash("1"), t371TeamA},
		{t371Patient2, t371Hash("2"), t371TeamA},
		{t371Patient3, t371Hash("3"), t371TeamB},
	} {
		// 一个占位符只落一个位置：patients.team_id 是 VARCHAR(32)，把同一 $n 再给别的参数位
		// 会被 PG 推断成 TEXT 而报 SQLSTATE 42P08（T366 踩过同一形态）
		_, err := pool.Exec(ctx, `INSERT INTO patients (patient_id, name, phone_enc, phone_hash, team_id, status)
			VALUES ($1, 'T371患者', '\x00'::bytea, $2, $3, 'active') ON CONFLICT (patient_id) DO NOTHING`,
			p.id, p.hash, p.teamID)
		require.NoError(t, err)
	}

	// 1. 反证「快照列不读」：库里确实还是 9，接口读出来必须是真名下的 2
	var staleCol int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT patient_count FROM teams WHERE team_id = $1`, t371TeamA).Scan(&staleCol))
	require.Equal(t, t371StaleColA, staleCol, "前置：快照列得留着与真值不同的假数，否则反证不成立")

	rowA := t371TeamRow(t, t371TeamA)
	assert.Equal(t, 2, rowA.PatientCount, "列表患者数须为 patients.team_id 实时计数，读到 9 就是还在用快照列")
	assert.NotEqual(t, staleCol, rowA.PatientCount)
	assert.Equal(t, 5, rowA.MemberCount, "member_count 仍走 teams 维护列：只换患者一列，整行没被打空")
	assert.Equal(t, "T371甲团队", rowA.Name)

	detailA, err := itStore.GetTeam(ctx, t371TeamA)
	require.NoError(t, err)
	assert.Equal(t, rowA.PatientCount, detailA.PatientCount, "详情与列表同一条表达式，不留两处口径")

	// 2. 全表通用不变式：每一行的患者数都等于按 team_id 现场数出来的数
	list, err := itStore.ListTeams(ctx)
	require.NoError(t, err)
	for _, r := range list {
		var real int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM patients WHERE team_id = $1`, r.TeamID).Scan(&real))
		assert.Equal(t, real, r.PatientCount, "团队 %s 列表患者数与实时计数不等", r.TeamID)
	}

	// 3. 实时跟随：一名患者从甲团队改到乙团队，两个数当场互换
	_, err = pool.Exec(ctx, `UPDATE patients SET team_id = $1 WHERE patient_id = $2`, t371TeamB, t371Patient2)
	require.NoError(t, err)
	assert.Equal(t, 1, t371TeamRow(t, t371TeamA).PatientCount, "换走后甲团队须减 1")
	assert.Equal(t, 2, t371TeamRow(t, t371TeamB).PatientCount, "换入后乙团队须加 1")
	detailA, err = itStore.GetTeam(ctx, t371TeamA)
	require.NoError(t, err)
	assert.Equal(t, 1, detailA.PatientCount)

	// 4. 与删除守卫同源：修前「列表 0 / 删除 409」互相打脸，修后两者必须是同一个数
	var inUse *ErrTeamInUse
	delErr := itStore.DeleteTeam(ctx, t371TeamA)
	require.ErrorAs(t, delErr, &inUse, "甲团队名下还有 1 名患者，删除必须被判在用")
	assert.Equal(t, t371TeamRow(t, t371TeamA).PatientCount, inUse.PatientCount,
		"守卫报的在用患者数必须与列表显示的患者数相等，否则两数仍在打脸")

	// 5. 归零后可删：快照列到这一步仍是 9，展示与删除判定都不认它 ⇒ 只有实时数在生效
	_, err = pool.Exec(ctx, `UPDATE patients SET team_id = $1 WHERE patient_id = $2`, t371TeamB, t371Patient1)
	require.NoError(t, err)
	assert.Equal(t, 0, t371TeamRow(t, t371TeamA).PatientCount)
	require.NoError(t, itStore.DeleteTeam(ctx, t371TeamA))
	var left int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM teams WHERE team_id = $1`, t371TeamA).Scan(&left))
	assert.Equal(t, 0, left)
}
