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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
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

// Client 微信服务端客户端
type Client struct {
	appID     string
	appSecret string
	baseURL   string
	httpCli   *http.Client

	// access_token 缓存（AccessTokenManager：缓存 + singleflight + 强制刷新）
	mu       sync.Mutex
	token    string
	tokenExp time.Time
	sf       singleflight.Group
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
	defer func() { _ = resp.Body.Close() }()
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

// ─────────────────────────────────────────────────────────────
// T085 AccessTokenManager + GetPhoneNumber
// ─────────────────────────────────────────────────────────────

// accessTokenSkew 缓存提前过期时间（避免边界使用过期 token）
const accessTokenSkew = 5 * time.Minute

// GetAccessToken 获取 access_token（缓存命中则直接返回；否则经 singleflight 合并并发上游调用）。
// 暴露以供测试断言 singleflight 行为。
func (c *Client) GetAccessToken(ctx context.Context) (string, error) {
	return c.getAccessToken(ctx, false)
}

// getAccessToken 获取 access_token。force=true 时跳过缓存强制刷新。
func (c *Client) getAccessToken(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	if !force && c.token != "" && time.Now().Before(c.tokenExp) {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	// singleflight 合并并发请求：同一时刻仅 1 次上游 /cgi-bin/token 调用
	v, err, _ := c.sf.Do("access_token", func() (interface{}, error) {
		return c.fetchAccessToken(ctx)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// fetchAccessToken 调用 /cgi-bin/token 获取并缓存 access_token
func (c *Client) fetchAccessToken(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqURL := c.baseURL + "/cgi-bin/token?" + url.Values{
		"grant_type": {"client_credential"},
		"appid":      {c.appID},
		"secret":     {c.appSecret},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return "", fmt.Errorf("http do token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token http status %d body %s", resp.StatusCode, truncate(body, 256))
	}
	var we WechatError
	_ = json.Unmarshal(body, &we)
	if we.ErrCode != 0 {
		return "", &we
	}
	var res struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if res.AccessToken == "" {
		return "", errors.New("empty access_token in wechat response")
	}
	exp := time.Duration(res.ExpiresIn) * time.Second
	if exp > accessTokenSkew {
		exp -= accessTokenSkew
	}
	c.mu.Lock()
	c.token = res.AccessToken
	c.tokenExp = time.Now().Add(exp)
	c.mu.Unlock()
	return res.AccessToken, nil
}

// phoneInfoResp phonenumber.getPhoneNumber 成功响应
type phoneInfoResp struct {
	ErrCode   int    `json:"errcode"`
	ErrMsg    string `json:"errmsg"`
	PhoneInfo struct {
		PhoneNumber     string `json:"phoneNumber"`
		PurePhoneNumber string `json:"purePhoneNumber"`
		CountryCode     string `json:"countryCode"`
	} `json:"phone_info"`
}

// GetPhoneNumber T085：微信 phonenumber.getPhoneNumber（code 换手机号）。
// 返回 (purePhoneNumber, countryCode, error)。
//   - 业务错误 errcode!=0 → *WechatError
//   - errcode 40001/42001（access_token 失效）→ 强制刷新并重试 1 次
//   - 网络/解析错误 → 普通 error
func (c *Client) GetPhoneNumber(ctx context.Context, code string) (string, string, error) {
	return c.getPhoneNumber(ctx, code, false)
}

func (c *Client) getPhoneNumber(ctx context.Context, code string, retried bool) (string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tok, err := c.getAccessToken(ctx, false)
	if err != nil {
		return "", "", err
	}
	reqURL := c.baseURL + "/phonenumber/getPhoneNumber?access_token=" + url.QueryEscape(tok)
	reqBody, _ := json.Marshal(map[string]string{"code": code})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", "", fmt.Errorf("build phone request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("http do phone: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read phone body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("phone http status %d body %s", resp.StatusCode, truncate(body, 256))
	}
	var pi phoneInfoResp
	if err := json.Unmarshal(body, &pi); err != nil {
		return "", "", fmt.Errorf("parse phone response: %w body %s", err, truncate(body, 256))
	}
	if pi.ErrCode != 0 {
		// access_token 失效 → 强制刷新重试 1 次
		if (pi.ErrCode == 40001 || pi.ErrCode == 42001) && !retried {
			_, _ = c.getAccessToken(ctx, true)
			return c.getPhoneNumber(ctx, code, true)
		}
		return "", "", &WechatError{ErrCode: pi.ErrCode, ErrMsg: pi.ErrMsg}
	}
	return pi.PhoneInfo.PurePhoneNumber, pi.PhoneInfo.CountryCode, nil
}
