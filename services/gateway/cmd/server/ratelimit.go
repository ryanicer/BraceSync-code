// Package main — gateway per-user 内存令牌桶限流（T091 provision-key 端点）
//
// 背景：T067 配网密钥端点此前裸组免鉴权，任何未登录请求可领取任意设备密钥。
// T091 收紧后除 JWT+RBAC 外，加 per-user 限流防脚本刷领（设备身份冒用/伪造设备接入）。
//
// 实现：内存 token bucket，per-user（X-User-Id 维度），阈值走环境变量
// PROVISION_KEY_RATE_LIMIT（次/分钟/用户，默认 10）。单实例部署足够；多实例需
// Redis 共享状态（Phase 2，架构 §4.7 预留）。
package main

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// defaultProvisionRateLimitPerMin 默认限流阈值（次/分钟/用户，联调期建议值）
const defaultProvisionRateLimitPerMin = 10

// tokenBucket 单用户令牌桶
type tokenBucket struct {
	tokens   float64   // 当前可用令牌数
	lastTime time.Time // 上次补充时刻
}

// userRateLimiter per-user 内存令牌桶（sync.Mutex 保护，并发安全）
type userRateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     float64 // 每秒补充令牌数 = limit/60
	capacity float64 // 桶容量 = limit（突发上限）
}

// newUserRateLimiter 构造限流器；阈值从 env PROVISION_KEY_RATE_LIMIT 读取（非法/空回退默认）
func newUserRateLimiter() *userRateLimiter {
	limit := defaultProvisionRateLimitPerMin
	if v := os.Getenv("PROVISION_KEY_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	return &userRateLimiter{
		buckets:  make(map[string]*tokenBucket),
		rate:     float64(limit) / 60.0,
		capacity: float64(limit),
	}
}

// Allow 判断 userID 本次请求是否放行；并发安全。
// 新用户初始满桶（capacity），按 rate 持续补充，上限不超过 capacity。
func (l *userRateLimiter) Allow(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[userID]
	if !ok {
		b = &tokenBucket{tokens: l.capacity, lastTime: now}
		l.buckets[userID] = b
	}

	// 按经过时间补充令牌
	elapsed := now.Sub(b.lastTime).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.capacity {
			b.tokens = l.capacity
		}
		b.lastTime = now
	}

	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

// provisionRateLimit 限流中间件：依赖 jwtAuth 注入的 X-User-Id；超限返回 429。
// 审计日志记录 user_id/role/device_id/result，便于事后追溯刷领行为。
func provisionRateLimit(l *userRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetHeader("X-User-Id")
		if userID == "" {
			// jwtAuth 已通过，X-User-Id 不应为空；fail-closed 拒绝
			abortJSON(c, http.StatusUnauthorized, http.StatusUnauthorized,
				"unauthorized: missing user identity")
			return
		}
		if !l.Allow(userID) {
			log.Warn().Str("user_id", userID).Str("role", c.GetHeader("X-Role")).
				Str("device_id", c.Param("deviceId")).Str("path", c.Request.URL.Path).
				Str("result", "rate_limited").Msg("provision-key request denied")
			abortJSON(c, http.StatusTooManyRequests, http.StatusTooManyRequests,
				"rate limit exceeded")
			return
		}
		c.Next()
	}
}
