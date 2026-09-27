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
	// T429（Boss 09-27 拍「禁用的不算总数」）：这条断言原来钉的是反方向
	// （assert.NotContains "status"，注释写着「改判属新增语义，须另立卡」），卡就是那张卡，按裁决翻转。
	// 两条腿**分开**钉：只给医生腿加过滤、技师腿漏掉，是这张页最容易重演的半改（半改=同一页两个数）。
	assert.Contains(t, teamMemberCountExpr, "dm.status = 'enabled'",
		"医生腿未排除禁用账号 ⇒ 列表/详情/统计卡三处仍会把禁用者算进成员数")
	assert.Contains(t, teamMemberCountExpr, "tc.status = 'enabled'",
		"技师腿未排除禁用账号 ⇒ 同上，且技师腿单独漏掉时统计卡与各团队行之和照样自洽，只有这条拦得住")
	// 反向锁：禁用态收口只落在「在职成员数」这一个维度上。成员明细两条腿（pg.go:553 ListDoctorsByTeam
	// 与 :613 ListTechniciansByTeam 复用的 doctorSelect / techFrom）与 DeleteTeam 引用计数
	// 必须继续看全量（禁用者仍要能在面板里被编辑/移除，且仍占着 team_id 外键），谁顺手给它们也加
	// status 过滤，就会把禁用成员变成删不掉的管理死角 —— 集成层用例 5/6 钉着这条。
	assert.NotContains(t, doctorSelect, "status = 'enabled'", "成员明细（医生腿）不得跟着收口")
	assert.NotContains(t, techFrom, "status = 'enabled'", "成员明细（技师腿）不得跟着收口")
}

func TestT385_StatsCardSumsThePerTeamExpr(t *testing.T) {
	assert.Contains(t, teamStatsSelect, "SUM("+teamMemberCountExpr+")",
		"统计卡须对列表那条表达式按团队求和，而不是另写一条口径")
	// 库里零团队时 SUM 返回 NULL，不兜 COALESCE 会把 pgx 的 int 扫描打成 error
	assert.Contains(t, teamStatsSelect, "COALESCE(")
}
