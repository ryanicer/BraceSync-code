// T350：patientWhere 的团队范围谓词（数据范围规则的服务端落点）。
//
// 这里守的是最危险的一格：TeamScoped 为真但团队值为空（医护账号无团队归属）时，
// 必须生成恒假谓词，而不是沿用「TeamID 空串 = 不按团队过滤」的运营侧语义。
package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestT350_PatientWhere_DoctorWithoutTeamIsFalsePredicate(t *testing.T) {
	where, args := patientWhere(PatientFilter{TeamScoped: true})
	assert.Contains(t, where, "false", "无团队归属的医护应为空集，而非全量")
	assert.Empty(t, args, "恒假谓词不携带参数")
}

func TestT350_PatientWhere_TeamScopedUsesPlaceholder(t *testing.T) {
	where, args := patientWhere(PatientFilter{TeamScoped: true, TeamID: "TEAM01"})
	assert.Contains(t, where, "p.team_id = $1")
	assert.Equal(t, []any{"TEAM01"}, args, "团队值只进占位符参数")
	assert.NotContains(t, where, "TEAM01", "团队值不得拼进 SQL 文本")
}

func TestT350_PatientWhere_UnscopedKeepsLegacySemantics(t *testing.T) {
	where, args := patientWhere(PatientFilter{})
	assert.NotContains(t, where, "false", "运营/客服不传 teamId 仍是全量")
	assert.Empty(t, args)

	where, args = patientWhere(PatientFilter{TeamID: "TEAM02"})
	assert.Contains(t, where, "p.team_id = $1")
	assert.Equal(t, []any{"TEAM02"}, args)
}
