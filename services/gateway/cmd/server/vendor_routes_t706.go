// T706 厂商 key 面的路由注册。
//
// P-2（设计稿 §4 末条）：vendor 面自成一个 r.Group("/api/v1") + 自己的 vendor-key 中间件，
// 不复用 JWT 组（否则会继承 jwtAuth/scopeAuthz/roleAuthz，合法厂商 key 直接吃 401/403）。
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// vendorRoutes 厂商面允许的 4 条（设计稿 §4 前四行）
var vendorRoutes = []proxyRoute{
	{http.MethodPost, "/vendor/devices"},                 // 批量注册（≤200，created/existed/failed）
	{http.MethodGet, "/vendor/devices"},                  // 本厂商设备列表（分页）
	{http.MethodGet, "/vendor/devices/:deviceId"},        // 本厂商设备元数据
	{http.MethodGet, "/vendor/devices/:deviceId/secret"}, // 逐台补取明文（reason 必填）
}

func registerVendorProxies(r *gin.Engine, agt *gatewayAuth) {
	v := r.Group("/api/v1")
	v.Use(vendorKeyAuth(agt.vendor))
	registerServiceRoutes(v, envOrURL("DEVICE_SERVICE_URL", defaultDeviceServiceURL), "device-service", vendorRoutes)
}
