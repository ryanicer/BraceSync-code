// Package auth — NonceStore 与 VerifyNonce 防重放测试（T606）
//
// 覆盖卡面判据 D1（同 nonce 重发必被拒）与 D2（20403 与 20401/20402 可区分）：
// 首发放行/重放拒、TTL 过期后放行、跨设备隔离、影子模式检出但不拒、
// 兼容档（X-Timestamp 纳入键）、空 nonce 恒放行。
package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// base 固定基准时刻，避免依赖真实时钟
var base = time.Unix(1700000000, 0)

func TestNonceStore_FirstPass_SecondReplay(t *testing.T) {
	s := NewNonceStore(NonceDedupTTL*time.Minute, false)
	replay, n := s.Check("D1", "nonce-abc", "1700000000", base)
	assert.False(t, replay, "首次出现放行")
	assert.Equal(t, int64(1), n)

	replay, n = s.Check("D1", "nonce-abc", "1700000000", base.Add(time.Minute))
	assert.True(t, replay, "TTL 内同一 nonce 第二次出现 → 重放")
	assert.Equal(t, int64(2), n, "重复计数递增（影子观测读数）")
}

func TestNonceStore_TTLExpiry_PassesAgain(t *testing.T) {
	s := NewNonceStore(2*time.Minute, false) // 缩短 TTL 便于测试
	replay, _ := s.Check("D1", "nonce-abc", "1700000000", base)
	assert.False(t, replay)
	replay, _ = s.Check("D1", "nonce-abc", "1700000000", base.Add(90*time.Second))
	assert.True(t, replay, "TTL 内仍判重放")
	// 重放会刷新 lastSeen（滑动窗）：过期从最后一次出现起算，
	// 所以第三发要在「第二次出现的时刻 + TTL」之后才放行。
	replay, _ = s.Check("D1", "nonce-abc", "1700000000", base.Add(90*time.Second+3*time.Minute))
	assert.False(t, replay, "距最后一次出现超过 TTL 后同一 nonce 放行（重新登记）")
}

func TestNonceStore_CrossDevice_Isolated(t *testing.T) {
	s := NewNonceStore(NonceDedupTTL*time.Minute, false)
	replay, _ := s.Check("D1", "nonce-abc", "1700000000", base)
	assert.False(t, replay)
	replay, _ = s.Check("D2", "nonce-abc", "1700000000", base)
	assert.False(t, replay, "同 nonce 不同设备 → 各自独立的键，不判重放")
}

func TestVerifyNonce_Enforce_ReplayRejected_20403(t *testing.T) {
	v := (&DeviceSigVerifier{}).WithNonceStore(NewNonceStore(NonceDedupTTL*time.Minute, false))
	v.EnforceNonce = true

	res := v.VerifyNonce("D1", "nonce-abc", "1700000000", base)
	assert.True(t, res.Valid, "首发放行")

	res = v.VerifyNonce("D1", "nonce-abc", "1700000000", base.Add(time.Minute))
	assert.False(t, res.Valid, "重放被拒（卡面判据 D1）")
	assert.Equal(t, "20403", res.ErrorCode, "错误码 20403，与 20401/20402 可区分（卡面判据 D2）")
	assert.Equal(t, "nonce replay detected", res.ErrorMessage)
	assert.True(t, res.NonceReplay)
	assert.Equal(t, int64(2), res.NonceOccurrences)
}

func TestVerifyNonce_Shadow_DetectedButAllowed(t *testing.T) {
	v := (&DeviceSigVerifier{}).WithNonceStore(NewNonceStore(NonceDedupTTL*time.Minute, false))
	// EnforceNonce 保持零值 false = 影子观测

	assert.True(t, v.VerifyNonce("D1", "nonce-abc", "1700000000", base).Valid)
	res := v.VerifyNonce("D1", "nonce-abc", "1700000000", base.Add(time.Minute))
	assert.True(t, res.Valid, "影子模式：重放被检出但不拒")
	assert.True(t, res.NonceReplay, "NonceReplay 置位供调用方落观测日志")
	assert.Equal(t, int64(2), res.NonceOccurrences)
	assert.Empty(t, res.ErrorCode, "影子模式不带 20403")
}

func TestVerifyNonce_TimestampKey_CompatMode(t *testing.T) {
	v := (&DeviceSigVerifier{}).WithNonceStore(NewNonceStore(NonceDedupTTL*time.Minute, true))
	v.EnforceNonce = true

	assert.True(t, v.VerifyNonce("D1", "fixed-nonce", "1700000000", base).Valid)
	// 固定 nonce + 新时间戳：兼容档下正常上报不受累
	res := v.VerifyNonce("D1", "fixed-nonce", "1700000060", base.Add(time.Minute))
	assert.True(t, res.Valid, "兼容档：nonce 相同但时间戳不同 → 不判重放")
	// 原样重发（同 nonce 同时间戳）：真重放仍被拒
	res = v.VerifyNonce("D1", "fixed-nonce", "1700000060", base.Add(2*time.Minute))
	assert.False(t, res.Valid, "兼容档：原样重发（时间戳不变）仍拒 20403")
	assert.Equal(t, "20403", res.ErrorCode)
}

func TestVerifyNonce_EmptyNonce_Pass(t *testing.T) {
	v := (&DeviceSigVerifier{}).WithNonceStore(NewNonceStore(NonceDedupTTL*time.Minute, false))
	v.EnforceNonce = true
	for i := 0; i < 3; i++ {
		res := v.VerifyNonce("D1", "", "1700000000", base)
		assert.True(t, res.Valid, "空 nonce 不参与去重（兼容未携带 X-Nonce 的既有模拟器），恒放行")
	}
}
