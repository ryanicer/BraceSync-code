package repo

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

func TestKeyBuilders(t *testing.T) {
	assert.Equal(t, "dev:lastseen:DEV1", KeyLastSeen("DEV1"))
	assert.Equal(t, "rt:frame:DEV1", KeyRealtimeFrame("DEV1"))
	assert.Equal(t, "stat:today:P1", KeyStatToday("P1"))
	assert.Equal(t, "rollup:queued:P1:2026-08-08", rollupMarkerKey("P1", "2026-08-08"))
}

func TestFrameArgs(t *testing.T) {
	f := PendingFrame{Ts: time.Now(), Battery: 80, FaultCode: 0}
	args := frameArgs("DEV1", "P1", f)
	require.Len(t, args, 23, "device_id + patient_id + ts + 20 points")
	assert.Equal(t, "DEV1", args[0])
	assert.Equal(t, "P1", args[1])
}

func TestMapPGError(t *testing.T) {
	// 分区缺失 → 哨兵错误
	pgErr := errors.New(`ERROR: no partition of relation "pressure_records" found for row`)
	mapped := mapPGError(pgErr)
	assert.True(t, IsNoPartitionError(mapped))

	// 其他错误原样返回
	other := errors.New("connection refused")
	assert.Equal(t, other, mapPGError(other))
	assert.False(t, IsNoPartitionError(other))

	// errors.Is 链路
	wrapped := fmt.Errorf("outer: %w", mapped)
	assert.True(t, IsNoPartitionError(wrapped))
}

func TestRecordRepoConstructors(t *testing.T) {
	assert.NotNil(t, NewRecordRepo(nil))
	assert.NotNil(t, NewDeviceRepo(nil))
	assert.NotNil(t, NewRedisCache(nil))
	assert.NotNil(t, NewConfigRepo(nil))
}

func TestStatTodayScriptDefined(t *testing.T) {
	require.NotNil(t, statTodayScript, "stat:today Lua 脚本须注册（EVALSHA 缓存）")
	assert.True(t, model.PointCount == 20)
}
