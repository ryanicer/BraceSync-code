package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// alert:pending 降级队列与补偿幂等键（T010，架构 §3.4 / §4.7）：
//
//	alert:pending                    List  —— data-service LPUSH / 本服务 RPOP（FIFO）
//	alert:evaluated:{device}:{unix}  NX+TTL —— 补偿评估幂等键（重复入队不重复告警）
//	alert:notify_pending             List  —— 通知推送失败重试队列（T019）
const (
	keyAlertPending    = "alert:pending"
	keyEvalDedupPrefix = "alert:evaluated:"
	keyNotifyPending   = "alert:notify_pending"

	// evalDedupTTL 幂等键保留时长：覆盖补传窗口（7 天）内的重放即可，
	// 取 48h 平衡内存占用（SET NX 小 key）与去重需要。
	evalDedupTTL = 48 * time.Hour
)

// RedisPendingQueue alert:pending 队列访问（实现 consumer.Queue）
type RedisPendingQueue struct {
	rdb redis.Cmdable
}

// NewRedisPendingQueue 创建 RedisPendingQueue
func NewRedisPendingQueue(rdb redis.Cmdable) *RedisPendingQueue {
	return &RedisPendingQueue{rdb: rdb}
}

// Pop RPOP 一条负载；队列空返回 ok=false（redis.Nil 归一化，非错误）
func (q *RedisPendingQueue) Pop(ctx context.Context) (string, bool, error) {
	v, err := q.rdb.RPop(ctx, keyAlertPending).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// Len LLEN alert:pending（Prometheus alert_pending_queue_length 数据源）
func (q *RedisPendingQueue) Len(ctx context.Context) (int64, error) {
	return q.rdb.LLen(ctx, keyAlertPending).Result()
}

// RedisEvalDedup 补偿评估幂等键（实现 consumer.EvalDeduper）
type RedisEvalDedup struct {
	rdb redis.Cmdable
}

// NewRedisEvalDedup 创建 RedisEvalDedup
func NewRedisEvalDedup(rdb redis.Cmdable) *RedisEvalDedup { return &RedisEvalDedup{rdb: rdb} }

func evalDedupKey(deviceID string, ts time.Time) string {
	return fmt.Sprintf("%s%s:%d", keyEvalDedupPrefix, deviceID, ts.Unix())
}

// MarkEvaluated SET NX (device_id, timestamp) 幂等键；
// first=true 本次首评，first=false 该帧已评估过（重复入队跳过）
func (d *RedisEvalDedup) MarkEvaluated(ctx context.Context, deviceID string, ts time.Time) (bool, error) {
	return d.rdb.SetNX(ctx, evalDedupKey(deviceID, ts), "1", evalDedupTTL).Result()
}

// ─────────────────────────────────────────────────────────────
// RedisNotifyRetryQueue 通知推送重试队列（T019，实现 consumer.RetryQueue）
// ─────────────────────────────────────────────────────────────

// RedisNotifyRetryQueue Redis LPUSH/RPOP 通知重试队列
type RedisNotifyRetryQueue struct {
	rdb redis.Cmdable
}

// NewRedisNotifyRetryQueue 创建 RedisNotifyRetryQueue
func NewRedisNotifyRetryQueue(rdb redis.Cmdable) *RedisNotifyRetryQueue {
	return &RedisNotifyRetryQueue{rdb: rdb}
}

// Push LPUSH 非阻塞推入重试队列
func (q *RedisNotifyRetryQueue) Push(ctx context.Context, payload string) error {
	return q.rdb.LPush(ctx, keyNotifyPending, payload).Err()
}

// Pop RPOP 一条重试负载；队列空返回 ok=false
func (q *RedisNotifyRetryQueue) Pop(ctx context.Context) (string, bool, error) {
	v, err := q.rdb.RPop(ctx, keyNotifyPending).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// Run 常驻消费重试（由外部 goroutine 调用，轮询间隔由调用方控制）
func (q *RedisNotifyRetryQueue) Run(ctx context.Context, interval time.Duration) {
	// Run 实际由 HTTPNotifier.DrainRetryOnce 驱动，此处仅占位满足接口。
	// 生产使用：在 main.go 中 go notifier.RunRetry(ctx, interval)
	_ = ctx
	_ = interval
}
