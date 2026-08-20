package service

import (
	"math"
	"sync"
	"time"
)

// 按 device 维度令牌桶限流（架构 §3.5）：
// 单帧实时与批量补传使用**独立限流通道**，补传配额低于实时通道，
// 防补传风暴挤占实时上报（架构 §1.4 突发估算）。

// 默认配额（可经环境变量覆盖）：
//
//	实时通道：2 rps / burst 10（常态 ~0.6 rps 全平台，单设备远低于此）
//	补传通道：0.5 rps / burst 2（单设备每 4s 一批，100 帧/批足够 7 天缓存回传）
const (
	DefaultRealtimeRate  = 2.0
	DefaultRealtimeBurst = 10.0
	DefaultBatchRate     = 0.5
	DefaultBatchBurst    = 2.0
)

// tokenBucket 单设备令牌桶
type tokenBucket struct {
	tokens float64
	last   time.Time
}

// Channel 一条限流通道（device_id → 令牌桶）
type Channel struct {
	ratePerSec float64 // 令牌填充速率
	burst      float64 // 桶容量

	mu      sync.Mutex
	buckets map[string]*tokenBucket
	now     func() time.Time
}

// NewChannel 创建限流通道
func NewChannel(ratePerSec, burst float64) *Channel {
	return &Channel{
		ratePerSec: ratePerSec,
		burst:      burst,
		buckets:    make(map[string]*tokenBucket),
		now:        time.Now,
	}
}

// Allow 为 deviceID 消耗 1 个令牌；不足返回 false
func (ch *Channel) Allow(deviceID string) bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	now := ch.now()
	b, ok := ch.buckets[deviceID]
	if !ok {
		b = &tokenBucket{tokens: ch.burst, last: now}
		ch.buckets[deviceID] = b
	}
	// 按流逝时间补充令牌（上限 burst）
	b.tokens = math.Min(ch.burst, b.tokens+now.Sub(b.last).Seconds()*ch.ratePerSec)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RetryAfterSec 触发限流后建议的退避秒数（补满 1 个令牌所需时间，至少 1s）
func (ch *Channel) RetryAfterSec() int {
	sec := math.Ceil(1.0 / ch.ratePerSec)
	if sec < 1 {
		sec = 1
	}
	return int(sec)
}

// RateLimiter 双通道限流器：Realtime=单帧实时，Batch=批量补传
type RateLimiter struct {
	Realtime *Channel
	Batch    *Channel
}

// NewRateLimiter 创建双通道限流器
func NewRateLimiter(realtimeRate, realtimeBurst, batchRate, batchBurst float64) *RateLimiter {
	return &RateLimiter{
		Realtime: NewChannel(realtimeRate, realtimeBurst),
		Batch:    NewChannel(batchRate, batchBurst),
	}
}

// NewDefaultRateLimiter 默认配额限流器
func NewDefaultRateLimiter() *RateLimiter {
	return NewRateLimiter(DefaultRealtimeRate, DefaultRealtimeBurst, DefaultBatchRate, DefaultBatchBurst)
}
