// Package testhelper provides BraceSync integration test utilities
// 对齐：T085 测试计划 · Phase 1 Fixture 搭建
//
// MockWechatClient 用于拦截电话获取 + 计数验证 zero-call 场景
package testhelper

import (
	"errors"
	"sync"
)

var (
	ErrPhoneNumberUnavailable = errors.New("phoneNumber unavailable")
	ErrInvalidCode            = errors.New("invalid code")
	ErrNetwork                = errors.New("network error")
)

// MockWechatClient 模拟微信客户端（GetPhoneNumber）
// 主要用途：
//   - 验证 phoneToken 重试不调用微信接口（断言零调用）
//   - 模拟业务错误返回（如 code 非法、access_token 失效）
//   - 记录实际 API 调用次数
type MockWechatClient struct {
	phoneNumber    string
	purePhoneNumber string
	countryCode   string
	shouldError   bool
	errorCode     int // 模拟微信 errcode
	callCount     int
	mu            sync.Mutex
}

// NewMockWechatClient 创建新的 mock 微信客户端
func NewMockWechatClient(phoneNumber string) *MockWechatClient {
	return &MockWechatClient{
		phoneNumber: phoneNumber,
	}
}

// GetPhoneNumber 模拟微信 phonenumber.getPhoneNumber 接口
// 返回值：(purePhoneNumber, countryCode)
// 错误处理：
//   - shouldError=true → 返回 ErrPhoneNumberUnavailable
//   - errorCode!=0 → 返回特定 errcode
func (m *MockWechatClient) GetPhoneNumber(code string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.callCount++
	
	if m.shouldError {
		if m.errorCode != 0 {
			return "", "", &WechatError{ErrCode: m.errorCode}
		}
		return "", "", ErrPhoneNumberUnavailable
	}
	
	// 模拟正常响应
	if code == "" {
		return "", "", ErrInvalidCode
	}
	
	// 纯手机号（无国家码前缀）
	return m.purePhoneNumber, m.countryCode, nil
}

// CallCount 返回实际 API 调用次数
func (m *MockWechatClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// ResetCount 重置调用计数为 0
func (m *MockWechatClient) ResetCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount = 0
}

// SetPhoneNumber 设置手机号（用于动态调整 mock 响应）
func (m *MockWechatClient) SetPhoneNumber(phone string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phoneNumber = phone
	m.purePhoneNumber = extractPurePhone(phone)
}

// EnableError 启用错误模式（模拟微信服务异常）
func (m *MockWechatClient) EnableError(errCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldError = true
	m.errorCode = errCode
}

// DisableError 禁用错误模式
func (m *MockWechatClient) DisableError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldError = false
	m.errorCode = 0
}

// WechatError 模拟微信 SDK 错误
type WechatError struct {
	ErrCode int
}

func (e *WechatError) Error() string {
	switch e.ErrCode {
	case 40001:
		return "access_token 失效，需强制刷新"
	case 41401:
		return "code 过期或已被使用"
	case 41208:
		return "code 已被使用（重复提交）"
	default:
		return "微信服务未知错误"
	}
}

// Is 实现 errors.Is 匹配（用于 assert.ErrorIs）
func (e *WechatError) Is(target error) bool {
	wechatErr, ok := target.(*WechatError)
	return ok && wechatErr.ErrCode == e.ErrCode
}

// extractPurePhone 从带国家码的手机号提取纯手机号部分
func extractPurePhone(phone string) string {
	// 简单实现：移除 "+86" 前缀
	if len(phone) > 3 && phone[:3] == "+86" {
		return phone[3:]
	}
	return phone
}
