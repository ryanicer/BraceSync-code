// Package service 单元测试：注册幂等/绑定互斥/解绑幂等/状态机/安装基线
//
// 对齐：docs/ §1（单元层）· docs/ 验收标准
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

func newTestSvc(t *testing.T) (*DeviceService, *testutil.FakeStore) {
	t.Helper()
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	store := testutil.NewFakeStore()
	return NewDeviceService(store, enc), store
}

func offsets20() []float32 {
	out := make([]float32, model.PointCount)
	for i := range out {
		out[i] = float32(i) * 0.5
	}
	return out
}

// ─────────────────────────────────────────────────────────────
// 设备注册：幂等 + 密钥加密存储 + 不返回明文
// ─────────────────────────────────────────────────────────────

func TestRegister_NewAndIdempotent(t *testing.T) {
	svc, store := newTestSvc(t)
	ctx := context.Background()

	dev, created, appErr := svc.Register(ctx, "DEV-REG-001", "")
	require.Nil(t, appErr)
	assert.True(t, created)
	assert.Equal(t, model.DefaultModel, dev.Model)
	assert.Equal(t, model.StatusUnbound, dev.Status)
	assert.Equal(t, 1, dev.SecretVersion)
	require.NotEmpty(t, dev.DeviceSecretEnc, "密文必须落库")

	// 密文可解密（供 gateway 验签取用），且不等于常见明文形态
	enc, _ := crypto.NewEncryptor(testutil.TestEncKey)
	secret, err := enc.Decrypt(dev.DeviceSecretEnc)
	require.NoError(t, err)
	assert.Len(t, secret, 64, "32 字节 hex secret")

	// 重复注册幂等：返回既有记录，密钥不被覆盖
	firstEnc := append([]byte(nil), dev.DeviceSecretEnc...)
	dev2, created2, appErr := svc.Register(ctx, "DEV-REG-001", "")
	require.Nil(t, appErr)
	assert.False(t, created2)
	assert.Equal(t, firstEnc, dev2.DeviceSecretEnc, "幂等注册不得覆盖既有密钥")
	_ = store
}

func TestRegister_InvalidDeviceID(t *testing.T) {
	svc, _ := newTestSvc(t)
	for _, bad := range []string{"", "AB1", "含中文", "dev id"} {
		_, _, appErr := svc.Register(context.Background(), bad, "")
		require.NotNil(t, appErr, "%q", bad)
		assert.Equal(t, model.CodeInvalidParam, appErr.Code)
	}
}

// ─────────────────────────────────────────────────────────────
// 绑定 / 换绑 / 解绑
// ─────────────────────────────────────────────────────────────

func registerAndPatient(t *testing.T, svc *DeviceService, store *testutil.FakeStore, deviceID, patientID string) {
	t.Helper()
	_, _, appErr := svc.Register(context.Background(), deviceID, "")
	require.Nil(t, appErr)
	store.AddPatient(patientID)
}

func TestBind_FirstBind_StateTransition(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-B-001", "P-001")
	ctx := context.Background()

	res, appErr := svc.Bind(ctx, "DEV-B-001", "P-001", "TECH-1")
	require.Nil(t, appErr)
	assert.False(t, res.Rebound, "首绑非换绑")
	assert.Equal(t, model.StatusOffline, res.Device.Status, "unbound→offline（绑定未上报）")
	require.NotNil(t, res.Device.PatientID)
	assert.Equal(t, "P-001", *res.Device.PatientID)
	require.NotNil(t, res.Device.BindTime)

	// active binding 恰好一条（互斥）
	bindings, appErr := svc.ListBindings(ctx, "DEV-B-001")
	require.Nil(t, appErr)
	require.Len(t, bindings, 1)
	assert.Nil(t, bindings[0].UnbindAt)
	assert.Equal(t, model.ReasonInstall, *bindings[0].Reason)

	// 同患者重复绑定幂等
	res2, appErr := svc.Bind(ctx, "DEV-B-001", "P-001", "TECH-1")
	require.Nil(t, appErr)
	assert.False(t, res2.Rebound)
	bindings, _ = svc.ListBindings(ctx, "DEV-B-001")
	assert.Len(t, bindings, 1, "幂等绑定不得新增 binding 行")
}

