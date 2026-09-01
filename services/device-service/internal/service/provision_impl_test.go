// Package service — GetProvisionKey 实现侧测试（T067；复用 newSecretSvc helper）
package service

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

func TestGetProvisionKey_Returns32Hex(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()

	_, _, appErr := svc.Register(ctx, "DEV-PROV-1", "")
	require.Nil(t, appErr)

	key, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-1")
	require.Nil(t, appErr)
	assert.Len(t, key, 32, "provision key 须 32 hex 字符（16B）")
	_, err := hex.DecodeString(key)
	assert.NoError(t, err, "provision key 须合法 hex")
}

func TestGetProvisionKey_Deterministic(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()
	_, _, _ = svc.Register(ctx, "DEV-PROV-2", "")

	k1, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-2")
	require.Nil(t, appErr)
	k2, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-2")
	require.Nil(t, appErr)
	assert.Equal(t, k1, k2, "相同 device 应确定性派生")
}

func TestGetProvisionKey_DifferentDevices(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()
	_, _, _ = svc.Register(ctx, "DEV-PROV-A", "")
	_, _, _ = svc.Register(ctx, "DEV-PROV-B", "")

	kA, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-A")
	require.Nil(t, appErr)
	kB, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-B")
	require.Nil(t, appErr)
	assert.NotEqual(t, kA, kB, "不同 device 应派生不同密钥")
}

func TestGetProvisionKey_Unknown_20404(t *testing.T) {
	svc := newSecretSvc(t)
	_, appErr := svc.GetProvisionKey(context.Background(), "DEV-GHOST-PROV")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code, "未注册设备 → 20404")
}

func TestGetProvisionKey_EmptyID_20400(t *testing.T) {
	svc := newSecretSvc(t)
	_, appErr := svc.GetProvisionKey(context.Background(), "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code, "空 device_id → 20400")
}
