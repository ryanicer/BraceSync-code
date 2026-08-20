// Package main msg-service 启动入口
//
// 环境变量（对齐 scripts/deploy/docker-compose.yml）：
//
//	PORT                  监听端口，默认 8086
//	DB_URL                PostgreSQL DSN
//	WEAR_TARGET_HOURS     每日佩戴目标小时数，默认 22（sys_configs wear_target_hours 同值）
//	REMINDER_SCAN_INTERVAL 佩戴提醒扫描间隔（Go duration），默认 15m（架构 §7）
//	RETRY_DRAIN_INTERVAL   重试队列排空间隔（Go duration），默认 1m
//	GIN_MODE              release 关闭 Gin 调试日志
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/msg-service/internal/handler"
	"github.com/bracesync/bracesync/services/msg-service/internal/repo"
	"github.com/bracesync/bracesync/services/msg-service/internal/service"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
		log.Warn().Str("key", key).Str("value", v).Msg("invalid duration, using default")
	}
	return def
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Info().Str("service", "msg-service").Msg("starting BraceSync Msg Service")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// PostgreSQL
	dbURL := envOr("DB_URL", "postgres://bracesync:bracesync@localhost:5432/bracesync?sslmode=disable")
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("pgxpool create failed")
	}
	defer pool.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := pool.Ping(pingCtx); err != nil {
		log.Fatal().Err(err).Msg("postgres ping failed")
	}
	cancel()

	// 组装：一期微信/短信用 mock 发送器（真实商配置就绪后替换，接口不变）
	store := repo.NewPGStore(pool)
	wechat := service.NewMockWechatSender(log.With().Str("sender", "wechat").Logger())
	sms := service.NewMockSMSSender(log.With().Str("sender", "sms").Logger())
	svc := service.NewNotifyService(store, wechat, sms, log.Logger)
	if hours, err := strconv.Atoi(envOr("WEAR_TARGET_HOURS", "22")); err == nil {
		svc.SetWearTargetMinutes(hours * 60)
	}

	// 常驻任务：佩戴提醒定时扫描（架构 §7 每 15min）+ 重试队列排空 worker
	go svc.RunReminderScheduler(ctx, envDuration("REMINDER_SCAN_INTERVAL", service.DefaultReminderScanInterval))
	go svc.RunRetryWorker(ctx, envDuration("RETRY_DRAIN_INTERVAL", service.DefaultRetryDrainInterval))

	router := handler.New(svc).Router()
	port := envOr("PORT", "8086")
	server := &http.Server{Addr: ":" + port, Handler: router}

	go func() {
		log.Info().Str("port", port).Msg("msg-service listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("msg-service listen failed")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down msg-service")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}
}
