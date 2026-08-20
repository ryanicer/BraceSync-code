//go:build integration
// +build integration

// Package repo — T023 安全审计 Part A · SQL 注入集成测试（真实 PG15 回归）
//
// 对齐：docs/ 注入面）
//
// 判据：OWASP 载荷作为用户输入传入公开查询方法时——
//  1. 不报 SQL 语法/执行错误（载荷被当纯字面量绑定）
//  2. 不放大结果集：`' OR 1=1--` 若被拼接会命中全表（种子 2 条患者/1 条反馈），
//     参数化下只能按字面量匹配 → 0 行。total 放大即注入成立。
//  3. 堆叠载荷（;DROP）不产生破坏：执行后种子数据仍在。
//
// 复用本包 integration_test.go 的 TestMain（testcontainers + 迁移 + 种子）。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSQLi_IT_ListPatients_PayloadReturnsZeroRows 注入载荷查患者列表 → 0 行且不报错
func TestSQLi_IT_ListPatients_PayloadReturnsZeroRows(t *testing.T) {
	ctx := context.Background()
	for _, p := range sqliPayloads {
		rows, total, err := itStore.ListPatients(ctx, PatientFilter{Keyword: p, Page: 1, PageSize: 20})
		require.NoError(t, err, "payload=%q 必须被参数化而非拼入 SQL", p)
		assert.Zero(t, total, "payload=%q 若注入成立将返回全表（种子 2 行）", p)
		assert.Empty(t, rows, p)

		// teamId 通道同判
		rows, total, err = itStore.ListPatients(ctx, PatientFilter{TeamID: p, Page: 1, PageSize: 20})
		require.NoError(t, err, "teamId payload=%q", p)
		assert.Zero(t, total, "teamId payload=%q", p)
		assert.Empty(t, rows)
	}
}

// TestSQLi_IT_ListFeedbacks_PayloadReturnsZeroRows 注入载荷查反馈列表 → 空且不报错
func TestSQLi_IT_ListFeedbacks_PayloadReturnsZeroRows(t *testing.T) {
	ctx := context.Background()
	for _, p := range sqliPayloads {
		list, err := itStore.ListFeedbacks(ctx, p)
		require.NoError(t, err, "payload=%q", p)
		assert.Empty(t, list, "payload=%q 不得命中任何反馈", p)
	}
}

// TestSQLi_IT_StackedPayload_NoDestructiveEffect 堆叠载荷不得破坏种子数据
func TestSQLi_IT_StackedPayload_NoDestructiveEffect(t *testing.T) {
	ctx := context.Background()
	const stacked = "'; DROP TABLE patients;--"

	_, _, err := itStore.ListPatients(ctx, PatientFilter{Keyword: stacked, Page: 1, PageSize: 20})
	require.NoError(t, err)

	// 攻击后种子完好：patient 表仍存在且种子行可查
	p, err := itStore.GetPatient(ctx, "P-USR-IT-1")
	require.NoError(t, err, "patients 表不得被堆叠注入破坏")
	require.NotNil(t, p)
}

// TestSQLi_IT_GetAdminByUsername_PayloadLiteral 登录查询载荷按字面量处理 → 查无此人
func TestSQLi_IT_GetAdminByUsername_PayloadLiteral(t *testing.T) {
	ctx := context.Background()
	for _, p := range []string{"' OR 1=1--", "it_admin'--", "admin' UNION SELECT 1--"} {
		admin, err := itStore.GetAdminByUsername(ctx, p)
		require.NoError(t, err, "payload=%q", p)
		assert.Nil(t, admin, "payload=%q 不得绕过用户名匹配", p)
	}
	// 种子账号仍可正常登录查询（表结构未受损）
	admin, err := itStore.GetAdminByUsername(ctx, "it_admin")
	require.NoError(t, err)
	require.NotNil(t, admin)
}
