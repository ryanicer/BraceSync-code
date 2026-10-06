// Package auth_test — T570 上报端点时间窗临时放宽的纯函数侧测试
//
// 断四格：① 现网那条真实偏差（慢 681 秒）在上报档被放行，而同一枚签名在默认档被拒——
// 放行是窗口给的，不是验签恒绿；② 上报档自己有界（31 分钟照样 20402，文案报得出 ±30min 这一档）；
// ③ 边界按「不大于」取（30 分钟整点在档内，多一秒在档外）；④ 另外两档一字未动。
package auth_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/gateway/internal/auth"
)

// t570SkewSec 现网实测偏差：设备自报 1791126964、服务端同一请求 1791127645，差 681 秒（T564 插桩）。
const t570SkewSec = 681

const t570Secret = "secret-abc"

func t570Sign(secret, method, path, body string, deviceTime time.Time) string {
	return auth.HMACSHA256(secret,
		auth.BuildSignString(method, path, "DEV-T570", t550AuthNonce, body, deviceTime))
}

func TestT570_ReportWindowTiers_AfterWidening(t *testing.T) {
	assert.Equal(t, 30, auth.DeviceReportWindow, "上报档取 30 分钟（派发单下限 1800 秒）")
	assert.Equal(t, 5, auth.SignatureTimeWindow, "默认档没动：T570 只放宽上报端点")
	assert.Equal(t, 24*60, auth.DeviceTimeSyncWindow, "校时档没动：T570 不碰 /device/time")
}

func TestT570_Skew681s_PassesOnReportWindowOnly(t *testing.T) {
	serverTime := time.Now()
	deviceTime := serverTime.Add(-t570SkewSec * time.Second)
	tsStr := strconv.FormatInt(deviceTime.Unix(), 10)
	body := `{"device_id":"DEV-T570","pressures":[10,20]}`
	sig := t570Sign(t570Secret, "POST", "/api/v1/device/records", body, deviceTime)

	v := &auth.DeviceSigVerifier{}

	// 正对照：上报档放行现网那一条偏差
	res := v.VerifySignatureWindowed("POST", "/api/v1/device/records", body, tsStr, sig,
		"DEV-T570", t570Secret, t550AuthNonce, serverTime, auth.DeviceReportWindow)
	require.True(t, res.Valid, "681 秒慢钟必须被上报档放行，否则本卡没解现网那一格：%+v", res)

	// 负对照：同一枚签名走默认档仍 20402——放行的功劳在窗口，不在验签被跳过
	def := v.VerifySignature("POST", "/api/v1/device/records", body, tsStr, sig,
		"DEV-T570", t570Secret, t550AuthNonce, serverTime)
	require.False(t, def.Valid, "默认档（±5min）必须照旧拒 681 秒，否则三档就没有区分度")
	assert.Equal(t, "20402", def.ErrorCode)
	assert.Contains(t, def.ErrorMessage, "±5min")
}

func TestT570_ReportWindow_IsBounded(t *testing.T) {
	serverTime := time.Now()
	beyond := serverTime.Add(-31 * time.Minute)
	tsStr := strconv.FormatInt(beyond.Unix(), 10)
	sig := t570Sign(t570Secret, "POST", "/api/v1/device/records", `{}`, beyond)

	v := &auth.DeviceSigVerifier{}
	res := v.VerifySignatureWindowed("POST", "/api/v1/device/records", `{}`, tsStr, sig,
		"DEV-T570", t570Secret, t550AuthNonce, serverTime, auth.DeviceReportWindow)

	require.False(t, res.Valid, "放宽是换窗口不是取消窗口")
	assert.Equal(t, "20402", res.ErrorCode)
	assert.Contains(t, res.ErrorMessage, "±30min", "文案要报出被拒时用的那一档，日志面才认得出是 T570 档在拦")
	assert.InDelta(t, float64(-31*60), res.SkewSec, 2, "T564 取证那一格在上报档照样要报得出偏差秒数")
}

func TestT570_ReportWindowBoundary(t *testing.T) {
	serverTime := time.Now()
	// 档内：恰好 30 分钟（判定是「不大于」）
	assert.True(t, auth.IsTimestampInWindow(serverTime.Add(-30*time.Minute), serverTime, auth.DeviceReportWindow))
	assert.True(t, auth.IsTimestampInWindow(serverTime.Add(30*time.Minute), serverTime, auth.DeviceReportWindow))
	// 档外：多一秒
	assert.False(t, auth.IsTimestampInWindow(serverTime.Add(-30*time.Minute-time.Second), serverTime, auth.DeviceReportWindow))
	assert.False(t, auth.IsTimestampInWindow(serverTime.Add(30*time.Minute+time.Second), serverTime, auth.DeviceReportWindow))
	// 现网那一格（681 秒）落在档内，且距上界还有 1119 秒
	assert.Less(t, t570SkewSec, auth.DeviceReportWindow*60, "窗口必须覆盖现网实测偏差")
	assert.Equal(t, auth.DeviceReportWindow*60-t570SkewSec, 1119, "681 秒之外的余量是 1119 秒，派发单的余量口径按这一句算")
}

func TestT570_WidenedWindow_StillVerifiesSignature(t *testing.T) {
	serverTime := time.Now()
	deviceTime := serverTime.Add(-t570SkewSec * time.Second)
	tsStr := strconv.FormatInt(deviceTime.Unix(), 10)
	body := `{"pressures":[1]}`

	v := &auth.DeviceSigVerifier{}

	badSig := t570Sign("wrong-secret", "POST", "/api/v1/device/records", body, deviceTime)
	res := v.VerifySignatureWindowed("POST", "/api/v1/device/records", body, tsStr, badSig,
		"DEV-T570", t570Secret, t550AuthNonce, serverTime, auth.DeviceReportWindow)
	require.False(t, res.Valid)
	assert.Equal(t, "20401", res.ErrorCode, "档内但签名不对 → 签名错，不是时钟异常")

	tampered := t570Sign(t570Secret, "POST", "/api/v1/device/records", body, deviceTime)
	res2 := v.VerifySignatureWindowed("POST", "/api/v1/device/records", `{"pressures":[9]}`, tsStr, tampered,
		"DEV-T570", t570Secret, t550AuthNonce, serverTime, auth.DeviceReportWindow)
	require.False(t, res2.Valid)
	assert.Equal(t, "20401", res2.ErrorCode, "档内但 body 被改（签名串里 body_sha256 对不上）→ 同样拒")
}
