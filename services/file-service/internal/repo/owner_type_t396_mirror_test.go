// T396 镜像判据：owner_type 三处词表（迁移 CHECK / model 枚举 / T378 患者材料子集）的划分闭合。
//
// 背景：T378 的团队收窄按 owner_type 分两类——患者材料按团队收窄、其余恒放行；
// T396 把「其余」从「任意未知值」收成有限集合。三处任一处单改就会出现
// 「新增枚举值既不受团队收窄、也没人判定过它是不是患者材料」的漏洞。
//
// 跑在普通 go test（无需 Docker），与本包 *_integration_test.go 互补。
package repo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
)

// splitSQLStringList 解析 `'a','b'` 形态的 SQL 字面量清单
func splitSQLStringList(lit string) []string {
	var out []string
	for _, part := range strings.Split(lit, ",") {
		out = append(out, strings.Trim(strings.TrimSpace(part), "'"))
	}
	return out
}

func TestT396PatientScopedTypesAreEnumMembers(t *testing.T) {
	scoped := splitSQLStringList(patientScopedTypes)
	assert.ElementsMatch(t,
		[]string{model.OwnerTypePatient, model.OwnerTypeAlert},
		scoped,
		"patientScopedTypes（收窄子集）不再是 patient+alert：要么同步改 OwnerInTeam 的 switch，要么在本用例里说明新划分")
	for _, ot := range scoped {
		assert.True(t, model.ValidOwnerType(ot), "收窄子集里的 %q 不在枚举内，库里已不可能出现该值", ot)
	}
}

// TestT396EnumPartitionIsClosed 枚举 = 患者材料 ∪ 非患者材料，且「非患者材料」正是要走
// OwnerInTeam default 分支（不受团队约束）的那一批：新增第五个取值必须在此显式归类。
func TestT396EnumPartitionIsClosed(t *testing.T) {
	all := []string{
		model.OwnerTypePatient,
		model.OwnerTypeAlert,
		model.OwnerTypeReviewTemplate,
		model.OwnerTypeInstallRecord,
	}
	scoped := splitSQLStringList(patientScopedTypes)

	var unscoped []string
	for _, ot := range all {
		if !strings.Contains(","+strings.Join(scoped, ",")+",", ","+ot+",") {
			unscoped = append(unscoped, ot)
		}
	}
	assert.ElementsMatch(t,
		[]string{model.OwnerTypeReviewTemplate, model.OwnerTypeInstallRecord},
		unscoped,
		"枚举里出现了未归类的 owner_type：它会被 OwnerInTeam 的 default 分支当非患者材料放行，需显式判定")
}
