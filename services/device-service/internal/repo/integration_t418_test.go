//go:build integration
// +build integration

// Package repo T418 集成测试：GetInstall 的三张 LEFT JOIN 在真库里真带出展示列
//
// 复用 query_integration_test.go 的 seedQueryData（患者「查询患者甲」/ 技师「查询技师」/
// 设备 PRS-QRY-IT-001 + 一条安装记录）—— 该种子插 devices 时不写 model，
// 正好走 devices.model 的列默认值 'PRS-ML05-RC'（000001_init_schema.up.sql:93）。
//
// 本文件只测「JOIN 命中」腿：install_records.patient_id / tech_id / device_id 三列都是
// NOT NULL + FK（:120-132），真库里造不出悬空引用 ⇒ 「未命中为 null」那格由
// handler 层的夹具用例锁（install_detail_join_t418_test.go），不在这里假称覆盖。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

func TestITGetInstallJoinColumns(t *testing.T) {
	seedQueryData(t)
	store := newITStore()
	ctx := context.Background()

	var installID int64
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT install_id FROM install_records WHERE device_id = $1`, qDevice).Scan(&installID))

	rec, err := store.GetInstall(ctx, installID)
	require.NoError(t, err)
	require.NotNil(t, rec.PatientName)
	assert.Equal(t, "查询患者甲", *rec.PatientName)
	require.NotNil(t, rec.TechName)
	assert.Equal(t, "查询技师", *rec.TechName)
	require.NotNil(t, rec.DeviceModel)
	assert.Equal(t, model.DefaultModel, *rec.DeviceModel)

	// 三列的值必须来自 join 而非同一行的别的列：与直接 SQL 读数对平（防将来有人把
	// DTO 改成常量/回落 ID 而本用例仍绿）
	var wantPatientName, wantTechName, wantModel string
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT p.name, t.name, d.model
		 FROM install_records i
		 LEFT JOIN patients p ON p.patient_id = i.patient_id
		 LEFT JOIN technicians t ON t.tech_id = i.tech_id
		 LEFT JOIN devices d ON d.device_id = i.device_id
		 WHERE i.install_id = $1`, installID).Scan(&wantPatientName, &wantTechName, &wantModel))
	assert.Equal(t, wantPatientName, *rec.PatientName)
	assert.Equal(t, wantTechName, *rec.TechName)
	assert.Equal(t, wantModel, *rec.DeviceModel)
}
