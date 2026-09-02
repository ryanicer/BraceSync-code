package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/data-service/internal/handler"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
	"github.com/bracesync/bracesync/services/data-service/internal/service"
)

// 环境变量（对齐 scripts/deploy/docker-compose.yml）：
//   PORT              监听端口，默认 8083
//   DB_URL            PostgreSQL DSN
//   REDIS_URL         Redis 连接串（redis://...）
//   ALERT_SERVICE_URL alert-service 基地址（空则全量走 alert:pending 降级队列）
//   ALERT_TIMEOUT_MS  内联评估熔断超时，默认 100（架构 §3.4）
//   GIN_MODE          release 关闭调试日志
//   ROLLUP_CRON       daily rollup cron，默认 "10 0 * * *"（每日 00:10 CST）
//   WEEKLY_CRON       周报 cron，默认 "30 0 * * 1"（周一 00:30 CST）
//   MONTHLY_CRON      月报 cron，默认 "30 0 1 * *"（每月 1 日 00:30 CST）
//   PARTITION_CRON    分区预建 cron，默认 "0 0 25 * *"（每月 25 日）
//   ARCHIVE_CRON      冷归档 cron，默认 "0 2 1 * *"（每月 1 日 02:00 CST）
//   ARCHIVE_DIR       冷归档导出目录，默认 /tmp/brace-archive

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Info().Str("service", "data-service").Msg("starting BraceSync Data Service")

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

	// Redis
	rdb, err := redis.ParseURL(envOr("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal().Err(err).Msg("parse REDIS_URL failed")
	}
	rdbClient := redis.NewClient(rdb)
	defer func() { _ = rdbClient.Close() }()
	if err := rdbClient.Ping(ctx).Err(); err != nil {
		log.Fatal().Err(err).Msg("redis ping failed")
	}

	// alert-service 内联评估客户端（未配置地址 → Noop 全量降级，由消费者补偿）
	var evaluator service.AlertEvaluator = service.NoopAlertClient{}
	if alertURL := os.Getenv("ALERT_SERVICE_URL"); alertURL != "" {
		timeout := service.DefaultAlertTimeout
		if ms := os.Getenv("ALERT_TIMEOUT_MS"); ms != "" {
			if n, convErr := time.ParseDuration(ms + "ms"); convErr == nil && n > 0 {
				timeout = n
			}
		}
		evaluator = service.NewHTTPAlertClient(alertURL, timeout)
		log.Info().Str("alert_service_url", alertURL).Msg("inline alert evaluation enabled")
	} else {
		log.Warn().Msg("ALERT_SERVICE_URL not set: all frames degrade to alert:pending")
	}

	// 组装
	svc := service.NewRecordService(
		repo.NewRecordRepo(pool),
		repo.NewDeviceRepo(pool),
		repo.NewConfigRepo(pool),
		repo.NewRedisCache(rdbClient),
		evaluator,
		service.NewDefaultRateLimiter(),
	)
	h := handler.New(svc)

	port := envOr("PORT", "8083")

	// ─────────────────────────────────────────────────────────────
	// T021：定时任务调度（rollup / 周报月报 / 分区预建 / 冷归档）
	// ─────────────────────────────────────────────────────────────
	rollupRepo := repo.NewRollupRepo(pool)
	reportRepo := repo.NewReportRepo(pool)
	partitionRepo := repo.NewPartitionRepo(pool)
	archiveRepo := repo.NewArchiveRepo(pool)
	redisCache := repo.NewRedisCache(rdbClient)
	configRepo := repo.NewConfigRepo(pool)

	// T030：健康报告查询端点数据源注入（契约 getHealthReports）
	h.SetReportLister(reportRepo)

	// T033：admin Dashboard 6 聚合查询端点（daily_wear_stats + kpi:dashboard 缓存）
	dashboardSvc := service.NewDashboardService(repo.NewDashboardRepo(pool), repo.NewDashboardCache(rdbClient))
	h.SetDashboardQuerier(dashboardSvc)

	// T076：患者日佩戴聚合端点（数据源 daily_wear_stats，患者自查 + admin 任意）
	dailyWearSvc := service.NewDailyWearService(rollupRepo)
	h.SetDailyWearQuerier(dailyWearSvc)

	router := h.Router()
	server := &http.Server{Addr: ":" + port, Handler: router}

	rollupSvc := service.NewRollupService(rollupRepo, redisCache, configRepo)
	reportSvc := service.NewReportService(reportRepo, rollupRepo)
	partitionSvc := service.NewPartitionService(partitionRepo)
	archiveSvc := service.NewArchiveService(archiveRepo, partitionRepo, envOr("ARCHIVE_DIR", "/tmp/brace-archive"))

	sched := service.NewCronScheduler()
	cronJobs := []struct {
		spec, name string
		fn         func(context.Context)
	}{
		{envOr("ROLLUP_CRON", "10 0 * * *"), "daily_rollup", func(ctx context.Context) {
			rollupSvc.RunDailyRollup(ctx)
			rollupSvc.ProcessBackfillQueue(ctx)
		}},
		{envOr("WEEKLY_CRON", "30 0 * * 1"), "weekly_report", reportSvc.RunWeeklyReport},
		{envOr("MONTHLY_CRON", "30 0 1 * *"), "monthly_report", reportSvc.RunMonthlyReport},
		{envOr("PARTITION_CRON", "0 0 25 * *"), "partition_precreate", partitionSvc.EnsureFuturePartitions},
		{envOr("ARCHIVE_CRON", "0 2 1 * *"), "cold_archive", archiveSvc.RunColdArchive},
	}
	for _, j := range cronJobs {
		if err := sched.Register(j.spec, j.fn, j.name); err != nil {
			log.Fatal().Err(err).Str("job", j.name).Str("spec", j.spec).Msg("cron register failed")
		}
	}
	sched.Start()

	go func() {
		log.Info().Str("port", port).Msg("data-service listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("data-service listen failed")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down data-service")
	sched.Stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}
}
