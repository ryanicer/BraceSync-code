// T570 上报端点时间窗临时放宽——网关侧读数。
//
// 派发单要的是「数据先进来」：现网设备慢 681 秒，±5min 那一档把它们全挡在 20402。
// 这里断四格：① 681 秒慢钟的单帧与批量上报在网关放行并真的打到 data-service（正对照）；
// ② 31 分钟偏差仍 401/20402 且不得触达后端（放宽后的门还有牙）；
// ③ 681 秒 + 错密钥仍 401/20401（放宽只放宽时间这一维）；
// ④ 校时端点档位没被本卡带动（同一枚 681 秒慢钟照样拿到校时，25 小时照样拒）。
package main

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/gateway/internal/auth"
)

// t570NetSkew 现网实测：设备自报时刻比服务端少 681 秒（T564 插桩，x_timestamp=1791126964 / time=1791127645）。
const t570NetSkew = 681 * time.Second

const t570SecretMain = "dev-secret-abc"

func t570Gateway(t *testing.T) (string, *[]string) {
	t.Helper()
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": t570SecretMain}})
	return gw.URL, received
}

func TestT570_ReportSkewed681s_Forwarded(t *testing.T) {
	gwURL, received := t570Gateway(t)
	deviceTime := time.Now().Add(-t570NetSkew)

	body := `{"device_id":"DEV-SIG-001","pressures":[10,20]}`
	hdrs := deviceHeaders(t570SecretMain, "POST", "/api/v1/device/records", body, deviceTime)
	code, respBody := httpDoFull(t, http.MethodPost, gwURL+"/api/v1/device/records", body, hdrs)

	require.Equal(t, http.StatusOK, code, "681 秒慢钟的上报必须进得来（本卡的存在理由）：%s", respBody)
	require.Len(t, *received, 1, "放行的那一条要真的打到 data-service，不是网关自己吞掉")
	assert.Contains(t, (*received)[0], "POST /api/v1/device/records")
	assert.Contains(t, (*received)[0], "body="+body, "透传的请求体一字不改")
}

func TestT570_BatchReportSkewed681s_Forwarded(t *testing.T) {
	gwURL, received := t570Gateway(t)
	deviceTime := time.Now().Add(-t570NetSkew)

	body := `{"frames":[{"ts":` + strconv.FormatInt(deviceTime.Unix(), 10) + `,"pressures":[1,2]}]}`
	hdrs := deviceHeaders(t570SecretMain, "POST", "/api/v1/device/records/batch", body, deviceTime)
	code, respBody := httpDoFull(t, http.MethodPost, gwURL+"/api/v1/device/records/batch", body, hdrs)

	require.Equal(t, http.StatusOK, code, "批量补传与单帧同组，窗口放宽要一起生效：%s", respBody)
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "POST /api/v1/device/records/batch")
}

func TestT570_ReportWindow_StillRejectsBeyondTier(t *testing.T) {
	gwURL, received := t570Gateway(t)
	hdrs := deviceHeaders(t570SecretMain, "POST", "/api/v1/device/records",
		`{"device_id":"DEV-SIG-001","pressures":[10,20]}`, time.Now().Add(-31*time.Minute))
	code, respBody := httpDoFull(t, http.MethodPost, gwURL+"/api/v1/device/records",
		`{"device_id":"DEV-SIG-001","pressures":[10,20]}`, hdrs)

	assert.Equal(t, http.StatusUnauthorized, code, "30 分钟档外仍要拒：放宽是换窗口不是取消窗口")
	assert.Contains(t, respBody, `"code":20402`)
	assert.Empty(t, *received, "被拒的上报不得触达后端")
}

func TestT570_ReportWindow_StillVerifiesSignature(t *testing.T) {
	gwURL, received := t570Gateway(t)
	hdrs := deviceHeaders("wrong-secret", "POST", "/api/v1/device/records",
		`{"device_id":"DEV-SIG-001","pressures":[10,20]}`, time.Now().Add(-t570NetSkew))
	code, respBody := httpDoFull(t, http.MethodPost, gwURL+"/api/v1/device/records",
		`{"device_id":"DEV-SIG-001","pressures":[10,20]}`, hdrs)

	assert.Equal(t, http.StatusUnauthorized, code, "窗口放宽不得把签名一起放宽")
	assert.Contains(t, respBody, `"code":20401`, "档内但签名不对 → 20401，不是 20402")
	assert.Empty(t, *received)
}

func TestT570_TimeSyncTier_Unchanged(t *testing.T) {
	gwURL, received := t570Gateway(t)

	hdrs := deviceHeaders(t570SecretMain, http.MethodGet, "/api/v1/device/time", "", time.Now().Add(-t570NetSkew))
	code, respBody := httpDoFull(t, http.MethodGet, gwURL+"/api/v1/device/time", "", hdrs)
	require.Equal(t, http.StatusOK, code, "校时端点档位一字未动（T550 那一格不回退）：%s", respBody)
	assert.Contains(t, respBody, "server_time")

	beyond := deviceHeaders(t570SecretMain, http.MethodGet, "/api/v1/device/time", "", time.Now().Add(-25*time.Hour))
	code2, _ := httpDoFull(t, http.MethodGet, gwURL+"/api/v1/device/time", "", beyond)
	assert.Equal(t, http.StatusUnauthorized, code2, "校时档仍是 ±24h 有界，不是被本卡顺手撑开")
	assert.Empty(t, *received, "设备域路由不得把请求转给后端（校时由 gateway 本地应答）")

	assert.Equal(t, 24*60, auth.DeviceTimeSyncWindow)
	assert.Equal(t, 30, auth.DeviceReportWindow)
}
