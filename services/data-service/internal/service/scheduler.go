// Package service T021：定时任务调度器（robfig/cron 封装）
//
// 对齐：架构 §3.6 定时任务清单 / alert-service scheduler 模式
// 时区统一 Asia/Shanghai，每个 job 内捕获 panic 防止调度器崩溃。
package service

import (
	"context"
	"runtime/debug"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

// CronScheduler data-service 多任务 cron 调度器
type CronScheduler struct {
	cron *cron.Cron
}

// NewCronScheduler 创建调度器（时区固定 Asia/Shanghai）
func NewCronScheduler() *CronScheduler {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return &CronScheduler{
		cron: cron.New(cron.WithLocation(loc)),
	}
}

// Register 注册命名定时任务。job 在独立 goroutine 中执行，panic 被捕获不影响调度器。
func (s *CronScheduler) Register(spec string, job func(ctx context.Context), name string) error {
	_, err := s.cron.AddFunc(spec, func() {
		ctx := context.Background()
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Str("job", name).
					Interface("panic", r).
					Str("stack", string(debug.Stack())).
					Msg("cron job panicked")
			}
		}()
		log.Info().Str("job", name).Msg("cron job started")
		job(ctx)
		log.Info().Str("job", name).Msg("cron job finished")
	})
	if err != nil {
		return err
	}
	log.Info().Str("job", name).Str("spec", spec).Msg("cron job registered")
	return nil
}

// Start 启动调度器
func (s *CronScheduler) Start() {
	s.cron.Start()
	log.Info().Msg("cron scheduler started (Asia/Shanghai)")
}

// Stop 优雅停止：等待正在执行的任务结束
func (s *CronScheduler) Stop() {
	<-s.cron.Stop().Done()
	log.Info().Msg("cron scheduler stopped")
}
