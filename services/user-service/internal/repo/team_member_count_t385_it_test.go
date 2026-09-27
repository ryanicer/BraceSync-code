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
//  2. 同源：列表成员数 = 详情成员数 = 成员明细两条腿里**在职**行数之和（明细本身列全量，含禁用）；
//  3. 统计卡与各团队行相加自洽（doctors.team_id / technicians.team_id 都带 REFERENCES teams
//     外键，孤儿归属建不出来 ⇒ 「卡数大于行和」在本仓不可能出现，写成断言钉死）；
//  4. 实时跟随：技师换团队，两行的成员数当场互换，统计卡总数不变（同池内挪动不改全局人数）；
//  5. T429 与删除守卫**刻意不同源**：守卫按引用计数（禁用者仍占 team_id 外键），列表按在职口径，
//     两数在「名下只剩禁用成员」时必须一个为 0、一个仍判在用；
//  6. 移空（含禁用者一并移走）后可删：到那一步维护列仍是 7，展示与删除判定都不认它。
//
// T429（Boss 09-27 拍「禁用的不算总数」）改了上面第 1、2、4、5、6 条的期望数字：夹具多挂两名禁用者
// （甲团队 1 名医生、乙团队 1 名技师），它们进明细不进数，第 3 条那条恒等式则整条按在职口径重算。
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
	t385Doc3  = "DOC-USR-T385-3" // T429：挂在甲团队名下但处于禁用态，进明细不进数
	t385Tech1 = "TECH-USR-T385-1"
	t385Tech2 = "TECH-USR-T385-2" // T429：挂在乙团队名下且禁用 —— 乙团队「有行、成员数 0」
	// teams.member_count 里故意写的两个假值：甲团队在职成员 3、乙团队在职成员 0
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

