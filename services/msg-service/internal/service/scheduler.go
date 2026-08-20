// Package service msg-service 业务层
//
// scheduler.go — 常驻循环：佩戴提醒定时扫描（架构 §7 每 15min）+ 重试队列排空 worker。
package service

import (
	"context"
	"time"
)

// 常驻循环默认间隔
const (
	// DefaultReminderScanInterval 佩戴提醒扫描间隔（架构 §7：每 15min）
	DefaultReminderScanInterval = 15 * time.Minute
	// DefaultRetryDrainInterval 重试队列排空间隔
	DefaultRetryDrainInterval = 1 * time.Minute
)

// RunReminderScheduler 佩戴提醒定时扫描循环（ctx 取消即退出）
func (s *NotifyService) RunReminderScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultReminderScanInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.log.Info().Msg("reminder scheduler stopped")
			return
		case <-ticker.C:
			pushed, err := s.ScanReminders(ctx)
			if err != nil {
				s.log.Error().Err(err).Msg("reminder scan failed")
				continue
			}
			if pushed > 0 {
				s.log.Info().Int("pushed", pushed).Msg("reminder scan done")
			}
		}
	}
}

// RunRetryWorker 重试队列排空循环（ctx 取消即退出；对齐 T010 常驻消费者模式）
func (s *NotifyService) RunRetryWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultRetryDrainInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.log.Info().Msg("retry worker stopped")
			return
		case <-ticker.C:
			n, err := s.DrainRetries(ctx)
			if err != nil {
				s.log.Error().Err(err).Msg("retry drain failed")
				continue
			}
			if n > 0 {
				s.log.Info().Int("processed", n).Msg("retry drain done")
			}
		}
	}
}
