// Package service — GetProvisionKey 实现侧测试（T067；T091 增加重发间隔）
package service

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

const testOperator = "TECH-001"

func TestGetProvisionKey_Returns32Hex(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()

	_, _, appErr := svc.Register(ctx, "DEV-PROV-1", "")
	require.Nil(t, appErr)

	key, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-1", testOperator)
	require.Nil(t, appErr)
	assert.Len(t, key, 32, "provision key 须 32 hex 字符（16B）")
	_, err := hex.DecodeString(key)
	assert.NoError(t, err, "provision key 须合法 hex")
}

func TestGetProvisionKey_Deterministic(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()
	_, _, _ = svc.Register(ctx, "DEV-PROV-2", "")
	// 关闭重发间隔以验证确定性（同设备多次调用返回相同密钥）
	svc.provisionInterval = 0

	k1, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-2", testOperator)
	require.Nil(t, appErr)
	k2, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-2", testOperator)
	require.Nil(t, appErr)
	assert.Equal(t, k1, k2, "相同 device 应确定性派生")
}

func TestGetProvisionKey_DifferentDevices(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()
	_, _, _ = svc.Register(ctx, "DEV-PROV-A", "")
	_, _, _ = svc.Register(ctx, "DEV-PROV-B", "")

	kA, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-A", testOperator)
	require.Nil(t, appErr)
	kB, appErr := svc.GetProvisionKey(ctx, "DEV-PROV-B", testOperator)
	require.Nil(t, appErr)
	assert.NotEqual(t, kA, kB, "不同 device 应派生不同密钥")
}

func TestGetProvisionKey_Unknown_20404(t *testing.T) {
	svc := newSecretSvc(t)
	_, appErr := svc.GetProvisionKey(context.Background(), "DEV-GHOST-PROV", testOperator)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code, "未注册设备 → 20404")
}

func TestGetProvisionKey_EmptyID_20400(t *testing.T) {
	svc := newSecretSvc(t)
	_, appErr := svc.GetProvisionKey(context.Background(), "", testOperator)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInvalidParam, appErr.Code, "空 device_id → 20400")
}

// TestGetProvisionKey_ReissueWithinInterval_20429 T091：同设备在重发间隔内重复领卡 → 20429。
// 注入可控时钟 + 显式间隔，避免依赖真实时间。
func TestGetProvisionKey_ReissueWithinInterval_20429(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()
	_, _, appErr := svc.Register(ctx, "DEV-REISSUE", "")
	require.Nil(t, appErr)

	// 注入可控时钟与间隔
	svc.provisionInterval = time.Minute
	var cur time.Time
	svc.now = func() time.Time { return cur }

	// 首次领卡成功
	cur = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	_, appErr = svc.GetProvisionKey(ctx, "DEV-REISSUE", testOperator)
	require.Nil(t, appErr, "首次领卡应成功")

	// 间隔内重复领卡 → 20429
	cur = cur.Add(30 * time.Second)
	_, appErr = svc.GetProvisionKey(ctx, "DEV-REISSUE", testOperator)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeTooMany, appErr.Code, "重发间隔内应返回 20429")
	assert.Equal(t, 429, appErr.HTTPStatus)

	// 间隔过后 → 成功
	cur = cur.Add(31 * time.Second) // 总计 61s > 60s
	_, appErr = svc.GetProvisionKey(ctx, "DEV-REISSUE", testOperator)
	require.Nil(t, appErr, "间隔过后应允许重发")
}

// TestGetProvisionKey_ReissueIntervalIndependentPerDevice T091：重发间隔按 device 维度独立，
// 设备 A 的领卡不影响设备 B。
func TestGetProvisionKey_ReissueIntervalIndependentPerDevice(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()
	_, _, _ = svc.Register(ctx, "DEV-A", "")
	_, _, _ = svc.Register(ctx, "DEV-B", "")

	svc.provisionInterval = time.Minute
	var cur time.Time
	svc.now = func() time.Time { return cur }
	cur = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	_, appErr := svc.GetProvisionKey(ctx, "DEV-A", testOperator)
	require.Nil(t, appErr)
	// 同一时刻领设备 B 不受 A 的间隔影响
	_, appErr = svc.GetProvisionKey(ctx, "DEV-B", testOperator)
	require.Nil(t, appErr, "不同设备的领卡互不影响")
}

// TestGetProvisionKey_UnknownDeviceDoesNotConsumeInterval T091：未注册设备的失败请求
// 不应占用重发间隔窗口（注册后立即可领）。
func TestGetProvisionKey_UnknownDeviceDoesNotConsumeInterval(t *testing.T) {
	svc := newSecretSvc(t)
	ctx := context.Background()

	svc.provisionInterval = time.Minute
	var cur time.Time
	svc.now = func() time.Time { return cur }
	cur = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	// 未注册设备领卡失败
	_, appErr := svc.GetProvisionKey(ctx, "DEV-LATE", testOperator)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeNotFound, appErr.Code)

	// 同一时刻注册后再领应成功（失败请求未占用间隔窗口）
	_, _, _ = svc.Register(ctx, "DEV-LATE", "")
	_, appErr = svc.GetProvisionKey(ctx, "DEV-LATE", testOperator)
	require.Nil(t, appErr, "未注册失败不应占用重发间隔")
}
