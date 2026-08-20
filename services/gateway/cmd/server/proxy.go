// Package main — gateway /api/v1/alerts 反向代理（T028，T019B 契约偏差补齐）
//
// 前端契约（docs/
//
//	GET  /api/v1/alerts                     → alert-service（转发查询参数）
//	POST /api/v1/alerts/:alertId/process    → alert-service
//
// 鉴权边界（架构 §3.3）：对外 /api/v1/* 经 gateway 统一鉴权（Phase 1 JWT 中间件
// 落地后挂载于 api 路由组）；/internal/* 为服务间直连，不经网关。
// 后端不可用时返回统一响应体 502（不泄漏内部地址）。
package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// defaultAlertServiceURL alert-service 默认地址（compose 服务名解析，scripts/deploy/docker-compose.yml）
const defaultAlertServiceURL = "http://alert-service:8080"

// proxyTimeout 转发超时上限（查询/处理均为轻量请求）
const proxyTimeout = 10 * time.Second

// newAlertsProxy 构造指向 alert-service 的反向代理：
// 保留原始路径与查询参数（patientId/type/status/page/pageSize 原样透传）
func newAlertsProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{ResponseHeaderTimeout: proxyTimeout}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Warn().Err(err).Msg("proxy to alert-service failed")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"code":502,"message":"alert-service unavailable"}`))
	}
	return proxy
}

// registerAlertsProxy 注册 /api/v1/alerts 代理路由（无鉴权组，T028 测试兼容入口）；
// 生产路由经 setupRouter → registerAPIProxies 挂载于 JWT 鉴权组（T032）。
// 目标地址非法时跳过注册并告警（不阻塞 gateway 其余路由启动）
func registerAlertsProxy(r *gin.Engine, targetURL string) {
	registerAlertsProxyOn(r.Group("/api/v1"), targetURL)
}

// registerAlertsProxyOn 在指定路由组注册 alerts 代理（T032：挂 JWT 鉴权组）
func registerAlertsProxyOn(api *gin.RouterGroup, targetURL string) {
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		log.Error().Str("url", targetURL).Msg("invalid ALERT_SERVICE_URL, alerts proxy disabled")
		return
	}
	proxy := newAlertsProxy(target)
	forward := func(c *gin.Context) { proxy.ServeHTTP(c.Writer, c.Request) }

	api.GET("/alerts", forward)
	api.POST("/alerts/:alertId/process", forward)
	log.Info().Str("target", targetURL).Msg("alerts proxy registered")
}
