// T281 —— threshold_pressure_low 量纲倒挂（T203 ÷10 漏改这一键）在 settings 校验上的现场与修复。
//
// 现场：库里是 high=5（000019 改的）/ low=10（000016 播的旧量纲，000019 没带它），
// 而 validateSettings 要求 high > low ⇒ 系统配置页哪怕原样回提（no-op PUT）也恒 400。
// 修复是数据侧的：迁移 000021 把 low 按同口径改成 1。
//
// 这两个用例不测迁移本身（真库验证在 repo 包的集成用例里），守的是校验路径两端：
// 修好的键值对必须放行，倒挂的键值对必须继续拒绝 —— 否则等于靠放宽校验绕过缺陷。
package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 迁移落库后的库里现值：000019 ⇒ high=5，000021 ⇒ low=1
const (
	t281MigratedHigh = "5"
	t281MigratedLow  = "1"
)

func TestT281_UpdateSettings_NoOpWithMigratedValuesPasses(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{
		keyCollectInterval: "30",
		keyPressureHigh:    t281MigratedHigh,
		keyPressureLow:     t281MigratedLow,
	}

	// 前端真实动作：GET 回来的两个压力阈值原样 PUT 回去
	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings",
		mergeBody(mergeBody(validSettingsBody(), "pressureHighThresholdN", 5), "pressureLowThresholdN", 1),
		map[string]string{"X-User-Id": "A0001"})
	require.Equal(t, http.StatusOK, w.Code, "000021 之后 no-op PUT 不得再被下限挡死：%s", resp.Message)

	// 响应与库里同值（不是回显请求、也不是默认值）
	dto := settingsOf(t, resp.Data)
	assert.Equal(t, 5.0, dto.PressureHighThresholdN)
	require.NotNil(t, dto.PressureLowThresholdN)
	assert.Equal(t, 1.0, *dto.PressureLowThresholdN)
}

func TestT281_UpdateSettings_InvertedPairStillRejected(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{
		keyCollectInterval: "30",
		keyPressureHigh:    t281MigratedHigh,
		keyPressureLow:     "10", // 未跑 000021 的库
	}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings",
		mergeBody(validSettingsBody(), "pressureHighThresholdN", 5), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp.Message, "threshold_pressure_low")
	assert.Nil(t, e.store.lastUpsert, "拒绝时不得触达写通道")
}
