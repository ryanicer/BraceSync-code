// Package main — T032 全量路由分发实现侧测试（不与既有测试文件重叠）
//
// 覆盖 setupRouter 全量路由：JWT 组下 user/device/data/msg/alerts 五服务
// 新补全端点转发（路径/查询/方法/请求体透传），设备域路由不与 JWT 组冲突。
package main

import (
	"net/http"
	"net/http/httptest"
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

// TestFullRoutes_ProvisionKeyGuarded T091：provision-key 端点从裸组迁入 JWT 组，
// 叠加 JWT 鉴权 + tech/admin RBAC + per-user 限流。原 TestFullRoutes_ProvisionKeyBareGroup
// 断言的"裸组免 JWT"语义已变更（契约收紧），本测试覆盖新门禁。
func TestFullRoutes_ProvisionKeyGuarded(t *testing.T) {
	deviceBackend, deviceReceived := captureBackend(t)
	gw := startFullGateway(t, "http://127.0.0.1:1", deviceBackend.URL,
		"http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1", testJWTSecretMain)

	path := "/api/v1/devices/DEV-PROV/provision-key"

	// ① 无 token → 401（JWT 门禁）
	code, body := httpDoFull(t, http.MethodPost, gw.URL+path, "", nil)
	assert.Equal(t, http.StatusUnauthorized, code, "无 token 应 401")
	assert.Contains(t, body, `"code":401`)

	// ② patient token → 403（RBAC：仅 tech/admin 可领）
	code, body = httpDoFull(t, http.MethodPost, gw.URL+path, "", rbacToken(t, "patient"))
	assert.Equal(t, http.StatusForbidden, code, "patient 应 403")
	assert.Contains(t, body, `"code":403`)

	// ③ tech token → 200 + 身份头注入后端
	code, _ = httpDoFull(t, http.MethodPost, gw.URL+path, "", rbacToken(t, "technician"))
	require.Equal(t, http.StatusOK, code, "technician 应放行")

	// ④ admin token → 200
	code, _ = httpDoFull(t, http.MethodPost, gw.URL+path, "", validBearer(t))
	require.Equal(t, http.StatusOK, code, "admin 应放行")

	// 审计断言：后端收到网关注入的 X-User-Id / X-Role（领卡人/角色可追溯）
	require.Len(t, *deviceReceived, 2, "仅 tech+admin 请求触达后端")
	assert.Contains(t, (*deviceReceived)[0], "uid=U-technician", "X-User-Id 取 JWT sub")
	assert.Contains(t, (*deviceReceived)[0], "role=technician", "X-Role 取 JWT role")
	assert.Contains(t, (*deviceReceived)[1], "uid=ADM001")
	assert.Contains(t, (*deviceReceived)[1], "role=ROLE_ADMIN")
}

// TestFullRoutes_ProvisionKeyRateLimit T091：per-user 限流，超限 → 429。
// 通过 env PROVISION_KEY_RATE_LIMIT=2 降低阈值，第 3 次请求应被拒。
func TestFullRoutes_ProvisionKeyRateLimit(t *testing.T) {
	t.Setenv("PROVISION_KEY_RATE_LIMIT", "2")
	deviceBackend, deviceReceived := captureBackend(t)
	gw := startFullGateway(t, "http://127.0.0.1:1", deviceBackend.URL,
		"http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1", testJWTSecretMain)

	hdrs := rbacToken(t, "technician")
	path := "/api/v1/devices/DEV-RL/provision-key"

	// 前 2 次放行（桶容量=2）
	for i := 0; i < 2; i++ {
		code, _ := httpDoFull(t, http.MethodPost, gw.URL+path, "", hdrs)
		require.Equal(t, http.StatusOK, code, "第 %d 次应放行", i+1)
	}
	// 第 3 次 → 429
	code, body := httpDoFull(t, http.MethodPost, gw.URL+path, "", hdrs)
	assert.Equal(t, http.StatusTooManyRequests, code, "超限应 429")
	assert.Contains(t, body, `"code":429`)

	require.Len(t, *deviceReceived, 2, "被限流请求不得触达后端")
}

// TestFullRoutes_ProvisionKeyUnregisteredDevice_20404 未注册设备 → device-service 返回 20404，
// gateway 透传。
func TestFullRoutes_ProvisionKeyUnregisteredDevice_20404(t *testing.T) {
	// 模拟 device-service 对未注册设备返回 20404
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":20404,"message":"device not registered","data":null}`))
	}))
	t.Cleanup(backend.Close)

	gw := startFullGateway(t, "http://127.0.0.1:1", backend.URL,
		"http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1", testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodPost,
		gw.URL+"/api/v1/devices/DEV-GHOST/provision-key", "", rbacToken(t, "technician"))
	assert.Equal(t, http.StatusNotFound, code)
	assert.Contains(t, body, `"code":20404`)
}
