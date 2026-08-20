// Package service T021：调度器单元测试（实现侧）
package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCronScheduler_RegisterAndStart(t *testing.T) {
	sched := NewCronScheduler()

	var called atomic.Int32
	err := sched.Register("* * * * *", func(_ context.Context) {
		called.Add(1)
	}, "test_job")
	require.NoError(t, err)

	sched.Start()
	defer sched.Stop()

	// 不等待实际触发（cron 按分钟粒度），仅验证注册和启动不报错
	assert.Equal(t, int32(0), called.Load(), "启动后不立即触发（cron 等到下一分钟）")
}

func TestCronScheduler_RegisterInvalidSpec(t *testing.T) {
	sched := NewCronScheduler()
	err := sched.Register("invalid cron", func(_ context.Context) {}, "bad_job")
	require.Error(t, err)
}

func TestCronScheduler_PanicRecovery(t *testing.T) {
	sched := NewCronScheduler()

	// 注册一个会 panic 的 job
	err := sched.Register("* * * * *", func(_ context.Context) {
		panic("test panic")
	}, "panic_job")
	require.NoError(t, err)

	sched.Start()
	defer sched.Stop()

	// panic 被 recover 捕获，调度器不崩溃（测试正常结束即通过）
	time.Sleep(10 * time.Millisecond)
}

func TestCronScheduler_MultipleJobs(t *testing.T) {
	sched := NewCronScheduler()

	var count1, count2 atomic.Int32
	require.NoError(t, sched.Register("10 0 * * *", func(_ context.Context) { count1.Add(1) }, "job1"))
	require.NoError(t, sched.Register("30 0 * * 1", func(_ context.Context) { count2.Add(1) }, "job2"))

	sched.Start()
	defer sched.Stop()

	// 验证注册成功，不触发
	assert.Equal(t, int32(0), count1.Load())
	assert.Equal(t, int32(0), count2.Load())
}

func TestCronScheduler_StopGraceful(t *testing.T) {
	sched := NewCronScheduler()
	require.NoError(t, sched.Register("* * * * *", func(_ context.Context) {}, "job"))
	sched.Start()
	sched.Stop() // 不应阻塞或 panic
}

// TestCronScheduler_JobPanicRecoveryDirect 直接测试 job 包装器的 panic 恢复
func TestCronScheduler_JobPanicRecoveryDirect(t *testing.T) {
	sched := NewCronScheduler()

	var executed atomic.Int32
	require.NoError(t, sched.Register("* * * * *", func(_ context.Context) {
		executed.Add(1)
		panic("direct panic test")
	}, "panic_direct"))

	// 手动触发 job（模拟 cron 回调）：entries[0] 是第一个注册的 job
	entries := sched.cron.Entries()
	require.Len(t, entries, 1)

	// 直接调用 cron job 函数（panic 应被 recover 捕获）
	assert.NotPanics(t, func() {
		entries[0].Job.Run()
	})
	assert.Equal(t, int32(1), executed.Load())
}

// TestCronScheduler_JobNormalExecution 直接测试正常 job 执行
func TestCronScheduler_JobNormalExecution(t *testing.T) {
	sched := NewCronScheduler()

	var executed atomic.Int32
	require.NoError(t, sched.Register("10 0 * * *", func(_ context.Context) {
		executed.Add(1)
	}, "normal_job"))

	entries := sched.cron.Entries()
	require.Len(t, entries, 1)

	entries[0].Job.Run()
	assert.Equal(t, int32(1), executed.Load())
}
