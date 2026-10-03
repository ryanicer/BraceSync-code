// Package auth_test — T550 校时端点宽窗豁免的纯函数侧测试
//
// 断两格：窗口档位确实随入参变（错误文案里带得出档位），以及放宽窗口不放宽签名。
package auth_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/bracesync/bracesync/services/gateway/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t550AuthNonce = "0123456789abcdef0123456789abcdef"

func t550Sign(secret, method, path, body string, deviceTime time.Time) string {
	return auth.HMACSHA256(secret,
		auth.BuildSignString(method, path, "DEV-T550", t550AuthNonce, body, deviceTime))
}

func TestT550_VerifySignatureWindowed_UsesPassedWindow(t *testing.T) {
	serverTime := time.Now()
	skewed := serverTime.Add(-6 * time.Hour)
	tsStr := strconv.FormatInt(skewed.Unix(), 10)
	sig := t550Sign("secret-abc", "GET", "/api/v1/device/time", "", skewed)

	v := &auth.DeviceSigVerifier{}

	// 默认档：6 小时偏差 → 20402，文案带 ±5min
	def := v.VerifySignature("GET", "/api/v1/device/time", "", tsStr, sig, "DEV-T550", "secret-abc", t550AuthNonce, serverTime)
	require.False(t, def.Valid, "默认窗必须仍拒 6 小时偏差，否则宽窗豁免没有区分度")
	assert.Equal(t, "20402", def.ErrorCode)
	assert.Contains(t, def.ErrorMessage, "±5min")

	// 宽窗档：同一枚请求 → 放行，文案档位随之变
	wide := v.VerifySignatureWindowed("GET", "/api/v1/device/time", "", tsStr, sig, "DEV-T550", "secret-abc", t550AuthNonce, serverTime, auth.DeviceTimeSyncWindow)
	assert.True(t, wide.Valid, "校时档（±24h）应放行 6 小时偏差：%+v", wide)

	// 宽窗有界：25 小时偏差 → 仍 20402，且文案报出的是宽窗档位
	beyond := serverTime.Add(-25 * time.Hour)
	beyondStr := strconv.FormatInt(beyond.Unix(), 10)
	bSig := t550Sign("secret-abc", "GET", "/api/v1/device/time", "", beyond)
	res := v.VerifySignatureWindowed("GET", "/api/v1/device/time", "", beyondStr, bSig, "DEV-T550", "secret-abc", t550AuthNonce, serverTime, auth.DeviceTimeSyncWindow)
	require.False(t, res.Valid)
	assert.Equal(t, "20402", res.ErrorCode)
	assert.Contains(t, res.ErrorMessage, "±1440min")
}

func TestT550_WidenedWindow_StillVerifiesSignature(t *testing.T) {
	serverTime := time.Now()
	skewed := serverTime.Add(-6 * time.Hour)
	tsStr := strconv.FormatInt(skewed.Unix(), 10)

	v := &auth.DeviceSigVerifier{}
	// 宽窗内但签名用错密钥 → 20401（不是 20402），证明放宽只放宽时间这一维
	bad := t550Sign("wrong-secret", "GET", "/api/v1/device/time", "", skewed)
	res := v.VerifySignatureWindowed("GET", "/api/v1/device/time", "", tsStr, bad, "DEV-T550", "secret-abc", t550AuthNonce, serverTime, auth.DeviceTimeSyncWindow)
	require.False(t, res.Valid)
	assert.Equal(t, "20401", res.ErrorCode)

	// 宽窗内但 body 被改（签名串里的 body_sha256 对不上）→ 同样 20401
	bodySig := t550Sign("secret-abc", "POST", "/api/v1/device/records", `{"pressures":[1]}`, skewed)
	res2 := v.VerifySignatureWindowed("POST", "/api/v1/device/records", `{"pressures":[9]}`, tsStr, bodySig, "DEV-T550", "secret-abc", t550AuthNonce, serverTime, auth.DeviceTimeSyncWindow)
	require.False(t, res2.Valid)
	assert.Equal(t, "20401", res2.ErrorCode)
}
