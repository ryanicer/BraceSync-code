// file-service 入口（T022：COS 预签名 URL 直传凭证 + 文件元数据）
//
// 环境变量（对齐 scripts/deploy/docker-compose.yml 与 runbook §7 COS 凭据）：
//
//	PORT            监听端口，默认 8085
//	DB_URL          PostgreSQL DSN
//	COS_SECRET_ID   腾讯云 SecretId（.env 注入，不入库）
//	COS_SECRET_KEY  腾讯云 SecretKey（.env 注入，不入库）
//	COS_BUCKET      桶访问域名（<bucket>.cos.<region>.myqcloud.com）
//	COS_REGION      区域（如 ap-guangzhou，仅日志展示用）
//	FILE_MOCK_COS   非空时启用打桩 COS 客户端（CI/离线测试专用，生产禁设）
//	GIN_MODE        release 关闭调试日志
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/file-service/internal/handler"
	"github.com/bracesync/bracesync/services/file-service/internal/repo"
	"github.com/bracesync/bracesync/services/file-service/internal/service"
	"github.com/bracesync/bracesync/services/file-service/internal/storage"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Info().Str("service", "file-service").Msg("starting BraceSync File Service")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// PostgreSQL（表 owner：file-service，migration 000006_file_service）
	dbURL := envOr("DB_URL", "postgres://bracesync:bracesync@localhost:5432/bracesync?sslmode=disable")
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("pgxpool create failed")
	}
	defer pool.Close()
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := pool.Ping(pingCtx); err != nil {
		log.Fatal().Err(err).Msg("postgres ping failed")
	}
	pingCancel()

	// COS 客户端：默认真实 SDK（cos-go-sdk-v5 预签名为本地计算，启动不依赖网络）；
	// FILE_MOCK_COS 非空时打桩（仅 CI/离线测试，生产禁设——防止签发假 URL）
	cosBucket := os.Getenv("COS_BUCKET")
	cosRegion := envOr("COS_REGION", "ap-guangzhou")
	var cosClient storage.StorageClient
	if os.Getenv("FILE_MOCK_COS") != "" {
		cosClient = storage.NewMockCOSClient()
		log.Warn().Msg("FILE_MOCK_COS set: using mock COS client (test only)")
	} else {
		realClient, err := storage.NewRealCOSClient(cosBucket, os.Getenv("COS_SECRET_ID"), os.Getenv("COS_SECRET_KEY"))
		if err != nil {
			log.Fatal().Err(err).Msg("init COS client failed: check COS_BUCKET")
		}
		cosClient = realClient
		log.Info().Str("cos_region", cosRegion).Msg("real COS client ready (presign via cos-go-sdk-v5)")
	}

	// 组装：repo → service → handler
	store := repo.NewPGStore(pool)
	svc := service.NewPresigner(cosClient, store, cosBucket, cosRegion)
	h := handler.NewFileHandler(svc, store)
	router := h.Router()

	port := envOr("PORT", "8085")
	server := &http.Server{Addr: ":" + port, Handler: router}

	go func() {
		log.Info().Str("port", port).Msg("file-service listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("file-service listen failed")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down file-service")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}
}
