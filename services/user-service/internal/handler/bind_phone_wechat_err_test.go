// Package handler T166：bind-phone 微信上游错误的响应契约。
//
// 这组测试把线上 502 的取证过程固化为可执行断言：网关日志曾记录
// upstream_status=502 + upstream_content_length=65 + application/json，而 user-service
// 在 "request parsed" 之后无任何出口日志。本文件锁定产生该响应的唯一作者点，
// 并补上此前因 mock 错误类型不匹配而零覆盖的 10604 分支。
package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/wechat"
)

// wxErrClient 以真实 wechat 包类型制造上游错误。
//
// 不用 testhelper.MockWechatClient：它返回 testhelper.WechatError，与 bind_phone.go 里
// errors.As 的目标 *wechat.WechatError 是不同类型，永远匹配不上——业务错误被吞成 502，
// 10604 分支因此从未被覆盖。
type wxErrClient struct {
	plain error
	we    *wechat.WechatError
}

func (c wxErrClient) DoCode2Session(_ context.Context, _ string) (*wechat.Code2SessionResult, error) {
	if c.we != nil {
		return nil, c.we
	}
	return nil, c.plain
}

func (c wxErrClient) GetPhoneNumber(_ context.Context, _ string) (string, string, error) {
	if c.we != nil {
		return "", "", c.we
	}
	return "", "", c.plain
}

// TestBindPhoneUpstream404_Returns65ByteWxUnavailable 复现 T166 线上故障形状。
//
// 根因：wechat.go 曾请求 /phonenumber/getPhoneNumber——那是微信文档里该 API 的短名、
// 不是 URL path。微信对未知 path 返回 HTTP 404 + 空 body → wechat 包包装成普通 error
// （非 *WechatError）→ 本 handler 落 10502 / HTTP 502。
func TestBindPhoneUpstream404_Returns65ByteWxUnavailable(t *testing.T) {
	t.Parallel()

	e := newBindPhoneEnv(t)
	e.store.patientLogin = patientLoginForPhone("P20260001", "患者小明", "active")
	// wechat.go:265 对非 2xx 的真实包装文本（实测微信对未知 path 返回 404 + 0 字节 body）
	e.h.SetWXClient(wxErrClient{plain: errors.New("phone http status 404 body ")})

	rec, resp := e.doBindPhone("wechat_code_xyz", "", e.createBindToken("bind_502"))

	assert.Equal(t, http.StatusBadGateway, rec.Code, "非业务错误应映射为 HTTP 502")
	assert.Equal(t, model.CodeWXUnavail, resp.Code, "业务码应为 10502")
	assert.Equal(t, `{"code":10502,"message":"wechat service unavailable","data":null}`, rec.Body.String(),
		"T166 取证锚点：响应体逐字节固定，网关 upstream_content_length=65 才能归因到本分支；"+
			"网关自身兜底 502 是 49 字节，用长度即可区分两层")
}

// TestBindPhoneWechatBizError_Returns10604 *WechatError → 10604 / HTTP 200。
//
// 判定意义：access_token 侧的业务错误（如微信后台未加服务器出口 IP 白名单 → 40164）
// 经 fetchAccessToken 返回 *WechatError，落这条而不是 502。所以「path 修好后仍失败」
// 的真机签名是 HTTP 200 + code 10604，与「path 仍错」的 502 一眼可分。
func TestBindPhoneWechatBizError_Returns10604(t *testing.T) {
	t.Parallel()

	e := newBindPhoneEnv(t)
	e.store.patientLogin = patientLoginForPhone("P20260001", "患者小明", "active")
	e.h.SetWXClient(wxErrClient{we: &wechat.WechatError{ErrCode: 40164, ErrMsg: "invalid ip"}})

	rec, resp := e.doBindPhone("wechat_code_xyz", "", e.createBindToken("bind_10604"))

	assert.Equal(t, http.StatusOK, rec.Code, "微信业务错误应以 HTTP 200 承载")
	assert.Equal(t, model.CodeInvalidPhone, resp.Code, "业务码应为 10604，而非 10502")
}

// TestWechatErrText_StripsQueryCredentials 守住出口日志的密钥红线。
//
// 用真实 wechat 客户端 + 已关闭的 server 产生 error：fetchAccessToken 把 appid/secret
// 放在 query 里，Go 的 *url.Error 会把整条 URL 拼进 Error()。若出口日志直接 .Err(err)，
// AppSecret 就进了 staging 日志。这里断言脱敏后 path 保留、凭据消失。
func TestWechatErrText_StripsQueryCredentials(t *testing.T) {
	t.Parallel()

	const appSecret = "SECRET_MUST_NEVER_REACH_LOG"

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // 之后任何请求都得到 dial tcp 连接错误（真实 *url.Error）

	cli := wechat.NewClientWithBaseURL("wx_appid_under_test", appSecret, srv.URL)
	_, _, err := cli.GetPhoneNumber(context.Background(), "code123")
	require.Error(t, err, "上游不可达应返回 error")

	got := wechatErrText(err)

	assert.Contains(t, got, "/cgi-bin/token", "path 必须保留，否则失去定位价值")
	assert.NotContains(t, got, appSecret, "AppSecret 不得出现在日志文本中")
	assert.NotContains(t, got, "secret=", "query 凭据必须剥离")
	assert.NotContains(t, got, "wx_appid_under_test", "appid 同属凭据")
}
