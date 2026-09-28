// T464 报错双通道门禁（设备域）：技术文本走日志、响应体只剩中文短句 + trace。
//
// 本文件同时是给同包其它用例复用的替身：t464CaptureLogs 把全局 logger 换成捕获 writer，
// t464EntryByRequestID 按 request_id 反查该请求的日志行（日志是 JSON 行，字段可直接取）。
package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
)

// t464CaptureLogs 捕获本用例期间的日志输出（结束自动还原全局 logger）。
func t464CaptureLogs(t *testing.T) *testhelper.LogCaptureHook {
	t.Helper()
	capture := testhelper.NewLogCaptureHook()
	original := log.Logger
	log.Logger = zerolog.New(capture).With().Timestamp().Logger()
	t.Cleanup(func() { log.Logger = original })
	return capture
}

// t464EntryByRequestID 取带该关联号的日志行；同一请求多条时取第一条命中 message 的。
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

// t464AnyLogContains 是否有任一日志行的 message 含该子串（技术文本改道日志后的反查口）。
func t464AnyLogContains(capture *testhelper.LogCaptureHook, needle string) bool {
	for _, entry := range capture.Entries() {
		if msg, _ := entry["message"].(string); strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// t464TechnicalLog 取本用例期间唯一那条带 request_id 的技术日志行的 message。
// 一条都没有 = 技术文本被丢了；多于一条 = 本用例没控制住请求数，两种都判红。
func t464TechnicalLog(t *testing.T, capture *testhelper.LogCaptureHook) string {
	t.Helper()
	var found []string
	for _, entry := range capture.Entries() {
		if _, hasRID := entry["request_id"]; hasRID {
			msg, _ := entry["message"].(string)
			found = append(found, msg)
		}
	}
	require.Len(t, found, 1, "期望恰好一条带 request_id 的技术日志行：%+v", found)
	return found[0]
}

// t464TraceOf 从响应体里解出 trace 两键（缺失即判红：T464 要求错误响应必带 trace）。
func t464TraceOf(t *testing.T, w *httptest.ResponseRecorder) (errorCode int, requestID string) {
	t.Helper()
	var resp struct {
		Trace *struct {
			ErrorCode int    `json:"errorCode"`
			RequestID string `json:"requestId"`
		} `json:"trace"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	require.NotNil(t, resp.Trace, "错误响应必须带 trace：%s", w.Body.String())
	return resp.Trace.ErrorCode, resp.Trace.RequestID
}

// t464HeaderRequestID 读响应头里的关联号（应与 trace.requestId 同值）。
func t464HeaderRequestID(w *httptest.ResponseRecorder) string {
	return w.Header().Get(HeaderRequestID)
}

// t464BodyHasNoTrace 成功响应不得出现 trace 键（omitempty 后应与改前逐字节一致）。
func t464BodyHasNoTrace(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.NotContains(t, w.Body.String(), `"trace"`, "成功响应不该带 trace：%s", w.Body.String())
}
