// alert-service 入口（T008/T009/T010）：佩戴中断定时扫描器 + 降级队列补偿消费者 + 内联评估接口。
//
// 进程职责：
//  1. 每 5min 扫描 Redis dev:lastseen:* → 超阈值生成 wear_interrupt（去重窗口 1×阈值）
//  2. 设备恢复上报自动 resolve active 中断告警
//  3. 扫描结果联动 devices.status 状态机落库
//  4. 告警阈值统一从 sys_configs 读取（T009）：启动加载 + 每轮扫描前热刷新，
//     加载时执行阈值联动校验（PRD §7D.12：中断阈值 ≥2×采集间隔）
//  5. T010：/internal/evaluate 内联评估接口（data-service 直连）+ alert:pending
//     常驻消费者补偿评估（幂等；>1h 积压帧仅补记录不推送）+ /metrics 观测端点
//
// 环境变量（对齐 scripts/deploy/docker-compose.yml）：
//
//	DB_URL           PostgreSQL DSN
//	REDIS_URL        Redis 连接串（redis://...）
//	SCAN_CRON        扫描 cron 表达式，默认 "*/5 * * * *"（架构 §3.6）
//	PORT             HTTP 监听端口，默认 8080（prometheus.yml 抓取端口）
//	PENDING_POLL_MS  降级队列轮询间隔（毫秒），默认 500
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
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/alert-service/internal/config"
	"github.com/bracesync/bracesync/services/alert-service/internal/consumer"
	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/handler"
	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
	"github.com/bracesync/bracesync/services/alert-service/internal/scheduler"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Info().Str("service", "alert-service").Msg("starting BraceSync Alert Service")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// PostgreSQL（shared database，alerts 表写归属本服务）
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

	// Redis（dev:lastseen:* 只读；alert:pending 降级队列消费，T010）
	redisOpts, err := redis.ParseURL(envOr("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal().Err(err).Msg("parse REDIS_URL failed")
	}
	rdb := redis.NewClient(redisOpts)
	defer func() { _ = rdb.Close() }()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal().Err(err).Msg("redis ping failed")
	}

	// 阈值配置统一入口（T009）：从 sys_configs 读取 + 联动校验（PRD §7D.12）。
	// 加载失败（校验拒绝/DB 不可达）时保持引擎默认口径（45N/30%/60min/2.8N）并记录错误，
	// 后续每轮扫描前重试热刷新，配置修复后无需重启即生效。
	eval := engine.NewDefaultRuleEvaluator()
	cfgMgr := config.NewManager(repo.NewConfigRepo(pool))
	if th, cfgErr := cfgMgr.Refresh(ctx, eval); cfgErr != nil {
		log.Error().Err(cfgErr).Msg("alert threshold config load failed, keep engine defaults; will retry on next scan")
	} else {
		log.Info().
			Float64("pressure_high_n", th.PressureHighN).
			Float64("fluctuation_pct", th.FluctuationPct).
			Int("wear_interrupt_min", th.WearInterruptMinutes).
			Float64("sensor_drift_n", th.SensorDriftN).
			Int("collect_interval_min", th.CollectIntervalMinutes).
			Msg("alert threshold config loaded")
	}

	// 组装扫描器：阈值口径由 cfgMgr 热更新注入（上方 eval）
	alertRepo := repo.NewAlertRepo(pool)
	scan := scanner.New(
		repo.NewDeviceRepo(pool),
		alertRepo,
		repo.NewRedisLastSeen(rdb),
		eval,
	)
	scan.SetLogger(log.Logger)

	// 调度：每 5min（Asia/Shanghai），启动即补跑一轮
	spec := envOr("SCAN_CRON", "*/5 * * * *")
	sched, err := scheduler.New(spec, func(scanCtx context.Context) {
		// 扫描前热刷新阈值（缓存过期自动重读 sys_configs = 热更新生效，引擎读最新值）；
		// 刷新失败（校验拒绝/DB 不可达）保持上一份生效值，不阻塞本轮扫描。
		if _, cfgErr := cfgMgr.Refresh(scanCtx, eval); cfgErr != nil {
			log.Error().Err(cfgErr).Msg("alert threshold config hot refresh failed, keep previous effective values")
		}
		report, scanErr := scan.Scan(scanCtx)
		if scanErr != nil {
			log.Error().Err(scanErr).Msg("wear-interrupt scan failed")
			return
		}
		log.Info().
			Int("scanned", report.Scanned).
			Int("alert_created", report.AlertCreated).
			Int("deduped", report.Deduped).
			Int64("resolved", report.Resolved).
			Int("status_changed", report.StatusChanged).
			Int("missed_lastseen", report.MissedLastSeen).
			Int("redis_errors", report.RedisErrors).
			Msg("wear-interrupt scan done")
	}, log.Logger)
	if err != nil {
		log.Fatal().Err(err).Str("spec", spec).Msg("scheduler create failed")
	}
	sched.Start(ctx)
	log.Info().Str("spec", spec).Msg("wear-interrupt scanner started")

	// T010：alert:pending 常驻消费者（服务可用即排空积压，不依赖重启触发）。
	// T019：Notifier 接入 msg-service HTTP 推送（超时/失败进 Redis 重试队列，不阻塞落库）。
	msgServiceURL := envOr("MSG_SERVICE_URL", "http://msg-service:8081")
	retryQueue := repo.NewRedisNotifyRetryQueue(rdb)
	notifier := consumer.NewHTTPNotifier(consumer.HTTPNotifierConfig{
		MsgServiceURL: msgServiceURL,
		Timeout:       time.Second,
		MaxRetries:    3,
		RetryQueue:    retryQueue,
		Logger:        log.Logger.With().Str("component", "notifier").Logger(),
	})
	cons := consumer.New(
		repo.NewRedisPendingQueue(rdb),
		repo.NewRedisEvalDedup(rdb),
		alertRepo,
		eval,
		notifier,
	)
	cons.SetLogger(log.Logger)
	pollInterval := consumer.DefaultPollInterval
	if ms := os.Getenv("PENDING_POLL_MS"); ms != "" {
		if n, convErr := strconv.Atoi(ms); convErr == nil && n > 0 {
			pollInterval = time.Duration(n) * time.Millisecond
		}
	}
	go cons.Run(ctx, pollInterval)

	// T019：通知重试消费者（排空 alert:notify_pending 队列，2s 轮询）
	go notifier.RunRetry(ctx, 2*time.Second)

	// T010：/internal/evaluate（data-service 内联评估）+ /metrics + /healthz
	// T028：/api/v1/alerts 公开查询 + /api/v1/alerts/{alertId}/process（经 gateway 代理）
	h := handler.New(eval, alertRepo, notifier)
	h.SetPublicStore(alertRepo)
	h.SetLogger(log.Logger)
	port := envOr("PORT", "8080")
	server := &http.Server{Addr: ":" + port, Handler: h.Router()}
	go func() {
		log.Info().Str("port", port).Msg("alert-service HTTP listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("alert-service listen failed")
		}
	}()

	log.Info().Str("spec", spec).Dur("pending_poll", pollInterval).Str("msg_service", msgServiceURL).Msg("alert-service running")

	<-ctx.Done()
	log.Info().Msg("shutting down alert-service")
	sched.Stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}
}
