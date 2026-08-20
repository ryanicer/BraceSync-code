// Package main — T030 admin 域代理实现侧测试（不与测试专家/既有 proxy_test.go 路径重叠）
//
// 双层真实 HTTP（httptest.Server：gateway 路由 + 模拟后端），覆盖：
// 四服务路由转发（路径/查询/方法透传）、502 兜底、非法目标跳过、env 覆盖。
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const adminEnvelope = `{"code":0,"message":"success","data":[]}`

// startAdminGateway 仅注册 T030 admin 代理（env 指向各自模拟后端）
func startAdminGateway(t *testing.T, userURL, deviceURL, dataURL, msgURL string) *httptest.Server {
	t.Helper()
	t.Setenv("USER_SERVICE_URL", userURL)
	t.Setenv("DEVICE_SERVICE_URL", deviceURL)
	t.Setenv("DATA_SERVICE_URL", dataURL)
	t.Setenv("MSG_SERVICE_URL", msgURL)
	r := gin.New()
	registerAdminProxies(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func TestAdminProxy_UserServiceRoutes(t *testing.T) {
	backend, received := startBackend(t)
	gw := startAdminGateway(t, backend.URL, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1")

	cases := []struct {
		method, path, want string
	}{
		{http.MethodPost, "/api/v1/auth/login", "POST /api/v1/auth/login"},
		{http.MethodGet, "/api/v1/admin/patients?keyword=x&page=2", "GET /api/v1/admin/patients?keyword=x&page=2"},
		{http.MethodGet, "/api/v1/admin/patients/P001", "GET /api/v1/admin/patients/P001"},
		{http.MethodGet, "/api/v1/teams", "GET /api/v1/teams"},
		{http.MethodGet, "/api/v1/teams/TEAM01/members", "GET /api/v1/teams/TEAM01/members"},
		{http.MethodGet, "/api/v1/doctors", "GET /api/v1/doctors"},
		{http.MethodGet, "/api/v1/technicians?page=1", "GET /api/v1/technicians?page=1"},
		{http.MethodPost, "/api/v1/admin/technicians", "POST /api/v1/admin/technicians"},
		{http.MethodPut, "/api/v1/admin/technicians/T001", "PUT /api/v1/admin/technicians/T001"},
		{http.MethodPost, "/api/v1/technicians/T001/toggle", "POST /api/v1/technicians/T001/toggle"},
		{http.MethodGet, "/api/v1/feedbacks", "GET /api/v1/feedbacks"},
		{http.MethodPost, "/api/v1/feedbacks/9/process", "POST /api/v1/feedbacks/9/process"},
		{http.MethodGet, "/api/v1/patients/P001/orthosis-plans", "GET /api/v1/patients/P001/orthosis-plans"},
		{http.MethodPost, "/api/v1/patients/P001/orthosis-plans", "POST /api/v1/patients/P001/orthosis-plans"},
		{http.MethodGet, "/api/v1/patients/P001/feeling-logs", "GET /api/v1/patients/P001/feeling-logs"},
		{http.MethodPost, "/api/v1/feeling-logs/5/reply", "POST /api/v1/feeling-logs/5/reply"},
		{http.MethodGet, "/api/v1/admin/roles", "GET /api/v1/admin/roles"},
		{http.MethodGet, "/api/v1/admin/roles/ROLE_ADMIN/permissions", "GET /api/v1/admin/roles/ROLE_ADMIN/permissions"},
		{http.MethodPut, "/api/v1/admin/roles/ROLE_ADMIN/permissions", "PUT /api/v1/admin/roles/ROLE_ADMIN/permissions"},
		{http.MethodGet, "/api/v1/admin/settings", "GET /api/v1/admin/settings"},
		{http.MethodPut, "/api/v1/admin/settings", "PUT /api/v1/admin/settings"},
	}
	for _, tc := range cases {
		code, _ := httpDo(t, tc.method, gw.URL+tc.path)
		require.Equal(t, http.StatusOK, code, tc.path)
	}
	require.Len(t, *received, len(cases))
	for i, tc := range cases {
		assert.Equal(t, tc.want, (*received)[i], "路径/查询/方法原样透传")
	}
}

func TestAdminProxy_OtherServices(t *testing.T) {
	deviceBackend, deviceReceived := startBackend(t)
	dataBackend, dataReceived := startBackend(t)
	msgBackend, msgReceived := startBackend(t)
	gw := startAdminGateway(t, "http://127.0.0.1:1", deviceBackend.URL, dataBackend.URL, msgBackend.URL)

	// device-service：设备列表 + 安装记录列表（T030 #3）
	code, _ := httpDo(t, http.MethodGet, gw.URL+"/api/v1/devices?keyword=PRS")
	require.Equal(t, http.StatusOK, code)
	code, _ = httpDo(t, http.MethodGet, gw.URL+"/api/v1/install-records?page=1")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, *deviceReceived, 2)
	assert.Equal(t, "GET /api/v1/devices?keyword=PRS", (*deviceReceived)[0])
	assert.Equal(t, "GET /api/v1/install-records?page=1", (*deviceReceived)[1])

	// data-service：realtime / records / health-reports
	code, _ = httpDo(t, http.MethodGet, gw.URL+"/api/v1/patients/P001/realtime")
	require.Equal(t, http.StatusOK, code)
	code, _ = httpDo(t, http.MethodGet, gw.URL+"/api/v1/patients/P001/records?period=day&date=2026-08-08")
	require.Equal(t, http.StatusOK, code)
	code, _ = httpDo(t, http.MethodGet, gw.URL+"/api/v1/patients/P001/health-reports")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, *dataReceived, 3)
	assert.Equal(t, "GET /api/v1/patients/P001/health-reports", (*dataReceived)[2])

	// msg-service：通知规则与发送记录
	code, _ = httpDo(t, http.MethodGet, gw.URL+"/api/v1/admin/notify-rules")
	require.Equal(t, http.StatusOK, code)
	code, _ = httpDo(t, http.MethodPut, gw.URL+"/api/v1/admin/notify-rules/pressure_high")
	require.Equal(t, http.StatusOK, code)
	code, _ = httpDo(t, http.MethodGet, gw.URL+"/api/v1/admin/notification-logs?status=sent")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, *msgReceived, 3)
	assert.Equal(t, "PUT /api/v1/admin/notify-rules/pressure_high", (*msgReceived)[1])
}

