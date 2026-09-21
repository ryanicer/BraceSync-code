// T302 F1 后端半边 —— PRD §7D.12 三项配置（空载校准偏差上限 / 微信模板消息 ID / 短信模板 ID）
// 进 GET/PUT /api/v1/admin/settings 的读写与校验口径。
//
// 守三件事：
//  1. GET 恒回三项（缺行按默认），前端不用判缺失；
//  2. 老前端不带这三个字段时，PUT 既不能写、也不能把库里现值抹掉（指针语义，同 T257 的下限）；
//  3. 校验是形状守卫：校准上限挡 0/越界，模板 ID 挡控制字符与超长但**不挡厂商格式**，
//     空串是合法值（= 未配置，显式清空）。
package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func kvValue(kvs []repo.ConfigKV, key string) (string, bool) {
	for _, kv := range kvs {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return "", false
}

func TestT302_GetSettings_ReturnsThreeNewKeys(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{
		keyCalibrationOffset: "0.05",
		keyWechatTemplateID:  "TPL_wx-AbC001",
		keySmsTemplateID:     "SMS_20261001",
	}

	w, resp := e.do(http.MethodGet, "/api/v1/admin/settings", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	dto := settingsOf(t, resp.Data)
	require.NotNil(t, dto.CalibrationOffsetN)
	assert.Equal(t, 0.05, *dto.CalibrationOffsetN)
	require.NotNil(t, dto.WechatTemplateID)
	assert.Equal(t, "TPL_wx-AbC001", *dto.WechatTemplateID)
	require.NotNil(t, dto.SmsTemplateID)
	assert.Equal(t, "SMS_20261001", *dto.SmsTemplateID)
}

// 只跑 migrations 不跑 seed 的库（CI 集成库）里三行由 000023 播；
// 若连 000023 也没跑，GET 仍要回兜底值而不是缺字段。
func TestT302_GetSettings_MissingRowsFallBackToDefaults(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	w, resp := e.do(http.MethodGet, "/api/v1/admin/settings", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	dto := settingsOf(t, resp.Data)
	require.NotNil(t, dto.CalibrationOffsetN)
	assert.Equal(t, defaultCalibrationOffsetN, *dto.CalibrationOffsetN)
	require.NotNil(t, dto.WechatTemplateID)
	assert.Equal(t, "", *dto.WechatTemplateID, "未配置 = 空串，不是缺字段")
	require.NotNil(t, dto.SmsTemplateID)
	assert.Equal(t, "", *dto.SmsTemplateID)
}

// 现网 admin-web 表单不带这三项 ⇒ 一次普通保存必须既不写键也不抹库里现值。
func TestT302_UpdateSettings_LegacyBodyDoesNotTouchNewKeys(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{
		keyCollectInterval:   "30",
		keyCalibrationOffset: "0.05",
		keyWechatTemplateID:  "TPL_keep_me",
		keySmsTemplateID:     "SMS_keep_me",
	}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings", validSettingsBody(),
		map[string]string{"X-User-Id": "A0001"})
	require.Equal(t, http.StatusOK, w.Code)

	for _, key := range []string{keyCalibrationOffset, keyWechatTemplateID, keySmsTemplateID} {
		_, written := kvValue(e.store.lastUpsert, key)
		assert.False(t, written, "请求没给 %s 就不得写该键", key)
	}
	dto := settingsOf(t, resp.Data)
	require.NotNil(t, dto.WechatTemplateID)
	assert.Equal(t, "TPL_keep_me", *dto.WechatTemplateID, "响应回显库里现值，前端不会以为被清空")
	require.NotNil(t, dto.SmsTemplateID)
	assert.Equal(t, "SMS_keep_me", *dto.SmsTemplateID)
	require.NotNil(t, dto.CalibrationOffsetN)
	assert.Equal(t, 0.05, *dto.CalibrationOffsetN)
}

func TestT302_UpdateSettings_WritesThreeKeysAndTrims(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	body := mergeBody(mergeBody(mergeBody(validSettingsBody(), "calibrationOffsetN", 0.2),
		"wechatTemplateId", "  TPL_wx-AbC001  "), "smsTemplateId", "")
	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings", body,
		map[string]string{"X-User-Id": "A0001"})
	require.Equal(t, http.StatusOK, w.Code)

	v, ok := kvValue(e.store.lastUpsert, keyCalibrationOffset)
	require.True(t, ok)
	assert.Equal(t, "0.2", v)
	v, ok = kvValue(e.store.lastUpsert, keyWechatTemplateID)
	require.True(t, ok)
	assert.Equal(t, "TPL_wx-AbC001", v, "入库前 trim，别把空白存进配置")
	v, ok = kvValue(e.store.lastUpsert, keySmsTemplateID)
	require.True(t, ok)
	assert.Equal(t, "", v, "给了空串 = 显式清空（与 nil「不改」是两件事）")

	dto := settingsOf(t, resp.Data)
	require.NotNil(t, dto.CalibrationOffsetN)
	assert.Equal(t, 0.2, *dto.CalibrationOffsetN)
	require.NotNil(t, dto.WechatTemplateID)
	assert.Equal(t, "TPL_wx-AbC001", *dto.WechatTemplateID)
}

func TestT302_UpdateSettings_RejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		val   any
		wants string
	}{
		{"校准上限为 0（每次空载校准必判失败）", "calibrationOffsetN", 0, "calibrationOffsetN"},
		{"校准上限负数", "calibrationOffsetN", -0.1, "calibrationOffsetN"},
		{"校准上限越界", "calibrationOffsetN", 21, "calibrationOffsetN"},
		{"微信模板 ID 含换行", "wechatTemplateId", "TPL\r\ninject", "control characters"},
		{"短信模板 ID 含 NUL 类控制符", "smsTemplateId", "SMS_1\x01", "control characters"},
		{"微信模板 ID 超长", "wechatTemplateId", strings.Repeat("T", maxTemplateIDLen+1), "exceeds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			e.store.configs = map[string]string{keyCollectInterval: "30"}

			w, resp := e.do(http.MethodPut, "/api/v1/admin/settings",
				mergeBody(validSettingsBody(), tc.key, tc.val),
				map[string]string{"X-User-Id": "A0001"})
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, resp.Message, tc.wants)
			assert.Nil(t, e.store.lastUpsert, "拒绝时不得触达写通道")
		})
	}
}

// 厂商格式不写死：微信 URL-safe 串与各家短信前缀都要能存（换了服务商不该改后端）。
func TestT302_UpdateSettings_AcceptsAnyVendorTemplateShape(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	body := mergeBody(mergeBody(validSettingsBody(),
		"wechatTemplateId", "_-kB1oY2pQ3rS4tU5vW6xY7z8"), "smsTemplateId", "TEMPLATE-2026_09_22_001")
	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings", body,
		map[string]string{"X-User-Id": "A0001"})
	require.Equal(t, http.StatusOK, w.Code, "%s", resp.Message)

	v, ok := kvValue(e.store.lastUpsert, keyWechatTemplateID)
	require.True(t, ok)
	assert.Equal(t, "_-kB1oY2pQ3rS4tU5vW6xY7z8", v)
	v, ok = kvValue(e.store.lastUpsert, keySmsTemplateID)
	require.True(t, ok)
	assert.Equal(t, "TEMPLATE-2026_09_22_001", v)
}
