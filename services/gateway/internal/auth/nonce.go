// Package auth — NonceStore：设备 nonce 一次性消费的去重存储（T606）。
//
// 存储面选型：gateway 单实例部署，进程内 map + TTL 惰性清理足够——与 T091
// provision-key 限流的内存令牌桶同款先例；多实例部署需 Redis 共享状态
// （Phase 2，架构 §4.7 预留），届时只换本结构实现，键形状与 Check 语义不变。
//
// 键形状两档（T606 卡面 D3 兼容策略）：
//   - 严格档（keyTS=false）：键 = device_id + nonce。重放防护本体：
//     同设备同 nonce 在 TTL 内第二次出现即判重放。
//   - 兼容档（keyTS=true）：X-Timestamp 纳入键。固件不换 nonce 时的平台侧
//     兼容档——真重放（原样重发、时间戳不变）仍被拒；固件带新时间戳的
//     正常上报不受累。影子观测（DEVICE_NONCE_ENFORCE 未设/为 off）固定用
//     严格档计数，读数即「若切严格档会有多少笔被拒」。
package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// nonceEntry 单个 nonce 键的登记记录。
// lastSeen 在重放时刷新（滑动窗）：严格档下固定 nonce 的设备只要持续上报
// 就始终处于已登记态，换新 nonce 才能重新通过——一次性消费语义对
// 「永不换 nonce」的固件也成立。
type nonceEntry struct {
	firstSeen time.Time
	lastSeen  time.Time
	count     int64
}

// maxNonceEntries 惰性清理触发水位：键数达到即先扫过期，扫完仍超则逐出
// 最早一笔，保证进程内内存有界。
const maxNonceEntries = 4096

// NonceStore 进程内 nonce 去重存储（sync.Mutex 保护，并发安全）。
type NonceStore struct {
	mu    sync.Mutex
	seen  map[string]*nonceEntry
	ttl   time.Duration
	keyTS bool // true = 兼容档，X-Timestamp 纳入键
}

// NewNonceStore 构造去重存储；keyTS=true 为兼容档（X-Timestamp 纳入键）。
func NewNonceStore(ttl time.Duration, keyTS bool) *NonceStore {
	return &NonceStore{seen: make(map[string]*nonceEntry), ttl: ttl, keyTS: keyTS}
}

// nonceKey 归一化键：各段之间 0x00 分隔防拼接歧义，取 sha256 前 16 hex。
// nonce 值本身不落任何日志/存储明文面（凭据形状）。
func nonceKey(deviceID, nonce, timestamp string, keyTS bool) string {
	material := deviceID + "\x00" + nonce
	if keyTS {
		material += "\x00" + timestamp
	}
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])[:16]
}

// Check 登记一次 (device, nonce) 出现并回答「TTL 内是否已见过」。
// replay=true 表示该键在 TTL 内第二次及以后的出现（重放）；
// occurrences 为该键累计出现次数（影子观测的重复计数读数）。
func (s *NonceStore) Check(deviceID, nonce, timestamp string, now time.Time) (replay bool, occurrences int64) {
	key := nonceKey(deviceID, nonce, timestamp, s.keyTS)
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.seen) >= maxNonceEntries {
		s.sweepLocked(now)
		if len(s.seen) >= maxNonceEntries {
			s.evictOldestLocked()
		}
	}

	e, ok := s.seen[key]
	if ok && now.Sub(e.lastSeen) < s.ttl {
		e.lastSeen = now
		e.count++
		return true, e.count
	}
	s.seen[key] = &nonceEntry{firstSeen: now, lastSeen: now, count: 1}
	return false, 1
}

// sweepLocked 删除全部过期键（lastSeen 距 now ≥ ttl）。
func (s *NonceStore) sweepLocked(now time.Time) {
	for k, e := range s.seen {
		if now.Sub(e.lastSeen) >= s.ttl {
			delete(s.seen, k)
		}
	}
}

// evictOldestLocked 逐出 lastSeen 最早的一笔（水位兜底，保证内存有界）。
func (s *NonceStore) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, e := range s.seen {
		if first || e.lastSeen.Before(oldest) {
			oldestKey, oldest, first = k, e.lastSeen, false
		}
	}
	if !first {
		delete(s.seen, oldestKey)
	}
}
