// T464 双通道门禁（用户域）测试基建：
//   - 响应体 message 只给 model.UserText(code) 的中文短句；
//   - 原始技术文本进日志，且与响应体 trace.requestId、响应头 X-Request-Id 同源，可按关联号反查。
//
// 本包用 TestMain 装一个贯穿整包的日志捕获：用例按「响应里的关联号」精确取自己那条日志行，
// 因此累加的缓冲区不会让跨用例的同名片段互相误命中（反查失败即判红）。
package handler

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
	"github.com/bracesync/bracesync/services/user-service/internal/model"
)

var t464Capture = testhelper.NewLogCaptureHook()

func TestMain(m *testing.M) {
	original := log.Logger
	log.Logger = zerolog.New(t464Capture).With().Timestamp().Logger()
	code := m.Run()
	log.Logger = original
	os.Exit(code)
}

// t464BodyRequestID 从响应体 trace 里取关联号（错误响应缺 trace 即判红）。
var t464RequestIDRe = regexp.MustCompile(`"requestId":"([A-Za-z0-9_-]{1,64})"`)

func t464BodyRequestID(t *testing.T, raw string) string {
	t.Helper()
	m := t464RequestIDRe.FindStringSubmatch(raw)
	require.NotNil(t, m, "错误响应必须带 trace.requestId：%s", raw)
	return m[1]
}

// t464RequestID 取本次请求的关联号：优先响应头（网关透传同源），再回落响应体 trace。
func t464RequestID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if rid := w.Header().Get(HeaderRequestID); rid != "" {
		require.Equal(t, t464BodyRequestID(t, w.Body.String()), rid, "响应头与 trace 必须同源")
		return rid
	}
	return t464BodyRequestID(t, w.Body.String())
}

// t464TechLog 按关联号反查该请求的技术日志行（响应体已不含技术文本，只能从这里核对）。
func t464TechLog(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	requestID := t464RequestID(t, w)
	for _, entry := range t464Capture.Entries() {
		if got, _ := entry["request_id"].(string); got == requestID {
			return entry
		}
	}
	t.Fatalf("没有 request_id=%s 的日志行 ⇒ 技术文本没进日志通道；捕获到 %+v", requestID, t464Capture.Entries())
	return nil
}

// t464TechLogContains 断言：技术文本（字段名/ID/底层报错）仍可从该请求的日志行反查。
func t464TechLogContains(t *testing.T, w *httptest.ResponseRecorder, substr string, msgAndArgs ...interface{}) {
	t.Helper()
	entry := t464TechLog(t, w)
	msg, _ := entry["message"].(string)
	require.Contains(t, msg, substr, msgAndArgs...)
}

// t464UserMessage 断言：响应体 message 是该码的中文用户短句，且 trace.errorCode 与信封 code 同值。
func t464UserMessage(t *testing.T, w *httptest.ResponseRecorder, code int) {
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
	require.Equal(t, code, resp.Code)
	require.Equal(t, resp.Code, resp.Trace.ErrorCode, "trace.errorCode 必须与信封 code 同值")
	require.Len(t, resp.Trace.RequestID, 16, "requestId 是 16 位十六进制（8 字节随机）")
	require.Equal(t, model.UserText(code), resp.Message, "响应体给用户的是中文短句")
}
