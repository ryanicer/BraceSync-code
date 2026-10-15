// T690 SendKfText 实现侧测试（小程序客服消息下发 /cgi-bin/message/custom/send）。
//
// 覆盖：请求契约（path / access_token query / touser+msgtype+text 体）、
// errcode 40001 强制刷新 access_token 并重试一次、不可重试业务错误原样包装。
package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kfTestServer 伪装微信 API：/cgi-bin/token 发号，custom/send 按脚本回包
type kfTestServer struct {
	tokenCalls atomic.Int32
	sendCalls  atomic.Int32
	sendBodies []map[string]any
	// sendErrCodes 依次消费；空 = 恒 0
	sendErrCodes []int
	srv          *httptest.Server
}

func newKFTestServer(t *testing.T) *kfTestServer {
	t.Helper()
	k := &kfTestServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/cgi-bin/token", func(w http.ResponseWriter, r *http.Request) {
		n := k.tokenCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-" + string(rune('0'+n)),
			"expires_in":   7200,
		})
	})
	mux.HandleFunc("/cgi-bin/message/custom/send", func(w http.ResponseWriter, r *http.Request) {
		k.sendCalls.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		k.sendBodies = append(k.sendBodies, body)
		code := 0
		if len(k.sendErrCodes) > 0 {
			code = k.sendErrCodes[0]
			k.sendErrCodes = k.sendErrCodes[1:]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": code, "errmsg": "ok"})
	})
	k.srv = httptest.NewServer(mux)
	t.Cleanup(k.srv.Close)
	return k
}

func TestSendKfText_Success_Contract(t *testing.T) {
	k := newKFTestServer(t)
	c := NewClientWithBaseURL("appid", "secret", k.srv.URL)

	err := c.SendKfText(context.Background(), "oUSER", "患者档案：林小雨（P20260001）")

	require.NoError(t, err)
	require.Equal(t, int32(1), k.sendCalls.Load())
	require.Len(t, k.sendBodies, 1)
	assert.Equal(t, "oUSER", k.sendBodies[0]["touser"])
	assert.Equal(t, "text", k.sendBodies[0]["msgtype"])
	text, ok := k.sendBodies[0]["text"].(map[string]any)
	require.True(t, ok, "体必须是 {touser, msgtype, text:{content}} 形")
	assert.Equal(t, "患者档案：林小雨（P20260001）", text["content"])
}

func TestSendKfText_TokenExpired_ForceRefreshRetryOnce(t *testing.T) {
	k := newKFTestServer(t)
	k.sendErrCodes = []int{40001, 0} // 首发 40001 → 强刷重试 → 成功
	c := NewClientWithBaseURL("appid", "secret", k.srv.URL)

	err := c.SendKfText(context.Background(), "oUSER", "hi")

	require.NoError(t, err)
	assert.Equal(t, int32(2), k.sendCalls.Load(), "40001 必须重试一次")
	assert.GreaterOrEqual(t, k.tokenCalls.Load(), int32(2), "重试前必须强刷 access_token")
}

func TestSendKfText_NonRetryableErr_Wrapped(t *testing.T) {
	k := newKFTestServer(t)
	k.sendErrCodes = []int{45002} // 超出客服接口下发限制：不重试
	c := NewClientWithBaseURL("appid", "secret", k.srv.URL)

	err := c.SendKfText(context.Background(), "oUSER", "hi")

	require.Error(t, err)
	var we *WechatError
	require.True(t, errors.As(err, &we))
	assert.Equal(t, 45002, we.ErrCode)
	assert.Equal(t, int32(1), k.sendCalls.Load(), "不可重试错误不得二次下发")
}
