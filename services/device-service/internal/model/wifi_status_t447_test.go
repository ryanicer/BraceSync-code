// T447：install_records.wifi_status 枚举扩到四值 —— 三处值集必须逐字相等。
//
// 三处 = 迁移 CHECK（scripts/db/migrations/000031_…up.sql）、Go 常量（model.WifiStatus*）、
// 类型层（packages/shared-types/src/index.ts 的 wifiStatus 联合类型）。
// 为什么不在集成测里做：这一条不需要真库就能挡住「只改一处」的漂移，跑在普通 go test 里，
// 没有 Docker 的作业也覆盖得到（对照 repo/wifi_status_t447_integration_test.go 的真库那一格）。
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

const (
	wifiStatusMigrationFile   = "000031_t447_wifi_status_four_values_winner.up.sql"
	wifiStatusSharedTypesPath = "packages/shared-types/src/index.ts"
	// 基数锚点：Boss 裁定②甲的四档。第五个取值要先回 PRD 定语义，再在这里显式放行。
	wifiStatusCardinality = 4
)

// repoRootT447 从本文件上溯 4 级到仓根（与 owner_type_t396_test.go / repo 包集成测同一取径）
func repoRootT447(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	require.NoError(t, err)
	return dir
}

// quotedLiterals 按顺序取出 region 内的单引号字面量（SQL 与 TS 两侧同一种取法）
func quotedLiterals(t *testing.T, region string) []string {
	t.Helper()
	var out []string
	for {
		q := strings.IndexByte(region, '\'')
		if q < 0 {
			return out
		}
		region = region[q+1:]
		e := strings.IndexByte(region, '\'')
		require.GreaterOrEqual(t, e, 0, "单引号未闭合：%q", region)
		out = append(out, region[:e])
		region = region[e+1:]
	}
}

// wifiStatusMigrationCheck 切出迁移里 wifi_status IN (...) 的字面量清单。
// 边界取「IN (」之后第一个 ASCII 右括号：该段内的中文注释一律用全角括号，故边界稳定。
func wifiStatusMigrationCheck(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRootT447(t), "scripts", "db", "migrations", wifiStatusMigrationFile))
	require.NoError(t, err, "迁移文件被移动或改名时本用例必须响，而不是让 CHECK 值集失去看守")
	sql := string(raw)

	const marker = "wifi_status IN ("
	i := strings.Index(sql, marker)
	require.GreaterOrEqual(t, i, 0, "迁移里找不到 "+marker+" —— CHECK 写法变了就要同步改本探测")
	rest := sql[i+len(marker):]
	end := strings.Index(rest, ")")
	require.GreaterOrEqual(t, end, 0, marker+" 之后没有右括号")
	return quotedLiterals(t, rest[:end])
}

// wifiStatusSharedTypes 切出 shared-types 里 wifiStatus 联合类型的字面量清单。
func wifiStatusSharedTypes(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRootT447(t), filepath.FromSlash(wifiStatusSharedTypesPath)))
	require.NoError(t, err, "shared-types 入口文件改名/移动时本用例必须响：类型层失去看守，前端可按两值窄联合自嗨")

	const marker = "wifiStatus: '"
	i := strings.Index(string(raw), marker)
	require.GreaterOrEqual(t, i, 0, "找不到 "+marker+" 声明")
	rest := string(raw)[i+len(marker)-1:] // 回退到首个单引号
	end := strings.Index(rest, ";")
	require.GreaterOrEqual(t, end, 0, "wifiStatus 联合类型没有分号结尾")
	return quotedLiterals(t, rest[:end])
}

// TestT447WifiStatusThreeLayersAgree 跨层一致性：迁移 CHECK == Go 常量 == shared-types 联合。
// 漂移后果：只改 Go → 库存得下应用层不认的值；只改 SQL → 过了应用层被 23514 拒在 UPDATE、handler 兜成 500；
// 只改 TS → 前端类型比库里能存的窄，新两态永远显示不出来（这正是 T446 记的「连接失败」那一档的死因）。
func TestT447WifiStatusThreeLayersAgree(t *testing.T) {
	fromSQL := wifiStatusMigrationCheck(t)
	fromGo := []string{WifiStatusConnected, WifiStatusUnconfigured, WifiStatusFailed, WifiStatusSkipped}
	fromTS := wifiStatusSharedTypes(t)

	// 正对照：本用例的读取器必须真能读出东西，否则「三处相等」会退化成「三处都读空」。
	require.NotEmpty(t, fromSQL, "迁移值集读出为空 = 探测失效")
	require.NotEmpty(t, fromTS, "shared-types 值集读出为空 = 探测失效")

	sqlSorted := append([]string(nil), fromSQL...)
	goSorted := append([]string(nil), fromGo...)
	tsSorted := append([]string(nil), fromTS...)
	sort.Strings(sqlSorted)
	sort.Strings(goSorted)
	sort.Strings(tsSorted)

	assert.Equal(t, goSorted, sqlSorted, "model 常量与迁移 000031 的 CHECK 值集不再相等（SQL=%q，Go=%q）", fromSQL, fromGo)
	assert.Equal(t, goSorted, tsSorted, "model 常量与 shared-types 的 wifiStatus 联合不再相等（TS=%q，Go=%q）", fromTS, fromGo)
	assert.Len(t, fromSQL, wifiStatusCardinality,
		"值集基数不再是 %d：增删 wifi_status 取值要先回 PRD 定语义，再同步 Go 常量 / shared-types / 展示层词表（词表归 T443）",
		wifiStatusCardinality)
}

// TestWifiStatusConstantsAreVerbatim 字面值锁定：常量名会骗人，落库的是值。
func TestWifiStatusConstantsAreVerbatim(t *testing.T) {
	assert.Equal(t, "connected", WifiStatusConnected)
	assert.Equal(t, "unconfigured", WifiStatusUnconfigured)
	assert.Equal(t, "failed", WifiStatusFailed)
	assert.Equal(t, "skipped", WifiStatusSkipped)
}

// TestValidWifiStatus 应用层枚举判定本身。
// 脏值里 CONNECTED / Connected 是本地真库那格实测被 CHECK 拒掉的同形值
// （见 .t447-evidence/01-pg14-updown-v2.txt STEP 4），这里保持同一大小写敏感口径。
func TestValidWifiStatus(t *testing.T) {
	for _, v := range []string{WifiStatusConnected, WifiStatusUnconfigured, WifiStatusFailed, WifiStatusSkipped} {
		assert.True(t, ValidWifiStatus(v), "wifi_status=%q 在四档之内，必须放行", v)
	}
	for _, v := range []string{"", " ", "CONNECTED", "Connected", "skip", "skipped ", "unknown", "configuring", "已连接"} {
		assert.False(t, ValidWifiStatus(v), "wifi_status=%q 不该入库", v)
	}
}
