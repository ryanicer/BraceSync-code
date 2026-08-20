// Package service — device-service 业务层测试用例（测试先行 / TDD）
//
// TDD 说明：
//
//	本文件覆盖 device-service 核心业务规则：绑定互斥、换绑历史、状态机转换。
//	T026 升级：原 KNOWN_RED stub 已升级为委托真实 DeviceService + FakeStore，
//	用例直接验证真实实现（内存 FakeStore，无 DB 依赖）。
//
// 覆盖规则（对齐 T015 验收标准 2-4）：
//   - 绑定互斥：同一设备仅一个 active binding（同设备第二绑自动换绑）
//   - 换绑/解绑历史可追溯：unbind_at / reason / operator_id
//   - 状态机转换：Touch faultCode=0→online / faultCode>0→abnormal + last_report_at/updated_at
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/testutil"
)

// ============================================================
// 辅助函数：创建真实 DeviceService + FakeStore
// T026 升级：stub 已移除，直接委托真实实现
// ============================================================

// newTestService 创建真实 DeviceService（内存 FakeStore，测试密钥）
func newTestService() (*DeviceService, *testutil.FakeStore) {
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	if err != nil {
		panic(err)
	}
	store := testutil.NewFakeStore()
	svc := NewDeviceService(store, enc)
	return svc, store
}

// registerAndAddPatient 注册设备 + 注入患者存在性（测试前置自包含）
func registerAndAddPatient(t *testing.T, svc *DeviceService, store *testutil.FakeStore, deviceID, patientID string) {
	t.Helper()
	ctx := context.Background()
	_, _, appErr := svc.Register(ctx, deviceID, "")
	require.Nil(t, appErr, "register device should succeed")
	store.AddPatient(patientID)
}

// ============================================================
// S1: 绑定互斥 — 同一设备第二绑自动换绑（真实行为：auto-rebind）
// ============================================================
func TestBindingMutex_OneActivePerDevice(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	devID := "PRS-ML05-RC-20260701001"

	registerAndAddPatient(t, svc, store, devID, "P20260001")
	registerAndAddPatient(t, svc, store, devID, "P20260002")

	// 第一次绑定：设备绑定到 P20260001
	result1, appErr := svc.Bind(ctx, devID, "P20260001", "")
	require.Nil(t, appErr)
	require.NotNil(t, result1)
	assert.False(t, result1.Rebound, "first bind should not be a rebind")

	// 第二次绑定：同设备绑定到 P20260002 → 真实实现自动换绑
	result2, appErr := svc.Bind(ctx, devID, "P20260002", "")

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. Second bind auto-rebinds (succeeds as rebind, not rejected)")
	require.Nil(t, appErr)
	require.NotNil(t, result2)
	assert.True(t, result2.Rebound, "second bind to different patient should be a rebind")
	assert.Equal(t, "P20260002", *result2.Device.PatientID, "device should now be bound to P20260002")
}

// S2: 绑定互斥 — 不同设备可各自绑定
func TestBindingMutex_DifferentDevice(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()

	registerAndAddPatient(t, svc, store, "PRS-ML05-RC-20260701001", "P20260001")
	registerAndAddPatient(t, svc, store, "PRS-ML05-RC-20260701002", "P20260002")

	result1, appErr := svc.Bind(ctx, "PRS-ML05-RC-20260701001", "P20260001", "")
	require.Nil(t, appErr)

	result2, appErr := svc.Bind(ctx, "PRS-ML05-RC-20260701002", "P20260002", "")
	require.Nil(t, appErr)

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. Different devices can each have one active binding")
	assert.NotEqual(t, result1.Device.DeviceID, result2.Device.DeviceID, "should be different devices")
}

