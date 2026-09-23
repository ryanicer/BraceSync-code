//go:build integration
// +build integration

// Package repo 集成测试（T340）：患者档案存在性查询走真库。
//
// 为什么值得占一条真库用例：PatientExists 是本次 404 判定的唯一事实来源，
// 它若写成「查 devices 表」「被 patient_id 类型隐式转换坑掉」，替身测试一律发现不了。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestITPatientExists(t *testing.T) {
	ctx := context.Background()
	repo := NewPatientRepo(rqPool)

	// rqPatient 由 seedReportsData 插入（status=active）
	ok, err := repo.PatientExists(ctx, rqPatient)
	require.NoError(t, err)
	assert.True(t, ok, "seed 里的患者必须判为存在，否则 404 会打到真实患者身上")

	// 形状像真 ID 但不存在：必须是 (false, nil)，不能是 error
	ok, err = repo.PatientExists(ctx, "P-RPT-IT-NOPE")
	require.NoError(t, err, "查无此人不是数据库错误")
	assert.False(t, ok)

	// 明显非法形状也不报错（VARCHAR(32) 列，无隐式类型转换）
	ok, err = repo.PatientExists(ctx, "NOPE")
	require.NoError(t, err)
	assert.False(t, ok)

	// 空串走同一条查询：不存在，且不 panic
	ok, err = repo.PatientExists(ctx, "")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestITPatientExistsIgnoresStatus 档案状态是生命周期，不是数据可见性 ——
// pending 患者也得判存在（本卡只修「查无此人」，不改可见性口径）。
func TestITPatientExistsIgnoresStatus(t *testing.T) {
	ctx := context.Background()
	repo := NewPatientRepo(rqPool)

	const pid = "P-RPT-IT-PENDING"
	_, err := rqPool.Exec(ctx,
		`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		 VALUES ($1, '待激活患者', '\x00'::bytea, 'rp99' || repeat('0', 60), 'pending')
		 ON CONFLICT (patient_id) DO NOTHING`, pid)
	require.NoError(t, err)

	ok, err := repo.PatientExists(ctx, pid)
	require.NoError(t, err)
	assert.True(t, ok, "pending 档案同样存在；404 只留给「库里没有这个患者」")
}
