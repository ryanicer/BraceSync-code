// Package testhelper provides BraceSync integration test utilities
// 对齐：T085 测试计划 · Phase 1 Fixture 搭建
//
// FixedClock 用于时间敏感测试的固定时钟注入（替代 wall-clock）
package testhelper

import (
	"sync"
	"time"
)

// FixedClock 提供确定性的时间控制，用于 JWT expiry 等时间判定场景
// 主要用途：
//   - bindToken TTL: 30min
//   - phoneToken TTL: 7 天
//   - access_token TTL: 7000s
type FixedClock struct {
	current time.Time
	mu      sync.Mutex
}

// NewFixedClock 创建新的固定时钟实例
func NewFixedClock(t time.Time) *FixedClock {
	return &FixedClock{
		current: t,
	}
}

// Now 返回当前固定时间
func (fc *FixedClock) Now() time.Time {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.current
}

// Add 在固定时间上增加 duration（非原子操作，需自行加锁）
func (fc *FixedClock) Add(d time.Duration) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.current = fc.current.Add(d)
}

// Set 设置固定时间为指定值
func (fc *FixedClock) Set(t time.Time) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.current = t
}

// Sleep 模拟时间延迟（仅记录时间前进，不实际休眠）
// 适用于需要时间推进但不想等待真实时间的测试
func (fc *FixedClock) Sleep(d time.Duration) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.current = fc.current.Add(d)
}

// Reset 重置为初始时间
func (fc *FixedClock) Reset(t time.Time) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.current = t
}
