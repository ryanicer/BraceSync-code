//go:build integration
// +build integration

// T715 的 SQL 面：注册 INSERT 真的把 contact_area_cm2 写进库，且 DO NOTHING 让幂等注册不动既有值。
// 这两条 handler 层的 FakeStore 用例证不到（假仓储不走 SQL），只有真库能判。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

func TestT715IT_RegisterPersistsDefaultArea(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	const dev = "DEV-T715-IT-001"

	created, err := store.RegisterDevice(ctx, &model.Device{
		DeviceID: dev, Model: model.DefaultModel,
		DeviceSecretEnc: itSecretEnc(), SecretVersion: 1, Status: model.StatusUnbound,
		ContactAreaCm2: model.DefaultContactArea(),
	})
	require.NoError(t, err)
	require.True(t, created, "首次注册应新建行")

	got, err := store.GetDevice(ctx, dev)
	require.NoError(t, err)
	require.NotNil(t, got.ContactAreaCm2, "判据一：真库里的注册行面积非空")
	// 精确相等而非近似：000034 选 DOUBLE PRECISION 就是为了 0.64 原样往返（REAL 会现形 0.63999998…）
	assert.Equal(t, model.DefaultContactAreaCm2, *got.ContactAreaCm2, "面积回读必须逐位等于 0.64")

	// 幂等再注册：这一支故意把面积留成 nil，既有 0.64 不许被洗成 NULL、密钥不许被换
	_, err = store.RegisterDevice(ctx, &model.Device{
		DeviceID: dev, Model: model.DefaultModel,
		DeviceSecretEnc: []byte{0xff, 0xff}, SecretVersion: 1, Status: model.StatusUnbound,
	})
	require.NoError(t, err)

	again, err := store.GetDevice(ctx, dev)
	require.NoError(t, err)
	require.NotNil(t, again.ContactAreaCm2, "判据二：幂等注册把库里的面积洗成了 NULL")
	assert.Equal(t, model.DefaultContactAreaCm2, *again.ContactAreaCm2, "判据二：幂等注册不得覆盖已配面积")
	assert.Equal(t, itSecretEnc(), again.DeviceSecretEnc, "判据二：幂等注册不得覆盖既有密钥")
}
