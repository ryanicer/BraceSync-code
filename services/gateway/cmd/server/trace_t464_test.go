// T464 双通道门禁（网关）：trace 必带、关联号与日志同源、技术文本只进日志、成功透传形状不变。
//
// 网关是关联号的生成点，所以这里锁的正是「逐跳同源」那件事：
// 客户端自报的合法关联号原样回传并透传给后端，非法/缺失则换成 16 位十六进制新号。
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

var t464RequestIDRe = regexp.MustCompile(`"requestId":"([A-Za-z0-9_-]{1,64})"`)

// t464Request 走真实 HTTP 并把响应头带回来（httpDoFull 只回状态码与体，头要另取一路）。
func t464Request(t *testing.T, method, target, body string, headers map[string]string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw), resp.Header
}

// t464TraceOf 解出错误响应的 trace 两键并锁住信封形态：
// trace.errorCode 与 code 同值、message 是该码的中文短句、requestId 是白名单形状
// （自报的合法号会原样保留，故这里不锁 16 位；生成号那条由各用例另锁）。
// 缺 trace 即判红。header 非 nil 时另锁「响应头与 trace 同源」。
func t464TraceOf(t *testing.T, code int, status int, body string, header http.Header) string {
	t.Helper()
	match := t464RequestIDRe.FindStringSubmatch(body)
	require.NotNil(t, match, "错误响应必须带 trace.requestId：%s", body)
	requestID := match[1]

	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Trace   *struct {
			ErrorCode int    `json:"errorCode"`
			RequestID string `json:"requestId"`
		} `json:"trace"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &envelope), "body=%s", body)
	require.NotNil(t, envelope.Trace, "错误响应必须带 trace：%s", body)
	assert.Equal(t, code, envelope.Code, "响应头状态 %d 下的信封 code", status)
	assert.Equal(t, envelope.Code, envelope.Trace.ErrorCode, "trace.errorCode 必须与信封 code 同值")
	assert.Regexp(t, requestIDPattern, requestID, "关联号必须是白名单形状（防注入/防超长）")
	assert.Equal(t, userText(code), envelope.Message, "响应体给用户的是中文短句")
	if header != nil {
		assert.Equal(t, requestID, header.Get(HeaderRequestID), "响应头要与 trace 同源")
	}
	return requestID
}

// t464TechLogContains 按关联号反查技术日志行，核对技术文本（底层报错/被挡原因/服务名）确实改道日志。
// 整行 JSON 一起比：zerolog 的结构化字段（service/upstream_url/error）都算日志侧取证面。
func t464TechLogContains(t *testing.T, capture *testhelper.LogCaptureHook, requestID, substr string) {
	t.Helper()
	var texts []string
	for _, entry := range capture.Entries() {
		got, _ := entry["request_id"].(string)
		if got != requestID {
			continue
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		texts = append(texts, string(raw))
	}
	require.NotEmpty(t, texts, "没有 request_id=%s 的日志行 ⇒ 技术文本没进日志通道；捕获到 %+v",
		requestID, capture.Entries())
	for _, text := range texts {
		if strings.Contains(text, substr) {
			return
		}
	}
	t.Fatalf("request_id=%s 的日志行里没有 %q（技术文本必须落在日志通道）：%+v", requestID, substr, texts)
}

// TestT464_NoToken401CarriesTraceCorrelatedWithLog 鉴权阶段的拒绝也带得上关联号：
// 响应体只剩中文短句，「missing or malformed Authorization header」这行原始文本进日志。
func TestT464_NoToken401CarriesTraceCorrelatedWithLog(t *testing.T) {
	logs := t464CaptureLogs(t)
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL,
		testJWTSecretMain)

	code, body, header := t464Request(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "", nil)
	require.Equal(t, http.StatusUnauthorized, code, body)
	requestID := t464TraceOf(t, http.StatusUnauthorized, code, body, header)
	assert.Regexp(t, `^[0-9a-f]{16}$`, requestID, "未自报时由网关生成 16 位十六进制")

	assert.NotContains(t, body, "Authorization header", "技术文本不得漏进响应体")
	t464TechLogContains(t, logs, requestID, "missing or malformed Authorization header")
	assert.Empty(t, *received, "401 必须在网关拦下，不转发后端")
}

// TestT464_Proxy502KeepsUpstreamInMachineReadableField 上游不可达：
// 响应体 message 改中文短句后，「502 来自网关兜底 vs 上游主动返 502」的判据落在 data.upstream，
// 底层 dial 报错只进日志（同一关联号可反查是哪条上游、哪种失败）。
func TestT464_Proxy502KeepsUpstreamInMachineReadableField(t *testing.T) {
	logs := t464CaptureLogs(t)
	gw := startFullGateway(t, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1",
		"http://127.0.0.1:1", "http://127.0.0.1:1", testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "", validBearer(t))
	require.Equal(t, http.StatusBadGateway, code, body)
	requestID := t464TraceOf(t, http.StatusBadGateway, code, body, nil)
	assert.Regexp(t, `^[0-9a-f]{16}$`, requestID, "客户端未自报时由网关生成 16 位十六进制")

	var envelope struct {
		Data struct {
			Upstream string `json:"upstream"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &envelope), "body=%s", body)
	assert.Equal(t, "user-service", envelope.Data.Upstream, "服务名走机器可读字段，不进 message")
	assert.NotContains(t, body, "unavailable", "英文技术文本不得留在响应体")
	t464TechLogContains(t, logs, requestID, "user-service")
}

// TestT464_InboundRequestIDReused 客户端自报的合法关联号原样采信：跨跳对齐的前提。
func TestT464_InboundRequestIDReused(t *testing.T) {
	logs := t464CaptureLogs(t)
	gw := startFullGateway(t, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1",
		"http://127.0.0.1:1", "http://127.0.0.1:1", testJWTSecretMain)

	headers := map[string]string{HeaderRequestID: "gw-t464-0001"}
	for k, v := range validBearer(t) {
		headers[k] = v
	}
	code, body, respHeader := t464Request(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "", headers)
	require.Equal(t, http.StatusBadGateway, code, body)
	requestID := t464TraceOf(t, http.StatusBadGateway, code, body, respHeader)
	assert.Equal(t, "gw-t464-0001", requestID)
	t464TechLogContains(t, logs, requestID, "dial tcp")
}

// TestT464_InvalidInboundRequestIDReplaced 非法形状（含空格/超长/注入字符）不采信，换新号——防日志注入。
// 取 401 这条最短路径即可：关联号在鉴权之前就已定形。
func TestT464_InvalidInboundRequestIDReplaced(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL,
		testJWTSecretMain)

	for _, inbound := range []string{"bad id", strings.Repeat("x", 65), "with;inject"} {
		code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "",
			map[string]string{HeaderRequestID: inbound})
		require.Equal(t, http.StatusUnauthorized, code, body)
		requestID := t464TraceOf(t, http.StatusUnauthorized, code, body, nil)
		assert.NotEqual(t, inbound, requestID, "非法入站关联号必须被替换")
		assert.Regexp(t, `^[0-9a-f]{16}$`, requestID)
	}
}

// TestT464_SuccessPassthroughKeepsShape 透传成功的响应体不得被塞进 trace
// （网关只在自己的错误响应上加，后端响应原样转发）。
func TestT464_SuccessPassthroughKeepsShape(t *testing.T) {
	backend, _ := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL,
		testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodGet, gw.URL+"/api/v1/admin/patients", "", validBearer(t))
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, `{"code":0,"message":"success","data":null}`, body)
}
