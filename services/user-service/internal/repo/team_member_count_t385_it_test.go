//go:build integration
// +build integration

// T385（Boss 2026-09-25 裁甲案，卡内评论 1133005753001001449）：团队「成员数」必须是实时计数。
//
// 修前的现网事实（Joe 2026-09-25 01:1x 一手读数）：teams.member_count 与 patient_count 同族，
// 全仓无应用写路径 —— 列表「成员数」读该维护列，同一行的「成员」明细与删除守卫计数按
// doctors.team_id / technicians.team_id 实时数，于是同一页面互相打脸：
// 列表 memberCount=2 而 TEAM01 明细是医生 2 + 技师 1 = 3；统计卡 6 = SUM(member_count) 而真实成员 7。
//
// 本文件跑真库（testcontainers PG15）守六件事，fakeStore 单测一条都守不住：
//  1. 反证两个方向：维护列在甲团队故意留 0（真成员 3，读小）、在乙团队留 7（真成员 0，读大），
//     两条假值都不许被读到 —— 只验一个方向会漏掉「读小」那半（现网正是这一半）；
//  2. 同源：列表成员数 = 详情成员数 = 成员明细两条腿（医生列表 + 技师列表）条数之和；
//  3. 统计卡与各团队行相加自洽（doctors.team_id / technicians.team_id 都带 REFERENCES teams
//     外键，孤儿归属建不出来 ⇒ 「卡数大于行和」在本仓不可能出现，写成断言钉死）；
//  4. 实时跟随：技师换团队，两行的成员数当场互换，统计卡总数不变（同池内挪动不改全局人数）；
//  5. 与删除守卫同源：乙团队名下 1 名技师、0 名患者，删除必须 409，且守卫报的成员数等于列表值；
//  6. 移空后可删：到那一步维护列仍是 7，展示与删除判定都不认它 ⇒ 只有实时数在生效。
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
	t385TeamA = "TEAM-USR-T385-A"
	t385TeamB = "TEAM-USR-T385-B"
	t385Doc1  = "DOC-USR-T385-1"
	t385Doc2  = "DOC-USR-T385-2"
	t385Tech1 = "TECH-USR-T385-1"
	// teams.member_count 里故意写的两个假值：甲团队真成员 3、乙团队真成员 0
	t385StaleMembA = 0
	t385StaleMembB = 7
)

// t385Hash 64 字符且各行互不相同（technicians.phone_hash 是 CHAR(64) + 唯一索引）：
// 4 位前缀 + 4 位序号 + 56 个 0 = 64
func t385Hash(seq string) string {
	return "cafe" + seq + strings.Repeat("0", 56)
}

