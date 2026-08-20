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

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/handler"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
	"github.com/bracesync/bracesync/services/device-service/internal/service"
)

// 环境变量（对齐 scripts/deploy/docker-compose.yml）：
//
//	PORT                  监听端口，默认 8082
//	DB_URL                PostgreSQL DSN
//	DEVICE_SECRET_ENC_KEY device_secret 列加密密钥（架构 §5.2 AES-GCM），64 位 hex（32 字节），必填
//	GIN_MODE              release 关闭调试日志

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Info().Str("service", "device-service").Msg("starting BraceSync Device Service")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// device_secret 加密密钥（缺失即拒绝启动：密钥材料不得明文落库）
	enc, err := crypto.NewEncryptor(os.Getenv("DEVICE_SECRET_ENC_KEY"))
	if err != nil {
		log.Fatal().Err(err).Msg("DEVICE_SECRET_ENC_KEY invalid: require 64 hex chars (32 bytes)")
	}

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

	// 组装（T030：ListStore 注入管理端列表查询，PGStore 同时实现 Store 与 ListStore）
	store := repo.NewPGStore(pool)
	svc := service.NewDeviceService(store, enc)
	h := handler.New(svc)
	h.SetListStore(store)
	router := h.Router()

	port := envOr("PORT", "8082")
	server := &http.Server{Addr: ":" + port, Handler: router}

	go func() {
		log.Info().Str("port", port).Msg("device-service listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("device-service listen failed")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down device-service")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}
}
