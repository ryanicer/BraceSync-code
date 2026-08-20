package main

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// setupRouter 创建并配置 Gin 路由（提取为可测试函数）
//
// T032 路由结构：
//   - /healthz               存活探针（免鉴权）
//   - /api/v1/*（JWT 组）     alerts（T028）+ user/device/data/msg 四服务全量端点
//   - /api/v1/device/*（验签组）单帧上报/批量补传（data-service）+ 校时（本地）
func setupRouter() *gin.Engine {
	r := gin.Default()
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	agt := loadGatewayAuth()           // JWT_SECRET + 设备密钥提供器（环境变量注入，不入库）
	registerAPIProxies(r, agt)         // T032：/api/v1 全量路由 + 统一 JWT 鉴权 + 端点级 RBAC（T039-H2）
	registerDeviceReportRoutes(r, agt) // T032：设备域路由 + HMAC 验签
	// TODO(Phase 2)：限流（令牌桶 IP/用户/设备三维）、审计埋点
	return r
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Info().Str("service", "gateway").Msg("starting BraceSync API Gateway")

	// T039-H1（T023-H1 fail-open 修复）：JWT_SECRET 必填，未配置拒绝启动（fail-closed）；
	// 中间件层另有空密钥 401 兜底（middleware.go），双保险防部署遗漏
	if os.Getenv("JWT_SECRET") == "" {
		log.Fatal().Msg("JWT_SECRET must be configured before starting gateway (T023-H1 fail-closed)")
	}

	r := setupRouter()

	if err := r.Run(":8080"); err != nil {
		log.Fatal().Err(err).Msg("gateway failed to start")
	}
}
