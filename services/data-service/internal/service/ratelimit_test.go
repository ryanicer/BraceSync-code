package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannel_BurstAndDeny(t *testing.T) {
	ch := NewChannel(1, 3)
	now := time.Now()
	ch.now = func() time.Time { return now }

	// burst=3：连续 3 次放行，第 4 次拒绝
	require.True(t, ch.Allow("DEV1"))
	require.True(t, ch.Allow("DEV1"))
	require.True(t, ch.Allow("DEV1"))
	assert.False(t, ch.Allow("DEV1"))

	// 设备间独立
	assert.True(t, ch.Allow("DEV2"))
}

func TestChannel_Refill(t *testing.T) {
	ch := NewChannel(0.5, 1) // 每 2s 补 1 令牌
	now := time.Now()
	ch.now = func() time.Time { return now }

	require.True(t, ch.Allow("DEV1"))
	assert.False(t, ch.Allow("DEV1"))

	now = now.Add(1 * time.Second) // 补 0.5 令牌，仍不足
	assert.False(t, ch.Allow("DEV1"))

	now = now.Add(1 * time.Second) // 累计 1 令牌
	assert.True(t, ch.Allow("DEV1"))
	assert.False(t, ch.Allow("DEV1"))

	// 补充上限 = burst：长时间空闲后也不会超过 burst
	now = now.Add(10 * time.Minute)
	assert.True(t, ch.Allow("DEV1"))
	assert.False(t, ch.Allow("DEV1"))
}

func TestChannel_RetryAfterSec(t *testing.T) {
	assert.Equal(t, 2, NewChannel(0.5, 1).RetryAfterSec())
	assert.Equal(t, 1, NewChannel(2, 1).RetryAfterSec())
	assert.Equal(t, 1, NewChannel(10, 1).RetryAfterSec()) // 至少 1s
}

func TestRateLimiter_IndependentChannels(t *testing.T) {
	limiter := NewRateLimiter(1, 1, 1, 1)
	require.True(t, limiter.Realtime.Allow("DEV1"))
	assert.False(t, limiter.Realtime.Allow("DEV1"))
	// 实时通道耗尽不影响补传通道
	assert.True(t, limiter.Batch.Allow("DEV1"))

	// 补传通道耗尽不影响实时通道
	require.True(t, limiter.Batch.Allow("DEV2"))
	assert.False(t, limiter.Batch.Allow("DEV2"))
	assert.True(t, limiter.Realtime.Allow("DEV2"))
}

func TestNewDefaultRateLimiter(t *testing.T) {
	limiter := NewDefaultRateLimiter()
	require.NotNil(t, limiter.Realtime)
	require.NotNil(t, limiter.Batch)
	// 补传配额低于实时通道（架构 §3.5）
	assert.Less(t, limiter.Batch.ratePerSec, limiter.Realtime.ratePerSec)
	assert.Less(t, limiter.Batch.burst, limiter.Realtime.burst)
}
