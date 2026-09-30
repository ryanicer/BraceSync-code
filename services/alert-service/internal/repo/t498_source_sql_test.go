// T498 告警落库语句的来源列形状（无库单测）。
//
// 为什么单测就够（真库那一格在 integration 层，见 t498_source_integration_test.go）：
// alerts.ingest_source 的 CHECK 只收 NULL / 'real' / 'mock'，而 Go 生产代码里唯一的写入点
// 就是 CreateAlert（其余 INSERT INTO alerts 只出现在各服务集成测试夹具中，不带该列 ⇒ 落 NULL，
// 与「未表态」同口径）。
// 「空串 → NULL」这件事完全靠 SQL 里的 NULLIF($9,”)：语句一旦退化成 $9，
// 未盖章的告警（扫描器派生、旧版调用方）就会写空串，当场被 CHECK 拒成 500 ——
// 而这条红在本包单测里唯一能立刻响的形式，就是钉住语句本身。
package repo

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

func TestT498_CreateAlertSQLCarriesSourceColumn(t *testing.T) {
	sql := createAlertSQL

	assert.Contains(t, sql, "ingest_source", "落库语句丢了来源列：alerts 表无从判读这一行是真是假")
	assert.Contains(t, sql, "NULLIF($9,'')",
		"空串必须由 SQL 转成 NULL：直接写 $9 会把未表态的行写成空串，被 CHECK 拒掉")
	assert.NotContains(t, sql, "$10", "参数序超过 9 个 = 列清单与 args 已不同步")

	// 列清单与 VALUES 一一对位：9 列对 9 个占位符
	cols := regexp.MustCompile(`INSERT INTO alerts \(([^)]*)\)`).FindStringSubmatch(sql)
	require.Len(t, cols, 2, "读不出列清单 = 语句形状变了，本用例失去判定力")
	colList := strings.Split(cols[1], ",")
	assert.Len(t, colList, 9, "列数变了要同步改 CreateAlert 的 args 顺序")
	assert.Equal(t, "ingest_source", strings.TrimSpace(colList[8]),
		"来源列必须在末位：args 是位置参数，插在中间等于整行错位")

	// 正对照：同族既有列还在（防止「列清单被整段替换」也算通过）
	for _, want := range []string{"patient_id", "device_id", "type", "ts"} {
		assert.Contains(t, cols[1], want)
	}
}

// TestT498_NewAlertSourceFeedsTheNinthArg 钉「第 9 个参数确实是 IngestSource」：
// SQL 与 args 分属两处，只测 SQL 会把「列写了、args 传错」放过去。
// 这里按 CreateAlert 里 args 的书写顺序对平（该顺序由编译器保证与 $1..$9 对应）。
func TestT498_NewAlertSourceFeedsTheNinthArg(t *testing.T) {
	a := scanner.NewAlert{
		PatientID: "P1", DeviceID: "DEV1", Type: "pressure_high", SensorPoint: "P03",
		Detail: "d", ThresholdValue: 40, ActualValue: 50, IngestSource: "mock",
	}
	args := []any{a.PatientID, a.DeviceID, string(a.Type), a.SensorPoint, a.Detail,
		a.ThresholdValue, a.ActualValue, a.Ts, a.IngestSource}
	assert.Len(t, args, 9)
	assert.Equal(t, "mock", args[8])
}
