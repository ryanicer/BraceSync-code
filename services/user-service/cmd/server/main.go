// user-service 启动入口（T030：admin 域端点落地）
//
// 环境变量（对齐 scripts/deploy/docker-compose.yml）：
//
//	PORT          监听端口，默认 8081
//	DB_URL        PostgreSQL DSN
//	JWT_SECRET    登录 token 签发密钥（与 gateway 共享，Phase 1 JWT 校验复用）
//	PHONE_ENC_KEY 手机号 AES-GCM 加密密钥（64 位 hex）；未配置时技师新建/编辑返回 500
//	WX_APPID      患者端小程序 AppID；缺失则 /patient/wx-login 返回 500（不影响其他登录端点）
//	WX_APP_SECRET 患者端小程序 AppSecret；缺失同上（与 WX_APPID 配对）
//	GIN_MODE      release 关闭调试日志
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

	"github.com/bracesync/bracesync/services/user-service/internal/handler"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
	"github.com/bracesync/bracesync/services/user-service/internal/wechat"
)

// tokenTTL 登录 token 有效期（运营后台一个工作日口径）
const tokenTTL = 8 * time.Hour

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Info().Str("service", "user-service").Msg("starting BraceSync User Service")

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

	// JWT 签发器（未配置 JWT_SECRET 时登录端点返回 500，其余端点不受影响）
	var signer *token.Signer
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		signer, err = token.NewSigner(secret, tokenTTL)
		if err != nil {
			log.Fatal().Err(err).Msg("token signer init failed")
		}
	} else {
		log.Warn().Msg("JWT_SECRET not set: login endpoint disabled")
	}

	// 手机号加密器（未配置 PHONE_ENC_KEY 时技师写入端点返回 500）
	var phoneCipher *phone.Cipher
	if key := os.Getenv("PHONE_ENC_KEY"); key != "" {
		phoneCipher, err = phone.NewCipher(key)
		if err != nil {
			log.Fatal().Err(err).Msg("phone cipher init failed")
		}
	} else {
		log.Warn().Msg("PHONE_ENC_KEY not set: technician create/update disabled")
	}

	// 微信登录客户端（WX_APPID + WX_APP_SECRET 均存在才启用；缺失仅 /patient/wx-login 降级 500）
	var wxClient *wechat.Client
	if appID := os.Getenv("WX_APPID"); appID != "" {
		if secret := os.Getenv("WX_APP_SECRET"); secret != "" {
			wxClient = wechat.NewClient(appID, secret)
			log.Info().Msg("wechat login client initialized")
		} else {
			log.Warn().Msg("WX_APPID set but WX_APP_SECRET missing: wechat login disabled")
		}
	} else {
		log.Warn().Msg("WX_APPID not set: wechat login disabled")
	}

	h := handler.New(repo.NewPGStore(pool), signer, phoneCipher)
	h.SetWXClient(wxClient) // 可选注入：nil 视为未配置，/patient/wx-login 降级 500

	// T085：phoneToken 签发/校验密钥（bind-phone 失败重试免二次微信调用）
	if pts := os.Getenv("PHONE_TOKEN_SECRET"); pts != "" {
		h.SetPhoneTokenSecret(pts)
	} else {
		log.Warn().Msg("PHONE_TOKEN_SECRET not set: bind-phone phoneToken retry disabled")
	}

	port := envOr("PORT", "8081")
	server := &http.Server{Addr: ":" + port, Handler: h.Router()}

	go func() {
		log.Info().Str("port", port).Msg("user-service listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("user-service listen failed")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down user-service")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}
}
