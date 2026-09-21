//go:build integration
// +build integration

// Package repo T299 集成测试（真实 PG15）：一患者一设备在写入口被显式拒绝，
// 且原绑定设备不被静默改绑（取代 T151 方案B 的静默清除）；并发窗口仍由 uk_devices_active_patient 兜底。
//
// 夹具隔离：本文件每个用例自带患者与设备（患者被占用正是本卡触发条件，共用夹具会互相污染），
// 并在收尾解绑，不把占用留给其它用例。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// itSeedPatientT299 幂等插入本文件专用患者（patients owner=user-service，此处仅集成测试最小夹具）
func itSeedPatientT299(ctx context.Context, t *testing.T, patientID, hashPrefix string) {
	t.Helper()
	_, err := itPool.Exec(ctx,
		`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		 VALUES ($1, 'T299患者', '\x00'::bytea, $2 || repeat('0', 60), 'active')
		 ON CONFLICT (patient_id) DO NOTHING`,
		patientID, hashPrefix)
	require.NoError(t, err, "插入 T299 患者夹具失败 %s", patientID)
}

// itUnbind 收尾释放患者占用
func itUnbind(ctx context.Context, t *testing.T, store Store, deviceID string) {
	t.Helper()
	_, err := store.Unbind(ctx, deviceID, itTech)
	require.NoError(t, err, "收尾解绑失败 %s", deviceID)
}

func TestIT_Bind_OneDevicePerPatient_Rejected(t *testing.T) {
	const (
		patientID  = "P-DEV-IT-T299-BIND"
		devA, devB = "DEV-IT-T299-A", "DEV-IT-T299-B"
	)
	ctx := context.Background()
	store := newITStore()
	itSeedPatientT299(ctx, t, patientID, "da29")
	itRegister(ctx, t, store, devA)
	itRegister(ctx, t, store, devB)

	_, err := store.Bind(ctx, BindParams{DeviceID: devA, PatientID: patientID, OperatorID: itTech})
	require.NoError(t, err)

	// 负例：同患者再绑另一台 → *ErrPatientHasDevice，且带占位设备号
	_, err = store.Bind(ctx, BindParams{DeviceID: devB, PatientID: patientID, OperatorID: itTech})
	var phd *ErrPatientHasDevice
	require.ErrorAs(t, err, &phd, "跨设备再绑同一患者必须被显式拒绝")
	assert.Equal(t, patientID, phd.PatientID)
	assert.Equal(t, devA, phd.OtherDeviceID, "错误须给出占位设备，供上层提示先解绑")

	// 关键回归：原绑定设备不得被静默清掉（T151 方案B 旧行为）
	dev, err := store.GetDevice(ctx, devA)
	require.NoError(t, err)
	require.NotNil(t, dev.PatientID, "被拒后原设备仍归属该患者")
	assert.Equal(t, patientID, *dev.PatientID)
	bindings, err := store.ListBindings(ctx, devA)
	require.NoError(t, err)
	require.Len(t, bindings, 1, "被拒不得新增/关闭 binding 行")
	assert.Nil(t, bindings[0].UnbindAt)

	// 被拒设备保持未绑定
	devBRow, err := store.GetDevice(ctx, devB)
	require.NoError(t, err)
	assert.Nil(t, devBRow.PatientID)

	// 正例：先解绑 A，再绑 B 成功（换机流程未被规则堵死）
	itUnbind(ctx, t, store, devA)
	_, err = store.Bind(ctx, BindParams{DeviceID: devB, PatientID: patientID, OperatorID: itTech})
	require.NoError(t, err, "解绑旧设备后新绑必须放行")
	devBRow, err = store.GetDevice(ctx, devB)
	require.NoError(t, err)
	require.NotNil(t, devBRow.PatientID)
	assert.Equal(t, patientID, *devBRow.PatientID)

	itUnbind(ctx, t, store, devB)
}

