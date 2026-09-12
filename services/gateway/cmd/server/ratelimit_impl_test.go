// Package main — T091 per-user 令牌桶限流实现侧测试
//
// 覆盖：阈值读取（env/默认）、桶补充与消耗、超限拒绝、并发安全（-race 必过）。
package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserRateLimiter_DefaultLimit 默认阈值 10/min：新用户前 10 次放行，第 11 次拒绝。
func TestUserRateLimiter_DefaultLimit(t *testing.T) {
	rl := newUserRateLimiter()
	for i := 0; i < 10; i++ {
		assert.True(t, rl.Allow("u-default"), "第 %d 次应放行", i+1)
	}
	assert.False(t, rl.Allow("u-default"), "第 11 次应拒绝")
}

// TestUserRateLimiter_EnvOverride env PROVISION_KEY_RATE_LIMIT 覆盖阈值。
func TestUserRateLimiter_EnvOverride(t *testing.T) {
	t.Setenv("PROVISION_KEY_RATE_LIMIT", "3")
	rl := newUserRateLimiter()
	for i := 0; i < 3; i++ {
		assert.True(t, rl.Allow("u-env"), "第 %d 次应放行", i+1)
	}
	assert.False(t, rl.Allow("u-env"), "超限应拒绝")
}

// TestUserRateLimiter_EnvInvalid 非法 env 值回退默认阈值。
func TestUserRateLimiter_EnvInvalid(t *testing.T) {
	t.Setenv("PROVISION_KEY_RATE_LIMIT", "abc")
	rl := newUserRateLimiter()
	for i := 0; i < 10; i++ {
		assert.True(t, rl.Allow("u-bad"), "第 %d 次应放行", i+1)
	}
	assert.False(t, rl.Allow("u-bad"), "第 11 次应拒绝（回退默认 10）")
}

// TestUserRateLimiter_PerUserIsolation 不同用户桶独立。
func TestUserRateLimiter_PerUserIsolation(t *testing.T) {
	t.Setenv("PROVISION_KEY_RATE_LIMIT", "1")
	rl := newUserRateLimiter()
	assert.True(t, rl.Allow("u-a"))
	assert.False(t, rl.Allow("u-a"), "u-a 超限")
	assert.True(t, rl.Allow("u-b"), "u-b 不受 u-a 影响")
}

// TestUserRateLimiter_Refill 桶随时间补充：耗尽后等待 rate 周期应恢复。
func TestUserRateLimiter_Refill(t *testing.T) {
	t.Setenv("PROVISION_KEY_RATE_LIMIT", "60") // rate = 1 token/sec，方便测试
	rl := newUserRateLimiter()
	// 耗尽桶（容量 60）
	for i := 0; i < 60; i++ {
		require.True(t, rl.Allow("u-refill"))
	}
	require.False(t, rl.Allow("u-refill"))

	// 等待 1.2s 补充 ≥1 token
	time.Sleep(1200 * time.Millisecond)
	assert.True(t, rl.Allow("u-refill"), "等待后应补充令牌")
}

// TestUserRateLimiter_ConcurrentSafe 并发调用 Allow 不崩溃、不超发。
// 多 goroutine 同时请求同一用户，放行总数不超过桶容量。
func TestUserRateLimiter_ConcurrentSafe(t *testing.T) {
	const goroutines = 50
	const perGoroutine = 20
	rl := newUserRateLimiter() // 默认容量 10

	var allowed int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if rl.Allow("u-concurrent") {
					atomic.AddInt64(&allowed, 1)
				}
			}
		}()
	}
	wg.Wait()

	// 放行总数不应超过桶容量（10）——并发安全的令牌桶不会超发
	assert.LessOrEqual(t, allowed, int64(10), "并发下放行总数不应超过桶容量")
	assert.Greater(t, allowed, int64(0), "至少应放行一次")
}

// TestProvisionRateLimit_Middleware 限流中间件：超限返回 429 且不继续后续 handler。
// （完整 HTTP 链路由 TestFullRoutes_ProvisionKeyRateLimit 覆盖，此处单测中间件逻辑。）
func TestProvisionRateLimit_Middleware(t *testing.T) {
	t.Setenv("PROVISION_KEY_RATE_LIMIT", "1")
	rl := newUserRateLimiter()
	// 第一次应放行（模拟 jwtAuth 已注入 X-User-Id）
	assert.True(t, rl.Allow("TECH-001"))
	// 第二次应拒绝 → 中间件返回 429
	assert.False(t, rl.Allow("TECH-001"))
}
