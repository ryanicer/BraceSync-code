// Package storage 实现侧测试（T022：真实 COS 客户端构造/预签名 + mock 打桩）
//
// 真实客户端预签名为纯本地计算（cos-go-sdk-v5 GetPresignedURL 不发网络请求），
// 因此可用假凭据离线验证签名 URL 结构，不依赖真实腾讯云账号（任务技术要点）。
package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRealCOSClient_EmptyBucketURL(t *testing.T) {
	_, err := NewRealCOSClient("", "id", "key")
	assert.Error(t, err)
}

func TestNewRealCOSClient_SchemeCompletion(t *testing.T) {
	// 缺 scheme 时自动补 https（生产 .env 常只给域名）
	c, err := NewRealCOSClient("bracesync-123.cos.ap-guangzhou.myqcloud.com", "id", "key")
	require.NoError(t, err)
	assert.NotNil(t, c.client)
}

func TestNewRealCOSClient_WithScheme(t *testing.T) {
	c, err := NewRealCOSClient("https://bracesync-123.cos.ap-guangzhou.myqcloud.com", "id", "key")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewRealCOSClient_InvalidURL(t *testing.T) {
	_, err := NewRealCOSClient("://bad url", "id", "key")
	assert.Error(t, err)
}

func TestRealCOSClient_Presign_MissingCredentials(t *testing.T) {
	c, err := NewRealCOSClient("https://bracesync-123.cos.ap-guangzhou.myqcloud.com", "", "")
	require.NoError(t, err)

	_, err = c.GeneratePresignedURL(context.Background(), "b", "k", "PUT", time.Minute)
	assert.ErrorContains(t, err, "credentials missing", "空凭据 fail-closed，不降级匿名 URL")
}

func TestRealCOSClient_Presign_GeneratesSignedURL(t *testing.T) {
	// 假凭据离线验证：GetPresignedURL 为本地 HMAC 计算，不需要真实账号
	c, err := NewRealCOSClient("https://bracesync-123.cos.ap-guangzhou.myqcloud.com", "fake-id", "fake-key")
	require.NoError(t, err)

	signed, err := c.GeneratePresignedURL(context.Background(), "bracesync-123", "patient/P1/x.jpg", "PUT", 10*time.Minute)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(signed, "https://bracesync-123.cos.ap-guangzhou.myqcloud.com/"), "URL 域名必须来自桶配置: %s", signed)
	assert.Contains(t, signed, "q-sign-algorithm=", "必须携带 COS 签名参数")
	assert.Contains(t, signed, "q-ak=fake-id", "签名必须绑定 SecretId")
	assert.Contains(t, signed, "q-sign-time=", "签名必须携带时效窗口")
}

func TestMockCOSClient_DeterministicURL(t *testing.T) {
	c := NewMockCOSClient()

	url1, err := c.GeneratePresignedURL(context.Background(), "b", "k", "PUT", time.Minute)
	require.NoError(t, err)
	assert.Contains(t, url1, "mock-cos.example.com")
	assert.Contains(t, url1, "b/k")
	assert.Contains(t, url1, "sign=")
}

// StorageClient 接口兼容性断言（两套实现可互换注入）
var _ StorageClient = (*RealCOSClient)(nil)
var _ StorageClient = (*MockCOSClient)(nil)
