package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/data-service/internal/calibration"
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
//   DEVICE_SERVICE_URL device-service 基地址（空则跳过 devices.last_report_at 回写）
//   DEVICE_REPORT_TIMEOUT_MS 上报状态回写熔断超时，默认 300
//   GIN_MODE          release 关闭调试日志
//   ROLLUP_CRON       daily rollup cron，默认 "10 0 * * *"（每日 00:10 CST）
//   WEEKLY_CRON       周报 cron，默认 "30 0 * * 1"（周一 00:30 CST）
//   MONTHLY_CRON      月报 cron，默认 "30 0 1 * *"（每月 1 日 00:30 CST）
//   PARTITION_CRON    分区预建 cron，默认 "0 0 25 * *"（每月 25 日）
//   ARCHIVE_CRON      冷归档 cron，默认 "0 2 1 * *"（每月 1 日 02:00 CST）
//   ARCHIVE_DIR       冷归档导出目录，默认 /tmp/brace-archive
//   APP_ENV           部署环境标记，默认 production（T498：未识别值一律按生产对待）
//   ALLOW_MOCK_INGEST T498 受控 mock 帧注入开关，默认关；取值 true/1 才算开。
//                     🔴 生产禁设：APP_ENV=production 时设了直接 log.Fatal，不给「起来忘了关」留窗口

// nonProdEnvs 允许开 mock 注入的环境白名单。白名单而不是「非 production 即可」：
// APP_ENV 拼错（prodction / prod）时按生产对待才是 fail-closed 的那一侧。
var nonProdEnvs = map[string]bool{"dev": true, "test": true, "staging": true}

// mockIngestGate T498 开关判定，抽成纯函数供单测覆盖真值表（main 里的 log.Fatal 不可测）。
// 返回 (是否启用, 拒绝原因)；原因非空 = 配置本身矛盾，调用方必须 Fatal。
func mockIngestGate(appEnv, allow string) (bool, string) {
	on := allow == "true" || allow == "1"
	if !on {
		return false, ""
	}
	if !nonProdEnvs[strings.ToLower(strings.TrimSpace(appEnv))] {
		return false, "ALLOW_MOCK_INGEST is set but APP_ENV is not a known non-production env"
	}
	return true, ""
}

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
	recordRepo := repo.NewRecordRepo(pool)
	deviceRepo := repo.NewDeviceRepo(pool) // T643 A 路：realtime / 历史 / 日佩戴三读路共用同一枚面积只读源
	svc := service.NewRecordService(
		recordRepo,
		deviceRepo,
		repo.NewConfigRepo(pool),
		repo.NewRedisCache(rdbClient),
		evaluator,
		service.NewDefaultRateLimiter(),
	)
	// T173 基线校准读侧装配：baselines 表只读（写归 device-service，D3）
	svc.SetCalibrator(calibration.NewCalibrator(repo.NewBaselineRepo(pool)))
	h := handler.New(svc)

	// T498 受控 mock 帧注入开关：默认关；生产环境设了开关直接启动失败（验收 1）。
	// 关掉时端点仍在路由表内，但每个请求在方法体最前判 403（验收 2），
	// 所以「开关」这件事只需要一次重启语义之外的保证：判定与请求同批。
	mockOn, reject := mockIngestGate(envOr("APP_ENV", "production"), os.Getenv("ALLOW_MOCK_INGEST"))
	if reject != "" {
		log.Fatal().Str("app_env", envOr("APP_ENV", "production")).Msg(reject)
	}
	svc.SetMockIngest(mockOn, recordRepo)

	// device-service 上报状态回写（devices.last_report_at 单调推进；devices 表写归 device-service）
	if deviceURL := os.Getenv("DEVICE_SERVICE_URL"); deviceURL != "" {
		timeout := service.DefaultDeviceReportTimeout
		if ms := os.Getenv("DEVICE_REPORT_TIMEOUT_MS"); ms != "" {
			if n, convErr := time.ParseDuration(ms + "ms"); convErr == nil && n > 0 {
				timeout = n
			}
		}
		svc.SetDeviceReporter(service.NewHTTPDeviceClient(deviceURL, timeout), timeout)
		log.Info().Str("device_service_url", deviceURL).Msg("device last_report_at writeback enabled")
	} else {
		log.Warn().Msg("DEVICE_SERVICE_URL not set: devices.last_report_at will not be backfilled")
	}

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

	// T340：患者档案存在性数据源（patients 表只读）——四个 patientId 查询端点据此区分
	// 「查无此人 404」与「有此人但暂无数据 200」
	h.SetPatientLookup(repo.NewPatientRepo(pool))

	// T033：admin Dashboard 6 聚合查询端点（daily_wear_stats + kpi:dashboard 缓存）
	dashboardSvc := service.NewDashboardService(repo.NewDashboardRepo(pool), repo.NewDashboardCache(rdbClient))
	h.SetDashboardQuerier(dashboardSvc)

	// T076：患者日佩戴聚合端点（数据源 daily_wear_stats，患者自查 + admin 任意）
	// T366：第二参数注入 pressure_records 明细佐证源，用于给无聚合印章的行判 corroborated / unsupported
	// T411：第三参数注入 sys_configs 假设值（佩戴阈值 + 采集间隔），供口径代次复算使用
	dailyWearSvc := service.NewDailyWearService(rollupRepo, repo.NewRecordRepo(pool), configRepo)
	// T643 A 路：逐行 avg/max 的 kPa 展示档同源派生（分母 devices.contact_area_cm2，只读、不落库）
	dailyWearSvc.SetDeviceStore(deviceRepo)
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
