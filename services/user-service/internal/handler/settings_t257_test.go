// T257 12.4（三档合两键）——§7D.12 系统参数页与告警管理页 Tab2 的上下限联动校验。
//
// 缺陷面：统一压力上限（threshold_pressure_high）两个页面都能写，统一下限
// （threshold_pressure_low）只有告警页能写。settings 页原先只看 [1,200]，
// 于是可以把上限改成 5N 而下限还是 10N —— 存出来的配置自相矛盾，
// 告警页 GET 会渲染出「上限 < 下限」，且压力偏高告警永不触发。
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
)

func TestT257_UpdateSettings_RejectsUpperNotAboveStoredLower(t *testing.T) {
	for _, upper := range []float64{5, 10} { // 10 = 与下限相等，同样拒绝
		e := newEnv(t, true, true)
		e.store.configs = map[string]string{keyPressureLow: "10"}

		w, resp := e.do(http.MethodPut, "/api/v1/admin/settings",
			mergeBody(validSettingsBody(), "pressureHighThresholdN", upper), nil)
		assert.Equal(t, http.StatusBadRequest, w.Code, "pressureHighThresholdN=%g", upper)
		assert.Contains(t, resp.Message, "threshold_pressure_low")
		assert.Nil(t, e.store.lastUpsert, "拒绝时不得触达写通道")
	}
}

func TestT257_UpdateSettings_MissingLowerKeyFallsBackToDefault(t *testing.T) {
	// 下限行缺失（未跑过告警页）⇒ 按默认 10N 校验，45N 仍须放行，
	// 否则 settings 页会因为一个它自己不写的键被锁死
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings", validSettingsBody(), nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	_, wroteLow := kvOf(e.store.lastUpsert, keyPressureLow)
	assert.False(t, wroteLow, "settings 页不接管下限键，避免两处各写一份")
}

func TestT257_UpdateSettings_LowerThanUpperStillAccepted(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyPressureLow: "44"}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings",
		mergeBody(validSettingsBody(), "pressureHighThresholdN", 45), nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	v, ok := kvOf(e.store.lastUpsert, keyPressureHigh)
	require.True(t, ok)
	assert.Equal(t, "45", v)
}

// ── 12.4：设计稿「低压上限」= 告警页「统一压力下限」，同键读写 ──

func TestT257_GetSettings_ExposesPressureLowThreshold(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodGet, "/api/v1/admin/settings", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	dto := settingsOf(t, resp.Data)
	require.NotNil(t, dto.PressureLowThresholdN, "GET 恒回数值，前端不用判缺失")
	assert.Equal(t, float64(defaultUnifiedLowerN), *dto.PressureLowThresholdN)

	e.store.configs = map[string]string{keyPressureLow: "20", keyPressureHigh: "60"}
	w, resp = e.do(http.MethodGet, "/api/v1/admin/settings", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	dto = settingsOf(t, resp.Data)
	assert.Equal(t, 20.0, *dto.PressureLowThresholdN, "读的就是告警页那个键")
	assert.Equal(t, 60.0, dto.PressureHighThresholdN)
}

func TestT257_UpdateSettings_WritesPressureLowWhenGiven(t *testing.T) {
	e := newEnv(t, true, true)
	body := mergeBody(validSettingsBody(), "pressureLowThresholdN", 20)

	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings", body, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.lastUpsert, 11, "多写一个键（下限），其余 10 项不变")
	v, ok := kvOf(e.store.lastUpsert, keyPressureLow)
	require.True(t, ok)
	assert.Equal(t, "20", v)

	dto := settingsOf(t, resp.Data)
	require.NotNil(t, dto.PressureLowThresholdN)
	assert.Equal(t, 20.0, *dto.PressureLowThresholdN, "PUT 响应与 GET 同形")
}

func TestT257_UpdateSettings_RejectsLowNotBelowHigh(t *testing.T) {
	for _, tc := range []struct {
		name string
		low  float64
		high float64
		msg  string
	}{
		{"下限高于上限", 50, 45, "threshold_pressure_low"},
		{"下限等于上限", 45, 45, "threshold_pressure_low"},
		{"下限量程为负", -1, 45, "pressureLowThresholdN"},
		{"下限量程超 200", 201, 45, "pressureLowThresholdN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			body := mergeBody(validSettingsBody(), "pressureLowThresholdN", tc.low)
			body = mergeBody(body, "pressureHighThresholdN", tc.high)

			w, resp := e.do(http.MethodPut, "/api/v1/admin/settings", body, nil)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, resp.Message, tc.msg)
			assert.Nil(t, e.store.lastUpsert, "拒绝时不得触达写通道")
		})
	}
}

// settingsOf 解析 settings 端点 data 段
func settingsOf(t *testing.T, raw json.RawMessage) model.SystemSettingsDTO {
	t.Helper()
	var dto model.SystemSettingsDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	return dto
}
