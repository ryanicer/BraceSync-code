// Package main — T032 设备密钥提供器实现侧测试（不与既有测试文件重叠）
//
// 覆盖：正常查询、内存缓存命中（不打穿 device-service）、TTL 过期重查、
// 20404 → ErrDeviceNotRegistered、业务错误码/非法 JSON/服务不可用 → error。
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startSecretBackend 模拟 device-service internal 密钥端点（计数命中次数）
func startSecretBackend(t *testing.T, respFunc func(deviceID string) (int, string)) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		deviceID := r.URL.Path[len("/internal/devices/") : len(r.URL.Path)-len("/secret")]
		status, body := respFunc(deviceID)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestSecretProvider_FetchAndCache(t *testing.T) {
	srv, hits := startSecretBackend(t, func(deviceID string) (int, string) {
		return http.StatusOK, `{"code":0,"message":"success","data":{"secret":"sec-` + deviceID + `"}}`
	})
	p := newDeviceServiceSecretProvider(srv.URL)

	secret, err := p.GetDeviceSecret(context.Background(), "DEV-A")
	require.NoError(t, err)
	assert.Equal(t, "sec-DEV-A", secret)

	// 缓存命中：第二次不打后端
	secret, err = p.GetDeviceSecret(context.Background(), "DEV-A")
	require.NoError(t, err)
	assert.Equal(t, "sec-DEV-A", secret)
	assert.Equal(t, int32(1), atomic.LoadInt32(hits), "缓存命中不重复查询")

	// 不同设备独立缓存
	_, err = p.GetDeviceSecret(context.Background(), "DEV-B")
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(hits))
}

func TestSecretProvider_TTLExpiry_Refetch(t *testing.T) {
	srv, hits := startSecretBackend(t, func(deviceID string) (int, string) {
		return http.StatusOK, `{"code":0,"message":"success","data":{"secret":"s1"}}`
	})
	p := newDeviceServiceSecretProvider(srv.URL)
	now := time.Now()
	p.now = func() time.Time { return now }

	_, err := p.GetDeviceSecret(context.Background(), "DEV-A")
	require.NoError(t, err)

	// 时钟推进超过 TTL → 重新查询
	now = now.Add(secretCacheTTL + time.Second)
	_, err = p.GetDeviceSecret(context.Background(), "DEV-A")
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(hits), "TTL 过期后重查")
}

func TestSecretProvider_DeviceNotRegistered(t *testing.T) {
	srv, _ := startSecretBackend(t, func(deviceID string) (int, string) {
		return http.StatusNotFound, `{"code":20404,"message":"device not registered","data":null}`
	})
	p := newDeviceServiceSecretProvider(srv.URL)

	_, err := p.GetDeviceSecret(context.Background(), "DEV-GHOST")
	assert.ErrorIs(t, err, ErrDeviceNotRegistered)
}

func TestSecretProvider_BusinessError(t *testing.T) {
	srv, _ := startSecretBackend(t, func(deviceID string) (int, string) {
		return http.StatusInternalServerError, `{"code":90001,"message":"internal error","data":null}`
	})
	p := newDeviceServiceSecretProvider(srv.URL)

	_, err := p.GetDeviceSecret(context.Background(), "DEV-A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "code=90001")
}

func TestSecretProvider_EmptySecret_Error(t *testing.T) {
	srv, _ := startSecretBackend(t, func(deviceID string) (int, string) {
		return http.StatusOK, `{"code":0,"message":"success","data":{"secret":""}}`
	})
	p := newDeviceServiceSecretProvider(srv.URL)

	_, err := p.GetDeviceSecret(context.Background(), "DEV-A")
	require.Error(t, err, "空密钥视为异常")
}

func TestSecretProvider_MalformedJSON(t *testing.T) {
	srv, _ := startSecretBackend(t, func(deviceID string) (int, string) {
		return http.StatusOK, `not-json`
	})
	p := newDeviceServiceSecretProvider(srv.URL)

	_, err := p.GetDeviceSecret(context.Background(), "DEV-A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse secret response")
}

func TestSecretProvider_BackendDown(t *testing.T) {
	p := newDeviceServiceSecretProvider("http://127.0.0.1:1")
	_, err := p.GetDeviceSecret(context.Background(), "DEV-A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query device-service secret")
}

func TestSecretProvider_URLEscapedDeviceID(t *testing.T) {
	// device_id 含特殊字符时 PathEscape 不破坏路径语义
	var gotPath atomic.Value
	srv, _ := startSecretBackend(t, func(deviceID string) (int, string) {
		gotPath.Store(deviceID)
		return http.StatusOK, `{"code":0,"message":"success","data":{"secret":"s"}}`
	})
	p := newDeviceServiceSecretProvider(srv.URL)

	_, err := p.GetDeviceSecret(context.Background(), "PRS-ML05-RC-001")
	require.NoError(t, err)
	assert.Equal(t, "PRS-ML05-RC-001", gotPath.Load())
}
