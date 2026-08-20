// Package scheduler — 调度器单测（T008）
package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 启动即补跑一轮（架构 §6.3：停摆期间漏扫由首扫补发）
func TestStartRunsImmediateScan(t *testing.T) {
	var runs atomic.Int32
	s, err := New("*/5 * * * *", func(context.Context) { runs.Add(1) }, zerolog.Nop())
	require.NoError(t, err)

	s.Start(context.Background())
	defer s.Stop()

	assert.EqualValues(t, 1, runs.Load(), "Start 应立即补跑一轮")
}

// cron 周期触发（@every 语法验证调度循环生效）
func TestCronPeriodicTrigger(t *testing.T) {
	var runs atomic.Int32
	s, err := New("@every 50ms", func(context.Context) { runs.Add(1) }, zerolog.Nop())
	require.NoError(t, err)

	s.Start(context.Background())
	// 轮询等待第二次触发（启动补跑 1 次 + 周期 ≥1 次），上限 2s 防挂死
	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	s.Stop()

	assert.GreaterOrEqual(t, runs.Load(), int32(2), "周期触发 + 启动补跑至少 2 次")
}

// 非法 cron 表达式 → 构造失败
func TestNewInvalidSpec(t *testing.T) {
	_, err := New("not a cron", func(context.Context) {}, zerolog.Nop())
	require.Error(t, err)
}

// 调度时区为 Asia/Shanghai（+08:00，无夏令时）
func TestCSTZone(t *testing.T) {
	_, offset := time.Now().In(CSTZone()).Zone()
	assert.Equal(t, 8*3600, offset)
}
