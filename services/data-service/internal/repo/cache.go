package repo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis Key 设计（架构 §4.7）：
//   dev:lastseen:{device_id}   String，无 TTL      —— 状态机推导输入、中断扫描
//   rt:frame:{device_id}       String，TTL 2h      —— 后台 30s 轮询零 DB 命中
//   stat:today:{patient_id}    Hash，当日 24:00 过期 —— 今日佩戴分钟/最大压力/异常数
//   alert:pending              List                 —— data→alert 降级队列（§3.4）
//   rollup:recompute           List                 —— 补传触发的 daily rollup 重算任务队列
//   rollup:queued:{p}:{date}   String NX，TTL 48h   —— rollup 任务幂等去重标记

const (
	keyAlertPending = "alert:pending"
	keyRollupQueue  = "rollup:recompute"

	realtimeFrameTTL = 2 * time.Hour
	rollupMarkerTTL  = 48 * time.Hour
)

// KeyLastSeen dev:lastseen:{device_id}
func KeyLastSeen(deviceID string) string { return "dev:lastseen:" + deviceID }

// KeyRealtimeFrame rt:frame:{device_id}
func KeyRealtimeFrame(deviceID string) string { return "rt:frame:" + deviceID }

// KeyStatToday stat:today:{patient_id}
func KeyStatToday(patientID string) string { return "stat:today:" + patientID }

func rollupMarkerKey(patientID, date string) string {
	return fmt.Sprintf("rollup:queued:%s:%s", patientID, date)
}

// CacheStore Redis 缓存契约（service 层消费）
type CacheStore interface {
	// SetLastSeen 单调推进 dev:lastseen:{device_id}（帧时间 Unix 秒）：
	// 仅当新值大于现值时写入。乱序/延迟帧（设备后补旧帧）不得回退 lastseen，
	// 否则在线推导（≤2h）与佩戴中断扫描会误判。
	SetLastSeen(ctx context.Context, deviceID string, ts time.Time) error
	// GetLastSeen 读 lastseen；无记录返回 ok=false
	GetLastSeen(ctx context.Context, deviceID string) (ts time.Time, ok bool, err error)
	// SetRealtimeFrame SET rt:frame:{device_id} = 最新帧 JSON（TTL 2h）
	SetRealtimeFrame(ctx context.Context, deviceID, frameJSON string) error
	// GetRealtimeFrame 读最新帧 JSON；无记录返回空串
	GetRealtimeFrame(ctx context.Context, deviceID string) (string, error)
	// ApplyStatToday 增量更新 stat:today（Lua 原子执行）：
	// wearMinutes>0 时累加佩戴分钟；maxPressure 大于现值时刷新最大压力与点位；
	// abnormalDelta>0 时累加异常数；并续期至今日 24:00（Asia/Shanghai）
	ApplyStatToday(ctx context.Context, patientID string, wearMinutes int, maxPressure float64, maxPoint string, abnormalDelta int, expireAt time.Time) error
	// GetStatToday HGETALL stat:today:{patient_id}
	GetStatToday(ctx context.Context, patientID string) (map[string]string, error)
	// PushAlertPending 降级帧引用 LPUSH alert:pending（alert-service 常驻消费补偿）
	PushAlertPending(ctx context.Context, payloadJSON string) error
	// EnqueueRollup 投递受影响日期的 rollup 重算任务（SET NX 标记保证幂等）；
	// 返回是否实际入队（标记已存在则跳过）
	EnqueueRollup(ctx context.Context, patientID, date string, payloadJSON string) (queued bool, err error)
	// DequeueRollup RPOP rollup:recompute 队列（返回 payload JSON）；队列空返回空串
	DequeueRollup(ctx context.Context) (payloadJSON string, err error)
}

