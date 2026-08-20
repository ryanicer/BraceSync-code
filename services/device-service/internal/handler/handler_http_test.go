// Package handler HTTP 层测试：路由/请求解析/统一响应体/错误码
//
// 对齐：docs/ §1（HTTP 层）· 架构 §3.5（code/message/data）
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/service"
	"github.com/bracesync/bracesync/services/device-service/internal/testutil"
)

type testEnv struct {
	router *gin.Engine
	store  *testutil.FakeStore
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	store := testutil.NewFakeStore()
	svc := service.NewDeviceService(store, enc)
	return &testEnv{router: New(svc).Router(), store: store}
}

// do 发起请求并解析统一响应体
func (e *testEnv) do(t *testing.T, method, path string, body any, headers map[string]string) (int, struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)

	var resp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if w.Code != http.StatusNotFound || w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	}
	return w.Code, resp
}

func TestHealthz(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRegisterHTTP_IdempotentAndNoSecret(t *testing.T) {
	env := newTestEnv(t)

	status, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-H-001"}, nil)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.CodeOK, resp.Code)

	var dev model.DeviceDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dev))
	assert.Equal(t, "DEV-H-001", dev.DeviceID)
	assert.Equal(t, model.DefaultModel, dev.Model)
	assert.Equal(t, model.StatusUnbound, dev.Status)
	assert.NotContains(t, string(resp.Data), "secret", "响应不得包含密钥字段")

	// 幂等重注册
	status, resp = env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-H-001"}, nil)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.CodeOK, resp.Code)

	// 非法 device_id → 20400
	status, resp = env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "x"}, nil)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

func TestBindUnbindHTTP(t *testing.T) {
	env := newTestEnv(t)
	env.store.AddPatient("P-100")
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-H-002"}, nil)
	require.Equal(t, model.CodeOK, resp.Code)

	// 绑定（操作人取 X-User-Id）
	status, resp := env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-002/bind",
		map[string]string{"patientId": "P-100"}, map[string]string{"X-User-Id": "TECH-8"})
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.CodeOK, resp.Code)
	assert.Equal(t, "null", string(resp.Data), "契约 bindDevice → ApiResponse<null>")

	// 绑定历史含 operator
	status, resp = env.do(t, http.MethodGet, "/api/v1/devices/DEV-H-002/bindings", nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(resp.Data), `"operatorId":"TECH-8"`)
	assert.Contains(t, string(resp.Data), `"reason":"install"`)

	// 绑定不存在的患者 → 10404
	status, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-002/bind",
		map[string]string{"patientId": "P-NONE"}, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeUserResNotFound, resp.Code)

	// 解绑（幂等）
	status, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-002/unbind", nil, nil)
	assert.Equal(t, http.StatusOK, status)
	status, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-002/unbind", nil, nil)
	assert.Equal(t, http.StatusOK, status, "重复解绑仍成功（幂等）")

	// 未注册设备 → 20404
	status, resp = env.do(t, http.MethodGet, "/api/v1/devices/DEV-NONE", nil, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeNotFound, resp.Code)
}

