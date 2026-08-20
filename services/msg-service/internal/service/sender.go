// Package service msg-service 业务层
//
// sender.go — 微信订阅消息 / 短信发送通道抽象（T017 需求 3：一期 mock/stub，
// 真实服务商配置就绪后替换实现，接口不变）。
package service

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
)

// WechatSender 微信订阅消息发送通道（架构 §2.5 一次性授权模型）
type WechatSender interface {
	// SendSubscribe 向患者推送订阅消息；消耗 1 次订阅授权额度由 service 在成功后内部扣减
	SendSubscribe(ctx context.Context, patientID, content string) error
}

// SMSSender 短信发送通道（告警类通知降级通道 + 运维告警，T017 需求 3）
type SMSSender interface {
	SendSMS(ctx context.Context, patientID, content string) error
}

// MockSend 一次 mock 发送记录（测试断言/一期日志审计用）
type MockSend struct {
	PatientID string
	Content   string
}

// MockWechatSender 微信订阅消息 mock（一期：无 appid/模板配置时的占位实现）
//
// 注入 Err 可模拟发送失败（重试队列用例）；真实商接入后以 HTTP 实现替换。
type MockWechatSender struct {
	mu   sync.Mutex
	log  zerolog.Logger
	Err  error
	Sent []MockSend
}

// NewMockWechatSender 创建微信 mock 发送器
func NewMockWechatSender(log zerolog.Logger) *MockWechatSender {
	return &MockWechatSender{log: log}
}

// SendSubscribe 记录发送并返回注入错误（默认成功）
func (m *MockWechatSender) SendSubscribe(_ context.Context, patientID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.Sent = append(m.Sent, MockSend{PatientID: patientID, Content: content})
	m.log.Info().Str("patient_id", patientID).Msg("mock wechat subscribe message sent")
	return nil
}

// Sends 返回已发送记录快照（测试用）
func (m *MockWechatSender) Sends() []MockSend {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockSend, len(m.Sent))
	copy(out, m.Sent)
	return out
}

// MockSMSSender 短信 mock（一期 stub，真实商入配置后启用）
type MockSMSSender struct {
	mu   sync.Mutex
	log  zerolog.Logger
	Err  error
	Sent []MockSend
}

// NewMockSMSSender 创建短信 mock 发送器
func NewMockSMSSender(log zerolog.Logger) *MockSMSSender {
	return &MockSMSSender{log: log}
}

// SendSMS 记录发送并返回注入错误（默认成功）
func (m *MockSMSSender) SendSMS(_ context.Context, patientID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.Sent = append(m.Sent, MockSend{PatientID: patientID, Content: content})
	m.log.Info().Str("patient_id", patientID).Msg("mock sms sent")
	return nil
}

// Sends 返回已发送记录快照（测试用）
func (m *MockSMSSender) Sends() []MockSend {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockSend, len(m.Sent))
	copy(out, m.Sent)
	return out
}