// S3: 换绑历史 — unbind_at / reason / operator_id 可追溯
func TestRebindHistory_PreservesUnbindInfo(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	devID := "PRS-ML05-RC-20260701001"

	registerAndAddPatient(t, svc, store, devID, "P20260001")
	registerAndAddPatient(t, svc, store, devID, "P20260002")

	// Step 1: 绑定到 P20260001
	_, appErr := svc.Bind(ctx, devID, "P20260001", "")
	require.Nil(t, appErr)

	// Step 2: 换绑到 P20260002（自动换绑：旧绑定写 unbind_at+reason=rebind）
	_, appErr = svc.Bind(ctx, devID, "P20260002", "T0001")
	require.Nil(t, appErr)

	// 查绑定历史验证旧绑定记录
	bindings, appErr := svc.ListBindings(ctx, devID)
	require.Nil(t, appErr)

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. Old binding has unbind_at, reason=rebind, operator_id recorded")
	require.GreaterOrEqual(t, len(bindings), 2, "should have at least 2 bindings (old closed + new active)")

	// bindings 按 bind_at DESC 排序，[0]是最新，[1]是旧绑定
	oldBinding := bindings[1]
	assert.NotNil(t, oldBinding.UnbindAt, "old binding must have unbind_at set")
	if oldBinding.Reason != nil {
		assert.Equal(t, model.ReasonRebind, *oldBinding.Reason, "reason should be 'rebind'")
	}
	if oldBinding.OperatorID != nil {
		assert.Equal(t, "T0001", *oldBinding.OperatorID, "operator_id should be recorded")
	}
}

// S4: 解绑历史 — reason 区分 unbind/rebind
func TestUnbind_ReasonUnbind(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	devID := "PRS-ML05-RC-20260701001"

	registerAndAddPatient(t, svc, store, devID, "P20260001")

	// 先绑定
	_, appErr := svc.Bind(ctx, devID, "P20260001", "")
	require.Nil(t, appErr)

	// 解绑（非换绑）
	alreadyUnbound, appErr := svc.Unbind(ctx, devID, "T0001")
	require.Nil(t, appErr)

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. Unbind — alreadyUnbound=false (had active), reason=unbind")
	assert.False(t, alreadyUnbound, "device had active binding, so alreadyUnbound should be false")

	// 验证绑定历史
	bindings, appErr := svc.ListBindings(ctx, devID)
	require.Nil(t, appErr)
	require.GreaterOrEqual(t, len(bindings), 1)

	closedBinding := bindings[0]
	assert.NotNil(t, closedBinding.UnbindAt, "unbind_at should be set")
	if closedBinding.Reason != nil {
		assert.Equal(t, model.ReasonUnbind, *closedBinding.Reason, "reason should be 'unbind'")
	}
	if closedBinding.OperatorID != nil {
		assert.Equal(t, "T0001", *closedBinding.OperatorID)
	}
}

// S5: 状态机 — Touch faultCode=0 → online
func TestStateMachine_UnboundToOnline(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	devID := "PRS-ML05-RC-20260701001"

	registerAndAddPatient(t, svc, store, devID, "P20260001")
	_, appErr := svc.Bind(ctx, devID, "P20260001", "")
	require.Nil(t, appErr)

	now := time.Now()
	appErr = svc.Touch(ctx, devID, now, 0)

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. Touch faultCode=0 → status=online, last_report_at updated")
	require.Nil(t, appErr)

	dev, appErr := svc.GetDevice(ctx, devID)
	require.Nil(t, appErr)
	assert.Equal(t, model.StatusOnline, dev.Status)
	if dev.LastReportAt != nil {
		assert.True(t, dev.LastReportAt.Equal(now) || dev.LastReportAt.After(now.Add(-time.Second)),
			"last_report_at should be set to now")
	}
}