func TestInstallBaselineHTTP_FullFlow(t *testing.T) {
	env := newTestEnv(t)
	env.store.AddPatient("P-200")
	env.store.AddTech("TECH-9")

	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-H-003"}, nil)
	require.Equal(t, model.CodeOK, resp.Code)
	_, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-003/bind",
		map[string]string{"patientId": "P-200"}, map[string]string{"X-User-Id": "TECH-9"})
	require.Equal(t, model.CodeOK, resp.Code)

	// 建安装记录
	status, resp := env.do(t, http.MethodPost, "/api/v1/install-records", map[string]string{
		"deviceId": "DEV-H-003", "patientId": "P-200", "techId": "TECH-9",
	}, nil)
	require.Equal(t, http.StatusOK, status)
	var created struct {
		InstallID string `json:"installId"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &created))
	require.NotEmpty(t, created.InstallID)

	// offset_values 长度 19 → 20400
	status, resp = env.do(t, http.MethodPost, "/api/v1/baselines", map[string]any{
		"installId": created.InstallID, "offsetValues": make([]float32, 19),
	}, map[string]string{"X-User-Id": "TECH-9"})
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)

	// 合法 20 点（契约 saveBaseline → ApiResponse<null>）
	offsets := make([]float32, model.PointCount)
	for i := range offsets {
		offsets[i] = float32(i)
	}
	status, resp = env.do(t, http.MethodPost, "/api/v1/baselines", map[string]any{
		"installId": created.InstallID, "offsetValues": offsets,
		"notes": "matrix 校准完成", "signatureUrl": "cos://sig/1.png",
	}, map[string]string{"X-User-Id": "TECH-9"})
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.CodeOK, resp.Code)

	// installId 非法 → 20400
	status, resp = env.do(t, http.MethodPost, "/api/v1/baselines", map[string]any{
		"installId": "abc", "offsetValues": offsets,
	}, nil)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

func TestReportHTTP_StateMachine(t *testing.T) {
	env := newTestEnv(t)
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-H-004"}, nil)
	require.Equal(t, model.CodeOK, resp.Code)

	// 正常上报 → online
	status, resp := env.do(t, http.MethodPost, "/internal/devices/DEV-H-004/report", map[string]any{
		"timestamp": 1780000000, "fault_code": 0,
	}, nil)
	assert.Equal(t, http.StatusOK, status)

	status, resp = env.do(t, http.MethodGet, "/api/v1/devices/DEV-H-004", nil, nil)
	require.Equal(t, http.StatusOK, status)
	var dev model.DeviceDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dev))
	assert.Equal(t, model.StatusOnline, dev.Status)
	require.NotNil(t, dev.LastReportAt)

	// 故障上报 → abnormal
	status, _ = env.do(t, http.MethodPost, "/internal/devices/DEV-H-004/report", map[string]any{
		"timestamp": 1780000060, "fault_code": 2,
	}, nil)
	assert.Equal(t, http.StatusOK, status)
	_, resp = env.do(t, http.MethodGet, "/api/v1/devices/DEV-H-004", nil, nil)
	require.NoError(t, json.Unmarshal(resp.Data, &dev))
	assert.Equal(t, model.StatusAbnormal, dev.Status)

	// 未注册设备上报 → 20404
	status, resp = env.do(t, http.MethodPost, "/internal/devices/DEV-NONE/report", map[string]any{}, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeNotFound, resp.Code)
}

func TestWifiHTTP(t *testing.T) {
	env := newTestEnv(t)
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-H-005"}, nil)
	require.Equal(t, model.CodeOK, resp.Code)

	status, resp := env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-005/wifi",
		map[string]string{"ssid": "Home-WiFi"}, nil)
	assert.Equal(t, http.StatusOK, status)

	status, resp = env.do(t, http.MethodGet, "/api/v1/devices/DEV-H-005", nil, nil)
	require.Equal(t, http.StatusOK, status)
	var dev model.DeviceDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dev))
	require.NotNil(t, dev.WifiSsid)
	assert.Equal(t, "Home-WiFi", *dev.WifiSsid)

	// 空 ssid → 20400
	status, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-005/wifi",
		map[string]string{"ssid": ""}, nil)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

// doRaw 发送原始 body（非法 JSON 用例）
func (e *testEnv) doRaw(t *testing.T, method, path, body string) (int, int) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	return w.Code, resp.Code
}

func TestHTTP_InvalidBodies(t *testing.T) {
	env := newTestEnv(t)
	env.store.AddPatient("P-300")
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-H-006"}, nil)
	require.Equal(t, model.CodeOK, resp.Code)

	bad := "{not-json"
	for _, route := range []string{
		"/api/v1/devices",
		"/api/v1/devices/DEV-H-006/bind",
		"/api/v1/devices/DEV-H-006/wifi",
		"/api/v1/install-records",
		"/api/v1/baselines",
		"/internal/devices/DEV-H-006/report",
	} {
		httpStatus, code := env.doRaw(t, http.MethodPost, route, bad)
		assert.Equal(t, http.StatusBadRequest, httpStatus, route)
		assert.Equal(t, model.CodeInvalidParam, code, route)
	}

	// unbind 带非法 body（ContentLength>0）→ 20400
	httpStatus, code := env.doRaw(t, http.MethodPost, "/api/v1/devices/DEV-H-006/unbind", bad)
	assert.Equal(t, http.StatusBadRequest, httpStatus)
	assert.Equal(t, model.CodeInvalidParam, code)
}

// TestRebindHTTP 换绑路由：绑定互斥 409 → rebind 成功 → 历史 reason=rebind
func TestRebindHTTP(t *testing.T) {
	env := newTestEnv(t)
	env.store.AddPatient("P-500")
	env.store.AddPatient("P-501")
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-H-008"}, nil)
	require.Equal(t, model.CodeOK, resp.Code)
	_, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-008/bind",
		map[string]string{"patientId": "P-500"}, map[string]string{"X-User-Id": "TECH-X"})
	require.Equal(t, model.CodeOK, resp.Code)

	// Bind 自动换绑（对齐 Ella H6 契约）：第二绑成功
	status, resp := env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-008/bind",
		map[string]string{"patientId": "P-501"}, nil)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.CodeOK, resp.Code)

	// 显式 rebind 路由：先解绑，无 active binding 时 rebind → 409
	status, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-008/unbind", nil, nil)
	require.Equal(t, http.StatusOK, status)
	status, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-008/rebind",
		map[string]string{"patientId": "P-500"}, nil)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, model.CodeConflict, resp.Code)

	// 重新绑定后显式 rebind 成功
	_, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-008/bind",
		map[string]string{"patientId": "P-500"}, map[string]string{"X-User-Id": "TECH-X"})
	require.Equal(t, model.CodeOK, resp.Code)
	status, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-008/rebind",
		map[string]string{"patientId": "P-501"}, map[string]string{"X-User-Id": "TECH-X"})
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.CodeOK, resp.Code)

	// 绑定历史含 reason=rebind
	status, resp = env.do(t, http.MethodGet, "/api/v1/devices/DEV-H-008/bindings", nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(resp.Data), `"reason":"rebind"`)

	// 未注册设备的绑定历史 → 20404
	status, resp = env.do(t, http.MethodGet, "/api/v1/devices/DEV-NONE/bindings", nil, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeNotFound, resp.Code)

	// rebind 非法 body → 20400
	httpStatus, code := env.doRaw(t, http.MethodPost, "/api/v1/devices/DEV-H-008/rebind", "{not-json")
	assert.Equal(t, http.StatusBadRequest, httpStatus)
	assert.Equal(t, model.CodeInvalidParam, code)
}

func TestBindHTTP_OperatorFromBody(t *testing.T) {
	env := newTestEnv(t)
	env.store.AddPatient("P-400")
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-H-007"}, nil)
	require.Equal(t, model.CodeOK, resp.Code)

	// body.operatorId 优先于 X-User-Id
	status, resp := env.do(t, http.MethodPost, "/api/v1/devices/DEV-H-007/bind",
		map[string]string{"patientId": "P-400", "operatorId": "TECH-BODY"},
		map[string]string{"X-User-Id": "TECH-HEADER"})
	require.Equal(t, http.StatusOK, status)

	status, resp = env.do(t, http.MethodGet, "/api/v1/devices/DEV-H-007/bindings", nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(resp.Data), `"operatorId":"TECH-BODY"`)
}
