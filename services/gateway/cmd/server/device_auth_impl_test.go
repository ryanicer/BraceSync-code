// Package main — T032 设备验签中间件实现侧测试（不与既有测试文件重叠）
//
// 覆盖：合法签名放行（body/X-Device-Id 原样透传）、篡改签名 401/20401、
// 时间窗外 401/20402、未注册设备 401/20404、密钥服务不可用 502、校时接口。
package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/gateway/internal/auth"
)

// fakeSecretsCtx 内存密钥提供器（可控错误注入）
type fakeSecretsCtx struct {
	secrets map[string]string
	err     error
}

// 编译期接口断言
var _ SecretProvider = (*fakeSecretsCtx)(nil)

func (f *fakeSecretsCtx) GetDeviceSecret(_ context.Context, deviceID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	s, ok := f.secrets[deviceID]
	if !ok {
		return "", ErrDeviceNotRegistered
	}
	return s, nil
}

// startDeviceGateway 仅注册设备域路由（验签组），data-service 指向模拟后端
func startDeviceGateway(t *testing.T, dataURL string, secrets SecretProvider) *httptest.Server {
	t.Helper()
	t.Setenv("DATA_SERVICE_URL", dataURL)
	r := gin.New()
	registerDeviceReportRoutes(r, newGatewayAuth("", secrets))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// deviceHeaders 构造设备签名头（对齐 device-simulator sign.go）
func deviceHeaders(secret, method, path, body string, ts time.Time) map[string]string {
	return map[string]string{
		"X-Device-Id": "DEV-SIG-001",
		"X-Timestamp": strconv.FormatInt(ts.Unix(), 10),
		"X-Signature": auth.HMACSHA256(secret, auth.BuildSignString(method, path, body, ts)),
	}
}

func TestDeviceSig_ValidSignature_ForwardsBodyAndIdentity(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": "dev-secret-abc"}})

	body := `{"device_id":"DEV-SIG-001","pressures":[10,20]}`
	hdrs := deviceHeaders("dev-secret-abc", "POST", "/api/v1/device/records", body, time.Now())
	code, respBody := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body, hdrs)

	require.Equal(t, http.StatusOK, code, "合法签名应放行：%s", respBody)
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "POST /api/v1/device/records")
	assert.Contains(t, (*received)[0], "dev=DEV-SIG-001", "验签通过注入 X-Device-Id")
	assert.Contains(t, (*received)[0], "body="+body, "请求体原样透传 data-service")
}

func TestDeviceSig_TamperedSignature_401_20401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": "dev-secret-abc"}})

	body := `{"pressures":[1]}`
	hdrs := deviceHeaders("dev-secret-abc", "POST", "/api/v1/device/records", body, time.Now())
	hdrs["X-Signature"] = "deadbeef" // 伪造签名
	code, respBody := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body, hdrs)

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Contains(t, respBody, `"code":20401`)
	assert.Empty(t, *received, "验签失败不得触达后端")
}

func TestDeviceSig_TamperedBody_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": "dev-secret-abc"}})

	// 用原始 body 签名，发送篡改后的 body
	orig := `{"pressures":[1]}`
	hdrs := deviceHeaders("dev-secret-abc", "POST", "/api/v1/device/records", orig, time.Now())
	code, _ := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", `{"pressures":[999]}`, hdrs)

	assert.Equal(t, http.StatusUnauthorized, code, "篡改 body → 签名不一致")
	assert.Empty(t, *received)
}

func TestDeviceSig_StaleTimestamp_401_20402(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": "dev-secret-abc"}})

	body := `{"pressures":[1]}`
	stale := time.Now().Add(-10 * time.Minute)
	hdrs := deviceHeaders("dev-secret-abc", "POST", "/api/v1/device/records", body, stale)
	code, respBody := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body, hdrs)

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Contains(t, respBody, `"code":20402`, "时间窗外 → 20402 时钟异常")
}

func TestDeviceSig_MissingHeaders_401(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": "dev-secret-abc"}})

	// 无 X-Device-Id
	code, respBody := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", `{}`, nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Contains(t, respBody, `"code":20401`)

	// 有设备 ID 但无签名头
	code, _ = httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", `{}`,
		map[string]string{"X-Device-Id": "DEV-SIG-001"})
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Empty(t, *received)
}

func TestDeviceSig_UnregisteredDevice_401_20404(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL, &fakeSecretsCtx{secrets: map[string]string{}})

	body := `{}`
	hdrs := deviceHeaders("whatever", "POST", "/api/v1/device/records", body, time.Now())
	hdrs["X-Device-Id"] = "DEV-GHOST"
	code, respBody := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body, hdrs)

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Contains(t, respBody, `"code":20404`, "未注册设备 → 20404")
	assert.Empty(t, *received)
}

func TestDeviceSig_SecretServiceDown_502(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{err: errors.New("connection refused")})

	body := `{}`
	hdrs := deviceHeaders("s", "POST", "/api/v1/device/records", body, time.Now())
	code, respBody := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body, hdrs)

	assert.Equal(t, http.StatusBadGateway, code, "密钥服务不可用 → 502 兜底")
	assert.Contains(t, respBody, `"code":502`)
	assert.Empty(t, *received)
}

func TestDeviceSig_BatchRoute_AlsoVerified(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": "dev-secret-abc"}})

	// 未签名 → 401
	code, _ := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records/batch", `{"frames":[]}`, nil)
	assert.Equal(t, http.StatusUnauthorized, code)

	// 合法签名 → 放行
	body := `{"device_id":"DEV-SIG-001","frames":[]}`
	hdrs := deviceHeaders("dev-secret-abc", "POST", "/api/v1/device/records/batch", body, time.Now())
	code, _ = httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records/batch", body, hdrs)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, *received, 1)
	assert.Contains(t, (*received)[0], "POST /api/v1/device/records/batch")
}

func TestDeviceSig_TimeEndpoint_LocalAnswer(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": "dev-secret-abc"}})

	// 校时接口（协议 §4.3）：空体签名，gateway 本地应答不打后端
	hdrs := deviceHeaders("dev-secret-abc", "GET", "/api/v1/device/time", "", time.Now())
	code, respBody := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/device/time", "", hdrs)
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, respBody, "server_time")
	assert.Empty(t, *received, "校时由 gateway 本地应答")

	// 未签名校时同样拒绝
	code, _ = httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/device/time", "", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
}
