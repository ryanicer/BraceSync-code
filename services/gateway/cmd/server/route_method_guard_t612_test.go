// T612 网关路由方法校验（路由注册层）的行为测试。
//
// 与既有测试不重叠：t550_device_time_test.go 锁的是校时窗口的行为、t564_log_forensics_test.go
// 锁的是 20402 那一行的取证字段，本文件锁的是「方法不匹配」与「路径未注册」这两格。
//
// 修复前的现读基线（本卡探针实测，2026-10-07）：POST /api/v1/device/time（只注册 GET）
// ⇒ status=404 ctype="text/plain" allow="" body="404 page not found"，且 zerolog 捕获面 0 行；
// 本文件的断言就是对着这两格翻面的，所以每一条都能在回滚修复后自己变红。
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
)

const (
	t612Secret = "dev-secret-abc"
	t612Body   = `{"device_id":"DEV-SIG-001","pressures":[10,20]}`
)

// t612DeviceGateway 设备域路由 + 本卡兜底（与 startDeviceGateway 同构，多挂一步 registerRouteMethodGuard）
func t612DeviceGateway(t *testing.T, dataURL string, secrets SecretProvider) *httptest.Server {
	t.Helper()
	t.Setenv("DATA_SERVICE_URL", dataURL)
	r := gin.New()
	registerDeviceReportRoutes(r, newGatewayAuth("", secrets))
	registerRouteMethodGuard(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// t612RegisteredGateway 挂好本卡兜底的设备域网关（DEV-SIG-001 已注册密钥）
func t612RegisteredGateway(t *testing.T) (string, *[]string) {
	t.Helper()
	backend, received := captureBackend(t)
	gw := t612DeviceGateway(t, backend.URL, &fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": t612Secret}})
	return gw.URL, received
}

// t612EntryByCode 按业务码取那一行日志；报 0 时同屏给出被扫面总行数（「没扫到」不等于「没有」）
func t612EntryByCode(t *testing.T, capture *testhelper.LogCaptureHook, code int) map[string]interface{} {
	t.Helper()
	entries := capture.Entries()
	var hits []map[string]interface{}
	for _, entry := range entries {
		if got, ok := entry["code"].(float64); ok && int(got) == code {
			hits = append(hits, entry)
		}
	}
	require.Len(t, hits, 1, "捕获面共 %d 行，期望恰好 1 行 code=%d", len(entries), code)
	return hits[0]
}

// t612String 取日志行里的字符串字段（缺格直接判红，不当空串放过）
func t612String(t *testing.T, entry map[string]interface{}, key string) string {
	t.Helper()
	value, exists := entry[key]
	require.True(t, exists, "日志行缺字段 %s，实际字段：%v", key, entry)
	text, isString := value.(string)
	require.True(t, isString, "日志字段 %s 不是字符串：%v", key, value)
	return text
}

// 判据 1 + 2：设备以 POST 打只注册 GET 的校时端点，返回体要能看出是「路由未匹配」，
// 且错误码不得与业务 404 混同。
func TestT612_PostOnGetOnlyTimeRoute_Returns405WithDistinguishableCode(t *testing.T) {
	gwURL, received := t612RegisteredGateway(t)

	hdrs := deviceHeaders(t612Secret, http.MethodPost, "/api/v1/device/time", "", time.Now())
	code, body, respHdr := t464Request(t, http.MethodPost, gwURL+"/api/v1/device/time", "", hdrs)

	require.Equal(t, http.StatusMethodNotAllowed, code, "方法不匹配必须从 404 黑洞翻到 405，实测体：%s", body)
	assert.Contains(t, body, `"code":20405`, body)
	assert.NotContains(t, body, "404 page not found", "不得再吐框架兜底纯文本：%s", body)
	assert.NotContains(t, body, `"code":20404`, "设备域「未注册」那一格不得被复用：%s", body)
	assert.Equal(t, http.MethodGet, respHdr.Get("Allow"), "RFC 7231：405 必须带 Allow")
	assert.Contains(t, body, `"allowed_methods":["GET"]`, body)
	assert.Empty(t, *received, "路由层就拒掉的请求不得触达 data-service")
}

// 反向那一格：GET 打只注册 POST 的上报端点，同样给 405 + 20405（Allow 里是 POST 两枚）
func TestT612_GetOnPostOnlyReportRoute_Returns405AllowPost(t *testing.T) {
	gwURL, received := t612RegisteredGateway(t)

	hdrs := deviceHeaders(t612Secret, http.MethodGet, "/api/v1/device/records", "", time.Now())
	code, body, respHdr := t464Request(t, http.MethodGet, gwURL+"/api/v1/device/records", "", hdrs)

	assert.Equal(t, http.StatusMethodNotAllowed, code, body)
	assert.Contains(t, body, `"code":20405`, body)
	assert.Equal(t, http.MethodPost, respHdr.Get("Allow"), "上报端点只注册 POST")
	assert.Empty(t, *received)
}

// 参数化路径那一格：路由树匹配才能认得 /devices/DEV-1，故 Allow 取网关自己算出的那一串
func TestT612_ParamRouteMismatch_AllowsRegisteredMethod(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, body, respHdr := t464Request(t, http.MethodDelete, gw.URL+"/api/v1/devices/DEV-1", "", nil)

	assert.Equal(t, http.StatusMethodNotAllowed, code, body)
	assert.Contains(t, body, `"code":20405`, body)
	assert.Equal(t, http.MethodGet, respHdr.Get("Allow"), "/devices/:deviceId 只注册 GET")
}

// 判据「服务端有日志指向真实原因」：被拒那一刻留下一行，method/path/Allow/关联号齐
func TestT612_MismatchLeavesOneTechnicalLogLine(t *testing.T) {
	gwURL, _ := t612RegisteredGateway(t)
	capture := t464CaptureLogs(t)

	hdrs := deviceHeaders(t612Secret, http.MethodPost, "/api/v1/device/time", "", time.Now())
	code, _, respHdr := t464Request(t, http.MethodPost, gwURL+"/api/v1/device/time", "", hdrs)
	require.Equal(t, http.StatusMethodNotAllowed, code)

	entry := t612EntryByCode(t, capture, codeRouteMethodNotAllowed)
	assert.Equal(t, http.MethodPost, t612String(t, entry, "method"))
	assert.Equal(t, "/api/v1/device/time", t612String(t, entry, "path"))
	assert.Equal(t, float64(http.StatusMethodNotAllowed), entry["http_status"])
	assert.Equal(t, float64(codeRouteMethodNotAllowed), entry["code"])
	assert.Contains(t, t612String(t, entry, "message"), "other methods only: GET")
	assert.NotEmpty(t, t612String(t, entry, "request_id"), "关联号要挂在被拒那一行上")
	assert.Equal(t, t612String(t, entry, "request_id"), respHdr.Get(HeaderRequestID), "响应头与日志行是同一枚关联号")
}

// T464 双通道：响应体只给中文用户文案，技术文本留在日志行里
func TestT612_ResponseBodyCarriesUserTextNotTechnicalDetail(t *testing.T) {
	gwURL, _ := t612RegisteredGateway(t)

	hdrs := deviceHeaders(t612Secret, http.MethodPost, "/api/v1/device/time", "", time.Now())
	code, body, _ := t464Request(t, http.MethodPost, gwURL+"/api/v1/device/time", "", hdrs)
	require.Equal(t, http.StatusMethodNotAllowed, code, body)

	assert.Contains(t, body, userText(codeRouteMethodNotAllowed), body)
	assert.NotContains(t, body, "other methods only", "技术文本不进用户面：%s", body)
	for _, leaked := range []string{"X-Signature", "X-Nonce", "hmac", "sha256"} {
		assert.NotContains(t, body, leaked, "签名材料不得出现在响应体：%s", body)
	}
}

// 「不得与业务 404 混同」的正侧：既有业务拒绝（20404 未注册、裸 404 端点不可用）形状不变
func TestT612_BusinessNotFoundCodesUnchanged(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := t612DeviceGateway(t, backend.URL, &fakeSecretsCtx{secrets: map[string]string{}})

	body := t612Body
	hdrs := deviceHeaders(t612Secret, http.MethodPost, "/api/v1/device/records", body, time.Now())
	code, respBody, _ := t464Request(t, http.MethodPost, gw.URL+"/api/v1/device/records", body, hdrs)
	assert.Equal(t, http.StatusUnauthorized, code, "未注册设备的上报仍走验签那一格：%s", respBody)
	assert.Contains(t, respBody, `"code":20404`, respBody)

	full := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)
	code2, respBody2, _ := t464Request(t, http.MethodDelete, full.URL+"/api/v1/admin/technicians/T1", "", validBearer(t))
	assert.Equal(t, http.StatusNotFound, code2, "已注册但刻意不可用的端点仍是 404：%s", respBody2)
	assert.Contains(t, respBody2, `"code":404`, respBody2)
	assert.NotContains(t, respBody2, `"code":20405`, respBody2)
}

// 路径根本没注册那一格：报文形状保持原样（本卡不扩到 404 改造），只补上缺失的日志行
func TestT612_UnregisteredPathKeepsPlain404ButLogsRouteMiss(t *testing.T) {
	gwURL, received := t612RegisteredGateway(t)
	capture := t464CaptureLogs(t)

	code, body, respHdr := t464Request(t, http.MethodGet, gwURL+"/api/v1/device/time-sync", "", nil)

	assert.Equal(t, http.StatusNotFound, code, "未注册路径的状态码不在本卡改动面内")
	assert.Equal(t, "404 page not found", strings.TrimSpace(body), "未注册路径的响应体形状保持不变")
	assert.Contains(t, respHdr.Get("Content-Type"), "text/plain", "未注册路径仍走框架兜底，本卡不改这一格报文形状")
	assert.Empty(t, *received)

	entry := t612EntryByCode(t, capture, http.StatusNotFound)
	assert.Contains(t, t612String(t, entry, "message"), "no route registered")
	assert.Equal(t, "/api/v1/device/time-sync", t612String(t, entry, "path"))
}

// 判据 3（回归）：正常上报链路与校时链路不受影响，方法匹配的请求仍走原处理链
func TestT612_NormalReportAndTimeSyncUnaffected(t *testing.T) {
	gwURL, received := t612RegisteredGateway(t)

	hdrs := deviceHeaders(t612Secret, http.MethodPost, "/api/v1/device/records", t612Body, time.Now())
	code, body, _ := t464Request(t, http.MethodPost, gwURL+"/api/v1/device/records", t612Body, hdrs)
	require.Equal(t, http.StatusOK, code, "合法上报必须照旧放行：%s", body)
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "POST /api/v1/device/records")

	hdrsGet := deviceHeaders(t612Secret, http.MethodGet, "/api/v1/device/time", "", time.Now())
	codeGet, bodyGet, _ := t464Request(t, http.MethodGet, gwURL+"/api/v1/device/time", "", hdrsGet)
	assert.Equal(t, http.StatusOK, codeGet, "合法方法的校时必须照旧应答：%s", bodyGet)
	assert.Contains(t, bodyGet, "server_time")
	assert.Len(t, *received, 1, "校时仍由 gateway 本地应答，不多打后端")
}

// 生产接线那一格：兜底挂在 setupRouter 上，不是只挂在测试夹具里
func TestT612_ProductionWiring_SetupRouterCarriesGuard(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	r := setupRouter()
	assert.True(t, r.HandleMethodNotAllowed, "setupRouter 必须打开方法不匹配分支（gin v1.10.0 默认为 false）")

	hdrs := deviceHeaders(t612Secret, http.MethodPost, "/api/v1/device/time", "", time.Now())
	code, body, respHdr := t464Request(t, http.MethodPost, gw.URL+"/api/v1/device/time", "", hdrs)
	assert.Equal(t, http.StatusMethodNotAllowed, code, "生产路由下设备方法用错必须可辨识：%s", body)
	assert.Contains(t, body, `"code":20405`, body)
	assert.NotEmpty(t, respHdr.Get(HeaderRequestID), "关联号在兜底链路上同样要发")
}
