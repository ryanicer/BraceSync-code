// Package main — T032 全量路由分发实现侧测试（不与既有测试文件重叠）
//
// 覆盖 setupRouter 全量路由：JWT 组下 user/device/data/msg/alerts 五服务
// 新补全端点转发（路径/查询/方法/请求体透传），设备域路由不与 JWT 组冲突。
package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullRoutes_DeviceManageRoutes(t *testing.T) {
	deviceBackend, deviceReceived := captureBackend(t)
	userBackend, _ := captureBackend(t)
	gw := startFullGateway(t, userBackend.URL, deviceBackend.URL,
		"http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1", testJWTSecretMain)
	hdrs := validBearer(t)

	cases := []struct {
		method, path, body, want string
	}{
		{http.MethodPost, "/api/v1/devices", `{"deviceId":"DEV-1","model":"ML05"}`, "POST /api/v1/devices"},
		{http.MethodGet, "/api/v1/devices/DEV-1", "", "GET /api/v1/devices/DEV-1"},
		{http.MethodGet, "/api/v1/devices/DEV-1/bindings", "", "GET /api/v1/devices/DEV-1/bindings"},
		{http.MethodPost, "/api/v1/devices/DEV-1/bind", `{"patientId":"P001"}`, "POST /api/v1/devices/DEV-1/bind"},
		{http.MethodPost, "/api/v1/devices/DEV-1/rebind", `{"patientId":"P002"}`, "POST /api/v1/devices/DEV-1/rebind"},
		{http.MethodPost, "/api/v1/devices/DEV-1/unbind", "", "POST /api/v1/devices/DEV-1/unbind"},
		{http.MethodPost, "/api/v1/devices/DEV-1/wifi", `{"ssid":"Home"}`, "POST /api/v1/devices/DEV-1/wifi"},
		{http.MethodPost, "/api/v1/install-records", `{"deviceId":"DEV-1"}`, "POST /api/v1/install-records"},
		{http.MethodPost, "/api/v1/baselines", `{"installId":"1"}`, "POST /api/v1/baselines"},
		{http.MethodGet, "/api/v1/devices?keyword=PRS&page=2", "", "GET /api/v1/devices?keyword=PRS&page=2"},
		{http.MethodGet, "/api/v1/install-records?page=1", "", "GET /api/v1/install-records?page=1"},
	}
	for _, tc := range cases {
		code, _ := httpDoFull(t, tc.method, gw.URL+tc.path, tc.body, hdrs)
		require.Equal(t, http.StatusOK, code, tc.path)
	}
	require.Len(t, *deviceReceived, len(cases))
	for i, tc := range cases {
		assert.Contains(t, (*deviceReceived)[i], tc.want, "路径/查询/方法透传 device-service")
	}
	// 请求体透传（bind body）
	assert.Contains(t, (*deviceReceived)[3], `body={"patientId":"P001"}`)
}

func TestFullRoutes_AllServicesReachable(t *testing.T) {
	userBackend, userReceived := captureBackend(t)
	deviceBackend, _ := captureBackend(t)
	dataBackend, dataReceived := captureBackend(t)
	msgBackend, msgReceived := captureBackend(t)
	alertBackend, alertReceived := captureBackend(t)
	gw := startFullGateway(t, userBackend.URL, deviceBackend.URL, dataBackend.URL,
		msgBackend.URL, alertBackend.URL, testJWTSecretMain)
	hdrs := validBearer(t)

	// 各服务抽一个端点验证全链路可达（含查询参数透传）
	flows := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/technicians?page=1"},                  // user-service
		{http.MethodGet, "/api/v1/patients/P001/records?period=day"},    // data-service
		{http.MethodGet, "/api/v1/admin/notification-logs?status=sent"}, // msg-service
		{http.MethodGet, "/api/v1/alerts?status=pending"},               // alert-service（T028 保持）
		{http.MethodPost, "/api/v1/alerts/7/process"},                   // alert-service
	}
	for _, f := range flows {
		code, _ := httpDoFull(t, f.method, gw.URL+f.path, "", hdrs)
		require.Equal(t, http.StatusOK, code, f.path)
	}
	require.Len(t, *userReceived, 1)
	assert.Contains(t, (*userReceived)[0], "GET /api/v1/technicians?page=1")
	require.Len(t, *dataReceived, 1)
	assert.Contains(t, (*dataReceived)[0], "GET /api/v1/patients/P001/records?period=day")
	require.Len(t, *msgReceived, 1)
	assert.Contains(t, (*msgReceived)[0], "GET /api/v1/admin/notification-logs?status=sent")
	require.Len(t, *alertReceived, 2)
	assert.Contains(t, (*alertReceived)[0], "GET /api/v1/alerts?status=pending")
	assert.Contains(t, (*alertReceived)[1], "POST /api/v1/alerts/7/process")
}

func TestFullRoutes_DeviceReportNotJWT(t *testing.T) {
	// 设备上报路由不经 JWT：未带 Bearer 也不返回 JWT 401（转由验签拒绝）
	dataBackend, _ := captureBackend(t)
	deviceBackend, _ := captureBackend(t)
	gw := startFullGateway(t, deviceBackend.URL, deviceBackend.URL, dataBackend.URL,
		deviceBackend.URL, deviceBackend.URL, testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", `{}`, nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Contains(t, body, `"code":20401`, "设备域拒绝走验签错误码而非 JWT 401")
}

func TestFullRoutes_UnmatchedPath_404(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	code, _ := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/unknown-route", "", validBearer(t))
	assert.Equal(t, http.StatusNotFound, code)
	assert.Empty(t, *received)
}

func TestFullRoutes_BackendDown_502WithJWT(t *testing.T) {
	// 鉴权通过后后端不可用 → 502 兜底（鉴权与代理错误分层）
	gw := startFullGateway(t, "http://127.0.0.1:1", "http://127.0.0.1:1",
		"http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1", testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "", validBearer(t))
	assert.Equal(t, http.StatusBadGateway, code)
	assert.Contains(t, body, "user-service unavailable")
}