func TestBind_Rebind_HistoryTraceable(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-B-002", "P-001")
	store.AddPatient("P-002")
	ctx := context.Background()

	_, appErr := svc.Bind(ctx, "DEV-B-002", "P-001", "TECH-1")
	require.Nil(t, appErr)

	// 换绑到 P-002（Bind 自动换绑，对齐 Ella H6 契约；旧行关闭 reason=rebind）
	res, appErr := svc.Bind(ctx, "DEV-B-002", "P-002", "TECH-2")
	require.Nil(t, appErr)
	assert.True(t, res.Rebound)
	require.NotNil(t, res.Device.PatientID)
	assert.Equal(t, "P-002", *res.Device.PatientID)

	// 历史：旧绑定 unbind_at+reason=rebind+operator；新绑定 active
	bindings, appErr := svc.ListBindings(ctx, "DEV-B-002")
	require.Nil(t, appErr)
	require.Len(t, bindings, 2)
	assert.NotNil(t, bindings[1].UnbindAt, "旧绑定必须已关闭")
	assert.Equal(t, model.ReasonRebind, *bindings[1].Reason)
	assert.Equal(t, "TECH-2", *bindings[1].OperatorID)
	assert.Nil(t, bindings[0].UnbindAt, "新绑定 active")
	assert.Equal(t, model.ReasonRebind, *bindings[0].Reason)

	// 互斥：active binding 仅一条
	active := 0
	for _, b := range bindings {
		if b.UnbindAt == nil {
			active++
		}
	}
	assert.Equal(t, 1, active, "同设备仅一个 active binding")

	// 无 active binding 时 Rebind 拒绝（应先走 Bind）
	_, appErr = svc.Unbind(ctx, "DEV-B-002", "TECH-2")
	require.Nil(t, appErr)
	_, appErr = svc.Rebind(ctx, "DEV-B-002", "P-001", "TECH-1")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeConflict, appErr.Code)
}

func TestRebind_ValidationErrors(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-R-001", "P-001")
	store.AddPatient("P-002")
	ctx := context.Background()

	// 设备不存在
	_, appErr := svc.Rebind(ctx, "DEV-NONE", "P-001", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code)

	// 患者不存在
	_, appErr = svc.Rebind(ctx, "DEV-R-001", "P-NONE", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeUserResNotFound, appErr.Code)

	// 参数缺失
	_, appErr = svc.Rebind(ctx, "", "P-001", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code)

	// 无 active binding → 409
	_, appErr = svc.Rebind(ctx, "DEV-R-001", "P-002", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeConflict, appErr.Code)

	// 换绑到同患者幂等（不视为换绑）
	_, appErr = svc.Bind(ctx, "DEV-R-001", "P-001", "")
	require.Nil(t, appErr)
	res, appErr := svc.Rebind(ctx, "DEV-R-001", "P-001", "")
	require.Nil(t, appErr)
	assert.False(t, res.Rebound, "同患者重复换绑幂等")
}

func TestBind_ValidationErrors(t *testing.T) {
	svc, store := newTestSvc(t)
	ctx := context.Background()
	registerAndPatient(t, svc, store, "DEV-B-003", "P-001")

	// 设备不存在
	_, appErr := svc.Bind(ctx, "DEV-NONE", "P-001", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code)

	// 患者不存在
	_, appErr = svc.Bind(ctx, "DEV-B-003", "P-NONE", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeUserResNotFound, appErr.Code)

	// 参数缺失
	_, appErr = svc.Bind(ctx, "", "P-001", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code)
}

