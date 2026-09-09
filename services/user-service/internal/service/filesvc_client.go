// Package service — user-service 内部服务依赖（T130）
//
// FileSvcClient：调用 file-service HTTP API 获取文件元数据与下载预签名 URL。
// 模式对齐 gateway→device-service 的 SecretProvider（服务间内部 HTTP 调用）。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FileMetadata file-service 文件元数据（对齐 file-service model.FileMetadata JSON 字段）
type FileMetadata struct {
	FileID      string    `json:"file_id"`
	Bucket      string    `json:"bucket"`
	ObjectKey   string    `json:"object_key"`
	URL         string    `json:"url"`
	FileType    string    `json:"file_type"`
	OwnerType   string    `json:"owner_type"`
	OwnerID     string    `json:"owner_id"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	Status      string    `json:"status"`
	UploadedAt  time.Time `json:"uploaded_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// fileSvcResponse file-service 统一响应体
type fileSvcResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// FileSvcClient file-service HTTP 客户端
type FileSvcClient struct {
	baseURL string
	client  *http.Client
}

// NewFileSvcClient 构造 file-service 客户端（baseURL 如 http://file-service:8085）
func NewFileSvcClient(baseURL string) *FileSvcClient {
	return &FileSvcClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

// doGet 执行 GET 请求并解析统一响应体
func (c *FileSvcClient) doGet(ctx context.Context, path string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	// 服务间调用注入服务身份头（file-service 要求 X-User-Id 非空）
	req.Header.Set("X-User-Id", "user-service")
	req.Header.Set("X-Role", "ROLE_ADMIN")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("file-service request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read file-service response: %w", err)
	}
	var envelope fileSvcResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("parse file-service response: %w", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("file-service error code=%d message=%s", envelope.Code, envelope.Message)
	}
	return envelope.Data, nil
}

// GetFileByID 按 file_id 查询文件元数据；不存在返回 (nil, nil)
func (c *FileSvcClient) GetFileByID(ctx context.Context, fileID string) (*FileMetadata, error) {
	data, err := c.doGet(ctx, "/api/v1/files/"+fileID)
	if err != nil {
		return nil, err
	}
	var fm FileMetadata
	if err := json.Unmarshal(data, &fm); err != nil {
		return nil, fmt.Errorf("parse file metadata: %w", err)
	}
	return &fm, nil
}

// GetDownloadURL 获取文件下载预签名 URL
func (c *FileSvcClient) GetDownloadURL(ctx context.Context, fileID string) (string, error) {
	data, err := c.doGet(ctx, "/api/v1/files/"+fileID+"/download")
	if err != nil {
		return "", err
	}
	var result struct {
		FileID      string `json:"file_id"`
		DownloadURL string `json:"download_url"`
		ExpiresAt   string `json:"expires_at"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse download url: %w", err)
	}
	return result.DownloadURL, nil
}
