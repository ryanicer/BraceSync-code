// T396：files.owner_type 枚举收口 —— 应用层集合与迁移 CHECK 必须逐字相等。
//
// 为什么不在集成测里做：这一条不需要真库就能挡住「只改 Go 不改 SQL（或反之）」的漂移，
// 跑在普通 go test 里，Docker 不可等的 CI 作业也覆盖得到。
package model

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ownerTypeMigrationFile = "000030_t396_files_owner_type_check_winner.up.sql"

// ownerTypeMigrationSQL 读迁移原文（相对本文件 4 级上到仓根，与 repo 包集成测同一取径）
func ownerTypeMigrationSQL(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file),
		"..", "..", "..", "..", "scripts", "db", "migrations", ownerTypeMigrationFile)
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "迁移文件被移动或改名时本用例必须响，而不是让 CHECK 值集失去看守")
	return string(raw)
}

// checkOwnerTypes 从迁移里切出 owner_type IN (...) 的字面量清单。
// 边界取「IN (」之后第一个 ASCII 右括号：该段内的中文注释一律用全角括号，故边界稳定。
func checkOwnerTypes(t *testing.T, sql string) []string {
	t.Helper()
	const marker = "owner_type IN ("
	i := strings.Index(sql, marker)
	require.GreaterOrEqual(t, i, 0, "迁移里找不到 "+marker+" —— CHECK 写法变了就要同步改本探测")
	rest := sql[i+len(marker):]
	end := strings.Index(rest, ")")
	require.GreaterOrEqual(t, end, 0, marker+" 之后没有右括号")
	region := rest[:end]

	var out []string
	for {
		q := strings.IndexByte(region, '\'')
		if q < 0 {
			break
		}
		region = region[q+1:]
		e := strings.IndexByte(region, '\'')
		require.GreaterOrEqual(t, e, 0, "单引号未闭合")
		out = append(out, region[:e])
		region = region[e+1:]
	}
	return out
}

// TestT396OwnerTypeEnumMatchesMigrationCheck 跨层一致性：Go 枚举 == 迁移 CHECK 值集。
// 漂移后果：只改 Go → 库存得下应用层不认的值；只改 SQL → 过了应用层被 23514 拒在 INSERT、handler 兜成 500。
func TestT396OwnerTypeEnumMatchesMigrationCheck(t *testing.T) {
	fromSQL := checkOwnerTypes(t, ownerTypeMigrationSQL(t))
	fromGo := []string{OwnerTypePatient, OwnerTypeAlert, OwnerTypeReviewTemplate, OwnerTypeInstallRecord}

	sqlSorted := append([]string(nil), fromSQL...)
	goSorted := append([]string(nil), fromGo...)
	sort.Strings(sqlSorted)
	sort.Strings(goSorted)
	assert.Equal(t, goSorted, sqlSorted,
		"model 常量与迁移 000030 的 CHECK 值集不再相等（SQL 侧=%q，Go 侧=%q）", fromSQL, fromGo)
	assert.Len(t, fromSQL, 4,
		"值集基数字生变了：增删 owner_type 必须同步判定它算不算患者材料（镜像判据在 repo 包 owner_type_t396_mirror_test.go）")
}

// TestValidOwnerType 应用层枚举判定本身
func TestValidOwnerType(t *testing.T) {
	for _, ot := range []string{OwnerTypePatient, OwnerTypeAlert, OwnerTypeReviewTemplate, OwnerTypeInstallRecord} {
		assert.True(t, ValidOwnerType(ot), "owner_type=%q 是实际写入方，必须放行否则打断上传", ot)
	}

	// 脏值清单：Patient / review 是 T391 取证里 staging 真实存过的大小写错值（已由 PM 删净）
	for _, ot := range []string{"", " ", "Patient", "PATIENT", "review", "patient ", "patients", "InstallRecord", "unknown"} {
		assert.False(t, ValidOwnerType(ot), "owner_type=%q 不该入库", ot)
	}
}

// TestT396OwnerTypeConstantsAreVerbatim 字面值锁定：常量名会骗人，落库的是值。
// handler 对非 staff 强制的是小写 patient（file_handler.go），与前端 review-records 页发出的字符串同值。
func TestT396OwnerTypeConstantsAreVerbatim(t *testing.T) {
	assert.Equal(t, "patient", OwnerTypePatient)
	assert.Equal(t, "alert", OwnerTypeAlert)
	assert.Equal(t, "ReviewTemplate", OwnerTypeReviewTemplate)
	assert.Equal(t, "install_record", OwnerTypeInstallRecord)
}
