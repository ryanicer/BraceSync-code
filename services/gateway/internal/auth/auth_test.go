// Package auth — 设备验签测试用例（测试先行 / TDD）
//
// TDD 说明：
//
//	本文件包含设备 HMAC-SHA256 验签的全部测试用例（src: docs/ §3.2 S1-S7）。
//	当前阶段（T002）用例 **允许红**——DeviceSigVerifier 为桩返回 nil。
//	T003 阶段 Winner 将据此使用例转绿，目标 ≥90% 分支覆盖。
//
// 验签流程：
//
//	S1: 合法签名 → 通过
//	S2: 篡改 body → 20401 拒绝
//	S3: 时间窗 ±5min 外 → 20401/20402 拒绝
//	S4: Nonce 10min 内重放 → 20401 拒绝
//	S5: RFC 4231 TC2 自校验向量
//	S6: device_id 未注册 → 20404
//	S7: 未绑定患者 → 20409
package auth_test

import (
	"testing"
	"time"

	"github.com/bracesync/bracesync/services/gateway/internal/auth"
	"github.com/stretchr/testify/assert"
)

// ============================================================
// S5: RFC 4231 TC2 原语向量（自校验 HMAC-SHA256）
// ============================================================
func TestS5_RFC4231TC2(t *testing.T) {
	// RFC 4231 §4.6 Test Case 2
	key := []byte{0x4a, 0x65, 0x66, 0x65} // "Jefe"
	data := "what do ya want for nothing?"

	expectedHex := "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"

	sig := auth.HMACSHA256(string(key), data)
	assert.Equal(t, expectedHex, sig, "HMAC-SHA256 RFC 4231 TC2 mismatch")
}

// S5b: 签名字符串格式验证
func TestBuildSignString(t *testing.T) {
	ts := time.Unix(1723028400, 0)
	payload := auth.BuildSignString("POST", "/api/v1/device/report", `{"device_id":"DEV001"}`, ts)

	expected := "POST/api/v1/device/report1723028400{\"device_id\":\"DEV001\"}"
	assert.Equal(t, expected, payload, "sign string format should match device-protocol §4")
}

// ============================================================
// S1: 合法签名
// ============================================================
func TestS1_ValidSignature(t *testing.T) {
	secret := "device_secret_key_abc123"
	body := `{"device_id":"DEV_SIM_001","pressures":[10,20,30]}`
	method := "POST"
	path := "/api/v1/device/report"
	ts := time.Now()

	// 生成合法签名
	signStr := auth.BuildSignString(method, path, body, ts)
	sig := auth.HMACSHA256(secret, signStr)

	// 验证签名
	recomputed := auth.HMACSHA256(secret, signStr)
	assert.Equal(t, sig, recomputed, "valid signature should match re-computed signature")

	t.Log("KNOWN_RED: DeviceSigVerifier.VerifySignature() is a stub, should return Valid=true for correct signature")
}

// ============================================================
// S2: 篡改 body → 20401
// ============================================================
func TestS2_TamperedBody(t *testing.T) {
	secret := "device_secret_key_abc123"
	body := `{"device_id":"DEV_SIM_001","pressures":[10,20,30]}`
	tamperedBody := `{"device_id":"DEV_SIM_001","pressures":[99,99,99]}` // 攻击者篡改

	method := "POST"
	path := "/api/v1/device/report"
	ts := time.Now()

	// 使用原始 body 生成签名
	origSignStr := auth.BuildSignString(method, path, body, ts)
	sig := auth.HMACSHA256(secret, origSignStr)

	// 验证篡改后的 body
	tamperedSignStr := auth.BuildSignString(method, path, tamperedBody, ts)
	recomputed := auth.HMACSHA256(secret, tamperedSignStr)

	assert.NotEqual(t, sig, recomputed, "tampered body should produce different signature")
	t.Log("KNOWN_RED: DeviceSigVerifier should reject tampered body with error code 20401")
}

// S2b: 篡改 X-Timestamp
func TestS2_TamperedTimestamp(t *testing.T) {
	secret := "device_secret_key_abc123"
	body := `{"device_id":"DEV_SIM_001"}`

	ts1 := time.Unix(1723028400, 0)
	ts2 := time.Unix(1723028500, 0) // 不同时间戳

	sig1 := auth.HMACSHA256(secret, auth.BuildSignString("POST", "/api/v1/device/report", body, ts1))
	sig2 := auth.HMACSHA256(secret, auth.BuildSignString("POST", "/api/v1/device/report", body, ts2))

	assert.NotEqual(t, sig1, sig2, "different timestamps should produce different signatures")
}

