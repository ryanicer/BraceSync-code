// T464 双通道门禁用例（设备域）：trace 必带、关联号与日志同源、成功响应形状不变。
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// t464Do 发一条可带自定义头的设备域请求，返回 recorder（门禁用例要读响应头与 trace）
func t464Do(t *testing.T, method, path, role, uid, reqID string) *httptest.ResponseRecorder {
	t.Helper()
	r, _, _ := t387Env(t)
	req := httptest.NewRequest(method, path, nil)
	if role != "" {
		req.Header.Set("X-Role", role)
	}
	if uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	if reqID != "" {
		req.Header.Set(HeaderRequestID, reqID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestT464_DeniedWriteCarriesTraceAndLogCorrelation(t *testing.T) {
	logs := t464CaptureLogs(t)
	w := t464Do(t, http.MethodPost, "/api/v1/devices/DEV-T464/wifi", roleDoctor, t387DoctorUID, "")

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	errorCode, requestID := t464TraceOf(t, w)
	assert.Equal(t, model.CodeForbidden, errorCode, "trace.errorCode 必须与信封 code 同值")
	assert.Len(t, requestID, 16, "requestId 是 16 位十六进制（8 字节随机）")
	assert.Equal(t, requestID, t464HeaderRequestID(w), "响应头 X-Request-Id 要与 trace 同源")

	// 判定①：响应体只给中文短句，角色名/动作名这类技术标识不得外漏。
	var body struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	assert.Equal(t, model.UserText(model.CodeForbidden), body.Message, "响应体 message 必须是中文短句")
	assert.NotContains(t, w.Body.String(), "may not", "技术文本不得留在响应体里")
	assert.NotContains(t, w.Body.String(), roleDoctor, "角色标识不得外漏（改道日志）")

	entry := t464EntryByRequestID(t, logs, requestID)
	assert.Equal(t, float64(model.CodeForbidden), entry["code"], "日志行的 code 要与响应同码，才能按关联号反查")
	assert.Equal(t, http.StatusForbidden, int(entry["http_status"].(float64)))
	assert.Contains(t, entry["message"], "may not", "角色/动作原因落在日志通道")
}

// TestT464_InboundRequestIDReused 网关透传的关联号必须原样回传（跨跳对齐的前提）。
func TestT464_InboundRequestIDReused(t *testing.T) {
	w := t464Do(t, http.MethodPost, "/api/v1/devices/DEV-T464/wifi", roleDoctor, t387DoctorUID, "gw-t464-0001")
	_, requestID := t464TraceOf(t, w)
	assert.Equal(t, "gw-t464-0001", requestID)
	assert.Equal(t, "gw-t464-0001", t464HeaderRequestID(w))
}

// TestT464_SuccessBodyKeepsShape 成功响应不得出现 trace 键（omitempty 后与改前逐字节同形）。
func TestT464_SuccessBodyKeepsShape(t *testing.T) {
	r, st, _ := t387Env(t)
	t387Install(t, st, t387Device)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+t387Device, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	t464BodyHasNoTrace(t, w)

	var resp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, model.CodeOK, resp.Code)
	assert.Equal(t, "success", resp.Message, "成功文案不改，避免连坐按 message 比对的老用例")
}
