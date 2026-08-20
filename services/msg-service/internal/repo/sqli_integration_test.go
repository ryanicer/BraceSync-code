//go:build integration
// +build integration

// Package repo — T023 安全审计 Part A · SQL 注入集成测试（通知记录查询，真实 PG15）
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

// msgSQLiPayloads OWASP 载荷（四个筛选通道：patientId/alertType/channel/status）
var msgSQLiPayloads = []string{
	"' OR 1=1--",
	"' UNION SELECT content FROM notification_records--",
	"' AND SUBSTRING(current_setting('is_superuser'),1,1)='t'--", // 盲注
}

// TestSQLi_IT_ListNotificationRecords_PayloadReturnsZeroRows 四通道注入载荷 → 0 行且不报错
func TestSQLi_IT_ListNotificationRecords_PayloadReturnsZeroRows(t *testing.T) {
	ctx := context.Background()
	store := newITStore()

	filters := func(p string) []RecordFilter {
		return []RecordFilter{
			{PatientID: p, Page: 1, PageSize: 20},
			{AlertType: p, Page: 1, PageSize: 20},
			{Channel: p, Page: 1, PageSize: 20},
			{Status: p, Page: 1, PageSize: 20},
		}
	}
	for _, p := range msgSQLiPayloads {
		for _, f := range filters(p) {
			recs, total, err := store.ListNotificationRecords(ctx, f)
			require.NoError(t, err, "payload=%q 必须被参数化", p)
			assert.Zero(t, total, "payload=%q 若注入成立将返回全表记录", p)
			assert.Empty(t, recs, p)
		}
	}
}

// TestSQLi_IT_ListNotificationRecords_StackedNoDamage 堆叠载荷不得破坏通知记录表
func TestSQLi_IT_ListNotificationRecords_StackedNoDamage(t *testing.T) {
	ctx := context.Background()
	store := newITStore()

	_, _, err := store.ListNotificationRecords(ctx,
		RecordFilter{PatientID: "'; DROP TABLE notification_records;--", Page: 1, PageSize: 20})
	require.NoError(t, err)

	// 表结构完好
	_, _, err = store.ListNotificationRecords(ctx, RecordFilter{Page: 1, PageSize: 20})
	require.NoError(t, err, "notification_records 表不得被堆叠注入破坏")
}