func TestIT_Rebind_OneDevicePerPatient_Rejected(t *testing.T) {
	const (
		patient1, patient2 = "P-DEV-IT-T299-RB1", "P-DEV-IT-T299-RB2"
		devC, devD         = "DEV-IT-T299-C", "DEV-IT-T299-D"
	)
	ctx := context.Background()
	store := newITStore()
	itSeedPatientT299(ctx, t, patient1, "da2b")
	itSeedPatientT299(ctx, t, patient2, "da2c")
	itRegister(ctx, t, store, devC)
	itRegister(ctx, t, store, devD)

	_, err := store.Bind(ctx, BindParams{DeviceID: devC, PatientID: patient1, OperatorID: itTech})
	require.NoError(t, err)
	_, err = store.Bind(ctx, BindParams{DeviceID: devD, PatientID: patient2, OperatorID: itTech})
	require.NoError(t, err)

	// 换绑到「已持有其它设备」的患者 → 拒绝，且 C 的原 active binding 未被半途关闭
	_, err = store.Rebind(ctx, BindParams{DeviceID: devC, PatientID: patient2, OperatorID: itTech})
	var phd *ErrPatientHasDevice
	require.ErrorAs(t, err, &phd)
	assert.Equal(t, devD, phd.OtherDeviceID)
	devCRow, err := store.GetDevice(ctx, devC)
	require.NoError(t, err)
	require.NotNil(t, devCRow.PatientID)
	assert.Equal(t, patient1, *devCRow.PatientID, "被拒换绑不得改动原归属")

	bindings, err := store.ListBindings(ctx, devC)
	require.NoError(t, err)
	require.Len(t, bindings, 1, "被拒换绑不得写入新 binding 行")
	assert.Nil(t, bindings[0].UnbindAt, "原 active binding 保持有效")

	// 正例：同患者重复换绑仍幂等（占用者就是本设备，不构成冲突）
	_, err = store.Rebind(ctx, BindParams{DeviceID: devC, PatientID: patient1, OperatorID: itTech})
	require.NoError(t, err)

	itUnbind(ctx, t, store, devC)
	itUnbind(ctx, t, store, devD)
}

// 库层防线仍在：绕过应用层直写 devices.patient_id 抢同一患者 → 23505
func TestIT_ActivePatientUniqueIndex_StillGuards(t *testing.T) {
	const (
		patientID  = "P-DEV-IT-T299-IDX"
		devE, devF = "DEV-IT-T299-E", "DEV-IT-T299-F"
	)
	ctx := context.Background()
	store := newITStore()
	itSeedPatientT299(ctx, t, patientID, "da2d")
	itRegister(ctx, t, store, devE)
	itRegister(ctx, t, store, devF)

	_, err := store.Bind(ctx, BindParams{DeviceID: devE, PatientID: patientID, OperatorID: itTech})
	require.NoError(t, err)

	_, err = itPool.Exec(ctx,
		`UPDATE devices SET patient_id = $2 WHERE device_id = $1`, devF, patientID)
	require.Error(t, err, "uk_devices_active_patient 必须拒绝第二台设备指向同一患者")
	assert.True(t, isUniqueViolation(err, ukActivePatient), "须被识别为患者占用冲突：%v", err)

	// 应用层走同一判据：F 抢绑该患者 → ErrPatientHasDevice（而非 90001）
	_, err = store.Bind(ctx, BindParams{DeviceID: devF, PatientID: patientID, OperatorID: itTech})
	var phd *ErrPatientHasDevice
	require.ErrorAs(t, err, &phd)
	assert.Equal(t, devE, phd.OtherDeviceID)

	itUnbind(ctx, t, store, devE)
	itUnbind(ctx, t, store, devF)
}

