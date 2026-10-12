//go:build integration
// +build integration

// T669 —— 配置下发通道端到端读数（真实 PG15 + Redis7，testcontainers）
//
// 写方在 user-service（UpsertConfigsBumpDeviceVersion 同事务递增版本），读方在本服务：
// 这里直接把 sys_configs 置成「后台刚把间隔改成 15 分钟、版本推进到 2」的提交后状态，
// 模拟设备单帧/批量上报，断言响应捎带的 config 与库内一致 —— 两服务以 sys_configs 为契约面，
// 各自的写/读 SQL 分别在两个服务的集成测试里锁死。
//
// data-service ConfigRepo 带 60s 内存缓存，故每个断言点都用 newITSvc 取冷实例，
// 不依赖「同实例 60s 内恰好过期」的时序。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// t669SetConfig 直接落库模拟后台保存结果；返回清理函数恢复 seed 缺省（30/1）。
func t669SetConfig(t *testing.T, intervalMinutes, version string) func() {
	t.Helper()
	ctx := context.Background()
	_, err := itPool.Exec(ctx,
		`INSERT INTO sys_configs (config_key, config_value) VALUES
		  ('collect_interval_minutes', $1), ('device_config_version', $2)
		 ON CONFLICT (config_key) DO UPDATE SET config_value = EXCLUDED.config_value`,
		intervalMinutes, version)
	require.NoError(t, err)
	return func() {
		_, rErr := itPool.Exec(ctx,
			`INSERT INTO sys_configs (config_key, config_value) VALUES
			  ('collect_interval_minutes', '30'), ('device_config_version', '1')
			 ON CONFLICT (config_key) DO UPDATE SET config_value = EXCLUDED.config_value`)
		require.NoError(t, rErr)
	}
}

func TestIT_T669_ReportCarriesNewIntervalAndVersion(t *testing.T) {
	ctx := context.Background()
	restore := t669SetConfig(t, "15", "2")
	defer restore()

	t.Run("单帧上报捎带15分钟/版本2", func(t *testing.T) {
		svc := newITSvc(t, nil) // 冷 ConfigRepo，不读 60s 缓存
		require.NoError(t, itRedis.FlushDB(ctx).Err())
		truncateRecords(ctx)

		ts := time.Unix(time.Now().Add(-3*time.Minute).Unix(), 0)
		req := &model.SingleFrameRequest{DeviceID: itDevice, Timestamp: ts.Unix(), Points: itPoints(32.5), Battery: 90, Firmware: "v1.2.0"}
		resp, appErr := svc.UploadSingle(ctx, itDevice, req)
		require.Nil(t, appErr)
		assert.Equal(t, 15, resp.Config.IntervalMinutes, "单帧响应必须捎带新间隔")
		assert.Equal(t, 2, resp.Config.ConfigVersion, "单帧响应必须捎带递增后的版本（固件靠它应用配置）")
	})

	t.Run("批量补传捎带15分钟/版本2", func(t *testing.T) {
		svc := newITSvc(t, nil)
		require.NoError(t, itRedis.FlushDB(ctx).Err())
		truncateRecords(ctx)

		now := time.Now()
		frames := []model.BatchFrame{
			{Timestamp: now.Add(-20 * time.Minute).Unix(), Points: itPoints(31.0), Battery: 80},
			{Timestamp: now.Add(-10 * time.Minute).Unix(), Points: itPoints(33.0), Battery: 80},
		}
		resp, appErr := svc.UploadBatch(ctx, itDevice, &model.BatchRequest{DeviceID: itDevice, Frames: frames})
		require.Nil(t, appErr)
		assert.Equal(t, 2, resp.Accepted)
		assert.Equal(t, 15, resp.Config.IntervalMinutes, "批量响应必须捎带新间隔")
		assert.Equal(t, 2, resp.Config.ConfigVersion, "批量响应必须捎带递增后的版本")
	})
}

func TestIT_T669_MissingConfigRowsFallbackDefaults(t *testing.T) {
	ctx := context.Background()
	// 两键都不存在（未初始化的库）：冷 repo 必须回退 30/1，上报不因此失败。
	_, err := itPool.Exec(ctx,
		`DELETE FROM sys_configs WHERE config_key IN ('collect_interval_minutes','device_config_version')`)
	require.NoError(t, err)
	defer t669SetConfig(t, "30", "1")()

	svc := newITSvc(t, nil)
	require.NoError(t, itRedis.FlushDB(ctx).Err())
	truncateRecords(ctx)

	ts := time.Unix(time.Now().Add(-2*time.Minute).Unix(), 0)
	resp, appErr := svc.UploadSingle(ctx, itDevice,
		&model.SingleFrameRequest{DeviceID: itDevice, Timestamp: ts.Unix(), Points: itPoints(32.5), Battery: 90, Firmware: "v1.2.0"})
	require.Nil(t, appErr)
	assert.Equal(t, 30, resp.Config.IntervalMinutes, "缺行回退缺省间隔 30")
	assert.Equal(t, 1, resp.Config.ConfigVersion, "缺行回退缺省版本 1（record.go 读失败路径另有单测）")
}
