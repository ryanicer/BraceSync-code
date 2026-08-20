// Package main — T028 alerts 代理实现侧测试（不与测试专家 main_test.go 路径重叠）
//
// 代理链路走真实 HTTP（httptest.Server 双层：gateway 路由 + 模拟 alert-service），
// 避免 httptest.ResponseRecorder 不支持 http.CloseNotify 导致 ReverseProxy panic。
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const alertsEnvelope = `{"code":0,"message":"success","data":{"list":[],"total":0,"page":1,"pageSize":20}}`

func init() { gin.SetMode(gin.TestMode) }

// startBackend 模拟 alert-service：记录收到的请求并回统一响应体
func startBackend(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var received []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(alertsEnvelope))
	}))
	t.Cleanup(srv.Close)
	return srv, &received
}

// startGateway 以真实 HTTP 服务暴露 gateway 路由（target = alert-service 地址）
func startGateway(t *testing.T, target string) *httptest.Server {
	t.Helper()
	r := gin.New()
	registerAlertsProxy(r, target)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func httpDo(t *testing.T, method, target string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, nil)
	require.NoError(t, err)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

func TestProxy_GetAlerts_ForwardsQueryParams(t *testing.T) {
	backend, received := startBackend(t)
	gw := startGateway(t, backend.URL)

	code, body := httpDo(t, http.MethodGet,
		gw.URL+"/api/v1/alerts?patientId=P001&type=wear_interrupt&status=pending&page=2&pageSize=5")

	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, alertsEnvelope, body, "后端统一响应体原样透传")
	require.Len(t, *received, 1)
	assert.Equal(t,
		"GET /api/v1/alerts?patientId=P001&type=wear_interrupt&status=pending&page=2&pageSize=5",
		(*received)[0], "路径与查询参数原样转发")
}

func TestProxy_ProcessAlert_ForwardsPath(t *testing.T) {
	backend, received := startBackend(t)
	gw := startGateway(t, backend.URL)

	code, _ := httpDo(t, http.MethodPost, gw.URL+"/api/v1/alerts/42/process")

	require.Equal(t, http.StatusOK, code)
	require.Len(t, *received, 1)
	assert.Equal(t, "POST /api/v1/alerts/42/process", (*received)[0])
}

func TestProxy_BackendDown_502Envelope(t *testing.T) {
	// 指向已关闭的端口 → 连接失败走 ErrorHandler
	gw := startGateway(t, "http://127.0.0.1:1")

	code, body := httpDo(t, http.MethodGet, gw.URL+"/api/v1/alerts")

	assert.Equal(t, http.StatusBadGateway, code)
	assert.Contains(t, body, `"code":502`)
	assert.Contains(t, body, "alert-service unavailable")
}

func TestProxy_InvalidTarget_RoutesNotRegistered(t *testing.T) {
	gw := startGateway(t, "not a url")

	code, _ := httpDo(t, http.MethodGet, gw.URL+"/api/v1/alerts")
	assert.Equal(t, http.StatusNotFound, code, "非法目标不注册代理路由")
}

func TestProxy_UnmatchedRoutes(t *testing.T) {
	backend, received := startBackend(t)
	gw := startGateway(t, backend.URL)

	// 未代理的路径不转发
	code, _ := httpDo(t, http.MethodGet, gw.URL+"/api/v1/devices")
	assert.Equal(t, http.StatusNotFound, code)

	// GET 到 process 路由（方法不匹配）
	code, _ = httpDo(t, http.MethodGet, gw.URL+"/api/v1/alerts/1/process")
	assert.True(t, code == http.StatusNotFound || code == http.StatusMethodNotAllowed)
	assert.Empty(t, *received, "未匹配请求不触达后端")
}

func TestSetupRouter_EnvWiring(t *testing.T) {
	backend, received := startBackend(t)
	t.Setenv("ALERT_SERVICE_URL", backend.URL)
	// T039-H1：setupRouter 走真实鉴权链，需注入密钥 + 有效 token（fail-closed 后裸请求 401）
	t.Setenv("JWT_SECRET", testJWTSecretMain)

	router := setupRouter()
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	// 代理路由生效
	code, _ := httpDoFull(t, http.MethodGet, srv.URL+"/api/v1/alerts?page=1", "", validBearer(t))
	require.Equal(t, http.StatusOK, code)
	require.Len(t, *received, 1)
	assert.Equal(t, "GET /api/v1/alerts?page=1", (*received)[0])

	// 原有 healthz 不受影响（Recorder 直测即可，无代理参与）
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewAlertsProxy_TimeoutConfigured(t *testing.T) {
	target, err := url.Parse("http://alert-service:8080")
	require.NoError(t, err)
	proxy := newAlertsProxy(target)
	require.NotNil(t, proxy.Transport, "超时控制依赖自定义 Transport")
}
