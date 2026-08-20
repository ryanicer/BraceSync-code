// Package storage — COS 对象存储客户端（T022，架构 §3.3/§8 ADR-11）
//
// 预签名直传：file-service 仅签发短时效 PUT 预签名 URL，文件字节流不经过本服务。
// 两套实现共用 StorageClient 接口：
//   - RealCOSClient：生产路径，cos-go-sdk-v5 签发真实预签名 URL（凭据走 .env COS_*）
//   - MockCOSClient：测试路径，CI 离线可跑，不依赖真实腾讯云凭据（任务技术要点）
package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// StorageClient 对象存储能力接口（预签名 URL 签发）
type StorageClient interface {
	// GeneratePresignedURL 签发单次 PUT 预签名 URL，expires 为 URL 有效期
	GeneratePresignedURL(ctx context.Context, bucket, key string, method string, expires time.Duration) (string, error)
}

// ─────────────────────────────────────────────────────────────
// RealCOSClient：生产实现（cos-go-sdk-v5）
// ─────────────────────────────────────────────────────────────

// RealCOSClient 腾讯云 COS 客户端封装（预签名 URL 签发）
type RealCOSClient struct {
	client    *cos.Client
	secretID  string
	secretKey string
}

// NewRealCOSClient 构造真实 COS 客户端。bucketURL 为桶访问地址
// （形如 https://<bucket>.cos.<region>.myqcloud.com，缺 scheme 时补 https）。
// 预签名在本地计算（不发网络请求），因此构造期不校验凭据有效性，
// 凭据缺失由 GeneratePresignedURL 返回错误暴露。
func NewRealCOSClient(bucketURL, secretID, secretKey string) (*RealCOSClient, error) {
	if bucketURL == "" {
		return nil, fmt.Errorf("cos bucket url is empty")
	}
	raw := bucketURL
	if !strings.Contains(bucketURL, "://") {
		raw = "https://" + bucketURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("parse cos bucket url %q: invalid bucket endpoint", bucketURL)
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{})
	return &RealCOSClient{client: client, secretID: secretID, secretKey: secretKey}, nil
}

// GeneratePresignedURL 用 cos-go-sdk-v5 签发短时效预签名 URL（本地计算，无网络调用）。
// 凭据为空时拒绝签发（fail-closed，不降级为匿名 URL）。
func (c *RealCOSClient) GeneratePresignedURL(ctx context.Context, bucket, key string, method string, expires time.Duration) (string, error) {
	if c.secretID == "" || c.secretKey == "" {
		return "", fmt.Errorf("cos credentials missing: COS_SECRET_ID/COS_SECRET_KEY must be set")
	}
	signed, err := c.client.Object.GetPresignedURL(ctx, method, key, c.secretID, c.secretKey, expires, nil)
	if err != nil {
		return "", fmt.Errorf("cos presign %q: %w", key, err)
	}
	return signed.String(), nil
}

// ─────────────────────────────────────────────────────────────
// MockCOSClient：测试实现（CI 离线，无真实凭据）
// ─────────────────────────────────────────────────────────────

// MockCOSClient 打桩客户端：生成可断言的确定性假 URL，不发任何网络请求
type MockCOSClient struct{}

// NewMockCOSClient 构造打桩客户端
func NewMockCOSClient() *MockCOSClient {
	return &MockCOSClient{}
}

// GeneratePresignedURL 返回确定性假预签名 URL（含过期时间戳，供测试断言时效）
func (c *MockCOSClient) GeneratePresignedURL(ctx context.Context, bucket, key string, method string, expires time.Duration) (string, error) {
	expiresAt := time.Now().Add(expires).Unix()
	return fmt.Sprintf("http://mock-cos.example.com/%s/%s?sign=%d", bucket, key, expiresAt), nil
}
