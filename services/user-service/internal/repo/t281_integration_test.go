//go:build integration
// +build integration

// T281 迁移 000021 的真库验证：threshold_pressure_low 补齐 T203 的 ÷10 漏改（10 → 1）。
//
// 守的不只是「值等于 1」，而是 user-service validateSettings 的不变量 high > low ——
// 两键一倒挂 PUT /api/v1/admin/settings 就恒 400，系统配置页保存不了（PM 2026-09-21 staging 实测）。
//
// 前置状态由本用例自己写死：同包 TestITT252AlertPointRulesRoundTrip 会把这两键改成
// high=45 / low=10 留在库里，而它的文件名排序在本文件之前 ⇒ 直接读库测到的是它的尾巴。
// 这里先造出「000016 播完 + 000019 跑完、000021 还没跑」的现场，再执行真实迁移文件：
// 只跑 SQL 文件、不复制其中的语句，测的就是交付物本身。
package repo

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t281PressureLow = "threshold_pressure_low"
	t281UpFile      = "000021_t281_pressure_low_div10_winner.up.sql"
	t281DownFile    = "000021_t281_pressure_low_div10_winner.down.sql"
	// t281LegacyDesc 000016:31 播的原描述，用于造修复前现场并核对 up/down 对称
	t281LegacyDesc = "统一压力下限(N)（T252 2.2 告警页 Tab2，设计稿示意值 10）"
)

// t281Siblings 同批（000016）播的其它三键：本迁移只准动下限，这三行必须原样不动
var t281Siblings = []string{"device_offline_minutes", "continuous_wear_max_hours", "report_timeout_minutes"}

func runMigrationFile(t *testing.T, name string) {
	t.Helper()
	sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir(), name))
	require.NoError(t, err, "迁移文件 %s 应存在", name)
	_, execErr := itStore.pool.Exec(context.Background(), string(sqlBytes))
	require.NoError(t, execErr, "执行 %s 失败", name)
}

func setSysConfigIT(t *testing.T, key, value, description string) {
	t.Helper()
	// UPSERT 而非 UPDATE：threshold_pressure_high 这一行**没有任何迁移播它**，只由 seed.sql 建
	// （000019 对它的 UPDATE 在只跑迁移不跑 seed 的库里影响 0 行）⇒ CI 集成库里可能压根没有这行。
	_, err := itStore.pool.Exec(context.Background(),
		`INSERT INTO sys_configs (config_key, config_value, description) VALUES ($1, $2, $3)
		 ON CONFLICT (config_key) DO UPDATE SET config_value = EXCLUDED.config_value,
		                                        description  = EXCLUDED.description`,
		key, value, description)
	require.NoError(t, err)
}

func sysConfigDesc(t *testing.T, key string) string {
	t.Helper()
	v, err := queryString(context.Background(),
		`SELECT COALESCE(description, '<NULL>') FROM sys_configs WHERE config_key = $1`, key)
	require.NoError(t, err)
	return v
}

func readThresholdN(t *testing.T, key string) float64 {
	t.Helper()
	raw, ok := sysConfigValue(t, key)
	require.True(t, ok, "键 %s 不存在", key)
	v, err := strconv.ParseFloat(raw, 64)
	require.NoError(t, err, "%s 的值 %q 不是数字", key, raw)
	return v
}

func snapshotSiblings(t *testing.T) map[string]string {
	t.Helper()
	out := make(map[string]string, len(t281Siblings))
	for _, key := range t281Siblings {
		v, ok := sysConfigValue(t, key)
		require.True(t, ok, "000016 应播 %s", key)
		out[key] = v + "|" + sysConfigDesc(t, key)
	}
	return out
}

func TestITT281_PressureLowDiv10RestoresInvariant(t *testing.T) {
	// 前置会写 high=5，收尾还原成用例进来时的状态，不给后续用例留中间态：
	// 集成库（只跑迁移）里 high 可能压根没有该行 ⇒ 是我建的就删掉，本来有就还原值。
	// 下限键不用还原：用例最后一步是重跑 up，库里留的就是修复后的正确值。
	// t.Cleanup 里禁使用 require（FailNow 只能用在测试协程内）。
	highBefore, highExisted := sysConfigValue(t, "threshold_pressure_high")
	t.Cleanup(func() {
		var err error
		if highExisted {
			_, err = itStore.pool.Exec(context.Background(),
				`UPDATE sys_configs SET config_value = $1 WHERE config_key = 'threshold_pressure_high'`, highBefore)
		} else {
			_, err = itStore.pool.Exec(context.Background(),
				`DELETE FROM sys_configs WHERE config_key = 'threshold_pressure_high'`)
		}
		if err != nil {
			t.Errorf("还原 threshold_pressure_high 失败: %v", err)
		}
	})

	// ── 现场：修复前 high=5（seed / 000019）/ low=10（000016）⇒ 倒挂 ──
	setSysConfigIT(t, "threshold_pressure_high", "5", "压力偏高阈值(N)")
	setSysConfigIT(t, t281PressureLow, "10", t281LegacyDesc)
	siblings := snapshotSiblings(t)
	assert.Greater(t, readThresholdN(t, t281PressureLow), readThresholdN(t, "threshold_pressure_high"),
		"修复前应是 low > high，本卡复现的正是这个现场")

	// ── up：按 T203 同口径 ÷10，10 → 1，不变量恢复 ──
	runMigrationFile(t, t281UpFile)
	got, ok := sysConfigValue(t, t281PressureLow)
	require.True(t, ok)
	assert.Equal(t, "1", got, "000021 up 后 threshold_pressure_low 应为 1")
	assert.Less(t, readThresholdN(t, t281PressureLow), readThresholdN(t, "threshold_pressure_high"),
		"PUT /admin/settings 的放行条件：low 必须小于 high")
	assert.NotEqual(t, t281LegacyDesc, sysConfigDesc(t, t281PressureLow),
		"up 要把「设计稿示意值 10」的描述一起改掉，否则库里留着误导口径")
	assert.Equal(t, siblings, snapshotSiblings(t), "000021 只准动下限一行的值")

	// ── down：回到 000016 播的旧量纲值，描述一并还原（up/down 对称）──
	runMigrationFile(t, t281DownFile)
	got, _ = sysConfigValue(t, t281PressureLow)
	assert.Equal(t, "10", got, "000021 down 应回到修复前的值")
	assert.Equal(t, t281LegacyDesc, sysConfigDesc(t, t281PressureLow), "down 须把 up 改的描述还原")
	assert.Equal(t, siblings, snapshotSiblings(t), "down 同样只准动下限一行")

	// ── 立刻重跑 up：库留在修复后的状态收尾 ──
	runMigrationFile(t, t281UpFile)
	got, _ = sysConfigValue(t, t281PressureLow)
	assert.Equal(t, "1", got)
}
