// Package repo — T023 安全审计 Part A · SQL 注入单元测试（WHERE 构造函数参数化断言）
//
// 对齐：docs/ 注入面）· OWASP SQLi payload 集
//
// 判据：任何用户可控输入（keyword/teamId）只允许出现在 pgx 绑定参数（args）中，
// 绝不允许拼接进 SQL 文本。集成层（真实 PG）回归见 sqli_integration_test.go。
package repo

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sqliPayloads OWASP 经典注入载荷（T023 验收清单：布尔绕过/union/盲注/堆叠）
var sqliPayloads = []string{
	"' OR 1=1--", // 布尔绕过（万能条件）
	"' UNION SELECT admin_id, username, password_hash FROM admins--", // union 拖库
	"'; DROP TABLE patients;--",                                      // 堆叠破坏
	"' AND 1=(SELECT COUNT(*) FROM information_schema.tables)--",     // 盲注探测
	"admin'/*", // 注释截断
	"%%';--",   // ILIKE 通配符 + 注释
}

// placeholderRe SQL 文本中合法的 pgx 占位符（$1..$N；边界限定排除载荷内 %';-- 类序列误匹配）
var placeholderRe = regexp.MustCompile(`\$\d+\b`)

// assertWhereParametrized 通用断言：payload 不拼入 SQL 文本、只进绑定参数
func assertWhereParametrized(t *testing.T, name, where string, args []any, payloads []string) {
	t.Helper()
	// 1) WHERE 文本不得包含任何载荷片段（去掉占位符后仍不得残留单引号/注入关键字）
	assert.NotContains(t, where, "'", name+": SQL 文本不得出现单引号")
	assert.NotContains(t, strings.ToLower(where), "union", name)
	assert.NotContains(t, strings.ToLower(where), "drop", name)
	assert.NotContains(t, strings.ToLower(where), "--", name)
	// 2) 占位符序号集合与参数数量一致（同一 $N 可在多列条件复用；pgx 强校验防错位注入）
	distinct := map[string]bool{}
	for _, ph := range placeholderRe.FindAllString(where, -1) {
		distinct[ph] = true
	}
	assert.Equal(t, len(args), len(distinct),
		name+": 去重后占位符数量必须等于绑定参数数量")
	// 3) 每个 payload 原样保留在绑定参数中（作为字面量，不做 SQL 语义解释）
	require.Equal(t, len(payloads), len(args), name)
	for i, p := range payloads {
		s, ok := args[i].(string)
		require.True(t, ok, name)
		assert.Contains(t, s, p, name+": payload 必须以字面量进入绑定参数")
	}
}

// TestSQLi_PatientWhere_PayloadStaysInArgs 患者列表筛选（keyword/teamId）注入载荷
func TestSQLi_PatientWhere_PayloadStaysInArgs(t *testing.T) {
	where, args := patientWhere(PatientFilter{Keyword: sqliPayloads[0]})
	assertWhereParametrized(t, "keyword", where, args, sqliPayloads[:1])
	assert.Equal(t, "%"+sqliPayloads[0]+"%", args[0], "keyword 仅做 ILIKE 通配包裹")

	// teamId 精确匹配同样参数化
	where, args = patientWhere(PatientFilter{TeamID: sqliPayloads[1]})
	assertWhereParametrized(t, "teamId", where, args, sqliPayloads[1:2])

	// keyword + teamId 组合：占位符序号递增无错位
	where, args = patientWhere(PatientFilter{Keyword: sqliPayloads[2], TeamID: sqliPayloads[3]})
	assert.Equal(t, 2, len(args))
	assert.Contains(t, where, "$1")
	assert.Contains(t, where, "$2")
	assert.Equal(t, "%"+sqliPayloads[2]+"%", args[0])
	assert.Equal(t, sqliPayloads[3], args[1])
}

// TestSQLi_PatientWhere_EmptyFilter 空筛选不产生 WHERE（无注入面也无语法错误）
func TestSQLi_PatientWhere_EmptyFilter(t *testing.T) {
	where, args := patientWhere(PatientFilter{})
	assert.Empty(t, where)
	assert.Empty(t, args)
}

// TestSQLi_AllPayloads_NeverConcatenated 全量载荷逐一过 patientWhere，全部只进 args
func TestSQLi_AllPayloads_NeverConcatenated(t *testing.T) {
	for _, p := range sqliPayloads {
		where, args := patientWhere(PatientFilter{Keyword: p})
		assertWhereParametrized(t, p, where, args, []string{p})
	}
}
