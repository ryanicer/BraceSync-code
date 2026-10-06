// Package main — T550 校时死锁豁免的行为测试（不与 device_auth_impl_test.go 重叠）
//
// 覆盖：时钟超差设备能否拿到校时（死锁是否解开）、放宽是否只落在校时端点
// （上报组仍按 ±5min 拒）、宽窗是否有界、验签与注册状态是否照旧生效。
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T550 死锁读数用的时钟偏差：6 小时（远超上报侧那一档——T550 时是 ±5min，T570 后是 ±30min，
// 两档下 6 小时都在门外；同时落在校时侧宽窗 ±24h 之内）
const t550ClockSkew = 6 * time.Hour

func t550Secrets() *fakeSecretsCtx {
	return &fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": "dev-secret-abc"}}
}

func TestT550_TimeEndpoint_WorksForClockSkewedDevice(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL, t550Secrets())

	skewed := time.Now().Add(-t550ClockSkew)
	hdrs := deviceHeaders("dev-secret-abc", http.MethodGet, "/api/v1/device/time", "", skewed)
	code, respBody := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/device/time", "", hdrs)

	require.Equal(t, http.StatusOK, code, "时钟超差的设备必须能拿到校时，否则死锁未解：%s", respBody)
	assert.Contains(t, respBody, "server_time")
	assert.Empty(t, *received, "校时仍由 gateway 本地应答，不打 data-service")
}

func TestT550_ReportRoute_StillRejectsBeyondReportWindow(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL, t550Secrets())

	body := `{"device_id":"DEV-SIG-001","pressures":[10,20]}`
	hdrs := deviceHeaders("dev-secret-abc", http.MethodPost, "/api/v1/device/records", body, time.Now().Add(-t550ClockSkew))
	code, respBody := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body, hdrs)

	assert.Equal(t, http.StatusUnauthorized, code, "上报档与校时档是两个独立的界：6 小时只在校时档之内，不在上报档之内")
	assert.Contains(t, respBody, `"code":20402`, "上报侧按上报档拒（T550 时 ±5min，T570 后 ±30min，6 小时两档都在门外；窗口档位差异在 auth 包单测里断，网关对外文案已被 T-error-copy 统一洗成中文）")
	assert.Empty(t, *received, "被拒的上报不得触达后端")
}

func TestT550_TimeEndpoint_WidenedWindowIsBounded(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL, t550Secrets())

	hdrs := deviceHeaders("dev-secret-abc", http.MethodGet, "/api/v1/device/time", "", time.Now().Add(-25*time.Hour))
	code, respBody := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/device/time", "", hdrs)

	assert.Equal(t, http.StatusUnauthorized, code, "放宽是换窗口不是取消窗口")
	assert.Contains(t, respBody, `"code":20402`, "超宽窗仍走 20402 时钟异常，不是签名错")
}

func TestT550_TimeEndpoint_StillVerifiesSignatureAndRegistration(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL, t550Secrets())

	// 错密钥签名（时钟同样超差）→ 签名不一致 20401，而不是被宽窗放过
	hdrs := deviceHeaders("wrong-secret", http.MethodGet, "/api/v1/device/time", "", time.Now().Add(-t550ClockSkew))
	code, respBody := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/device/time", "", hdrs)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Contains(t, respBody, `"code":20401`, "放宽窗口不放宽签名：%s", respBody)

	// 未注册设备 → 20404（注册状态校验同样不随窗口变）
	gw2 := startDeviceGateway(t, backend.URL, &fakeSecretsCtx{secrets: map[string]string{}})
	hdrs2 := deviceHeaders("dev-secret-abc", http.MethodGet, "/api/v1/device/time", "", time.Now())
	code2, respBody2 := httpDoFull(t, http.MethodGet, gw2.URL+"/api/v1/device/time", "", hdrs2)
	assert.Equal(t, http.StatusUnauthorized, code2)
	assert.Contains(t, respBody2, `"code":20404`, "校时端点仍要求设备已注册：%s", respBody2)
}
