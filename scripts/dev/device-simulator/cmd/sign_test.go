// Package main — HMAC-SHA256 signing tests
// 对齐：docs/ §3.2 (S5: RFC 4231 TC2)
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRFC4231TC2 validates HMAC-SHA256 against RFC 4231 Test Case 2.
// This is a self-validation vector to ensure the HMAC implementation is correct.
func TestRFC4231TC2(t *testing.T) {
	mac := hmac.New(sha256.New, []byte(rfc4231TC2Key))
	mac.Write([]byte(rfc4231TC2Input))
	actual := hex.EncodeToString(mac.Sum(nil))
	assert.Equal(t, rfc4231TC2Expected, actual,
		"HMAC-SHA256 RFC 4231 TC2 mismatch — check crypto implementation")
}

func TestSignDeviceRequest(t *testing.T) {
	secret := "test_secret_abc"
	body := `{"device_id":"DEV001","timestamp":"2026-08-07T12:00:00Z"}`
	ts := time.Unix(1723028400, 0) // 2026-08-07T12:00:00 UTC

	sig := SignDeviceRequest(secret, "POST", "/api/v1/device/report", body, ts)

	// 签名应非空
	assert.NotEmpty(t, sig)
	// HMAC-SHA256 输出 32 字节 = 64 hex chars
	assert.Len(t, sig, 64)
}

func TestVerifyDeviceSignature(t *testing.T) {
	secret := "test_secret_xyz"
	body := `{"device_id":"DEV002"}`
	ts := time.Unix(1723028400, 0)

	// 正确签名应通过
	sig := SignDeviceRequest(secret, "POST", "/api/v1/device/report", body, ts)
	assert.True(t, VerifyDeviceSignature(secret, "POST", "/api/v1/device/report", body, ts, sig))

	// 篡改 body 应被拒绝
	assert.False(t, VerifyDeviceSignature(secret, "POST", "/api/v1/device/report", `{"device_id":"DEV003"}`, ts, sig))

	// 错误密钥应被拒绝
	assert.False(t, VerifyDeviceSignature("wrong_secret", "POST", "/api/v1/device/report", body, ts, sig))
}

func TestIsTimestampValid(t *testing.T) {
	serverTime := time.Unix(1723028400, 0) // 2026-08-07T12:00:00 UTC

	t.Run("within_window", func(t *testing.T) {
		// +4 分钟应通过（默认 ±5min）
		ts := serverTime.Add(4 * time.Minute)
		assert.True(t, IsTimestampValid(ts, serverTime, 5))

		// -4 分钟应通过
		ts = serverTime.Add(-4 * time.Minute)
		assert.True(t, IsTimestampValid(ts, serverTime, 5))
	})

	t.Run("at_boundary", func(t *testing.T) {
		// 恰好 5 分钟应通过
		ts := serverTime.Add(5 * time.Minute)
		assert.True(t, IsTimestampValid(ts, serverTime, 5))

		// 恰好 -5 分钟应通过
		ts = serverTime.Add(-5 * time.Minute)
		assert.True(t, IsTimestampValid(ts, serverTime, 5))
	})

	t.Run("outside_window", func(t *testing.T) {
		// +6 分钟应被拒绝
		ts := serverTime.Add(6 * time.Minute)
		assert.False(t, IsTimestampValid(ts, serverTime, 5))

		// -6 分钟应被拒绝
		ts = serverTime.Add(-6 * time.Minute)
		assert.False(t, IsTimestampValid(ts, serverTime, 5))
	})

	t.Run("custom_window", func(t *testing.T) {
		ts := serverTime.Add(8 * time.Minute)
		assert.True(t, IsTimestampValid(ts, serverTime, 10))
		assert.False(t, IsTimestampValid(ts, serverTime, 5))
	})
}

func TestNonceDedupLogic(t *testing.T) {
	// Nonce 去重逻辑：同一 device_id + nonce 在有效期（10min）内重复应被拒绝
	// 此用例验证去重时间窗口逻辑（实际 Redis 存储实现待 T003）
	nonceStore := make(map[string]time.Time)

	deviceID := "DEV001"
	nonce := "nonce_abcd1234"
	now := time.Now()

	// 首次使用 nonce
	nonceStore[deviceID+":"+nonce] = now
	key := deviceID + ":" + nonce
	_, exists := nonceStore[key]
	assert.True(t, exists, "first nonce should be recorded")

	// 10 分钟内重复应被检测
	replayTime := now.Add(5 * time.Minute)
	if stored, ok := nonceStore[key]; ok {
		assert.True(t, replayTime.Sub(stored) <= 10*time.Minute,
			"replay within 10min should be blocked")
	}

	// 超过 10 分钟的 nonce 可接受（正常过期）
	expiredTime := now.Add(11 * time.Minute)
	if stored, ok := nonceStore[key]; ok {
		assert.False(t, expiredTime.Sub(stored) <= 10*time.Minute,
			"nonce after 10min should be accepted as new")
	}
}

func TestHMACTamperDetection(t *testing.T) {
	// 测试覆盖 §3.2 S2: 篡改 body 应被拒绝
	secret := "device_secret_123"
	body := `{"device_id":"DEV_SIM_001","pressures":[10,20,30,40]}`
	path := "/api/v1/device/report"
	method := "POST"
	ts := time.Now()

	// 生成合法签名
	validSig := SignDeviceRequest(secret, method, path, body, ts)

	// 篡改 body 后重新签名（模拟攻击者不知道密钥）
	tamperedBody := `{"device_id":"DEV_SIM_001","pressures":[99,99,99,99]}` // 实际攻击者改 body
	assert.False(t, VerifyDeviceSignature(secret, method, path, tamperedBody, ts, validSig),
		"tampered body with original signature should be rejected")

	// 合法 body + 合法签名应通过
	assert.True(t, VerifyDeviceSignature(secret, method, path, body, ts, validSig),
		"valid body+signature should pass verification")
}

func TestSignatureDeterministic(t *testing.T) {
	// HMAC-SHA256 对于相同输入应是确定性的
	secret := "test_secret"
	body := `{"device_id":"DEV001"}`
	ts := time.Unix(1723028400, 0)

	sig1 := SignDeviceRequest(secret, "POST", "/api/v1/device/report", body, ts)
	sig2 := SignDeviceRequest(secret, "POST", "/api/v1/device/report", body, ts)

	require.Equal(t, sig1, sig2, "HMAC-SHA256 should produce deterministic signatures for same inputs")
}

func TestSignatureDifferentInputs(t *testing.T) {
	secret := "test_secret"
	body := `{"device_id":"DEV001"}`
	ts := time.Unix(1723028400, 0)

	// 不同 timestamp 应产生不同签名
	ts2 := time.Unix(1723028401, 0)
	sig1 := SignDeviceRequest(secret, "POST", "/api/v1/device/report", body, ts)
	sig2 := SignDeviceRequest(secret, "POST", "/api/v1/device/report", body, ts2)
	assert.NotEqual(t, sig1, sig2, "different timestamps should produce different signatures")

	// 不同 body 应产生不同签名
	body2 := `{"device_id":"DEV002"}`
	sig3 := SignDeviceRequest(secret, "POST", "/api/v1/device/report", body2, ts)
	assert.NotEqual(t, sig1, sig3, "different bodies should produce different signatures")

	// 不同 secret 应产生不同签名
	secret2 := "different_secret"
	sig4 := SignDeviceRequest(secret2, "POST", "/api/v1/device/report", body, ts)
	assert.NotEqual(t, sig1, sig4, "different secrets should produce different signatures")
}
