//go:build integration
// +build integration

// Package repo T353 集成测试：PatientExists 在真实 PG 上对「已建档患者」与
// 「patients 表无此 ID」分别给出 true / false 且不报错（false 必须是 (false, nil)，
// handler 侧据此把「查无此人」映射为 404 加 10404，而不是 500）。
//
// 运行：make test-integration（需 Docker，本机无 Docker，由 CI go-integration 执行）
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestITT353PatientExists(t *testing.T) {
	store := newITStore()
	ctx := context.Background()

	// 种子患者（runIT 里 INSERT INTO patients）→ 存在
	// patient_preferences 无行不影响判定：判定只看 patients 表
	_, err := store.GetWearReminder(ctx, itPatient)
	require.NoError(t, err, "前置：种子患者应可读佩戴提醒（无偏好行走默认值）")

	exists, err := store.PatientExists(ctx, itPatient)
	require.NoError(t, err)
	assert.True(t, exists, "%s 是种子患者，必须判存在", itPatient)

	// 从未建档的 ID → 不存在（(false, nil)，不是 error）
	missing, err := store.PatientExists(ctx, "P-MSG-IT-NOT-EXIST")
	require.NoError(t, err, "查无此人必须是 (false, nil)，否则 handler 会误映射为 500")
	assert.False(t, missing)

	// 空串不 panic，按不存在处理
	empty, err := store.PatientExists(ctx, "")
	require.NoError(t, err)
	assert.False(t, empty)
}
