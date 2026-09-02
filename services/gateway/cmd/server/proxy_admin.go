// Package main — gateway admin 域反向代理（T030，T020 契约偏差 11 项补齐 #11）
//
// 沿用 T028 proxy.go 模式：httputil.ReverseProxy + 环境变量 URL + 10s 超时 + 502 兜底。
// 代理目标服务（架构 §5 职责矩阵归属，不新建服务）：
//
//	user-service   /api/v1/auth/login · /api/v1/admin/patients · /api/v1/teams ·
//	               /api/v1/doctors · /api/v1/technicians · /api/v1/feedbacks ·
//	               /api/v1/patients/{id}/orthosis-plans · /api/v1/patients/{id}/feeling-logs ·
//	               /api/v1/feeling-logs/{id}/reply · /api/v1/admin/roles · /api/v1/admin/settings
//	device-service /api/v1/devices（列表，T030 #3）· /api/v1/install-records（列表）
//	data-service   /api/v1/patients/{id}/realtime · /api/v1/patients/{id}/records ·
//	               /api/v1/patients/{id}/health-reports · /api/v1/admin/dashboard/*（T033）
//	msg-service    /api/v1/admin/notify-rules · /api/v1/admin/notification-logs
//
// 鉴权边界（架构 §3.3）：对外 /api/v1/* 经 gateway 统一鉴权；Phase 1 JWT 中间件
// 落地后挂载于 api 路由组（TODO 挂载点见 registerAdminProxies）。登录接口本身免 JWT。
package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// compose 服务名默认地址（scripts/deploy/docker-compose.yml）
const (
	defaultUserServiceURL   = "http://user-service:8081"
	defaultDeviceServiceURL = "http://device-service:8082"
	defaultDataServiceURL   = "http://data-service:8083"
	defaultMsgServiceURL    = "http://msg-service:8086"
	defaultFileServiceURL   = "http://file-service:8085"
)

// proxyRoute 单条代理路由（method + gin 路径）
type proxyRoute struct {
	method string
	path   string
}

