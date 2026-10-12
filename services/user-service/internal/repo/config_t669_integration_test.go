//go:build integration
// +build integration

// T669 —— device_config_version 递增通道（真实 PG15，testcontainers）
//
// 守三件 handler 单测够不到的事：
//  1. 间隔键与版本键在同一事务里落库（要么都生效要么都不生效）；
//  2. 版本递增是 SQL 原子语义：缺键首改落 2、连续改 1→2→3、并发两次不丢递增；
//  3. 脏值（非数字）落 2 而不是报错或写坏。
package repo

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func t669Config(ctx context.Context, t *testing.T) (interval, version string) {
	t.Helper()
	got, err := itStore.GetConfigs(ctx, []string{"collect_interval_minutes", DeviceConfigVersionKey})
	require.NoError(t, err)
	return got["collect_interval_minutes"], got[DeviceConfigVersionKey]
}

func TestIT_T669_DeviceConfigVersionBump(t *testing.T) {
	ctx := context.Background()

	// 起点确定：删掉两键，模拟「从未通过后台保存过采集间隔」的库。
	_, err := itStore.pool.Exec(ctx,
		`DELETE FROM sys_configs WHERE config_key IN ('collect_interval_minutes', $1)`,
		DeviceConfigVersionKey)
	require.NoError(t, err)

	// 缺键首改：间隔 30→15，版本直接落 2（出厂缺省=1，首改必须与缺省不同）。
	require.NoError(t, itStore.UpsertConfigsBumpDeviceVersion(ctx,
		[]ConfigKV{{Key: "collect_interval_minutes", Value: "15"}}, "T669-IT"))
	interval, version := t669Config(ctx, t)
	assert.Equal(t, "15", interval, "间隔键必须与版本键同事务落库")
	assert.Equal(t, "2", version, "缺键首改版本应落 2")

	// 再改一次：2→3。
	require.NoError(t, itStore.UpsertConfigsBumpDeviceVersion(ctx,
		[]ConfigKV{{Key: "collect_interval_minutes", Value: "60"}}, "T669-IT"))
	interval, version = t669Config(ctx, t)
	assert.Equal(t, "60", interval)
	assert.Equal(t, "3", version)

	// 普通 UPSERT 不碰版本：版本停在 3。
	require.NoError(t, itStore.UpsertConfigs(ctx,
		[]ConfigKV{{Key: "collect_interval_minutes", Value: "60"}, {Key: "it_t669_other", Value: "x"}},
		"T669-IT"))
	_, version = t669Config(ctx, t)
	assert.Equal(t, "3", version, "普通保存不得偷改设备配置版本")

	// 脏值（非数字）后再改：重置为 2，不让递增通道卡死或产出非法版本。
	_, err = itStore.pool.Exec(ctx,
		`UPDATE sys_configs SET config_value='not-a-number' WHERE config_key=$1`, DeviceConfigVersionKey)
	require.NoError(t, err)
	require.NoError(t, itStore.UpsertConfigsBumpDeviceVersion(ctx,
		[]ConfigKV{{Key: "collect_interval_minutes", Value: "45"}}, "T669-IT"))
	_, version = t669Config(ctx, t)
	assert.Equal(t, "2", version, "脏值必须被收口为 2（与出厂缺省 1 仍不同）")

	// 收尾还原：清掉本用例造的临时键，版本留 2（后续并发用例会自行置位）。
	_, err = itStore.pool.Exec(ctx, `DELETE FROM sys_configs WHERE config_key='it_t669_other'`)
	require.NoError(t, err)
}

func TestIT_T669_DeviceConfigVersionConcurrentBumps(t *testing.T) {
	ctx := context.Background()

	// 置位确定起点，不依赖用例执行顺序。
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO sys_configs (config_key, config_value) VALUES ($1, '10')
		 ON CONFLICT (config_key) DO UPDATE SET config_value='10'`, DeviceConfigVersionKey)
	require.NoError(t, err)

	current := func() int {
		got, gErr := itStore.GetConfigs(ctx, []string{DeviceConfigVersionKey})
		require.NoError(t, gErr)
		n, pErr := strconv.Atoi(got[DeviceConfigVersionKey])
		require.NoError(t, pErr)
		return n
	}
	require.Equal(t, 10, current())

	// 两笔并发保存都必须各 +1：真实场景就是两位管理员同时改配置，
	// READ COMMITTED 下第二笔 UPDATE 会在第一笔提交后按新行值重算，不得丢递增。
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- itStore.UpsertConfigsBumpDeviceVersion(ctx,
				[]ConfigKV{{Key: "collect_interval_minutes", Value: "20"}}, "T669-IT-CONC")
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	assert.Equal(t, 12, current(), "两次并发保存必须各 +1，不得丢递增")
}
