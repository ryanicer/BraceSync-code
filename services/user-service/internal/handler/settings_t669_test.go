// T669 —— 设备配置下发通道 version 断链：后台改采集间隔时必须同步递增 device_config_version，
// 否则固件按「版本不变不应用」跳过新间隔（协议 §4.1 唯一下发通道）。
//
// 这几个用例守 handler 的分流口径：
//   - 间隔实际变化（含库里缺键而新值不等于缺省 30）→ 走带版本递增的保存通道；
//   - 间隔原样回提 / 缺键但就是缺省值 → 走普通 UPSERT，不白递增版本；
//   - 两种走法写入的 kvs 都必须同时带秒口径键与分钟键（T256 换算链不动）。
//
// 递增 SQL 本身（并发原子 +1、缺键/脏值落 2）在 repo 包集成测试里验。
package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestT669_UpdateSettings_IntervalChangeBumpsDeviceConfigVersion(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings",
		mergeBody(validSettingsBody(), "collectIntervalSeconds", 900), // 30 分钟 → 15 分钟
		map[string]string{"X-User-Id": "A0001"})
	require.Equal(t, http.StatusOK, w.Code, "改间隔应放行：%s", resp.Message)

	assert.True(t, e.store.lastUpsertBumpedVersion, "间隔 30→15 必须走 device_config_version 递增通道")
	minutes, ok := kvValue(e.store.lastUpsert, keyCollectInterval)
	require.True(t, ok, "kvs 必须仍带分钟键（device/alert 服务消费）")
	assert.Equal(t, "15", minutes)
	seconds, ok := kvValue(e.store.lastUpsert, keyCollectIntervalSeconds)
	require.True(t, ok, "kvs 必须仍带秒口径键（API 口径）")
	assert.Equal(t, "900", seconds)
}

func TestT669_UpdateSettings_UnchangedIntervalDoesNotBump(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings",
		validSettingsBody(), // 1800 秒 = 30 分钟，原样回提
		map[string]string{"X-User-Id": "A0001"})
	require.Equal(t, http.StatusOK, w.Code, "原样回提应放行：%s", resp.Message)

	assert.False(t, e.store.lastUpsertBumpedVersion, "间隔没变不该白递增版本（固件无配置差可应用）")
	minutes, ok := kvValue(e.store.lastUpsert, keyCollectInterval)
	require.True(t, ok)
	assert.Equal(t, "30", minutes)
}

func TestT669_UpdateSettings_MissingIntervalKeyBumpsOnlyOnEffectiveChange(t *testing.T) {
	// 库里没有 collect_interval_minutes：data-service 读缺省回退 30/1。
	// 首次保存 15 分钟（与缺省不同）→ 必须递增；保存的就是 30 → 无配置差，不递增。
	t.Run("缺键首存15分钟", func(t *testing.T) {
		e := newEnv(t, true, true)
		e.store.configs = map[string]string{}

		w, resp := e.do(http.MethodPut, "/api/v1/admin/settings",
			mergeBody(validSettingsBody(), "collectIntervalSeconds", 900),
			map[string]string{"X-User-Id": "A0001"})
		require.Equal(t, http.StatusOK, w.Code, "%s", resp.Message)
		assert.True(t, e.store.lastUpsertBumpedVersion, "缺键且新值≠缺省30，必须递增版本让固件应用")
	})

	t.Run("缺键首存缺省30分钟", func(t *testing.T) {
		e := newEnv(t, true, true)
		e.store.configs = map[string]string{}

		w, resp := e.do(http.MethodPut, "/api/v1/admin/settings",
			validSettingsBody(),
			map[string]string{"X-User-Id": "A0001"})
		require.Equal(t, http.StatusOK, w.Code, "%s", resp.Message)
		assert.False(t, e.store.lastUpsertBumpedVersion, "与缺省等效的 30 分钟不该产生版本差")
	})
}

func TestT669_UpdateSettings_SaveFailureStill500(t *testing.T) {
	// 版本递增通道与普通通道共用错误映射：存储失败必须照样 500，不能因换了方法吞错。
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}
	e.store.upsertErr = errors.New("db busy")

	w, _ := e.do(http.MethodPut, "/api/v1/admin/settings",
		mergeBody(validSettingsBody(), "collectIntervalSeconds", 900),
		map[string]string{"X-User-Id": "A0001"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.True(t, e.store.lastUpsertBumpedVersion)
}