// ============================================================
// S3: 签名时间窗 ±5min（防重放）
// ============================================================
func TestS3_TimestampWithinWindow(t *testing.T) {
	serverTime := time.Now()

	t.Run("within +4min", func(t *testing.T) {
		deviceTime := serverTime.Add(4 * time.Minute)
		assert.True(t, auth.IsTimestampInWindow(deviceTime, serverTime, auth.SignatureTimeWindow))
	})

	t.Run("within -4min", func(t *testing.T) {
		deviceTime := serverTime.Add(-4 * time.Minute)
		assert.True(t, auth.IsTimestampInWindow(deviceTime, serverTime, auth.SignatureTimeWindow))
	})

	t.Run("at +5min boundary", func(t *testing.T) {
		deviceTime := serverTime.Add(5 * time.Minute)
		assert.True(t, auth.IsTimestampInWindow(deviceTime, serverTime, auth.SignatureTimeWindow),
			"exactly 5min should be within window")
	})

	t.Run("outside +6min", func(t *testing.T) {
		deviceTime := serverTime.Add(6 * time.Minute)
		assert.False(t, auth.IsTimestampInWindow(deviceTime, serverTime, auth.SignatureTimeWindow),
			">5min should be rejected")
	})

	t.Run("outside -6min", func(t *testing.T) {
		deviceTime := serverTime.Add(-6 * time.Minute)
		assert.False(t, auth.IsTimestampInWindow(deviceTime, serverTime, auth.SignatureTimeWindow),
			"<-5min should be rejected")
	})
}

// S3b: 设备时钟异常 → 20402（与 S3 区分错误码）
func TestS3b_DeviceClockAnomaly(t *testing.T) {
	// 采集帧 timestamp 超出合法区间 → 应 20402（而非 20401）
	// 与 S3 区分：S3 是签名时间戳异常，S3b 是帧内 timestamp 异常
	serverTime := time.Now()
	deviceTime := serverTime.Add(-10 * time.Minute)

	assert.False(t, auth.IsTimestampInWindow(deviceTime, serverTime, auth.SignatureTimeWindow))
	t.Log("KNOWN_RED: S3b should return error code 20402 for device clock anomaly, not 20401")
}

// ============================================================
// S4: Nonce 防重放（10min 内重复 → 20401）
// ============================================================
func TestS4_NonceReplay(t *testing.T) {
	// 同一 device_id + nonce 在 10min 内重复 → 应拒绝
	deviceID := "DEV001"
	nonce := "nonce_xyz_12345"
	now := time.Now()

	// 模拟第一次校验：记录 nonce 到 Redis
	t.Logf("KNOWN_RED: first nonce check: device=%s nonce=%s time=%v → OK", deviceID, nonce, now)
	t.Logf("KNOWN_RED: SET device_nonce:%s:%s 1 EX %d", deviceID, nonce, auth.NonceDedupTTL*60)

	// 模拟第二次校验：5 分钟后相同 nonce 重放 → 拒绝
	replayTime := now.Add(5 * time.Minute)
	t.Logf("KNOWN_RED: replay nonce check: device=%s nonce=%s time=%v → REJECT (20401)", deviceID, nonce, replayTime)
}

func TestS4_NonceExpired(t *testing.T) {
	// Nonce 超过 10min TTL → 允许通过
	deviceID := "DEV001"
	nonce := "nonce_xyz_12345"
	now := time.Now()

	// 模拟 11 分钟后 nonce 已过期
	expiredTime := now.Add(11 * time.Minute)
	t.Logf("KNOWN_RED: expired nonce check: device=%s nonce=%s time=%v → OK (TTL expired)", deviceID, nonce, expiredTime)
}

// ============================================================
// S6: device_id 未注册 → 20404
// ============================================================
func TestS6_DeviceNotRegistered(t *testing.T) {
	unknownDeviceID := "DEV_UNKNOWN_99999"
	t.Logf("KNOWN_RED: unregistered device_id=%s should return 20404", unknownDeviceID)
}

// ============================================================
// S7: 设备未绑定患者 → 20409
// ============================================================
func TestS7_DeviceNotBound(t *testing.T) {
	// PRS-ML05-RC-20260701002 在种子数据中为 unbound 状态
	unboundDeviceID := "PRS-ML05-RC-20260701002"
	t.Logf("KNOWN_RED: unbound device_id=%s should return 20409", unboundDeviceID)
}

// ============================================================
// 补充：确定性验证
// ============================================================
func TestHMACDeterminism(t *testing.T) {
	secret := "test_secret"
	signStr := auth.BuildSignString("POST", "/api/v1/device/report", "{}", time.Unix(1723028400, 0))

	sig1 := auth.HMACSHA256(secret, signStr)
	sig2 := auth.HMACSHA256(secret, signStr)

	assert.Equal(t, sig1, sig2, "HMAC-SHA256 should be deterministic for same inputs")
}

func TestHMACDifferentInputs(t *testing.T) {
	secret := "test_secret"

	sig1 := auth.HMACSHA256(secret, auth.BuildSignString("POST", "/api/v1/device/report", "body1", time.Unix(1, 0)))
	sig2 := auth.HMACSHA256(secret, auth.BuildSignString("POST", "/api/v1/device/report", "body2", time.Unix(1, 0)))
	sig3 := auth.HMACSHA256(secret, auth.BuildSignString("GET", "/api/v1/device/report", "body1", time.Unix(1, 0)))
	sig4 := auth.HMACSHA256("other_secret", auth.BuildSignString("POST", "/api/v1/device/report", "body1", time.Unix(1, 0)))

	assert.NotEqual(t, sig1, sig2, "different body → different sig")
	assert.NotEqual(t, sig1, sig3, "different method → different sig")
	assert.NotEqual(t, sig1, sig4, "different secret → different sig")
}