func TestUnbind_AndIdempotent(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-U-001", "P-001")
	ctx := context.Background()
	_, appErr := svc.Bind(ctx, "DEV-U-001", "P-001", "TECH-1")
	require.Nil(t, appErr)

	already, appErr := svc.Unbind(ctx, "DEV-U-001", "OP-9")
	require.Nil(t, appErr)
	assert.False(t, already)

	dev, appErr := svc.GetDevice(ctx, "DEV-U-001")
	require.Nil(t, appErr)
	assert.Equal(t, model.StatusUnbound, dev.Status)
	assert.Nil(t, dev.PatientID)
	assert.Nil(t, dev.BindTime)

	// 历史行 reason=unbind + operator 完整
	bindings, _ := svc.ListBindings(ctx, "DEV-U-001")
	require.Len(t, bindings, 1)
	assert.Equal(t, model.ReasonUnbind, *bindings[0].Reason)
	assert.Equal(t, "OP-9", *bindings[0].OperatorID)
	assert.NotNil(t, bindings[0].UnbindAt)

	// 幂等：重复解绑返回成功
	already, appErr = svc.Unbind(ctx, "DEV-U-001", "OP-9")
	require.Nil(t, appErr)
	assert.True(t, already)

	// 未注册设备
	_, appErr = svc.Unbind(ctx, "DEV-NONE", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code)
}

// ─────────────────────────────────────────────────────────────
// 状态机落库：上报/补传更新 last_report_at + status（单调推进）
// ─────────────────────────────────────────────────────────────

func TestTouch_StateMachineAndMonotonic(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T-001", "P-001")
	ctx := context.Background()
	_, appErr := svc.Bind(ctx, "DEV-T-001", "P-001", "")
	require.Nil(t, appErr)

	now := time.Now()

	// 正常上报 → online + last_report_at 更新
	appErr = svc.Touch(ctx, "DEV-T-001", now, 0)
	require.Nil(t, appErr)
	dev, _ := svc.GetDevice(ctx, "DEV-T-001")
	assert.Equal(t, model.StatusOnline, dev.Status)
	require.NotNil(t, dev.LastReportAt)
	assert.True(t, dev.LastReportAt.Equal(now))

	// 故障上报 → abnormal
	appErr = svc.Touch(ctx, "DEV-T-001", now.Add(time.Minute), 5)
	require.Nil(t, appErr)
	dev, _ = svc.GetDevice(ctx, "DEV-T-001")
	assert.Equal(t, model.StatusAbnormal, dev.Status)

	// 故障解除（正常帧）→ 自动恢复 online
	appErr = svc.Touch(ctx, "DEV-T-001", now.Add(2*time.Minute), 0)
	require.Nil(t, appErr)
	dev, _ = svc.GetDevice(ctx, "DEV-T-001")
	assert.Equal(t, model.StatusOnline, dev.Status)

	// 陈旧补传帧（乱序）：不得回退 last_report_at，也不得把已恢复设备打回 abnormal
	appErr = svc.Touch(ctx, "DEV-T-001", now.Add(-time.Hour), 9)
	require.Nil(t, appErr)
	dev, _ = svc.GetDevice(ctx, "DEV-T-001")
	assert.Equal(t, model.StatusOnline, dev.Status, "陈旧故障帧不得覆盖最新状态")
	require.NotNil(t, dev.LastReportAt)
	assert.True(t, dev.LastReportAt.Equal(now.Add(2*time.Minute)), "last_report_at 单调推进不得回退")

	// 未注册设备
	appErr = svc.Touch(ctx, "DEV-NONE", now, 0)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code)
}

// ─────────────────────────────────────────────────────────────
// 安装记录 + 校准基线（技师安装流程闭环）
// ─────────────────────────────────────────────────────────────

func TestCreateInstall_Flow(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-I-001", "P-001")
	store.AddTech("TECH-1")
	ctx := context.Background()

	// 未绑定先建安装 → 拒绝（防错装）
	_, appErr := svc.CreateInstall(ctx, &CreateInstallRequest{DeviceID: "DEV-I-001", PatientID: "P-001", TechID: "TECH-1"})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeConflict, appErr.Code)

	_, appErr = svc.Bind(ctx, "DEV-I-001", "P-001", "TECH-1")
	require.Nil(t, appErr)

	rec, appErr := svc.CreateInstall(ctx, &CreateInstallRequest{DeviceID: "DEV-I-001", PatientID: "P-001", TechID: "TECH-1"})
	require.Nil(t, appErr)
	assert.Greater(t, rec.InstallID, int64(0))
	assert.Equal(t, "unconfigured", rec.WifiStatus)

	// 技师不存在
	_, appErr = svc.CreateInstall(ctx, &CreateInstallRequest{DeviceID: "DEV-I-001", PatientID: "P-001", TechID: "TECH-NONE"})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeUserResNotFound, appErr.Code)

	// 设备绑定的是 P-001，却用 P-002 建安装 → 拒绝
	store.AddPatient("P-002")
	_, appErr = svc.CreateInstall(ctx, &CreateInstallRequest{DeviceID: "DEV-I-001", PatientID: "P-002", TechID: "TECH-1"})
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeConflict, appErr.Code)
}

