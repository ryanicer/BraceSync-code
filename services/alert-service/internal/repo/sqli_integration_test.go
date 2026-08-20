//go:build integration
// +build integration

// Package repo — T023 安全审计 Part A · SQL 注入集成测试（告警查询，真实 PG15）
//
// 对齐：docs/ 注入面）
// 复用本包 integration_test.go 的 TestMain（testcontainers + 迁移 + 种子）。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSQLi_IT_ListAlerts_PayloadReturnsZeroRows 三筛选通道注入载荷 → 0 行且不报错
func TestSQLi_IT_ListAlerts_PayloadReturnsZeroRows(t *testing.T) {
	ctx := context.Background()
	r := NewAlertRepo(itPool)

	filters := func(p string) []AlertQueryFilter {
		return []AlertQueryFilter{
			{PatientID: p, Page: 1, PageSize: 20},
			{Type: p, Page: 1, PageSize: 20},
			{Status: p, Page: 1, PageSize: 20},
		}
	}
	for _, p := range alertSQLiPayloads {
		for _, f := range filters(p) {
			rows, total, err := r.ListAlerts(ctx, f)
			require.NoError(t, err, "payload=%q 必须被参数化", p)
			assert.Zero(t, total, "payload=%q 若注入成立将返回全表告警", p)
			assert.Empty(t, rows, p)
		}
	}
}

// TestSQLi_IT_ListAlerts_StackedNoDamage 堆叠载荷不得破坏 alerts 表
func TestSQLi_IT_ListAlerts_StackedNoDamage(t *testing.T) {
	ctx := context.Background()
	r := NewAlertRepo(itPool)

	_, _, err := r.ListAlerts(ctx, AlertQueryFilter{
		PatientID: "'; DROP TABLE alerts;--", Page: 1, PageSize: 20,
	})
	require.NoError(t, err)

	// alerts 表仍可用：空筛选查询不报错
	_, _, err = r.ListAlerts(ctx, AlertQueryFilter{Page: 1, PageSize: 20})
	require.NoError(t, err, "alerts 表不得被堆叠注入破坏")
}
