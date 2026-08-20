// Package auth — 设备验签与身份认证接口（类型与纯函数；T032 已实现，见 verifier.go/jwt.go）
//
// 测试用例 src: docs/ §3.2（S1-S7）
// 参照：scripts/dev/device-simulator/cmd/sign.go（参考实现）
//
// 验证流程（verifier.go）：
//  1. 校验 X-Timestamp 时间窗（±5min）
//  2. HMAC-SHA256 签名比对（常量时间）
//  3. Nonce 防重放待 gateway 接入 Redis 后实现（10min TTL，架构 §4.7）
//  4. 设备注册/绑定状态由 gateway 中间件依据密钥查询结果判定
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// SignatureTimeWindow 签名有效时间窗（分钟）
const SignatureTimeWindow = 5

// NonceDedupTTL Nonce 去重 TTL（分钟）
const NonceDedupTTL = 10

// VerifyResult 验签结果
type VerifyResult struct {
	Valid        bool
	ErrorCode    string // 20401=签名错误, 20402=时钟异常, 20404=设备未注册, 20409=未绑定患者
	ErrorMessage string
}

// DeviceSigVerifier 设备签名验证器（实现见 verifier.go，T032 转绿）
type DeviceSigVerifier struct{}

// HMACSHA256 计算 HMAC-SHA256 签名（参考实现，对齐 device-simulator）
// 签名串 = method + path + timestamp + body
func HMACSHA256(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// BuildSignString 构造签名字符串
func BuildSignString(method, path, body string, ts time.Time) string {
	return fmt.Sprintf("%s%s%d%s", method, path, ts.Unix(), body)
}

// IsTimestampInWindow 检查时间戳是否在 ±windowMinutes 窗口内
func IsTimestampInWindow(deviceTime, serverTime time.Time, windowMinutes int) bool {
	diff := deviceTime.Sub(serverTime)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(windowMinutes)*time.Minute
}