// statTodayScript 原子更新今日统计 Hash：
// ARGV: 1=wear_minutes 增量 2=max_pressure 3=max_point 4=abnormal 增量 5=PEXPIREAT 毫秒
var statTodayScript = redis.NewScript(`
local key = KEYS[1]
local wear = tonumber(ARGV[1])
local maxp = tonumber(ARGV[2])
local point = ARGV[3]
local abn = tonumber(ARGV[4])
local expireAt = tonumber(ARGV[5])
if wear > 0 then
  redis.call('HINCRBY', key, 'wear_minutes', wear)
end
if abn > 0 then
  redis.call('HINCRBY', key, 'abnormal_count', abn)
end
local cur = tonumber(redis.call('HGET', key, 'max_pressure') or '0')
if maxp > cur then
  redis.call('HSET', key, 'max_pressure', ARGV[2], 'max_point', point)
end
redis.call('PEXPIREAT', key, expireAt)
return 1
`)

// RedisCache CacheStore 的 go-redis 实现
type RedisCache struct {
	rdb *redis.Client
}

// NewRedisCache 创建 RedisCache
func NewRedisCache(rdb *redis.Client) *RedisCache { return &RedisCache{rdb: rdb} }

// lastSeenScript 单调推进 lastseen（原子）：ARGV[1]=新帧时间 Unix 秒。
// 现值缺失/非法或新值更大时写入；否则保持（乱序旧帧不回退）。
var lastSeenScript = redis.NewScript(`
local newv = tonumber(ARGV[1])
local cur = redis.call('GET', KEYS[1])
local curv = tonumber(cur)
if cur == false or curv == nil or newv > curv then
  redis.call('SET', KEYS[1], ARGV[1])
  return 1
end
return 0
`)

func (c *RedisCache) SetLastSeen(ctx context.Context, deviceID string, ts time.Time) error {
	_, err := lastSeenScript.Run(ctx, c.rdb, []string{KeyLastSeen(deviceID)}, ts.Unix()).Result()
	return err
}

func (c *RedisCache) GetLastSeen(ctx context.Context, deviceID string) (time.Time, bool, error) {
	v, err := c.rdb.Get(ctx, KeyLastSeen(deviceID)).Result()
	if errors.Is(err, redis.Nil) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, false, err
	}
	return time.Unix(sec, 0), true, nil
}

func (c *RedisCache) SetRealtimeFrame(ctx context.Context, deviceID, frameJSON string) error {
	return c.rdb.Set(ctx, KeyRealtimeFrame(deviceID), frameJSON, realtimeFrameTTL).Err()
}

func (c *RedisCache) GetRealtimeFrame(ctx context.Context, deviceID string) (string, error) {
	v, err := c.rdb.Get(ctx, KeyRealtimeFrame(deviceID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return v, err
}

func (c *RedisCache) ApplyStatToday(ctx context.Context, patientID string, wearMinutes int, maxPressure float64, maxPoint string, abnormalDelta int, expireAt time.Time) error {
	_, err := statTodayScript.Run(ctx, c.rdb, []string{KeyStatToday(patientID)},
		wearMinutes,
		strconv.FormatFloat(maxPressure, 'f', -1, 64),
		maxPoint,
		abnormalDelta,
		expireAt.UnixMilli(),
	).Result()
	return err
}

func (c *RedisCache) GetStatToday(ctx context.Context, patientID string) (map[string]string, error) {
	return c.rdb.HGetAll(ctx, KeyStatToday(patientID)).Result()
}

func (c *RedisCache) PushAlertPending(ctx context.Context, payloadJSON string) error {
	return c.rdb.LPush(ctx, keyAlertPending, payloadJSON).Err()
}

func (c *RedisCache) EnqueueRollup(ctx context.Context, patientID, date string, payloadJSON string) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, rollupMarkerKey(patientID, date), "1", rollupMarkerTTL).Result()
	if err != nil || !ok {
		return false, err // 标记已存在：重复投递被幂等跳过
	}
	if err := c.rdb.RPush(ctx, keyRollupQueue, payloadJSON).Err(); err != nil {
		return false, err
	}
	return true, nil
}

// DequeueRollup RPOP rollup:recompute（T021：定时任务消费补传重算队列）
func (c *RedisCache) DequeueRollup(ctx context.Context) (string, error) {
	v, err := c.rdb.RPop(ctx, keyRollupQueue).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil // 队列空
	}
	return v, err
}
