// Package auth — DeviceSigVerifier 实现（T032：T002 桩转绿）
//
// 验签流程（T067 对齐硬件清单 §2.2 与 device-simulator sign.go 参考实现）：
//  1. X-Timestamp 解析失败 / 超出 ±SignatureTimeWindow → 20402（时钟异常，
//     设备按协议 §4.4 强制校时；与签名错误 20401 区分错误码）
//  2. 签名串 = 6 行 \n 分隔格式（BuildSignString，含 device_id/nonce/body_sha256_hex），
//     HMAC-SHA256(device_secret) 常量时间比对 → 不一致 20401
//
// 设备注册状态（20404）/绑定状态（20409）不在本函数职责内——由调用方
// （gateway 中间件）依据密钥查询结果判定；Nonce 防重放（VerifyNonce）
// 待 gateway 接入 Redis 后实现（T032 留 TODO，架构 §4.7 sec:nonce:*）。
package auth

import (
	"crypto/hmac"
	"strconv"
	"time"
)

// VerifySignature 验证设备请求签名（T002 桩转绿）。
// deviceID/deviceSecret/nonce 由调用方先行解析（密钥查询失败即 20404，不进入本函数）。
// nonce 取自 X-Nonce header，参与签名串（硬件清单 §2.2）。
// 返回 *VerifyResult 永不为 nil：Valid=true 或携带 ErrorCode。
func (v *DeviceSigVerifier) VerifySignature(method, path, body, timestampStr, signature, deviceID, deviceSecret, nonce string, serverTime time.Time) *VerifyResult {
	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return &VerifyResult{Valid: false, ErrorCode: "20402", ErrorMessage: "invalid X-Timestamp"}
	}
	deviceTime := time.Unix(ts, 0)
	if !IsTimestampInWindow(deviceTime, serverTime, SignatureTimeWindow) {
		return &VerifyResult{Valid: false, ErrorCode: "20402",
			ErrorMessage: "timestamp outside ±" + strconv.Itoa(SignatureTimeWindow) + "min window"}
	}

	want := HMACSHA256(deviceSecret, BuildSignString(method, path, deviceID, nonce, body, deviceTime))
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return &VerifyResult{Valid: false, ErrorCode: "20401", ErrorMessage: "signature mismatch"}
	}
	return &VerifyResult{Valid: true}
}

// VerifyNonce Nonce 防重放（T002 桩转绿——占位实现）。
// TODO（后续任务）：gateway 接入 Redis 后按架构 §4.7 实现
// SET NX sec:nonce:{device_id}:{nonce_hash} EX NonceDedupTTL*60，命中重复 → 20401。
// 当前无 Redis 依赖，恒放行（nonce 为空亦放行，兼容未携带 X-Nonce 的既有模拟器）。
func (v *DeviceSigVerifier) VerifyNonce(deviceID, nonce string, now time.Time) *VerifyResult {
	return &VerifyResult{Valid: true}
}
