// Package auth — 设备验签与身份认证接口（类型与纯函数；T032 已实现，见 verifier.go/jwt.go）
//
// 测试用例 src: docs/ §3.2（S1-S7）
// 参照：scripts/dev/device-simulator/cmd/sign.go（参考实现）
//
// 签名串格式（T067 对齐硬件清单 §2.2，6 行 \n 分隔，无尾随换行）：
//
//	{METHOD}\n{path}\n{device_id}\n{timestamp_unix_sec}\n{nonce}\n{body_sha256_hex}
//
// 验证流程（verifier.go）：
//  1. 校验 X-Timestamp 时间窗（默认档 ±5min；上报组与校时组各用专用档，见下方三个常量）
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

// SignatureTimeWindow 签名有效时间窗（分钟）—— VerifySignature 的默认档，
// 上报组与校时组都不再用它（各自有专用档），保留它是为了让包内单测与
// 任何「默认档」调用点仍有一个未放宽的基准可比。
const SignatureTimeWindow = 5

// DeviceReportWindow 上报端点（POST /device/records 与 /records/batch）专用时间窗（分钟，T570）。
// T570 临时：设备时钟偏 681s（见 T564），2 天后复核，治本在设备侧校时。
// 取 30 分钟（1800 秒）的理由：现网单点偏差 681 秒（x_timestamp=1791126964、
// time=1791127645，差正好 681），300 秒的默认档必然拒；1800 秒在 681 秒之外
// 还留 1119 秒余量。下游 data-service 对 body 里的 timestamp 只卡
// 「不早于 2026-01-01、不晚于 now+1 小时」，两头都比这一档宽，
// 所以上报侧真正生效的界就是这里，放宽不会被下游另一道界挡回来。
// 只放宽窗口这一维：HMAC 比对、常量时间比较、body 上限、设备注册状态一律不变。
const DeviceReportWindow = 30

// DeviceTimeSyncWindow 校时端点专用时间窗（分钟，T550）。
// 上报组用 ±DeviceReportWindow（T570 临时档）；/device/time 需要更宽的窗，
// 因为时钟已经超差的设备若与上报同窗，就被锁在校时门外、拿不到校时，
// 形成「越不准越进不来」的死锁。
// 只放宽这一个端点的时间窗：HMAC 比对、设备注册状态、body 上限一律不变。
const DeviceTimeSyncWindow = 24 * 60

// NonceDedupTTL Nonce 去重 TTL（分钟）。T606：10 → 60，取上报时间窗
// DeviceReportWindow（30min）的 2 倍上限（卡面建议 TTL 取窗口值的 1~2 倍）：
// 重放者在整个签名有效窗内重发都命中同一登记，窗口闭合后签名本身也已过期。
const NonceDedupTTL = 60

// VerifyResult 验签结果
type VerifyResult struct {
	Valid        bool
	ErrorCode    string // 20401=签名错误, 20402=时钟异常, 20403=nonce 重放(T606), 20404=设备未注册, 20409=未绑定患者
	ErrorMessage string
	// T564 取证：超窗那一支带出「设备声明的时刻比服务端早/晚几秒」（带符号，设备−服务端；
	// 原值本身由调用方按 X-Timestamp 头落日志）。
	// SkewMeasured=false 表示这个数没测出来（X-Timestamp 解析失败），
	// 此时零值是占位，不许被读成「偏差 0 秒」。
	SkewSec      int64
	SkewMeasured bool
	// T606 nonce 防重放读数：NonceReplay=true 表示同设备同 nonce 在 TTL 内
	// 第二次及以后的出现被检出；NonceOccurrences 是该 nonce 的累计出现次数。
	// enforce 模式下 Valid=false 且 ErrorCode=20403；影子模式下 Valid=true
	// （放行），两个字段仍置位，由调用方落观测日志。
	NonceReplay      bool
	NonceOccurrences int64
}

// DeviceSigVerifier 设备签名验证器（实现见 verifier.go，T032 转绿）。
// 零值可用（nonce 存储未接线时 VerifyNonce 恒放行）；经 WithNonceStore
// 接线后 nonce 去重生效，EnforceNonce 决定拒（20403）还是影子放行。
type DeviceSigVerifier struct {
	nonces       *NonceStore // T606 接线的去重存储；nil = 未接线
	EnforceNonce bool        // true = 重放拒 20403；false = 影子观测（检出但不拒）
}

// WithNonceStore 接线 nonce 去重存储（返回 v 以便链式构造）。
func (v *DeviceSigVerifier) WithNonceStore(s *NonceStore) *DeviceSigVerifier {
	v.nonces = s
	return v
}

// BodySHA256Hex 返回请求 body 的 SHA256 hex（硬件清单 §2.2）。
// 空 body = hex(sha256("")) = e3b0c442...（空字符串的哈希，非空串）。
func BodySHA256Hex(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

// HMACSHA256 计算 HMAC-SHA256 签名（参考实现，对齐 device-simulator）
func HMACSHA256(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// BuildSignString 构造签名字符串（T067 6 行 \n 分隔格式）。
// 签名串 = {METHOD}\n{path}\n{device_id}\n{timestamp_unix_sec}\n{nonce}\n{body_sha256_hex}
func BuildSignString(method, path, deviceID, nonce, body string, ts time.Time) string {
	return fmt.Sprintf("%s\n%s\n%s\n%d\n%s\n%s", method, path, deviceID, ts.Unix(), nonce, BodySHA256Hex(body))
}

// IsTimestampInWindow 检查时间戳是否在 ±windowMinutes 窗口内
func IsTimestampInWindow(deviceTime, serverTime time.Time, windowMinutes int) bool {
	diff := deviceTime.Sub(serverTime)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(windowMinutes)*time.Minute
}
