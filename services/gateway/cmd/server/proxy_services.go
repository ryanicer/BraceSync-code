// Package main — gateway 全量路由分发（T032，真实 API 联调地基）
//
// 路由总表（代理模式沿用 T028：ReverseProxy + 环境变量 URL + 10s 超时 + 502 兜底）：
//
//	/api/v1/device/records · /records/batch        → data-service   （设备验签组，非 JWT）
//	/api/v1/device/time                            → gateway 本地   （协议 §4.3 校时，验签组）
//	/api/v1/patients/:id/realtime|records|health-reports → data-service（JWT 组，T030 已有）
//	/api/v1/admin/dashboard/*                      → data-service（JWT 组，T033 聚合查询）
//	/api/v1/devices|install-records|baselines/*    → device-service （JWT 组：T030 列表 + T032 注册/绑定/基线）
//	/api/v1/auth/* · /api/v1/admin/* · teams/doctors/technicians/feedbacks/patients 子资源
//	                                               → user-service   （JWT 组，登录白名单）
//	/api/v1/admin/notify-rules|notification-logs   → msg-service    （JWT 组）
//	/api/v1/files/*                                → file-service   （JWT 组，T022 COS 预签名）
//	/api/v1/alerts*                                → alert-service  （JWT 组，T028 保持）
package main

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// deviceManageRoutes device-service 注册/绑定/基线/安装端点（T032 补全；
// T030 的 GET /devices、GET /install-records 列表保持在 deviceServiceRoutes）
var deviceManageRoutes = []proxyRoute{
	{http.MethodPost, "/devices"},                   // 设备注册（幂等）
	{http.MethodGet, "/devices/:deviceId"},          // 设备详情
	{http.MethodGet, "/devices/:deviceId/bindings"}, // 绑定历史
	{http.MethodPost, "/devices/:deviceId/bind"},    // 绑定（互斥）
	{http.MethodPost, "/devices/:deviceId/rebind"},  // 换绑
	{http.MethodPost, "/devices/:deviceId/unbind"},  // 解绑（幂等）
	{http.MethodPost, "/devices/:deviceId/wifi"},    // 配网状态
	{http.MethodPost, "/install-records"},           // 新建安装记录
	{http.MethodPost, "/baselines"},                 // 校准基线落库
}

// deviceReportRoutes 设备域上报路由（data-service；走设备验签组，非 JWT）
var deviceReportRoutes = []proxyRoute{
	{http.MethodPost, "/device/records"},       // 单帧实时上报
	{http.MethodPost, "/device/records/batch"}, // 离线批量补传
}

// registerAPIProxies 注册全部 /api/v1 业务路由（JWT 鉴权组）：
// alerts（T028）+ user/device/data/msg 四服务（T030 列表 + T032 补全）。
func registerAPIProxies(r *gin.Engine, agt *gatewayAuth) {
	api := r.Group("/api/v1")
	api.Use(jwtAuth(agt)) // T032：统一 JWT 鉴权（白名单见 middleware.go）
	api.Use(scopeAuthz()) // T085：scope 授权（bind 仅放行 bind-phone；full 禁 bind-phone）
	api.Use(roleAuthz())  // T039-H2：端点级 RBAC（admin 专属端点矩阵，见 rbac.go）

	registerAlertsProxyOn(api, envOrURL("ALERT_SERVICE_URL", defaultAlertServiceURL)) // T028 保持
	registerServiceRoutes(api, envOrURL("USER_SERVICE_URL", defaultUserServiceURL), "user-service", userServiceRoutes)
	registerServiceRoutes(api, envOrURL("DEVICE_SERVICE_URL", defaultDeviceServiceURL), "device-service",
		append(append([]proxyRoute{}, deviceServiceRoutes...), deviceManageRoutes...))
	registerServiceRoutes(api, envOrURL("DATA_SERVICE_URL", defaultDataServiceURL), "data-service", dataServiceRoutes)
	registerServiceRoutes(api, envOrURL("MSG_SERVICE_URL", defaultMsgServiceURL), "msg-service", msgServiceRoutes)
	registerServiceRoutes(api, envOrURL("FILE_SERVICE_URL", defaultFileServiceURL), "file-service", fileServiceRoutes)

	// T039-H2：DELETE 技师不在代理契约内，但属 admin 专属操作——补注册使 RBAC 中间件
	// 对低角色返回 403（否则无路由匹配直接 404，绕过授权语义）；ROLE_ADMIN 透传后端 404，行为不变
	api.DELETE("/admin/technicians/:techId", func(c *gin.Context) {
		abortJSON(c, http.StatusNotFound, http.StatusNotFound, "endpoint not available")
	})

	// T091：配网密钥端点从裸组迁入 JWT 组，叠加 tech+admin RBAC 与 per-user 限流。
	// 子组继承 api 的 jwtAuth/scopeAuthz/roleAuthz，再加 provisionRateLimit 中间件。
	prov := api.Group("")
	prov.Use(provisionRateLimit(newUserRateLimiter()))
	registerServiceRoutes(prov, envOrURL("DEVICE_SERVICE_URL", defaultDeviceServiceURL), "device-service",
		[]proxyRoute{{http.MethodPost, "/devices/:deviceId/provision-key"}})
}

// registerDeviceReportRoutes 注册设备域路由（设备验签组，不经 JWT）：
// 单帧上报/批量补传代理 data-service + 校时接口 gateway 本地应答（协议 §4.3）。
func registerDeviceReportRoutes(r *gin.Engine, agt *gatewayAuth) {
	dev := r.Group("/api/v1")
	dev.Use(deviceSigAuth(agt))

	registerServiceRoutes(dev, envOrURL("DATA_SERVICE_URL", defaultDataServiceURL), "data-service", deviceReportRoutes)

	// 协议 §4.3 校时：gateway 本地应答（data-service 无此端点；签名已由中间件校验）
	dev.GET("/device/time", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0, "message": "success",
			"data": gin.H{"server_time": time.Now().Unix()},
		})
	})
	log.Info().Msg("device report routes registered (device-signature auth)")
}

// loadGatewayAuth 组装鉴权依赖：JWT_SECRET + 设备密钥提供器。
// T039-H1：空密钥不再降级关闭鉴权——main 启动即拒绝（log.Fatal），
// 中间件层 fail-closed 401 兜底（middleware.go jwtAuth）。
func loadGatewayAuth() *gatewayAuth {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Warn().Msg("JWT_SECRET empty: gateway will fail-closed (main refuses to start; tests may inject explicitly)")
	}
	secrets := newDeviceServiceSecretProvider(envOrURL("DEVICE_SERVICE_URL", defaultDeviceServiceURL))
	return newGatewayAuth(jwtSecret, secrets)
}
