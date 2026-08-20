// Package service — GetDeviceSecret 实现侧测试（T032；不与 Ella device_test.go 重叠）
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

func newSecretSvc(t *testing.T) *DeviceService {
	t.Helper()
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	return NewDeviceService(testutil.NewFakeStore(), enc)
}

func TestGetDeviceSecret_RoundTrip(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()

	_, _, appErr := svc.Register(ctx, "DEV-SVC-SEC", "")
	require.Nil(t, appErr)

	secret, appErr := svc.GetDeviceSecret(ctx, "DEV-SVC-SEC")
	require.Nil(t, appErr)
	assert.Len(t, secret, 64, "hex(secret) 64 字符")

	// 幂等注册后密钥不变
	_, _, appErr = svc.Register(ctx, "DEV-SVC-SEC", "")
	require.Nil(t, appErr)
	again, appErr := svc.GetDeviceSecret(ctx, "DEV-SVC-SEC")
	require.Nil(t, appErr)
	assert.Equal(t, secret, again)
}

func TestGetDeviceSecret_Unknown_20404(t *testing.T) {
	svc := newSecretSvc(t)
	_, appErr := svc.GetDeviceSecret(context.Background(), "DEV-GHOST")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code)
}

func TestGetDeviceSecret_EmptyID_20400(t *testing.T) {
	svc := newSecretSvc(t)
	_, appErr := svc.GetDeviceSecret(context.Background(), "")
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code)
}
