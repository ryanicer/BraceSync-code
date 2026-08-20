//go:build integration
// +build integration

// Package repo 集成测试（T030）：管理端列表查询 join/筛选/分页（真实 PG15）
//
// 复用 integration_test.go 的 TestMain（迁移+种子由既有用例基座完成）；
// 本文件仅新增用例与专用种子，不触碰既有测试专家文件。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	qPatient  = "P-QRY-IT-001"
	qPatient2 = "P-QRY-IT-002"
	qTech     = "TECH-QRY-IT-001"
	qDevice   = "PRS-QRY-IT-001"
	qDevice2  = "PRS-QRY-IT-002"
)

// seedQueryData T030 查询用例专用种子（独立 ID，避免与既有用例相互影响）
func seedQueryData(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status) VALUES
		   ($1, '查询患者甲', '\x00'::bytea, 'qd01' || repeat('0', 60), 'active'),
		   ($2, '查询患者乙', '\x00'::bytea, 'qd02' || repeat('0', 60), 'active')
		 ON CONFLICT (patient_id) DO NOTHING`, []any{qPatient, qPatient2}},
		{`INSERT INTO technicians (tech_id, name, phone_enc, phone_hash, install_count) VALUES
		   ($1, '查询技师', '\x00'::bytea, 'qd03' || repeat('0', 60), 0)
		 ON CONFLICT (tech_id) DO NOTHING`, []any{qTech}},
		{`INSERT INTO devices (device_id, device_secret_enc, patient_id, status) VALUES
		   ($1, '\x00'::bytea, $3, 'online'),
		   ($2, '\x00'::bytea, NULL, 'unbound')
		 ON CONFLICT (device_id) DO NOTHING`, []any{qDevice, qDevice2, qPatient}},
	}
	for _, s := range stmts {
		_, err := itPool.Exec(ctx, s.sql, s.args...)
		require.NoError(t, err)
	}
	// 安装记录（依赖上述患者/技师/设备）
	var n int64
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM install_records WHERE device_id = $1`, qDevice).Scan(&n))
	if n == 0 {
		_, err := itPool.Exec(ctx,
			`INSERT INTO install_records (device_id, patient_id, tech_id, calibrate_time)
			 VALUES ($1, $2, $3, now())`, qDevice, qPatient, qTech)
		require.NoError(t, err)
	}
}

func TestITListDevicesJoinFilterPaging(t *testing.T) {
	seedQueryData(t)
	store := newITStore()
	ctx := context.Background()

	// 全量含 join：绑定设备返回患者姓名，未绑定为 nil
	rows, total, err := store.ListDevices(ctx, "PRS-QRY-IT", 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, rows, 2)
	var bound, unbound *DeviceListItem
	for i := range rows {
		switch rows[i].DeviceID {
		case qDevice:
			bound = &rows[i]
		case qDevice2:
			unbound = &rows[i]
		}
	}
	require.NotNil(t, bound)
	require.NotNil(t, bound.PatientName)
	assert.Equal(t, "查询患者甲", *bound.PatientName)
	require.NotNil(t, unbound)
	assert.Nil(t, unbound.PatientName)
	assert.Nil(t, unbound.PatientID)

	// keyword 命中患者姓名
	rows, total, err = store.ListDevices(ctx, "查询患者甲", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	assert.Equal(t, qDevice, rows[0].DeviceID)

	// keyword 命中患者ID
	rows, total, err = store.ListDevices(ctx, qPatient2, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total) // P-QRY-IT-002 无绑定设备
	assert.Empty(t, rows)

	// 分页
	rows, total, err = store.ListDevices(ctx, "PRS-QRY-IT", 2, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, rows, 1)
}

func TestITListInstallRecordsJoinFilter(t *testing.T) {
	seedQueryData(t)
	store := newITStore()
	ctx := context.Background()

	rows, total, err := store.ListInstallRecords(ctx, qDevice, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].PatientName)
	assert.Equal(t, "查询患者甲", *rows[0].PatientName)
	require.NotNil(t, rows[0].TechName)
	assert.Equal(t, "查询技师", *rows[0].TechName)
	assert.Equal(t, "unconfigured", rows[0].WifiStatus)

	// keyword 命中技师姓名
	rows, total, err = store.ListInstallRecords(ctx, "查询技师", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	// keyword 命中患者姓名
	rows, _, err = store.ListInstallRecords(ctx, "查询患者甲", 1, 10)
	require.NoError(t, err)
	assert.Len(t, rows, 1)

	// 无命中
	rows, total, err = store.ListInstallRecords(ctx, "不存在关键词xyz", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, rows)
}