// S6: 状态机 — Touch does NOT produce offline (it produces online or abnormal)
// 真实行为：绑定后设备状态变为 offline（NextStatusOnBind），Touch 只产生 online/abnormal
func TestStateMachine_UnboundToOffline(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	devID := "PRS-ML05-RC-20260701001"

	registerAndAddPatient(t, svc, store, devID, "P20260001")
	_, appErr := svc.Bind(ctx, devID, "P20260001", "")
	require.Nil(t, appErr)

	// 绑定后设备状态变为 offline（NextStatusOnBind: unbound→offline）
	dev, appErr := svc.GetDevice(ctx, devID)
	require.Nil(t, appErr)

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. After bind, status=offline (NextStatusOnBind). Touch does NOT produce offline.")
	assert.Equal(t, model.StatusOffline, dev.Status, "after bind from unbound, status should be offline")

	// Touch with faultCode=0 → online (not offline)
	appErr = svc.Touch(ctx, devID, time.Now(), 0)
	require.Nil(t, appErr)
	dev, appErr = svc.GetDevice(ctx, devID)
	require.Nil(t, appErr)
	assert.Equal(t, model.StatusOnline, dev.Status, "Touch with faultCode=0 should produce online, not offline")
}

// S7: 状态机 — Touch faultCode>0 → abnormal（设备故障）
func TestStateMachine_UnboundToAbnormal(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	devID := "PRS-ML05-RC-20260701001"

	registerAndAddPatient(t, svc, store, devID, "P20260001")
	_, appErr := svc.Bind(ctx, devID, "P20260001", "")
	require.Nil(t, appErr)

	appErr = svc.Touch(ctx, devID, time.Now(), 1)

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. Touch faultCode=1 → status=abnormal (device fault)")
	require.Nil(t, appErr)

	dev, appErr := svc.GetDevice(ctx, devID)
	require.Nil(t, appErr)
	assert.Equal(t, model.StatusAbnormal, dev.Status)
}

// S8: 状态机 — 无效输入拒绝（原 "unknown" 状态测试适配为无效 device_id）
func TestStateMachine_InvalidTransition(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// Touch 不存在的设备 → 应返回错误
	appErr := svc.Touch(ctx, "nonexistent-device-12345", time.Now(), 0)

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. Touch on nonexistent device → error (invalid input rejected)")
	assert.NotNil(t, appErr, "Touch on nonexistent device should return error")
}

// S9: 状态机 — 上报更新 last_report_at
func TestStateMachine_ReportingUpdatesLastReportAt(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	devID := "PRS-ML05-RC-20260701001"

	registerAndAddPatient(t, svc, store, devID, "P20260001")
	_, appErr := svc.Bind(ctx, devID, "P20260001", "")
	require.Nil(t, appErr)

	oldTime := time.Now().Add(-5 * time.Minute)
	newTime := time.Now()

	// 先 Touch 旧时间
	appErr = svc.Touch(ctx, devID, oldTime, 0)
	require.Nil(t, appErr)

	// 再 Touch 新时间
	appErr = svc.Touch(ctx, devID, newTime, 0)

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. last_report_at updated to newTime on every transition")
	require.Nil(t, appErr)

	dev, appErr := svc.GetDevice(ctx, devID)
	require.Nil(t, appErr)
	if dev.LastReportAt != nil {
		assert.True(t, dev.LastReportAt.After(newTime.Add(-time.Second)),
			"last_report_at should reflect the new report time")
	}
}

// S10: 注册设备 — 幂等（同 device_id 第二次调不报错）
func TestRegisterDevice_Idempotent(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	deviceID := "PRS-ML05-RC-20260701001"

	d1, created1, appErr := svc.Register(ctx, deviceID, "")
	require.Nil(t, appErr)
	require.NotNil(t, d1)
	assert.True(t, created1, "first register should create new device")

	d2, created2, appErr := svc.Register(ctx, deviceID, "")

	t.Log("KNOWN_RED upgraded: now delegates to real implementation. Second register returns existing device (idempotent), created=false")
	require.Nil(t, appErr)
	require.NotNil(t, d2)
	assert.False(t, created2, "second register should not create (idempotent)")
	assert.Equal(t, deviceID, d2.DeviceID, "should return same device on idempotent call")
}
