// Package wechat 微信服务端 API 封装（T069 仅 jscode2session）
//
// 设计要点：
//   - Client 结构体内含 appID/appSecret/baseURL/httpCli；仅构造时注入，
//     不读 os.Getenv，不打日志（避免 AppSecret 泄漏）
//   - 单测使用 NewClientWithBaseURL 注入 httptest.NewServer，不访问真实外网
//   - 微信返回 errcode != 0 时包装为 *WechatError（errors.As 可识别）；
//     网络错误/JSON 解析失败等直接返回普通 error
package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Code2SessionResult 微信 jscode2session 成功响应（unionid 可选）
type Code2SessionResult struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid,omitempty"`
}

// WechatError 微信返回的业务错误（errcode != 0 时返回）
type WechatError struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func (e *WechatError) Error() string {
	return fmt.Sprintf("wechat errcode=%d errmsg=%s", e.ErrCode, e.ErrMsg)
}

const (
	defaultBaseURL = "https://api.weixin.qq.com"
	requestTimeout = 5 * time.Second
)

// Client 微信服务端客户端（仅对外暴露构造 + DoCode2Session）
type Client struct {
	appID     string
	appSecret string
	baseURL   string
	httpCli   *http.Client
}

// NewClient 使用默认 baseURL（api.weixin.qq.com）构造
func NewClient(appID, appSecret string) *Client {
	return NewClientWithBaseURL(appID, appSecret, defaultBaseURL)
}

// NewClientWithBaseURL 指定 baseURL 构造（单测注入 httptest server 地址）
func NewClientWithBaseURL(appID, appSecret, baseURL string) *Client {
	return &Client{
		appID:     appID,
		appSecret: appSecret,
		baseURL:   baseURL,
		httpCli:   &http.Client{Timeout: requestTimeout},
	}
}

// DoCode2Session 调用 jscode2session：
//   - 成功 → (*Code2SessionResult, nil)
//   - 微信返回 errcode != 0 → (nil, *WechatError)
//   - 网络/解析/HTTP 非 200 → (nil, 普通 error)
//
// ctx 为 nil 时等价于 context.Background（保持与项目其他 HTTP 调用习惯一致）
func (c *Client) DoCode2Session(ctx context.Context, code string) (*Code2SessionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqURL := c.baseURL + "/sns/jscode2session?" + url.Values{
		"appid":      {c.appID},
		"secret":     {c.appSecret},
		"js_code":    {code},
		"grant_type": {"authorization_code"},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d body %s", resp.StatusCode, truncate(body, 256))
	}

	// 先判断业务错误（微信接口 errcode!=0 时 HTTP 仍可能 200）
	var we WechatError
	_ = json.Unmarshal(body, &we) // 失败忽略；后续 Unmarshal 再统一处理
	if we.ErrCode != 0 {
		return nil, &we
	}

	var res Code2SessionResult
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("parse response: %w body %s", err, truncate(body, 256))
	}
	if res.OpenID == "" {
		return nil, errors.New("empty openid in wechat response")
	}
	return &res, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
