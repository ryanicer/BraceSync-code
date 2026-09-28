// T464 双通道门禁（数据域）：trace 必带、关联号与日志同源、成功响应形状不变、技术文本不漏进响应体。
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/testhelper"
)

// t350TraceRID trace.requestId 每请求随机，按响应体形状比对的用例比对前先抹掉它。
var t350TraceRID = regexp.MustCompile(`"requestId":"[A-Za-z0-9_-]{1,64}"`)

// t464CaptureLogs 捕获本用例期间的全局日志（结束自动还原）。
func t464CaptureLogs(t *testing.T) *testhelper.LogCaptureHook {
	t.Helper()
	capture := testhelper.NewLogCaptureHook()
	original := log.Logger
	log.Logger = zerolog.New(capture).With().Timestamp().Logger()
	t.Cleanup(func() { log.Logger = original })
	return capture
}

// t464EntryByRequestID 按关联号反查该请求的日志行（响应体的 trace 与日志同源，才算双通道成立）。
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

// t464TraceOf 解出错误响应的 trace 两键（缺 trace 即判红）。
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

func TestT464_DeniedReadCarriesTraceCorrelatedWithLog(t *testing.T) {
	logs := t464CaptureLogs(t)
	srv := newTestServer(nil)
	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/P1/records?date=2026-08-08", "",
		map[string]string{"X-Role": "ROLE_PATIENT", "X-User-Id": "P2"})

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	errorCode, requestID := t464TraceOf(t, w)
	assert.Equal(t, model.CodeForbidden, errorCode, "trace.errorCode 必须与信封 code 同值")
	assert.Len(t, requestID, 16, "requestId 是 16 位十六进制（8 字节随机）")
	assert.Equal(t, requestID, w.Header().Get(HeaderRequestID), "响应头要与 trace 同源")

	entry := t464EntryByRequestID(t, logs, requestID)
	assert.Equal(t, float64(model.CodeForbidden), entry["code"])
	assert.Contains(t, entry["message"], "your own data", "技术文本落在日志通道")
}

// TestT464_TechnicalTextNeverReachesBody 判定①：越权响应体里不得再出现英文技术文本。
func TestT464_TechnicalTextNeverReachesBody(t *testing.T) {
	srv := newTestServer(nil)
	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/P1/records?date=2026-08-08", "",
		map[string]string{"X-Role": "ROLE_PATIENT", "X-User-Id": "P2"})

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "your own data")
	assert.Contains(t, w.Body.String(), model.UserText(model.CodeForbidden), "响应体给的是中文短句")
}

// TestT464_InboundRequestIDReused 网关透传的关联号原样回传（跨跳对齐的前提）。
func TestT464_InboundRequestIDReused(t *testing.T) {
	srv := newTestServer(nil)
	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/P1/records?date=2026-08-08", "",
		map[string]string{"X-Role": "ROLE_PATIENT", "X-User-Id": "P2", HeaderRequestID: "gw-t464-0002"})

	_, requestID := t464TraceOf(t, w)
	assert.Equal(t, "gw-t464-0002", requestID)
}

// TestT464_SuccessBodyKeepsShape 成功响应不得出现 trace 键（omitempty 后与改前逐字节同形）。
func TestT464_SuccessBodyKeepsShape(t *testing.T) {
	srv := newTestServer(nil)
	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/P1/records?date=2026-08-08", "",
		map[string]string{"X-Role": "ROLE_ADMIN", "X-User-Id": "ADMIN-001"})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), `"trace"`, "成功响应不该带 trace：%s", w.Body.String())
	assert.Contains(t, w.Body.String(), `"message":"success"`)
}