// 确认换绑（PRD §7C.3 技师确认后重试）：同事务内解 A 绑 B，两步要么都成要么都不落库
func TestIT_Bind_ConfirmSwap_SwapsInOneTransaction(t *testing.T) {
	const (
		patientID  = "P-DEV-IT-T299-SW"
		devG, devH = "DEV-IT-T299-G", "DEV-IT-T299-H"
	)
	ctx := context.Background()
	store := newITStore()
	itSeedPatientT299(ctx, t, patientID, "da2e")
	itRegister(ctx, t, store, devG)
	itRegister(ctx, t, store, devH)

	_, err := store.Bind(ctx, BindParams{DeviceID: devG, PatientID: patientID, OperatorID: itTech})
	require.NoError(t, err)

	out, err := store.Bind(ctx, BindParams{
		DeviceID: devH, PatientID: patientID, OperatorID: itTech, ConfirmSwap: true,
	})
	require.NoError(t, err, "带换绑意图必须放行")
	require.NotNil(t, out)
	assert.Equal(t, devG, out.PatientSwappedFrom, "须带出被解除的原设备号")

	devGRow, err := store.GetDevice(ctx, devG)
	require.NoError(t, err)
	assert.Nil(t, devGRow.PatientID, "旧设备归属须在事务内清空")
	assert.Equal(t, model.StatusUnbound, devGRow.Status)

	devHRow, err := store.GetDevice(ctx, devH)
	require.NoError(t, err)
	require.NotNil(t, devHRow.PatientID)
	assert.Equal(t, patientID, *devHRow.PatientID)

	// 历史可追溯：旧设备 binding 被关闭且 reason=rebind，且没多出活跃行
	bindings, err := store.ListBindings(ctx, devG)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.NotNil(t, bindings[0].UnbindAt)
	require.NotNil(t, bindings[0].Reason)
	assert.Equal(t, model.ReasonRebind, *bindings[0].Reason)

	// 库层不变式：该患者名下活跃设备恰好一台
	var active int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM devices WHERE patient_id = $1`, patientID).Scan(&active))
	assert.Equal(t, 1, active, "确认换绑后患者仍只能持有一台生效设备")

	itUnbind(ctx, t, store, devH)
}

// 确认换绑对 rebind 写入口同样生效，且不带意图时仍拒（同一事务边界，两个入口口径一致）
func TestIT_Rebind_ConfirmSwap_SwapsPatientDevice(t *testing.T) {
	const (
		patient3, patient4 = "P-DEV-IT-T299-RB3", "P-DEV-IT-T299-RB4"
		devI, devJ, devK   = "DEV-IT-T299-I", "DEV-IT-T299-J", "DEV-IT-T299-K"
	)
	ctx := context.Background()
	store := newITStore()
	itSeedPatientT299(ctx, t, patient3, "da2f")
	itSeedPatientT299(ctx, t, patient4, "da30")
	for _, d := range []string{devI, devJ, devK} {
		itRegister(ctx, t, store, d)
	}
	_, err := store.Bind(ctx, BindParams{DeviceID: devI, PatientID: patient3, OperatorID: itTech})
	require.NoError(t, err)
	_, err = store.Bind(ctx, BindParams{DeviceID: devJ, PatientID: patient4, OperatorID: itTech})
	require.NoError(t, err)
	_, err = store.Bind(ctx, BindParams{DeviceID: devK, PatientID: patient4, OperatorID: itTech})
	var phd *ErrPatientHasDevice
	require.ErrorAs(t, err, &phd, "换绑前该患者已占用两台设备，先拒掉多余的那台")

	out, err := store.Rebind(ctx, BindParams{
		DeviceID: devI, PatientID: patient4, OperatorID: itTech, ConfirmSwap: true,
	})
	require.NoError(t, err, "rebind 带意图须放行")
	require.NotNil(t, out)
	assert.Equal(t, devJ, out.PatientSwappedFrom, "被解除的应是目标患者原设备")

	devIRow, err := store.GetDevice(ctx, devI)
	require.NoError(t, err)
	require.NotNil(t, devIRow.PatientID)
	assert.Equal(t, patient4, *devIRow.PatientID)

	devJRow, err := store.GetDevice(ctx, devJ)
	require.NoError(t, err)
	assert.Nil(t, devJRow.PatientID, "目标患者原设备被显式解除")

	itUnbind(ctx, t, store, devI)
}
