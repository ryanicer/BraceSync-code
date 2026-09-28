// T464 双通道门禁（消息域）：trace 必带、关联号与日志同源、技术文本不漏进响应体、成功响应形状不变。
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/testhelper"
)

// t464CaptureLogs 捕获本用例期间的全局日志（结束自动还原；不还原会吞掉后续用例的日志行）。
func t464CaptureLogs(t *testing.T) *testhelper.LogCaptureHook {
	t.Helper()
	capture := testhelper.NewLogCaptureHook()
	original := log.Logger
	log.Logger = zerolog.New(capture).With().Timestamp().Logger()
	t.Cleanup(func() { log.Logger = original })
	return capture
}

// t464TraceOf 解出错误响应的 trace 并锁信封形态：trace.errorCode 与信封 code 同值、
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
	assert.Equal(t, model.UserText(expectedCode), resp.Message, "响应体给用户的是中文短句")
	return resp.Trace.RequestID
}

// t464TechLogContains 按关联号反查该请求的技术日志行（响应体已不含技术文本，只能从这里核对）。
func t464TechLogContains(t *testing.T, capture *testhelper.LogCaptureHook, requestID, substr string) {
	t.Helper()
	for _, entry := range capture.Entries() {
		if got, _ := entry["request_id"].(string); got != requestID {
			continue
		}
		if msg, _ := entry["message"].(string); strings.Contains(msg, substr) {
			return
		}
	}
	t.Fatalf("request_id=%s 的日志行里没有 %q（技术文本必须落在日志通道）：%+v",
		requestID, substr, capture.Entries())
}

// TestT464_MsgInternalAuthz401CarriesTraceCorrelatedWithLog 内部接口缺鉴权头：
// 响应体只给「仅限内部服务调用」，头名与缺失原因进日志。
func TestT464_MsgInternalAuthz401CarriesTraceCorrelatedWithLog(t *testing.T) {
	logs := t464CaptureLogs(t)
	f := newHTTPFixture(t)

	w, _ := f.do(t, http.MethodPost, "/internal/msg/send",
		`{"alertId":"A-1","type":"pressure_high","patientId":"P20260001","detail":"x"}`, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())

	requestID := t464TraceOf(t, model.CodeInternalDisabled, w)
	assert.NotContains(t, w.Body.String(), "X-Internal-Service", "技术文本不得漏进响应体")
	t464TechLogContains(t, logs, requestID, "missing X-Internal-Service header")
}

// TestT464_MsgForbiddenKeepsReasonInLog 水平越权 403：越权原因只进日志。
func TestT464_MsgForbiddenKeepsReasonInLog(t *testing.T) {
	logs := t464CaptureLogs(t)
	f := newHTTPFixture(t)

	w, _ := f.do(t, http.MethodGet, "/api/v1/patients/P9999999/wear-reminder", "", hdrSelf)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	requestID := t464TraceOf(t, model.CodeForbidden, w)
	assert.NotContains(t, w.Body.String(), "your own", "技术文本不得漏进响应体")
	t464TechLogContains(t, logs, requestID, "may only access your own resources")
}

// TestT464_InboundRequestIDReused 网关透传的关联号原样回传（跨跳对齐的前提）。
func TestT464_InboundRequestIDReused(t *testing.T) {
	f := newHTTPFixture(t)

	w, _ := f.do(t, http.MethodGet, "/api/v1/patients/P9999999/wear-reminder", "",
		withHdr(hdrSelf, map[string]string{HeaderRequestID: "msg-t464-0001"}))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Equal(t, "msg-t464-0001", t464TraceOf(t, model.CodeForbidden, w))
}

// TestT464_GeneratedRequestIDShape 客户端未自报时由本域生成 16 位十六进制。
func TestT464_GeneratedRequestIDShape(t *testing.T) {
	f := newHTTPFixture(t)

	w, _ := f.do(t, http.MethodGet, "/api/v1/patients/P9999999/wear-reminder", "", hdrSelf)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Regexp(t, `^[0-9a-f]{16}$`, t464TraceOf(t, model.CodeForbidden, w))
}

// TestT464_SuccessBodyKeepsShape 成功响应不得出现 trace 键（omitempty 后与改前逐字节同形）。
func TestT464_SuccessBodyKeepsShape(t *testing.T) {
	f := newHTTPFixture(t)

	w, _ := f.do(t, http.MethodGet, "/api/v1/patients/P20260001/wear-reminder", "", hdrSelf)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), `"trace"`, "成功响应不该带 trace：%s", w.Body.String())
	assert.Contains(t, w.Body.String(), `"message":"success"`)
}
