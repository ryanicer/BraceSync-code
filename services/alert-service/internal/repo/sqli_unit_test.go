// Package repo — T023 安全审计 Part A · SQL 注入单元测试（告警查询 WHERE 构造）
//
// 对齐：docs/ 注入面）
//
// buildAlertWhere 是告警列表三筛选条件（patientId/type/status）的唯一拼接点，
// 断言注入载荷只进绑定参数、SQL 文本无字面量残留。真实 PG 回归见 sqli_integration_test.go。
package repo

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var alertPlaceholderRe = regexp.MustCompile(`\$[0-9]+`)

// alertSQLiPayloads OWASP 载荷（布尔绕过/union/盲注）
var alertSQLiPayloads = []string{
	"' OR 1=1--",
	"' UNION SELECT username, password_hash, role_id FROM admins--",
	"' AND (SELECT COUNT(*) FROM admins) > 0--", // 盲注
}

// TestSQLi_BuildAlertWhere_PayloadStaysInArgs 三筛选条件逐一注入载荷
func TestSQLi_BuildAlertWhere_PayloadStaysInArgs(t *testing.T) {
	cases := []struct {
		name string
		f    AlertQueryFilter
		want string // 载荷原样进入 args[0]
	}{
		{"patientId", AlertQueryFilter{PatientID: alertSQLiPayloads[0]}, alertSQLiPayloads[0]},
		{"type", AlertQueryFilter{Type: alertSQLiPayloads[1]}, alertSQLiPayloads[1]},
		{"status", AlertQueryFilter{Status: alertSQLiPayloads[2]}, alertSQLiPayloads[2]},
	}
	for _, c := range cases {
		where, args := buildAlertWhere(c.f)
		assert.NotContains(t, where, "'", c.name+": SQL 文本不得出现单引号")
		assert.NotContains(t, strings.ToLower(where), "union", c.name)
		assert.NotContains(t, strings.ToLower(where), "--", c.name)
		require.Len(t, args, 1, c.name)
		assert.Equal(t, c.want, args[0], c.name+": 载荷以字面量进绑定参数")
		assert.Equal(t, 1, len(alertPlaceholderRe.FindAllString(where, -1)), c.name)
	}
}

// TestSQLi_BuildAlertWhere_Combined_NoPlaceholderDrift 组合筛选占位符序号连续无错位
func TestSQLi_BuildAlertWhere_Combined_NoPlaceholderDrift(t *testing.T) {
	where, args := buildAlertWhere(AlertQueryFilter{
		PatientID: alertSQLiPayloads[0],
		Type:      alertSQLiPayloads[1],
		Status:    alertSQLiPayloads[2],
	})
	assert.Equal(t, 3, len(args))
	assert.Equal(t, 3, len(alertPlaceholderRe.FindAllString(where, -1)))
	assert.Contains(t, where, "$1")
	assert.Contains(t, where, "$2")
	assert.Contains(t, where, "$3")
	for i, p := range alertSQLiPayloads {
		assert.Equal(t, p, args[i])
	}
}

// TestSQLi_BuildAlertWhere_EmptyFilter 空筛选返回空 WHERE（无注入面）
func TestSQLi_BuildAlertWhere_EmptyFilter(t *testing.T) {
	where, args := buildAlertWhere(AlertQueryFilter{})
	assert.Empty(t, where)
	assert.Empty(t, args)
}
