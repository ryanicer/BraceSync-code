//go:build integration
// +build integration

// Package repo — T023 安全审计 Part A · SQL 注入集成测试（设备/安装记录查询，真实 PG15）
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

// devSQLiPayloads OWASP 载荷（keyword 走 ILIKE 通道：布尔绕过/union/堆叠/盲注）
var devSQLiPayloads = []string{
	"' OR 1=1--",
	"' UNION SELECT username, password_hash FROM admins--",
	"'); DROP TABLE devices;--",
	"' AND 1=CAST((SELECT username FROM admins LIMIT 1) AS int)--", // 报错型盲注
}

// TestSQLi_IT_ListDevices_PayloadReturnsZeroRows 设备列表 keyword 注入载荷 → 0 行且不报错
func TestSQLi_IT_ListDevices_PayloadReturnsZeroRows(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)

	for _, p := range devSQLiPayloads {
		rows, total, err := store.ListDevices(ctx, p, 1, 20)
		require.NoError(t, err, "payload=%q 必须被参数化", p)
		assert.Zero(t, total, "payload=%q 若注入成立将返回全表设备", p)
		assert.Empty(t, rows, p)
	}
}

// TestSQLi_IT_ListInstallRecords_PayloadReturnsZeroRows 安装记录 keyword 注入载荷 → 0 行
func TestSQLi_IT_ListInstallRecords_PayloadReturnsZeroRows(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)

	for _, p := range devSQLiPayloads {
		rows, total, err := store.ListInstallRecords(ctx, p, 1, 20)
		require.NoError(t, err, "payload=%q", p)
		assert.Zero(t, total, "payload=%q", p)
		assert.Empty(t, rows, p)
	}
}

// TestSQLi_IT_StackedPayload_NoDestructiveEffect 堆叠载荷不得破坏 devices/install_records 表
func TestSQLi_IT_StackedPayload_NoDestructiveEffect(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)

	_, _, err := store.ListDevices(ctx, "'; DROP TABLE devices;--", 1, 20)
	require.NoError(t, err)

	// 攻击后表结构完好：正常查询不报错
	_, _, err = store.ListDevices(ctx, "", 1, 20)
	require.NoError(t, err, "devices 表不得被堆叠注入破坏")
}