// newNamedProxy 构造指向指定服务的反向代理（保留原始路径/查询参数/方法）；
// 后端不可用返回统一响应体 502（不泄漏内部地址），超时沿用 proxyTimeout（T028）。
func newNamedProxy(target *url.URL, serviceName string) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{ResponseHeaderTimeout: proxyTimeout}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Warn().Err(err).Str("service", serviceName).Msg("proxy request failed")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"code":502,"message":"` + serviceName + ` unavailable"}`))
	}
	return proxy
}

// registerServiceRoutes 注册单个服务的全部代理路由；目标地址非法时跳过并告警（不阻塞启动）
func registerServiceRoutes(api *gin.RouterGroup, targetURL, serviceName string, routes []proxyRoute) {
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		log.Error().Str("url", targetURL).Str("service", serviceName).Msg("invalid service URL, proxy disabled")
		return
	}
	proxy := newNamedProxy(target, serviceName)
	forward := func(c *gin.Context) { proxy.ServeHTTP(c.Writer, c.Request) }
	for _, rt := range routes {
		api.Handle(rt.method, rt.path, forward)
	}
	log.Info().Str("target", targetURL).Str("service", serviceName).Int("routes", len(routes)).Msg("service proxy registered")
}

// userServiceRoutes T030 admin 域用户侧端点（patients/teams/doctors/technicians/feedbacks/
// orthosis/feeling-logs/roles/settings/login，全部 user-service 归属）
var userServiceRoutes = []proxyRoute{
	{http.MethodPost, "/auth/login"},       // T030 #9 admin 登录（免 JWT 白名单）
	{http.MethodPost, "/tech/login"},       // T037 技师登录（免 JWT 白名单）
	{http.MethodPost, "/patient/login"},    // T037 患者登录（免 JWT 白名单）
	{http.MethodPost, "/patient/wx-login"}, // T069 患者微信小程序登录（免 JWT 白名单）

	{http.MethodGet, "/admin/patients"},                     // T030 #1
	{http.MethodGet, "/admin/patients/:patientId"},          // T030 #2
	{http.MethodPost, "/admin/patients"},                    // T057 创建患者
	{http.MethodPut, "/admin/patients/:patientId/team"},     // T057 分配团队
	{http.MethodPost, "/admin/patients/batch-bind"},         // T057 批量绑定
	{http.MethodGet, "/teams"},                              // T030 #10 概要
	{http.MethodGet, "/teams/:teamId/members"},              // T030 #10 成员明细
	{http.MethodPost, "/teams"},                             // T059 创建团队
	{http.MethodPut, "/teams/:teamId"},                      // T059 编辑团队
	{http.MethodDelete, "/teams/:teamId"},                   // T059 删除团队
	{http.MethodPost, "/teams/:teamId/members"},             // T059 添加成员
	{http.MethodPut, "/teams/:teamId/members/:memberId"},    // T059 编辑成员
	{http.MethodDelete, "/teams/:teamId/members/:memberId"}, // T059 移除成员
	{http.MethodGet, "/doctors"},
	{http.MethodGet, "/technicians"},
	{http.MethodPost, "/admin/technicians"},        // T030 #4 新建
	{http.MethodPut, "/admin/technicians/:techId"}, // T030 #4 编辑
	{http.MethodPost, "/technicians/:techId/toggle"},
	{http.MethodGet, "/feedbacks"},
	{http.MethodPost, "/feedbacks/:feedbackId/process"}, // T030 #5 replyContent
	{http.MethodGet, "/patients/:patientId/orthosis-plans"},
	{http.MethodPost, "/patients/:patientId/orthosis-plans"},
	{http.MethodGet, "/patients/:patientId/feeling-logs"},
	{http.MethodPost, "/feeling-logs/:logId/reply"}, // T030 #6 医生回复
	{http.MethodGet, "/admin/roles"},                // T030 #7
	{http.MethodGet, "/admin/roles/:roleId/permissions"},
	{http.MethodPut, "/admin/roles/:roleId/permissions"},
	{http.MethodGet, "/admin/settings"}, // T030 #8
	{http.MethodPut, "/admin/settings"},
}

// deviceServiceRoutes 设备/安装记录管理端列表（T030 #3 patientName join）
var deviceServiceRoutes = []proxyRoute{
	{http.MethodGet, "/devices"},
	{http.MethodGet, "/install-records"},
}

// dataServiceRoutes 患者数据查询（realtime/records 既有契约 + T030 health-reports +
// T033 admin Dashboard 6 聚合查询端点 + T076 患者日佩戴聚合）
var dataServiceRoutes = []proxyRoute{
	{http.MethodGet, "/patients/:patientId/realtime"},
	{http.MethodGet, "/patients/:patientId/records"},
	{http.MethodGet, "/patients/:patientId/health-reports"},
	{http.MethodGet, "/patients/:patientId/daily-wear"}, // T076：患者日佩戴聚合（患者自查 + admin 任意）

	// T033：admin Dashboard（KPI/趋势/排行/分布，data-service 聚合层）
	{http.MethodGet, "/admin/dashboard/kpi"},
	{http.MethodGet, "/admin/dashboard/wear-trend"},
	{http.MethodGet, "/admin/dashboard/alert-trend"},
	{http.MethodGet, "/admin/dashboard/team-ranking"},
	{http.MethodGet, "/admin/dashboard/doctor-ranking"},
	{http.MethodGet, "/admin/dashboard/wear-distribution"},
}

// msgServiceRoutes 通知规则/发送记录（契约 getNotifyRules/getNotificationLogs，msg-service 已实现）
var msgServiceRoutes = []proxyRoute{
	{http.MethodGet, "/admin/notify-rules"},
	{http.MethodPut, "/admin/notify-rules/:type"},
	{http.MethodGet, "/admin/notification-logs"},
}

// fileServiceRoutes 文件上传预签名/元数据查询（T022，COS 直传凭证）
var fileServiceRoutes = []proxyRoute{
	{http.MethodPost, "/files/presign"},
	{http.MethodPost, "/files/upload-complete"},
	{http.MethodGet, "/files/:fileID"},
	{http.MethodGet, "/files/query"},
}

// envOrURL 环境变量优先，缺省回 compose 服务名地址
func envOrURL(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// registerAdminProxies 注册 T030 全部 admin 域代理路由（无鉴权组，T030 测试兼容入口）。
// 生产路由经 setupRouter → registerAPIProxies 挂载于 JWT 鉴权组（T032）。
func registerAdminProxies(r *gin.Engine) {
	api := r.Group("/api/v1")
	registerServiceRoutes(api, envOrURL("USER_SERVICE_URL", defaultUserServiceURL), "user-service", userServiceRoutes)
	registerServiceRoutes(api, envOrURL("DEVICE_SERVICE_URL", defaultDeviceServiceURL), "device-service", deviceServiceRoutes)
	registerServiceRoutes(api, envOrURL("DATA_SERVICE_URL", defaultDataServiceURL), "data-service", dataServiceRoutes)
	registerServiceRoutes(api, envOrURL("MSG_SERVICE_URL", defaultMsgServiceURL), "msg-service", msgServiceRoutes)
}
