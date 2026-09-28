// T464 双通道门禁（文件域）：错误响应必带 trace、trace 与日志同源、技术文本不进响应体、
// 成功响应形状不变（trace 用 omitempty，改前后逐字节同形）。
package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
)

// t464CaptureLogs 捕获本用例期间的全局日志（结束自动还原）。
func t464CaptureLogs(t *testing.T) *testhelper.LogCaptureHook {
	t.Helper()
	capture := testhelper.NewLogCaptureHook()
	original := log.Logger
	log.Logger = zerolog.New(capture).With().Timestamp().Logger()
	t.Cleanup(func() { log.Logger = original })
	return capture
}

// t464EntryByRequestID 按关联号反查该请求的日志行（响应体 trace 与日志同源才算双通道成立）。
func t464EntryByRequestID(t *testing.T, capture *testhelper.LogCaptureHook, requestID string) map[string]interface{} {
	t.Helper()
	for _, entry := range capture.Entries() {
		if got, _ := entry["request_id"].(string); got == requestID {
			return entry
		}
	}
	t.Fatalf("没有 request_id=%s 的日志行 ⇒ 技术文本没进日志通道；捕获到 %+v", requestID, capture.Entries())
	return nil
}

// t464TraceOfBody 从已解析的响应体里取 trace 两键（缺 trace 即判红）。
func t464TraceOfBody(t *testing.T, body map[string]interface{}) (float64, string) {
	t.Helper()
	trace, ok := body["trace"].(map[string]interface{})
	require.True(t, ok, "错误响应必须带 trace：%v", body)
	requestID, isString := trace["requestId"].(string)
	require.True(t, isString, "trace.requestId 必须是字符串：%v", trace)
	return trace["errorCode"].(float64), requestID
}

// t464TraceMatchesBody 锁定 trace.errorCode 与信封 code 同值、关联号是 16 位十六进制。
func t464TraceMatchesBody(t *testing.T, body map[string]interface{}, wantCode int) {
	t.Helper()
	errorCode, requestID := t464TraceOfBody(t, body)
	assert.Equal(t, float64(wantCode), errorCode, "trace.errorCode 必须与信封 code 同值")
	assert.Len(t, requestID, 16, "requestId 是 16 位十六进制（8 字节随机）")
}

// t464BodyJSON 把已解析的响应体重新序列化，供「某串是否出现在体里」类判定
// （技术文本/trace 键的漏出检查按整段原文读更容易核对）。
func t464BodyJSON(t *testing.T, body map[string]interface{}) string {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return string(raw)
}

// t464RawRequest 发一次可读到响应头的请求（trace 与 X-Request-Id 必须同源）。
func t464RawRequest(t *testing.T, method, url, userID, role, requestID string) (int, http.Header, map[string]interface{}) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	if userID != "" {
		req.Header.Set("X-User-Id", userID)
	}
	if role != "" {
		req.Header.Set("X-Role", role)
	}
	if requestID != "" {
		req.Header.Set(HeaderRequestID, requestID)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &parsed), "body=%s", raw)
	return resp.StatusCode, resp.Header, parsed
}

func TestT464_FileDeniedReadCarriesTraceCorrelatedWithLog(t *testing.T) {
	logs := t464CaptureLogs(t)
	srv := t378Srv(t, t378Store())
	defer srv.Close()

	code, header, body := t464RawRequest(t, http.MethodGet, srv.URL+"/api/v1/files/F-OTHER", t378Doc, "ROLE_DOCTOR", "")
	require.Equal(t, http.StatusForbidden, code, "%v", body)

	errorCode, requestID := t464TraceOfBody(t, body)
	assert.Equal(t, float64(ErrorCodeForbidden), errorCode, "trace.errorCode 必须与信封 code 同值")
	assert.Len(t, requestID, 16, "requestId 是 16 位十六进制（8 字节随机）")
	assert.Equal(t, requestID, header.Get(HeaderRequestID), "响应头要与 trace 同源")

	entry := t464EntryByRequestID(t, logs, requestID)
	assert.Equal(t, float64(ErrorCodeForbidden), entry["code"])
	assert.Equal(t, http.StatusForbidden, int(entry["http_status"].(float64)))
	assert.Equal(t, http.MethodGet, entry["method"])
	assert.Contains(t, entry["path"], "/api/v1/files/")
	assert.Contains(t, entry["message"], "not allowed", "技术文本落在日志通道")
	assert.Equal(t, UserText(ErrorCodeForbidden), body["message"])
}

// TestT464_TechnicalTextNeverReachesBody 500 分支：底层报错措辞只进日志，用户拿到兜底中文。
func TestT464_TechnicalTextNeverReachesBody(t *testing.T) {
	logs := t464CaptureLogs(t)
	srv := setupFailServer(failStore{})
	defer srv.Close()

	code, _, body := t464RawRequest(t, http.MethodGet, srv.URL+"/api/v1/files/file_x", "T0001", "technician", "")
	require.Equal(t, http.StatusInternalServerError, code, "%v", body)
	assert.Equal(t, UserText(ErrorCodeInternal), body["message"])
	assert.NotContains(t, t464BodyJSON(t, body), "error retrieving", "技术文本不得进响应体")

	_, requestID := t464TraceOfBody(t, body)
	entry := t464EntryByRequestID(t, logs, requestID)
	assert.Contains(t, entry["message"], "error retrieving file")
}

// TestT464_InboundRequestIDReused 网关透传的关联号原样回传（跨跳对齐的前提）。
func TestT464_InboundRequestIDReused(t *testing.T) {
	srv := t378Srv(t, t378Store())
	defer srv.Close()

	_, header, body := t464RawRequest(t, http.MethodGet, srv.URL+"/api/v1/files/F-OTHER", t378Doc, "ROLE_DOCTOR", "gw-t464-0003")
	_, requestID := t464TraceOfBody(t, body)
	assert.Equal(t, "gw-t464-0003", requestID)
	assert.Equal(t, "gw-t464-0003", header.Get(HeaderRequestID))
}

// TestT464_SuccessBodyKeepsShape 成功响应不得出现 trace 键。
func TestT464_SuccessBodyKeepsShape(t *testing.T) {
	srv := t378Srv(t, t378Store())
	defer srv.Close()

	code, header, body := t464RawRequest(t, http.MethodGet, srv.URL+"/api/v1/files/F-OWN", t378Doc, "ROLE_DOCTOR", "gw-t464-0004")
	require.Equal(t, http.StatusOK, code, "%v", body)
	assert.NotContains(t, t464BodyJSON(t, body), `"trace"`, "成功响应不该带 trace")
	assert.Equal(t, "gw-t464-0004", header.Get(HeaderRequestID))
}