func TestSaveBaseline_LengthValidationAndConflict(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-I-002", "P-001")
	store.AddTech("TECH-1")
	ctx := context.Background()
	_, appErr := svc.Bind(ctx, "DEV-I-002", "P-001", "TECH-1")
	require.Nil(t, appErr)
	rec, appErr := svc.CreateInstall(ctx, &CreateInstallRequest{DeviceID: "DEV-I-002", PatientID: "P-001", TechID: "TECH-1"})
	require.Nil(t, appErr)

	// 长度 != 20 → 20400（服务层先行校验，DB CHECK 兜底）
	for _, badLen := range []int{0, 19, 21} {
		_, appErr = svc.SaveBaseline(ctx, rec.InstallID, make([]float32, badLen), "TECH-1")
		require.NotNil(t, appErr, "len=%d", badLen)
		assert.Equal(t, model.CodeInvalidParam, appErr.Code)
	}

	// 缺 calibrator
	_, appErr = svc.SaveBaseline(ctx, rec.InstallID, offsets20(), "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code)

	// 合法 20 点落库
	baselineID, appErr := svc.SaveBaseline(ctx, rec.InstallID, offsets20(), "TECH-1")
	require.Nil(t, appErr)
	assert.Greater(t, baselineID, int64(0))

	// install 回填 baseline_id（1:1）
	stored, err := store.GetInstall(ctx, rec.InstallID)
	require.NoError(t, err)
	require.NotNil(t, stored.BaselineID)
	assert.Equal(t, baselineID, *stored.BaselineID)

	// 一次安装至多一条基线 → 409
	_, appErr = svc.SaveBaseline(ctx, rec.InstallID, offsets20(), "TECH-1")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeConflict, appErr.Code)

	// install 不存在 → 20404
	_, appErr = svc.SaveBaseline(ctx, 999999, offsets20(), "TECH-1")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code)
}

// ─────────────────────────────────────────────────────────────
// 安装记录元数据回填
// ─────────────────────────────────────────────────────────────

func TestUpdateInstallMeta(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-I-003", "P-001")
	store.AddTech("TECH-1")
	ctx := context.Background()
	_, appErr := svc.Bind(ctx, "DEV-I-003", "P-001", "TECH-1")
	require.Nil(t, appErr)
	rec, appErr := svc.CreateInstall(ctx, &CreateInstallRequest{DeviceID: "DEV-I-003", PatientID: "P-001", TechID: "TECH-1"})
	require.Nil(t, appErr)

	notes := "备注"
	appErr = svc.UpdateInstallMeta(ctx, rec.InstallID, &notes, nil)
	require.Nil(t, appErr)

	// nil 参数无副作用成功
	appErr = svc.UpdateInstallMeta(ctx, rec.InstallID, nil, nil)
	require.Nil(t, appErr)

	// install 不存在 → 20404
	appErr = svc.UpdateInstallMeta(ctx, 999999, &notes, nil)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code)
}

// ─────────────────────────────────────────────────────────────
// WiFi 配置状态
// ─────────────────────────────────────────────────────────────

func TestSetWifiSSID(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-W-001", "P-001")
	ctx := context.Background()

	appErr := svc.SetWifiSSID(ctx, "DEV-W-001", "BraceHome-5G")
	require.Nil(t, appErr)
	dev, _ := svc.GetDevice(ctx, "DEV-W-001")
	require.NotNil(t, dev.WifiSSID)
	assert.Equal(t, "BraceHome-5G", *dev.WifiSSID)

	appErr = svc.SetWifiSSID(ctx, "DEV-W-001", "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code)

	appErr = svc.SetWifiSSID(ctx, "DEV-NONE", "ssid")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code)
}