// t385RealMembers 按事实源现场数某个团队的**在职**成员（医生 + 技师，排除 status=disabled）。
// T429 起这条才是「成员数」的事实源；它必须与被测表达式同口径，否则本用例等于自证。
func t385RealMembers(t *testing.T, teamID string) int {
	t.Helper()
	var n int
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT (SELECT COUNT(*) FROM doctors WHERE team_id = $1 AND status = 'enabled')
		        + (SELECT COUNT(*) FROM technicians WHERE team_id = $1 AND status = 'enabled')`, teamID).Scan(&n))
	return n
}

// t385AllMembers 某个团队名下的成员**全量**行数（不看 status）—— 删除守卫那条引用计数的事实源。
// 与 t385RealMembers 分开写是故意的：两数在 T429 之后就是两个量，合成一条断言就看不出谁漏了过滤。
func t385AllMembers(t *testing.T, teamID string) int {
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
	seedDoctor := func(doctorID, teamID, status string) {
		_, err := pool.Exec(ctx, `INSERT INTO doctors (doctor_id, name, title, department, team_id, status)
			VALUES ($1, 'T385医生', '主治医师', '骨科', $2, $3) ON CONFLICT (doctor_id) DO NOTHING`,
			doctorID, teamID, status)
		require.NoError(t, err)
	}
	seedTech := func(techID, teamID, hashSeq, status string) {
		_, err := pool.Exec(ctx, `INSERT INTO technicians (tech_id, name, phone_enc, phone_hash, team_id, status)
			VALUES ($1, 'T385技师', '\x00'::bytea, $2, $3, $4) ON CONFLICT (tech_id) DO NOTHING`,
			techID, t385Hash(hashSeq), teamID, status)
		require.NoError(t, err)
	}
	seedTeam(t385TeamA, "T385甲团队", t385StaleMembA)
	seedTeam(t385TeamB, "T385乙团队", t385StaleMembB)
	seedDoctor(t385Doc1, t385TeamA, "enabled")
	seedDoctor(t385Doc2, t385TeamA, "enabled")
	seedDoctor(t385Doc3, t385TeamA, "disabled") // T429：在册但已禁用，不得进成员数
	seedTech(t385Tech1, t385TeamA, "3851", "enabled")
	seedTech(t385Tech2, t385TeamB, "3852", "disabled") // T429：乙团队「有行、在职 0」
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM doctors WHERE doctor_id IN ($1, $2, $3)`, t385Doc1, t385Doc2, t385Doc3)
		_, _ = pool.Exec(ctx, `DELETE FROM technicians WHERE tech_id IN ($1, $2)`, t385Tech1, t385Tech2)
		_, _ = pool.Exec(ctx, `DELETE FROM teams WHERE team_id IN ($1, $2)`, t385TeamA, t385TeamB)
	})
	// 前置：禁用行必须真落成了 disabled（列有 CHECK 约束，写错值会直接报错而不是静默改口径）
	var doc3Status, tech2Status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM doctors WHERE doctor_id = $1`, t385Doc3).Scan(&doc3Status))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM technicians WHERE tech_id = $1`, t385Tech2).Scan(&tech2Status))
	require.Equal(t, "disabled", doc3Status)
	require.Equal(t, "disabled", tech2Status)

	// 1. 反证两个方向：库里确实还是 0 / 7，接口读出来必须是 3 / 0
	var colA, colB int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT (SELECT member_count FROM teams WHERE team_id = $1),
		        (SELECT member_count FROM teams WHERE team_id = $2)`, t385TeamA, t385TeamB).
		Scan(&colA, &colB))
	require.Equal(t, t385StaleMembA, colA, "前置：甲团队的维护列得是小于真值的假数")
	require.Equal(t, t385StaleMembB, colB, "前置：乙团队的维护列得是大于真值的假数")

	rowA := t385RowOf(t, t385TeamA)
	assert.Equal(t, 3, rowA.MemberCount,
		"列表成员数须实时且只数在职（在职医生 2 + 在职技师 1；名下另有 1 名禁用医生）——读到 0 是还在用维护列，读到 4 是禁用态收口没落地")
	rowB := t385RowOf(t, t385TeamB)
	assert.Equal(t, 0, rowB.MemberCount,
		"列表成员数须实时（乙团队名下只有 1 名禁用技师，在职口径为 0），读到 7 是还在用维护列，读到 1 是禁用态收口没落地")

	detailA, err := itStore.GetTeam(ctx, t385TeamA)
	require.NoError(t, err)
	detailB, err := itStore.GetTeam(ctx, t385TeamB)
	require.NoError(t, err)

	// 2. 同源：列表 = 详情；成员明细两条腿仍列全量，其中在职行数之和 = 列表成员数
	assert.Equal(t, rowA.MemberCount, detailA.MemberCount, "详情与列表同一条表达式，不留两处口径")
	assert.Equal(t, rowB.MemberCount, detailB.MemberCount)
	docsA, err := itStore.ListDoctorsByTeam(ctx, t385TeamA)
	require.NoError(t, err)
	techsA, err := itStore.ListTechniciansByTeam(ctx, t385TeamA)
	require.NoError(t, err)
	assert.Len(t, docsA, 3, "成员明细（医生腿）必须仍列出禁用者：面板的「编辑 / 移除」要落得到这一行")
	assert.Len(t, techsA, 1)
	enabledInPanel := 0
	for _, d := range docsA {
		if d.Status == "enabled" {
			enabledInPanel++
		}
	}
	for _, tc := range techsA {
		if tc.Status == "enabled" {
			enabledInPanel++
		}
	}
	assert.Equal(t, len(docsA)+len(techsA), 4, "前置：面板必须真的列出了 4 行（含 1 名禁用），否则本用例没在测两口径")
	assert.Equal(t, enabledInPanel, rowA.MemberCount,
		"同一页面的自洽式：成员数 == 明细里在职行数（T385 修的是「列表 2 对明细 3」那种打脸，"+
			"T429 之后自洽式换成这条 —— 明细列全量并自带「状态」列，计数只数在职）")

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

	// 5. 与删除守卫**刻意不同源**（T429）：此刻乙团队名下 1 名在职技师 + 1 名禁用技师。
	//    守卫按引用计数（禁用者仍占 team_id 外键），列表按在职口径，两数必须分开。
	var inUse *ErrTeamInUse
	delErr := itStore.DeleteTeam(ctx, t385TeamB)
	require.ErrorAs(t, delErr, &inUse, "乙团队名下还有技师（1 在职 + 1 禁用），删除必须被判在用")
	assert.Equal(t, t385AllMembers(t, t385TeamB), inUse.MemberCount,
		"守卫报的是「引用数」：禁用者仍挂在团队上，必须一起算（跟着在职口径收口 = 把禁用成员变成删不掉的死角）")
	assert.Equal(t, 1, t385RowOf(t, t385TeamB).MemberCount, "同一时刻列表按在职口径显示 1")
	assert.Greater(t, inUse.MemberCount, t385RowOf(t, t385TeamB).MemberCount,
		"两个量各是各的：守卫引用数须大于列表在职数")

	// 6. 名下只剩禁用成员：列表显示 0 人，删除仍判在用；连禁用者一并移走才放行（维护列始终没人写，仍是 7）
	_, err = pool.Exec(ctx, `DELETE FROM technicians WHERE tech_id = $1`, t385Tech1)
	require.NoError(t, err)
	assert.Equal(t, 0, t385RowOf(t, t385TeamB).MemberCount, "在职技师移走后列表须回到 0")
	var onlyDisabled *ErrTeamInUse
	require.ErrorAs(t, itStore.DeleteTeam(ctx, t385TeamB), &onlyDisabled,
		"名下还剩 1 名禁用技师 ⇒ 删除仍须判在用")
	assert.Equal(t, 1, onlyDisabled.MemberCount)
	assert.Equal(t, 0, t385RealMembers(t, t385TeamB), "同一时刻在职口径为 0 —— 这两数不等就是本卡裁定的样子")

	_, err = pool.Exec(ctx, `DELETE FROM technicians WHERE tech_id = $1`, t385Tech2)
	require.NoError(t, err)
	require.NoError(t, itStore.DeleteTeam(ctx, t385TeamB), "成员（含禁用）移空后删除必须放行，尽管维护列还写着 7")
	var left int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM teams WHERE team_id = $1`, t385TeamB).Scan(&left))
	assert.Equal(t, 0, left)
}