func TestAdminProxy_BackendDown_502(t *testing.T) {
	gw := startAdminGateway(t, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1")

	code, body := httpDo(t, http.MethodGet, gw.URL+"/api/v1/admin/patients")
	assert.Equal(t, http.StatusBadGateway, code)
	assert.Contains(t, body, `"code":502`)
	assert.Contains(t, body, "user-service unavailable")

	code, body = httpDo(t, http.MethodGet, gw.URL+"/api/v1/devices")
	assert.Equal(t, http.StatusBadGateway, code)
	assert.Contains(t, body, "device-service unavailable")
}

func TestAdminProxy_InvalidTarget_RoutesSkipped(t *testing.T) {
	r := gin.New()
	t.Setenv("USER_SERVICE_URL", "not a url") // 非法 → 该服务路由不注册
	t.Setenv("DEVICE_SERVICE_URL", "http://device-service:8082")
	t.Setenv("DATA_SERVICE_URL", "http://data-service:8083")
	t.Setenv("MSG_SERVICE_URL", "http://msg-service:8086")
	registerAdminProxies(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	code, _ := httpDo(t, http.MethodGet, srv.URL+"/api/v1/admin/patients")
	assert.Equal(t, http.StatusNotFound, code, "非法目标服务的路由不注册")
}

func TestAdminProxy_UnmatchedRoutes(t *testing.T) {
	backend, received := startBackend(t)
	gw := startAdminGateway(t, backend.URL, backend.URL, backend.URL, backend.URL)

	// 未代理路径
	code, _ := httpDo(t, http.MethodGet, gw.URL+"/api/v1/unknown")
	assert.Equal(t, http.StatusNotFound, code)
	// 方法不匹配（DELETE 未注册）
	code, _ = httpDo(t, http.MethodDelete, gw.URL+"/api/v1/admin/settings")
	assert.True(t, code == http.StatusNotFound || code == http.StatusMethodNotAllowed)
	assert.Empty(t, *received, "未匹配请求不触达后端")
}

func TestRegisterServiceRoutes_EnvelopePassthrough(t *testing.T) {
	// 后端统一响应体原样透传（不做二次包装）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(adminEnvelope))
	}))
	t.Cleanup(srv.Close)
	gw := startAdminGateway(t, srv.URL, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1")

	code, body := httpDo(t, http.MethodGet, gw.URL+"/api/v1/teams")
	require.Equal(t, http.StatusOK, code)
	assert.True(t, strings.Contains(body, `"code":0`))
}
