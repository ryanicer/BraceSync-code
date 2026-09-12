// Package auth — DeviceSigVerifier 实现侧测试（T032 转绿；不与 Ella auth_test.go 重叠）
//
// T067：签名串对齐硬件清单 §2.2 的 6 行 \n 分隔格式
//
//	{METHOD}\n{path}\n{device_id}\n{timestamp_unix_sec}\n{nonce}\n{body_sha256_hex}
package auth

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// testNonce 32 字符 hex 测试固定 nonce（硬件清单 §2.2 要求 32 hex）
const testNonce = "0123456789abcdef0123456789abcdef"

func signedHeaders(secret, method, path, body string, ts time.Time) (string, string) {
	return strconv.FormatInt(ts.Unix(), 10),
		HMACSHA256(secret, BuildSignString(method, path, "D1", testNonce, body, ts))
}

// TestBuildSignString_SixLineFormat 验证签名串为 6 行 \n 分隔格式（硬件清单 §2.2）。
func TestBuildSignString_SixLineFormat(t *testing.T) {
	ts := time.Unix(1723028400, 0)
	got := BuildSignString("GET", "/api/v1/device/time", "D1", testNonce, "", ts)

	// 空 body → hex(sha256("")) = e3b0c442...
	want := "GET\n/api/v1/device/time\nD1\n1723028400\n" + testNonce + "\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	assert.Equal(t, want, got, "签名串须为 6 行 \\n 分隔，含 device_id/nonce/body_sha256_hex")
}

func TestVerifySignature_Valid(t *testing.T) {
	v := &DeviceSigVerifier{}
	now := time.Now()
	tsStr, sig := signedHeaders("dev-secret", "POST", "/api/v1/device/records", `{"device_id":"D1"}`, now)

	res := v.VerifySignature("POST", "/api/v1/device/records", `{"device_id":"D1"}`, tsStr, sig, "D1", "dev-secret", testNonce, now)
	assert.True(t, res.Valid, "合法签名应通过")
}

func TestVerifySignature_TamperedBody_20401(t *testing.T) {
	v := &DeviceSigVerifier{}
	now := time.Now()
	tsStr, sig := signedHeaders("dev-secret", "POST", "/api/v1/device/records", `{"pressures":[1]}`, now)

	res := v.VerifySignature("POST", "/api/v1/device/records", `{"pressures":[99]}`, tsStr, sig, "D1", "dev-secret", testNonce, now)
	assert.False(t, res.Valid)
	assert.Equal(t, "20401", res.ErrorCode, "篡改 body → 20401")
}

func TestVerifySignature_WrongSecret_20401(t *testing.T) {
	v := &DeviceSigVerifier{}
	now := time.Now()
	tsStr, sig := signedHeaders("secret-A", "POST", "/api/v1/device/records", "body", now)

	res := v.VerifySignature("POST", "/api/v1/device/records", "body", tsStr, sig, "D1", "secret-B", testNonce, now)
	assert.False(t, res.Valid)
	assert.Equal(t, "20401", res.ErrorCode)
}

func TestVerifySignature_BadTimestamp_20402(t *testing.T) {
	v := &DeviceSigVerifier{}
	res := v.VerifySignature("POST", "/api/v1/device/records", "body", "not-a-number", "sig", "D1", "secret", testNonce, time.Now())
	assert.False(t, res.Valid)
	assert.Equal(t, "20402", res.ErrorCode, "X-Timestamp 非法 → 20402")
}

func TestVerifySignature_TimestampOutOfWindow_20402(t *testing.T) {
	v := &DeviceSigVerifier{}
	now := time.Now()
	stale := now.Add(-6 * time.Minute)
	tsStr, sig := signedHeaders("dev-secret", "POST", "/api/v1/device/records", "body", stale)

	res := v.VerifySignature("POST", "/api/v1/device/records", "body", tsStr, sig, "D1", "dev-secret", testNonce, now)
	assert.False(t, res.Valid)
	assert.Equal(t, "20402", res.ErrorCode, "±5min 时间窗外 → 20402")
}

func TestVerifySignature_WindowBoundary_Pass(t *testing.T) {
	v := &DeviceSigVerifier{}
	now := time.Unix(time.Now().Unix(), 0) // 秒级对齐（X-Timestamp 为 Unix 秒）
	edge := now.Add(-5 * time.Minute)      // 恰好 5min 边界内
	tsStr, sig := signedHeaders("dev-secret", "POST", "/api/v1/device/records", "body", edge)

	res := v.VerifySignature("POST", "/api/v1/device/records", "body", tsStr, sig, "D1", "dev-secret", testNonce, now)
	assert.True(t, res.Valid, "恰好 ±5min 应放行")
}

func TestVerifySignature_EmptyBody_Valid(t *testing.T) {
	v := &DeviceSigVerifier{}
	now := time.Now()
	tsStr, sig := signedHeaders("dev-secret", "GET", "/api/v1/device/time", "", now)

	res := v.VerifySignature("GET", "/api/v1/device/time", "", tsStr, sig, "D1", "dev-secret", testNonce, now)
	assert.True(t, res.Valid, "空体校时请求验签应通过")
}

// TestVerifySignature_NonceMismatch_20401 签名串包含 nonce，nonce 不一致 → 20401。
func TestVerifySignature_NonceMismatch_20401(t *testing.T) {
	v := &DeviceSigVerifier{}
	now := time.Now()
	tsStr, sig := signedHeaders("dev-secret", "POST", "/api/v1/device/records", "body", now)

	// 服务端用不同 nonce 验签 → 签名串不一致
	res := v.VerifySignature("POST", "/api/v1/device/records", "body", tsStr, sig, "D1", "dev-secret",
		"ffffffffffffffffffffffffffffffff", now)
	assert.False(t, res.Valid)
	assert.Equal(t, "20401", res.ErrorCode, "nonce 不一致 → 20401")
}

func TestVerifyNonce_Placeholder_Pass(t *testing.T) {
	v := &DeviceSigVerifier{}
	res := v.VerifyNonce("D1", "nonce-abc", time.Now())
	assert.True(t, res.Valid, "Redis 接入前 Nonce 恒放行（TODO 已标注）")
}