// t385RowOf 从 ListTeams 结果里取一行（找不到即失败，不许静默给零值）
func t385RowOf(t *testing.T, teamID string) TeamRow {
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

// t385RealMembers 按事实源现场数某个团队的成员（医生 + 技师）
func t385RealMembers(t *testing.T, teamID string) int {
	t.Helper()
	var n int
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT (SELECT COUNT(*) FROM doctors WHERE team_id = $1)
		        + (SELECT COUNT(*) FROM technicians WHERE team_id = $1)`, teamID).Scan(&n))
	return n
}

func TestITT385TeamMemberCountIsRealtime(t *testing.T) {
	ctx := context.Background()
	pool := itStore.pool

	seedTeam := func(teamID, name string, memberCol int) {
		_, err := pool.Exec(ctx, `INSERT INTO teams (team_id, name, member_count, patient_count)
			VALUES ($1, $2, $3, 0) ON CONFLICT (team_id) DO NOTHING`, teamID, name, memberCol)
		require.NoError(t, err)
	}
	seedDoctor := func(doctorID, teamID string) {
		_, err := pool.Exec(ctx, `INSERT INTO doctors (doctor_id, name, title, department, team_id)
			VALUES ($1, 'T385医生', '主治医师', '骨科', $2) ON CONFLICT (doctor_id) DO NOTHING`,
			doctorID, teamID)
		require.NoError(t, err)
	}
	seedTech := func(techID, teamID string) {
		_, err := pool.Exec(ctx, `INSERT INTO technicians (tech_id, name, phone_enc, phone_hash, team_id)
			VALUES ($1, 'T385技师', '\x00'::bytea, $2, $3) ON CONFLICT (tech_id) DO NOTHING`,
			techID, t385Hash("3851"), teamID)
		require.NoError(t, err)
	}
	seedTeam(t385TeamA, "T385甲团队", t385StaleMembA)
	seedTeam(t385TeamB, "T385乙团队", t385StaleMembB)
	seedDoctor(t385Doc1, t385TeamA)
	seedDoctor(t385Doc2, t385TeamA)
	seedTech(t385Tech1, t385TeamA)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM doctors WHERE doctor_id IN ($1, $2)`, t385Doc1, t385Doc2)
		_, _ = pool.Exec(ctx, `DELETE FROM technicians WHERE tech_id = $1`, t385Tech1)
		_, _ = pool.Exec(ctx, `DELETE FROM teams WHERE team_id IN ($1, $2)`, t385TeamA, t385TeamB)
	})

	// 1. 反证两个方向：库里确实还是 0 / 7，接口读出来必须是 3 / 0
	var colA, colB int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT (SELECT member_count FROM teams WHERE team_id = $1),
		        (SELECT member_count FROM teams WHERE team_id = $2)`, t385TeamA, t385TeamB).
		Scan(&colA, &colB))
	require.Equal(t, t385StaleMembA, colA, "前置：甲团队的维护列得是小于真值的假数")
	require.Equal(t, t385StaleMembB, colB, "前置：乙团队的维护列得是大于真值的假数")

	rowA := t385RowOf(t, t385TeamA)
	assert.Equal(t, 3, rowA.MemberCount, "列表成员数须实时（医生 2 + 技师 1），读到 0 就是还在用维护列")
	rowB := t385RowOf(t, t385TeamB)
	assert.Equal(t, 0, rowB.MemberCount, "列表成员数须实时（名下无人），读到 7 就是还在用维护列")

	detailA, err := itStore.GetTeam(ctx, t385TeamA)
	require.NoError(t, err)
	detailB, err := itStore.GetTeam(ctx, t385TeamB)
	require.NoError(t, err)

	// 2. 同源：列表 = 详情 = 成员明细两条腿之和（同一页面三处读点，修前两处打脸）
	assert.Equal(t, rowA.MemberCount, detailA.MemberCount, "详情与列表同一条表达式，不留两处口径")
	assert.Equal(t, rowB.MemberCount, detailB.MemberCount)
	docsA, err := itStore.ListDoctorsByTeam(ctx, t385TeamA)
	require.NoError(t, err)
	techsA, err := itStore.ListTechniciansByTeam(ctx, t385TeamA)
	require.NoError(t, err)
	assert.Len(t, docsA, 2)
	assert.Len(t, techsA, 1)
	assert.Equal(t, len(docsA)+len(techsA), rowA.MemberCount,
		"成员明细条数必须等于列表成员数，否则同一页点开还是打脸（Joe 现网：列表 2 对明细 3）")

	// 3. 统计卡与各行相加自洽（外键挡住孤儿归属，两数必须相等）
	_, statsMembers, _, _, err := itStore.GetTeamStats(ctx)
	require.NoError(t, err)
	list, err := itStore.ListTeams(ctx)
	require.NoError(t, err)
	sumOfRows := 0
	for _, r := range list {
		assert.Equal(t, t385RealMembers(t, r.TeamID), r.MemberCount,
			"团队 %s 列表成员数与实时计数不等", r.TeamID)
		sumOfRows += r.MemberCount
	}
	assert.Equal(t, sumOfRows, statsMembers, "统计卡成员总数须等于各团队行之和（卡 6 对真实 7 就是这条不等）")

	// 4. 实时跟随：技师换到乙团队，两行当场互换，统计卡总数不变
	_, err = pool.Exec(ctx, `UPDATE technicians SET team_id = $1 WHERE tech_id = $2`, t385TeamB, t385Tech1)
	require.NoError(t, err)
	assert.Equal(t, 2, t385RowOf(t, t385TeamA).MemberCount, "换走后甲团队须减 1")
	assert.Equal(t, 1, t385RowOf(t, t385TeamB).MemberCount, "换入后乙团队须加 1")
	_, statsAfterMove, _, _, err := itStore.GetTeamStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, statsMembers, statsAfterMove, "同池内换团队不改变成员总数，统计卡须原地不动")

	// 5. 与删除守卫同源：乙团队名下 1 名技师、0 名患者，删除必须判在用，且计数等于列表值
	var inUse *ErrTeamInUse
	delErr := itStore.DeleteTeam(ctx, t385TeamB)
	require.ErrorAs(t, delErr, &inUse, "乙团队名下还有 1 名技师，删除必须被判在用")
	assert.Equal(t, t385RowOf(t, t385TeamB).MemberCount, inUse.MemberCount,
		"守卫报的在用成员数必须与列表显示的成员数相等，否则两数仍在打脸")

	// 6. 移空后可删：到这一步维护列仍是 7，展示与删除判定都不认它
	_, err = pool.Exec(ctx, `DELETE FROM technicians WHERE tech_id = $1`, t385Tech1)
	require.NoError(t, err)
	assert.Equal(t, 0, t385RowOf(t, t385TeamB).MemberCount)
	require.NoError(t, itStore.DeleteTeam(ctx, t385TeamB), "成员移空后删除必须放行，尽管维护列还写着 7")
	var left int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM teams WHERE team_id = $1`, t385TeamB).Scan(&left))
	assert.Equal(t, 0, left)
}
