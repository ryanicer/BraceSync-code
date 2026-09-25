// T385：团队「成员数」三处读点必须共用同一条实时表达式。
//
// 集成层（team_member_count_t385_it_test.go）要 Docker 才跑得到，本机没有时回归未必覆盖；
// 这一层盯住「谁偷偷把某一处读点改回 teams.member_count」—— 团队管理页那四格数字就是被
// 四处读点各说一套搞坏的（Joe 2026-09-25 现网一手读数：列表 2 对成员明细 3、统计卡 6 对真实 7）。
// 三处读点的 SQL 因此都收在包级常量里，常量被改回内联拼接时本用例即失去着力点，一并算回归。
package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestT385_MemberCountReadersAllUseTheRealtimeExpr(t *testing.T) {
	readers := map[string]string{
		"团队列表 ListTeams":   listTeamsSelect,
		"团队详情 GetTeam":     teamDetailSelect,
		"统计卡 GetTeamStats": teamStatsSelect,
	}
	for name, sql := range readers {
		assert.Contains(t, sql, teamMemberCountExpr, "%s 未复用 teamMemberCountExpr，两处口径会漂", name)
		assert.NotContains(t, sql, "t.member_count", "%s 又去读 teams.member_count 维护列", name)
		assert.NotContains(t, sql, "SUM(member_count", "%s 又回到该维护列的汇总", name)
	}
}

func TestT385_MemberCountExprCountsBothStaffTablesByTeam(t *testing.T) {
	assert.Contains(t, teamMemberCountExpr, "FROM doctors")
	assert.Contains(t, teamMemberCountExpr, "FROM technicians")
	assert.Contains(t, teamMemberCountExpr, "team_id = t.team_id")
	assert.NotContains(t, teamMemberCountExpr, "member_count", "表达式自身不许回读那张维护列")
	// 卡面第四节子口径 1：禁用医护仍算成员（成员明细与删除守卫都不看状态）。
	// 若将来改判「不算」，那是新增语义、要连带改明细与守卫，本断言会先把这一处拦下来。
	assert.NotContains(t, teamMemberCountExpr, "status", "现状不看 status；改判属新增语义，须另立卡")
}

func TestT385_StatsCardSumsThePerTeamExpr(t *testing.T) {
	assert.Contains(t, teamStatsSelect, "SUM("+teamMemberCountExpr+")",
		"统计卡须对列表那条表达式按团队求和，而不是另写一条口径")
	// 库里零团队时 SUM 返回 NULL，不兜 COALESCE 会把 pgx 的 int 扫描打成 error
	assert.Contains(t, teamStatsSelect, "COALESCE(")
}
