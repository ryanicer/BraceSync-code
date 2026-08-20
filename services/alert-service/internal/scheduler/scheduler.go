// Package scheduler — 定时任务调度（T008）
//
// 对齐：架构 §3.1 cron 选型（robfig/cron）/ §3.5 调度时区 Asia/Shanghai /
// §3.6 设备状态扫描每 5min / §6.3 启动后首轮补扫（停摆期间中断由首扫补发）。
//
// 一期单副本进程内执行即安全；扩副本前须加 Redis 分布式锁（架构 §3.6 演进前置）。
package scheduler

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

// CSTZone Asia/Shanghai（架构 §3.5：定时任务统一北京时间）
func CSTZone() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		// 容器无 tzdata 时兜底固定 +08:00（中国无夏令时）
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

// Scheduler 佩戴中断扫描调度器
type Scheduler struct {
	cron *cron.Cron
	run  func(ctx context.Context)
	log  zerolog.Logger
}

// New 创建调度器：spec 为标准 5 段 cron 表达式（如 "*/5 * * * *"），时区固定 Asia/Shanghai。
// run 为单轮扫描闭包（由 main 注入 scanner.Scan + 日志/指标包装）。
func New(spec string, run func(ctx context.Context), logger zerolog.Logger) (*Scheduler, error) {
	c := cron.New(cron.WithLocation(CSTZone()))
	if _, err := c.AddFunc(spec, func() {
		run(context.Background())
	}); err != nil {
		return nil, err
	}
	return &Scheduler{cron: c, run: run, log: logger}, nil
}

// Start 启动调度，并立即补跑一轮（覆盖进程停摆期间漏扫的中断，架构 §6.3；
// 去重窗口保证补扫不产生重复告警）。补跑同步执行，失败仅记录不阻塞调度启动。
func (s *Scheduler) Start(ctx context.Context) {
	s.run(ctx)
	s.cron.Start()
	s.log.Info().Msg("scheduler started (wear-interrupt scan, Asia/Shanghai)")
}

// Stop 优雅停止：等待正在执行的扫描结束（cron.Stop 返回停止时的 ctx）
func (s *Scheduler) Stop() {
	<-s.cron.Stop().Done()
	s.log.Info().Msg("scheduler stopped")
}
