// T350：buildAlertWhere 的团队范围谓词（纯函数，无库可跑的一侧）。
package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestT350_BuildAlertWhere_DoctorWithoutTeamIsFalsePredicate(t *testing.T) {
	where, args := buildAlertWhere(AlertQueryFilter{TeamScoped: true})
	assert.Equal(t, " WHERE false", where, "无团队归属的医护 → 空集，而非全量")
	assert.Empty(t, args)
}

func TestT350_BuildAlertWhere_TeamScopedUsesExistsSubquery(t *testing.T) {
	where, args := buildAlertWhere(AlertQueryFilter{
		PatientID:  "P20260001",
		TeamScoped: true,
		TeamID:     "TEAM01",
	})
	assert.Contains(t, where, "EXISTS (SELECT 1 FROM patients AS pt WHERE pt.patient_id = a.patient_id")
	assert.Contains(t, where, "pt.team_id = $2", "团队值只进占位符，且参数位须排在 patientId 之后")
	assert.Equal(t, []any{"P20260001", "TEAM01"}, args)
	assert.NotContains(t, where, "TEAM01", "团队值不得拼进 SQL 文本")
}

// TestT350_BuildAlertWhere_TeamParamNumberingFollowsStatus 参数序号须与 args 一一对应：
// 团队条件插在 process_status 之后、日期区间之前，漏算会让 start 绑错参数。
func TestT350_BuildAlertWhere_TeamParamNumberingFollowsStatus(t *testing.T) {
	where, args := buildAlertWhere(AlertQueryFilter{Status: "pending", TeamScoped: true, TeamID: "TEAM03"})
	assert.Contains(t, where, "a.process_status = $1")
	assert.Contains(t, where, "pt.team_id = $2")
	assert.Equal(t, []any{"pending", "TEAM03"}, args)
}

func TestT350_BuildAlertWhere_UnscopedUnchanged(t *testing.T) {
	where, args := buildAlertWhere(AlertQueryFilter{})
	assert.Equal(t, "", where, "运营/客服（TeamScoped=false）条件串逐字不变")
	assert.Empty(t, args)
}
