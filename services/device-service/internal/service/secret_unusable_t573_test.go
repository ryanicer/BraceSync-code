// Package service — T573：密钥材料不可用那一支必须带上机器可读的 reason。
//
// 夹具用的是 seed 里那批设备的真实密文形状（scripts/db/seed/seed.sql 把 device_secret_enc
// 写成 1 字节占位），短于 AES-GCM 的 12 字节 nonce，永远解不出来 —— 这是按设备、永久、
// 复跑不变的数据状态，不是 device-service 不可用。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/testutil"
)

func TestGetDeviceSecret_T573_SeedPlaceholder_CarriesReason(t *testing.T) {
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	store := testutil.NewFakeStore()
	svc := NewDeviceService(store, enc)
	ctx := context.Background()

	_, err = store.RegisterDevice(ctx, &model.Device{
		DeviceID:        "PRS-ML05-RC-20260701003",
		DeviceSecretEnc: []byte{0x00}, // seed 占位形状：1 字节 < 12 字节 nonce
	})
	require.NoError(t, err)

	_, appErr := svc.GetDeviceSecret(ctx, "PRS-ML05-RC-20260701003")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code, "码表不动：仍按系统错误对外")

	data, ok := appErr.Data.(map[string]any)
	require.True(t, ok, "拒绝支必须带结构化附带数据，实际 %T", appErr.Data)
	assert.Equal(t, model.ReasonDeviceSecretUnusable, data[model.ReasonKey],
		"网关靠这一格把它从 502 分出去；键名或值写歪，跨服务边界就退回老形态")
}

func TestGetDeviceSecret_T573_RegisteredDevice_HasNoReason(t *testing.T) {
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	svc := NewDeviceService(testutil.NewFakeStore(), enc)
	ctx := context.Background()

	_, _, appErr := svc.Register(ctx, "DEV-T573-OK", "")
	require.Nil(t, appErr)

	secret, appErr := svc.GetDeviceSecret(ctx, "DEV-T573-OK")
	require.Nil(t, appErr)
	assert.Len(t, secret, 64, "正常设备不受这次分叉影响")
}
