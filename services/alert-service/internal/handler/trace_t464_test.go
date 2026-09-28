// T464 双通道门禁（告警域）：trace 必带、关联号与日志同源、技术文本不漏进响应体、成功响应形状不变。
//
// 本域reject 用的是 Handler 自带的 logger（非全局），所以捕获要 SetLogger 注进去。
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
)

// t464CaptureLogs 捕获本用例期间的日志输出（交给 Handler 的 logger，用完不影响全局）。
func t464CaptureLogs(t *testing.T, h *Handler) *testhelper.LogCaptureHook {
	t.Helper()
	capture := testhelper.NewLogCaptureHook()
	h.SetLogger(zerolog.New(capture).With().Timestamp().Logger())
	return capture
}

// t464TraceOf 解出错误响应的 trace 并锁信封形态：trace.errorCode 与 code 同值、
// message 是该码的中文短句、响应头与 trace 同源。缺 trace 即判红。
func t464TraceOf(t *testing.T, expectedCode int, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Trace   *struct {
			ErrorCode int    `json:"errorCode"`
			RequestID string `json:"requestId"`
		} `json:"trace"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	require.NotNil(t, resp.Trace, "错误响应必须带 trace：%s", w.Body.String())
	require.Equal(t, expectedCode, resp.Code)
	assert.Equal(t, resp.Code, resp.Trace.ErrorCode, "trace.errorCode 必须与信封 code 同值")
	assert.Regexp(t, requestIDPattern, resp.Trace.RequestID, "关联号必须是白名单形状")
	assert.Equal(t, resp.Trace.RequestID, w.Header().Get(HeaderRequestID), "响应头要与 trace 同源")
	assert.Equal(t, userText(expectedCode), resp.Message, "响应体给用户的是中文短句")
	return resp.Trace.RequestID
}

// t464TechLogContains 按关联号反查该请求的技术日志行（响应体已不含技术文本，只能从这里核对）。
func t464TechLogContains(t *testing.T, capture *testhelper.LogCaptureHook, requestID, substr string) {
	t.Helper()
	for _, entry := range capture.Entries() {
		if got, _ := entry["request_id"].(string); got != requestID {
			continue
		}
		msg, _ := entry["message"].(string)
		if strings.Contains(msg, substr) {
			return
		}
	}
	t.Fatalf("request_id=%s 的日志行里没有 %q（技术文本必须落在日志通道）：%+v",
		requestID, substr, capture.Entries())
}

// TestT464_AlertDeniedListCarriesTraceCorrelatedWithLog 患者缺身份 403：
// 响应体只剩中文短句，"missing user identity" 进日志且与 trace.requestId 同源。
func TestT464_AlertDeniedListCarriesTraceCorrelatedWithLog(t *testing.T) {
	h := newPublicHandler(&fakePublicStore{})
	logs := t464CaptureLogs(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req.Header.Set(headerRole, "patient") // 无 X-User-Id
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	requestID := t464TraceOf(t, codeForbidden, w)
	assert.NotContains(t, w.Body.String(), "missing user identity", "技术文本不得漏进响应体")
	t464TechLogContains(t, logs, requestID, "missing user identity")
}

// TestT464_TechnicalTextNeverReachesBody 入参回显类技术文本（invalid type: X）只进日志。
func TestT464_TechnicalTextNeverReachesBody(t *testing.T) {
	h := newPublicHandler(&fakePublicStore{})
	logs := t464CaptureLogs(t, h)

	w := doGet(h, "/api/v1/alerts?type=BOOM_TYPE")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requestID := t464TraceOf(t, codeInvalidParam, w)

	assert.NotContains(t, w.Body.String(), "BOOM_TYPE", "被拒的取值不得回显给用户")
	t464TechLogContains(t, logs, requestID, "invalid type: BOOM_TYPE")
}

// TestT464_InboundRequestIDReused 网关透传的关联号原样回传（跨跳对齐的前提）。
func TestT464_InboundRequestIDReused(t *testing.T) {
	h := newPublicHandler(&fakePublicStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req.Header.Set(headerRole, "patient")
	req.Header.Set(HeaderRequestID, "alert-t464-0001")
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Equal(t, "alert-t464-0001", t464TraceOf(t, codeForbidden, w))
}

// TestT464_SuccessBodyKeepsShape 成功响应不得出现 trace 键（omitempty 后与改前逐字节同形）。
func TestT464_SuccessBodyKeepsShape(t *testing.T) {
	h := newPublicHandler(&fakePublicStore{})

	w := doGet(h, "/api/v1/alerts")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), `"trace"`, "成功响应不该带 trace：%s", w.Body.String())
	assert.Contains(t, w.Body.String(), `"message":"success"`)
}

// TestT464_GeneratedRequestIDShape 客户端未自报时由本域生成 16 位十六进制。
func TestT464_GeneratedRequestIDShape(t *testing.T) {
	h := newPublicHandler(&fakePublicStore{})

	w := doGet(h, "/api/v1/alerts?type=BOOM_TYPE")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Regexp(t, `^[0-9a-f]{16}$`, t464TraceOf(t, codeInvalidParam, w))
}
