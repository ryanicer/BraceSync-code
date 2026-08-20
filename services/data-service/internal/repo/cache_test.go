package repo

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startMiniRedis 启动内存 Redis（支持 Lua），返回 RedisCache 与控制器
func startMiniRedis(t *testing.T) (*RedisCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewRedisCache(rdb), mr
}

// listOf 读 miniredis List（带错误断言）
func listOf(t *testing.T, mr *miniredis.Miniredis, key string) []string {
	t.Helper()
	list, err := mr.List(key)
	require.NoError(t, err)
	return list
}

func TestRedisCache_LastSeen(t *testing.T) {
	cache, mr := startMiniRedis(t)
	ctx := context.Background()
	ts := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)

	// 无记录
	_, ok, err := cache.GetLastSeen(ctx, "DEV1")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, cache.SetLastSeen(ctx, "DEV1", ts))
	got, ok, err := cache.GetLastSeen(ctx, "DEV1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, got.Equal(ts))
	assert.Equal(t, time.Duration(0), mr.TTL(KeyLastSeen("DEV1")), "lastseen 无 TTL")

	// 单调推进：更新的帧时间覆盖
	require.NoError(t, cache.SetLastSeen(ctx, "DEV1", ts.Add(10*time.Minute)))
	got, _, _ = cache.GetLastSeen(ctx, "DEV1")
	assert.True(t, got.Equal(ts.Add(10*time.Minute)))

	// 乱序旧帧不得回退 lastseen（设备后补旧帧场景）
	require.NoError(t, cache.SetLastSeen(ctx, "DEV1", ts.Add(-5*time.Minute)))
	got, _, _ = cache.GetLastSeen(ctx, "DEV1")
	assert.True(t, got.Equal(ts.Add(10*time.Minute)), "旧帧时间不得回退 lastseen")

	// 非法值容错
	_ = mr.Set(KeyLastSeen("DEV2"), "not-a-number")
	_, _, err = cache.GetLastSeen(ctx, "DEV2")
	assert.Error(t, err)
}

func TestRedisCache_RealtimeFrame(t *testing.T) {
	cache, mr := startMiniRedis(t)
	ctx := context.Background()

	v, err := cache.GetRealtimeFrame(ctx, "DEV1")
	require.NoError(t, err)
	assert.Empty(t, v)

	require.NoError(t, cache.SetRealtimeFrame(ctx, "DEV1", `{"deviceId":"DEV1"}`))
	v, err = cache.GetRealtimeFrame(ctx, "DEV1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"deviceId":"DEV1"}`, v)

	ttl := mr.TTL(KeyRealtimeFrame("DEV1"))
	assert.Equal(t, 2*time.Hour, ttl, "rt:frame TTL 2h（架构 §4.7）")
}

func TestRedisCache_StatTodayLua(t *testing.T) {
	cache, mr := startMiniRedis(t)
	ctx := context.Background()
	expireAt := time.Now().Add(time.Hour)

	// 佩戴帧：wear+30，max 刷新
	require.NoError(t, cache.ApplyStatToday(ctx, "P1", 30, 25.5, "P03", 0, expireAt))
	stat, err := cache.GetStatToday(ctx, "P1")
	require.NoError(t, err)
	assert.Equal(t, "30", stat["wear_minutes"])
	assert.Equal(t, "25.5", stat["max_pressure"])
	assert.Equal(t, "P03", stat["max_point"])

	// 非佩戴帧：wear 不加；更小的 max 不覆盖
	require.NoError(t, cache.ApplyStatToday(ctx, "P1", 0, 10.0, "P01", 0, expireAt))
	stat, _ = cache.GetStatToday(ctx, "P1")
	assert.Equal(t, "30", stat["wear_minutes"])
	assert.Equal(t, "25.5", stat["max_pressure"])
	assert.Equal(t, "P03", stat["max_point"], "较小压力不得覆盖最大点位")

	// 更大 max 覆盖 + 异常数增量
	require.NoError(t, cache.ApplyStatToday(ctx, "P1", 30, 40.2, "P07", 1, expireAt))
	stat, _ = cache.GetStatToday(ctx, "P1")
	assert.Equal(t, "60", stat["wear_minutes"])
	assert.Equal(t, "40.2", stat["max_pressure"])
	assert.Equal(t, "P07", stat["max_point"])
	assert.Equal(t, "1", stat["abnormal_count"])

	// 过期时间到达后 key 失效（生产为今日 24:00 Asia/Shanghai）
	mr.FastForward(2 * time.Hour)
	stat, _ = cache.GetStatToday(ctx, "P1")
	assert.Empty(t, stat, "stat:today 过当日 24:00 后过期")
}

func TestRedisCache_AlertPending(t *testing.T) {
	cache, mr := startMiniRedis(t)
	ctx := context.Background()

	require.NoError(t, cache.PushAlertPending(ctx, `{"frame":"f1"}`))
	require.NoError(t, cache.PushAlertPending(ctx, `{"frame":"f2"}`))
	assert.Len(t, listOf(t, mr, "alert:pending"), 2)
	// LPUSH：最新帧在队首（消费者 RPOP/LPOP 均可按序处理）
	assert.Equal(t, []string{`{"frame":"f2"}`, `{"frame":"f1"}`}, listOf(t, mr, "alert:pending"))
}

func TestRedisCache_RollupEnqueueIdempotent(t *testing.T) {
	cache, mr := startMiniRedis(t)
	ctx := context.Background()

	queued, err := cache.EnqueueRollup(ctx, "P1", "2026-08-07", `{"patient_id":"P1","date":"2026-08-07"}`)
	require.NoError(t, err)
	assert.True(t, queued)
	assert.Len(t, listOf(t, mr, "rollup:recompute"), 1)

	// 同患者同日期重复投递 → 幂等跳过
	queued, err = cache.EnqueueRollup(ctx, "P1", "2026-08-07", `{}`)
	require.NoError(t, err)
	assert.False(t, queued)
	assert.Len(t, listOf(t, mr, "rollup:recompute"), 1)

	// 不同日期正常入队
	queued, err = cache.EnqueueRollup(ctx, "P1", "2026-08-08", `{}`)
	require.NoError(t, err)
	assert.True(t, queued)
	assert.Len(t, listOf(t, mr, "rollup:recompute"), 2)

	// 标记带 48h TTL
	ttl := mr.TTL("rollup:queued:P1:2026-08-07")
	assert.InDelta(t, 48*time.Hour, ttl, float64(time.Minute))
}

func TestBuildBatchInsertSQL(t *testing.T) {
	sql := buildBatchInsertSQL(2)
	assert.Contains(t, sql, "ON CONFLICT (device_id, ts) DO NOTHING RETURNING ts")
	assert.Contains(t, sql, "($1,$2,$3")
	assert.Contains(t, sql, "$46)") // 2 帧 × 23 列
	assert.NotContains(t, buildBatchInsertSQL(1), "$24")
}

func TestApplyConfigValue(t *testing.T) {
	interval, version := 30, 1
	applyConfigValue("collect_interval_minutes", "45", &interval, &version)
	assert.Equal(t, 45, interval)
	applyConfigValue("device_config_version", "3", &interval, &version)
	assert.Equal(t, 3, version)

	// 非法/非正值/未知 key 不覆盖
	applyConfigValue("collect_interval_minutes", "abc", &interval, &version)
	applyConfigValue("collect_interval_minutes", "0", &interval, &version)
	applyConfigValue("unknown_key", "99", &interval, &version)
	assert.Equal(t, 45, interval)
	assert.Equal(t, 3, version)
}
