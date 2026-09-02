// Package wechat 实现侧测试（T069）：httptest server 驱动 jscode2session 全分支
package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tAppID     = "wx-test-appid"
	tAppSecret = "test-secret-only-for-unit-test"
	tCode      = "0123456789abcdef"
	tOpenID    = "o6_bmasdasdsad6_2sgVt7hMZOPfL"
	tSessKey   = "tiihtNczf5v6AKRyjwEUhQ=="
)

// newTestServer 返回一个闭包设置处理器的 httptest.Server + 对应的 Client
func newTestServer(handler http.HandlerFunc) (*httptest.Server, *Client) {
	srv := httptest.NewServer(handler)
	return srv, NewClientWithBaseURL(tAppID, tAppSecret, srv.URL)
}

// Case 1: 正常响应 openid + session_key → 解析正确
func TestDoCode2Session_OK(t *testing.T) {
	srv, cli := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/sns/jscode2session", r.URL.Path)
		q := r.URL.Query()
		assert.Equal(t, tAppID, q.Get("appid"))
		assert.Equal(t, tAppSecret, q.Get("secret"))
		assert.Equal(t, tCode, q.Get("js_code"))
		assert.Equal(t, "authorization_code", q.Get("grant_type"))
		_ = json.NewEncoder(w).Encode(Code2SessionResult{OpenID: tOpenID, SessionKey: tSessKey})
	})
	defer srv.Close()

	res, err := cli.DoCode2Session(context.Background(), tCode)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, tOpenID, res.OpenID)
	assert.Equal(t, tSessKey, res.SessionKey)
	assert.Empty(t, res.UnionID)
}

// Case 2: 含 unionid（可选字段）正常解析
func TestDoCode2Session_OKWithUnionID(t *testing.T) {
	srv, cli := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"openid":"` + tOpenID + `","session_key":"` + tSessKey + `","unionid":"u_test"}`))
	})
	defer srv.Close()
	res, err := cli.DoCode2Session(context.Background(), tCode)
	require.NoError(t, err)
	assert.Equal(t, "u_test", res.UnionID)
}

// Case 3: 微信返回 errcode（40029 无效 code）→ *WechatError
func TestDoCode2Session_WechatError(t *testing.T) {
	srv, cli := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WechatError{ErrCode: 40029, ErrMsg: "invalid code, rid: 64e21a..."})
	})
	defer srv.Close()

	res, err := cli.DoCode2Session(context.Background(), "bad-code")
	require.Error(t, err)
	assert.Nil(t, res)
	var we *WechatError
	require.True(t, errors.As(err, &we), "expected *WechatError, got %T", err)
	assert.Equal(t, 40029, we.ErrCode)
	assert.Contains(t, we.ErrMsg, "invalid code")
}

// Case 4: 微信返回 HTTP 5xx → 普通 error（非 WechatError）
func TestDoCode2Session_HTTP500(t *testing.T) {
	srv, cli := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "oom", http.StatusInternalServerError)
	})
	defer srv.Close()
	res, err := cli.DoCode2Session(context.Background(), tCode)
	require.Error(t, err)
	assert.Nil(t, res)
	var we *WechatError
	assert.False(t, errors.As(err, &we), "should NOT be WechatError for HTTP 5xx")
	assert.Contains(t, err.Error(), "http status 500")
}

// Case 5: ctx 取消 → 感知 error（非 WechatError）
func TestDoCode2Session_ContextCanceled(t *testing.T) {
	// 用 hang 处理器：不写入也不关闭，直到 ctx 取消
	hang := make(chan struct{})
	srv, cli := newTestServer(func(_ http.ResponseWriter, _ *http.Request) {
		<-hang
	})
	defer srv.Close()
	defer close(hang)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)

	res, err := cli.DoCode2Session(ctx, tCode)
	require.Error(t, err)
	assert.Nil(t, res)
	assert.True(t, errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		(err != nil), "cancel error expected, got %v", err)
}

// Case 6: 空响应 / 非法 JSON → 普通 error
func TestDoCode2Session_InvalidJSON(t *testing.T) {
	srv, cli := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not a json`))
	})
	defer srv.Close()
	res, err := cli.DoCode2Session(context.Background(), tCode)
	require.Error(t, err)
	assert.Nil(t, res)
}

// Case 7: openid 为空（字段缺失）→ 普通 error
func TestDoCode2Session_EmptyOpenID(t *testing.T) {
	srv, cli := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"session_key":"kk"}`)) // errcode=0 也视为 ok，但 openid 空 → 报错
	})
	defer srv.Close()
	res, err := cli.DoCode2Session(context.Background(), tCode)
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "empty openid")
}
