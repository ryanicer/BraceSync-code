// Package main provides HMAC-SHA256 signing utilities for device simulator.
// 对齐：docs/ §4 · docs/ §3.2 · 硬件清单 §2.2（T067 6 行 \n 分隔格式）
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// RandomNonce 生成 32 字符 hex nonce（16 随机字节，硬件清单 §2.2）。
func RandomNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// BodySHA256Hex 返回请求 body 的 SHA256 hex（空 body = hex(sha256(""))）。
func BodySHA256Hex(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

// SignDeviceRequest generates HMAC-SHA256 signature for device API requests.
// Signature string format (§2.2, 6-line \n separated, no trailing newline):
//
//	{METHOD}\n{path}\n{device_id}\n{timestamp_unix_sec}\n{nonce}\n{body_sha256_hex}
func SignDeviceRequest(secret, method, path, deviceID, nonce, body string, ts time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	sigStr := fmt.Sprintf("%s\n%s\n%s\n%d\n%s\n%s", method, path, deviceID, ts.Unix(), nonce, BodySHA256Hex(body))
	mac.Write([]byte(sigStr))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyDeviceSignature verifies a device HMAC-SHA256 signature.
// Returns true if the signature matches.
func VerifyDeviceSignature(secret, method, path, deviceID, nonce, body string, ts time.Time, expectedSig string) bool {
	actualSig := SignDeviceRequest(secret, method, path, deviceID, nonce, body, ts)
	return hmac.Equal([]byte(actualSig), []byte(expectedSig))
}

// IsTimestampValid checks if the request timestamp is within ±5 minutes of server time.
// This prevents replay attacks by rejecting requests with stale timestamps.
func IsTimestampValid(ts time.Time, serverTime time.Time, windowMinutes int) bool {
	diff := serverTime.Sub(ts)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(windowMinutes)*time.Minute
}

// RFC4231TC2 key/input for self-validation
const rfc4231TC2Key = "Jefe"
const rfc4231TC2Input = "what do ya want for nothing?"
const rfc4231TC2Expected = "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
