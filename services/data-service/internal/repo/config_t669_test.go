// T669 —— applyConfigValue 解析口径（data-service 侧零代码改动，单测把读契约定死，
// 防止 user-service 写入侧放开后，这里把非正整数/脏值误当成合法配置下发）。
package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

func TestT669_ApplyConfigValue(t *testing.T) {
	t.Run("正整数生效", func(t *testing.T) {
		snap := configSnapshot{interval: DefaultIntervalMinutes, version: defaultConfigVersion, th: model.DefaultPressureThresholds()}
		applyConfigValue("collect_interval_minutes", "15", &snap)
		applyConfigValue("device_config_version", "7", &snap)
		assert.Equal(t, 15, snap.interval)
		assert.Equal(t, 7, snap.version)
	})

	t.Run("非法值保持缺省不覆盖", func(t *testing.T) {
		for _, bad := range []string{"", "x", "0", "-3", "15.0"} {
			snap := configSnapshot{interval: DefaultIntervalMinutes, version: defaultConfigVersion, th: model.DefaultPressureThresholds()}
			applyConfigValue("collect_interval_minutes", bad, &snap)
			assert.Equal(t, DefaultIntervalMinutes, snap.interval, "interval 脏值 %q 必须保持缺省 30", bad)
		}
		for _, bad := range []string{"", "x", "0", "-1", "2.0"} {
			snap := configSnapshot{interval: DefaultIntervalMinutes, version: defaultConfigVersion, th: model.DefaultPressureThresholds()}
			applyConfigValue("device_config_version", bad, &snap)
			assert.Equal(t, defaultConfigVersion, snap.version, "version 脏值 %q 必须保持缺省 1", bad)
		}
	})

	t.Run("未知键不改动快照", func(t *testing.T) {
		snap := configSnapshot{interval: 12, version: 3, th: model.DefaultPressureThresholds()}
		applyConfigValue("some_other_key", "999", &snap)
		assert.Equal(t, 12, snap.interval)
		assert.Equal(t, 3, snap.version)
	})
}
