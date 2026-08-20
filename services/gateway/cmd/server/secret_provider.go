// Package main — 设备验签密钥提供器（T032）
//
// gateway 验签需要 device_secret：密钥写归 device-service（devices 表 owner，
// 架构 §4.2），经 /internal/devices/:deviceId/secret 服务间白名单直连获取
// （§5.2 内部信任链，不经网关）；gateway 侧内存缓存（短 TTL）避免每帧上报
// 打穿 device-service（30min/帧 × 设备数，缓存命中后 0 查询）。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ErrDeviceNotRegistered device-service 返回 20404（设备未注册，协议 §4.4）
var ErrDeviceNotRegistered = errors.New("device not registered")

// SecretProvider 设备验签密钥查询抽象（测试可注入内存实现）
type SecretProvider interface {
	GetDeviceSecret(ctx context.Context, deviceID string) (string, error)
}

// secretCacheTTL 密钥缓存时长（密钥仅注册时生成、不轮换，短 TTL 兼顾一致性）
const secretCacheTTL = 5 * time.Minute

// secretProviderTimeout 密钥查询超时（上报链路 P95≤200ms，查询须快失败）
const secretProviderTimeout = 3 * time.Second

type secretEntry struct {
	secret string
	expiry time.Time
}

// deviceServiceSecretProvider 经 device-service internal 端点查询密钥（带内存缓存）
type deviceServiceSecretProvider struct {
	baseURL string
	client  *http.Client
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]secretEntry
}

// newDeviceServiceSecretProvider 构造密钥提供器；baseURL 为 device-service 地址
func newDeviceServiceSecretProvider(baseURL string) *deviceServiceSecretProvider {
	return &deviceServiceSecretProvider{
		baseURL: baseURL,
		client:  &http.Client{Timeout: secretProviderTimeout},
		now:     time.Now,
		cache:   map[string]secretEntry{},
	}
}

// secretEnvelope device-service 统一响应体（架构 §3.5）
type secretEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Secret string `json:"secret"`
	} `json:"data"`
}

// GetDeviceSecret 查密钥：缓存命中直返；未命中查 device-service 并回填。
// 设备未注册返回 ErrDeviceNotRegistered；其余错误（不可达/5xx）原样上抛。
func (p *deviceServiceSecretProvider) GetDeviceSecret(ctx context.Context, deviceID string) (string, error) {
	now := p.now()

	p.mu.Lock()
	if e, ok := p.cache[deviceID]; ok && now.Before(e.expiry) {
		p.mu.Unlock()
		return e.secret, nil
	}
	delete(p.cache, deviceID) // 过期条目清除
	p.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.baseURL+"/internal/devices/"+url.PathEscape(deviceID)+"/secret", nil)
	if err != nil {
		return "", fmt.Errorf("build secret request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("query device-service secret: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read secret response: %w", err)
	}

	var env secretEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("parse secret response: %w", err)
	}
	if env.Code == 20404 {
		return "", ErrDeviceNotRegistered
	}
	if env.Code != 0 || env.Data.Secret == "" {
		return "", fmt.Errorf("device-service secret query failed: code=%d message=%s", env.Code, env.Message)
	}

	p.mu.Lock()
	p.cache[deviceID] = secretEntry{secret: env.Data.Secret, expiry: now.Add(secretCacheTTL)}
	p.mu.Unlock()
	return env.Data.Secret, nil
}
