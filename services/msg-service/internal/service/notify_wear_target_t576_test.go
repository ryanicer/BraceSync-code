// Package service — T576 佩戴目标同源化：真源 sys_configs.wear_target_hours + 读不到时的降级口径
//
// 判据（派发单第五节第 3 条）：读不到配置走降级分支、降级值 = 22、上报判定不出现空值/0。
// 负对照按「降级值若是 0 会怎样」那一形注入（target=0 ⇒ 任何佩戴量都算达标 ⇒ 一条提醒都不发）。
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/repo"
)

func TestT576SyncWearTargetUsesSysConfigsAsSource(t *testing.T) {
	f := newImplFixture(t)
	f.store.SeedSysConfig(WearTargetConfigKey, "20")

	hours, source := f.svc.SyncWearTarget(context.Background(), "")
	assert.Equal(t, 20, hours, "真源是 sys_configs 那一列")
	assert.Equal(t, WearTargetFromConfig, source)
	assert.Equal(t, 20*60, f.svc.wearTargetMinutes)
}

func TestT576SyncWearTargetDegradation(t *testing.T) {
	cases := []struct {
		name       string
		seedValue  *string // nil = 不预置该键（走 ErrConfigMissing）
		seedErr    error   // 非 nil = 注入查询失败
		envRaw     string
		wantHours  int
		wantSource WearTargetSource
	}{
		{name: "键缺失且环境变量没配", seedValue: nil, envRaw: "", wantHours: 22, wantSource: WearTargetFromDefault},
		{name: "键缺失但环境变量有效", seedValue: nil, envRaw: "19", wantHours: 19, wantSource: WearTargetFromEnv},
		{name: "查询失败走降级", seedValue: nil, seedErr: errors.New("dial tcp: connection refused"), envRaw: "", wantHours: 22, wantSource: WearTargetFromDefault},
		{name: "值非数字", seedValue: strPtr("abc"), envRaw: "", wantHours: 22, wantSource: WearTargetFromDefault},
		{name: "值为空串", seedValue: strPtr(""), envRaw: "", wantHours: 22, wantSource: WearTargetFromDefault},
		{name: "值为 0 不许落到 0", seedValue: strPtr("0"), envRaw: "", wantHours: 22, wantSource: WearTargetFromDefault},
		{name: "值为负数", seedValue: strPtr("-3"), envRaw: "18", wantHours: 18, wantSource: WearTargetFromEnv},
		{name: "值超上限 24", seedValue: strPtr("25"), envRaw: "", wantHours: 22, wantSource: WearTargetFromDefault},
		{name: "值两侧带空格仍算有效", seedValue: strPtr("  21  "), envRaw: "", wantHours: 21, wantSource: WearTargetFromConfig},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newImplFixture(t)
			switch {
			case c.seedErr != nil:
				f.store.SeedSysConfigError(WearTargetConfigKey, c.seedErr)
			case c.seedValue != nil:
				f.store.SeedSysConfig(WearTargetConfigKey, *c.seedValue)
			}

			hours, source := f.svc.SyncWearTarget(context.Background(), c.envRaw)
			assert.Equal(t, c.wantHours, hours)
			assert.Equal(t, c.wantSource, source)
			assert.Equal(t, c.wantHours*60, f.svc.wearTargetMinutes)
			assert.Positive(t, f.svc.wearTargetMinutes, "降级不许把目标写成 0 或空")
		})
	}
}

// TestT576DegradedTargetStillDrivesReminder 降级后功能仍可用：目标降到 22 时，未达标患者照常推送。
// 这一格同时是否证「降级值 = 0」那一形：target=0 时 0 >= 0 成立，所有患者都被判达标，一条都发不出去。
func TestT576DegradedTargetStillDrivesReminder(t *testing.T) {
	f := newImplFixture(t) // now = 2026-08-10 20:05（业务时区）
	rt := "20:00"
	_, err := f.store.UpdateWearReminder(context.Background(), "P20260001", true, &rt)
	require.NoError(t, err)

	hours, source := f.svc.SyncWearTarget(context.Background(), "")
	require.Equal(t, 22, hours)
	require.Equal(t, WearTargetFromDefault, source)

	f.store.SeedWearMinutes("P20260001", "2026-08-10", 21*60+59) // 差一分钟未达标
	pushed, err := f.svc.ScanReminders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, pushed, "降级到 22 之后判定链路仍然在跑")

	f.store.SeedWearMinutes("P20260001", "2026-08-10", 22*60) // 正好达标
	pushed, err = f.svc.ScanReminders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, pushed, "22h 是判定用的那颗值，不是装饰")
}

// TestT576MissingKeyIsSentinelNotFailure 钉住「读不到」这一档在假件与真库同形：
// 键缺失必须回 repo.ErrConfigMissing（服务层据此走降级），而不是被当成查询失败。
func TestT576MissingKeyIsSentinelNotFailure(t *testing.T) {
	f := newImplFixture(t)

	_, err := f.store.GetSysConfigValue(context.Background(), WearTargetConfigKey)
	require.Error(t, err)
	assert.True(t, errors.Is(err, repo.ErrConfigMissing), "键缺失那一档要与 PGStore 同形")

	f.store.SeedSysConfig(WearTargetConfigKey, "22")
	v, err := f.store.GetSysConfigValue(context.Background(), WearTargetConfigKey)
	require.NoError(t, err)
	assert.Equal(t, "22", v)
}

func strPtr(s string) *string { return &s }
