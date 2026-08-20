// Package main — gateway 端点级 RBAC 授权中间件（T039-H2，T023-H2 垂直越权修复）
//
// 背景（T023-安全审计报告.md §3）：此前全链路无端点级角色强制，ROLE_CS/ROLE_DOCTOR
// 合法 token 可调用任意 admin 端点（改配置/权限矩阵/技师管理）。本中间件在 jwtAuth
// 注入 X-Role 之后、反代转发之前做端点级授权：无权限 → 403，请求不得触达后端。
//
// 同源约定：admin 专属端点矩阵倒推自前端 ROLE_PAGE_MATRIX 中 admin 专属页面
// （apps/admin-web/src/router/permissions.ts，PRD §7D.11）：
//
//	/settings          → /api/v1/admin/settings · /admin/roles* · /admin/notify-rules* · /admin/notification-logs
//	/roles             → /api/v1/admin/roles · /admin/roles/:roleId/permissions
//	/technicians       → /api/v1/technicians · /admin/technicians* · /technicians/:techId/toggle
//	/teams             → /api/v1/teams · /teams/:teamId/members · /doctors
//
// 非 admin 专属页面（dashboard/monitor/patients/alerts/communication/orthosis-log 等）
// 的端点保持全角色可用：数据范围收敛（医生仅本团队 / 客服仅沟通域）为后端 RBAC 细化
// 职责（Phase 2，user-service/internal/rbac），本期仅关闭垂直越权面。
// 前端 ROLE_PAGE_MATRIX 为 UX 层守卫，本网关矩阵是安全控制，两者须同步变更。
package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// 预置角色常量（对齐 user-service/internal/rbac.Role*，跨模块不直接依赖）
const (
	roleAdmin = "ROLE_ADMIN"
)

// rbacPattern admin 专属端点（method + gin 风格路径模板，":param" 段匹配任意值）
type rbacPattern struct {
	method   string
	segments []string
}

// adminOnlyPatterns admin 专属端点矩阵（非 ROLE_ADMIN 命中即 403）
var adminOnlyPatterns = []rbacPattern{
	// /settings 页（含通知规则/发送记录 tab）+ /roles 页
	rbacOf(http.MethodGet, "/api/v1/admin/settings"),
	rbacOf(http.MethodPut, "/api/v1/admin/settings"),
	rbacOf(http.MethodGet, "/api/v1/admin/roles"),
	rbacOf(http.MethodGet, "/api/v1/admin/roles/:roleId/permissions"),
	rbacOf(http.MethodPut, "/api/v1/admin/roles/:roleId/permissions"),
	rbacOf(http.MethodGet, "/api/v1/admin/notify-rules"),
	rbacOf(http.MethodPut, "/api/v1/admin/notify-rules/:type"),
	rbacOf(http.MethodGet, "/api/v1/admin/notification-logs"),

	// /technicians 页（列表/新建/编辑/启停）
	rbacOf(http.MethodGet, "/api/v1/technicians"),
	rbacOf(http.MethodPost, "/api/v1/admin/technicians"),
	rbacOf(http.MethodPut, "/api/v1/admin/technicians/:techId"),
	rbacOf(http.MethodDelete, "/api/v1/admin/technicians/:techId"), // 未注册代理方法，补注册防绕过（见 registerAPIProxies）
	rbacOf(http.MethodPost, "/api/v1/technicians/:techId/toggle"),

	// /teams 页（团队/成员/医生）
	rbacOf(http.MethodGet, "/api/v1/teams"),
	rbacOf(http.MethodGet, "/api/v1/teams/:teamId/members"),
	rbacOf(http.MethodGet, "/api/v1/doctors"),
}

// rbacOf 构造 admin 专属端点模式（路径按 "/" 切段存储）
func rbacOf(method, path string) rbacPattern {
	return rbacPattern{method: method, segments: strings.Split(path, "/")}
}

// matchRBACPattern 判断 method+path 是否命中 admin 专属端点矩阵
func matchRBACPattern(method, path string) bool {
	segments := strings.Split(path, "/")
	for _, p := range adminOnlyPatterns {
		if p.method != method || len(p.segments) != len(segments) {
			continue
		}
		matched := true
		for i, seg := range p.segments {
			if strings.HasPrefix(seg, ":") {
				continue // 参数段匹配任意值
			}
			if seg != segments[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// roleAuthz 端点级 RBAC 授权中间件：挂载于 /api/v1 JWT 组，紧随 jwtAuth（依赖其注入
// X-Role）。命中 admin 专属端点且角色非 ROLE_ADMIN → 403 统一响应体，不转发后端。
// fail-closed：X-Role 缺失（如鉴权链路异常）视同无权限。
func roleAuthz() gin.HandlerFunc {
	return func(c *gin.Context) {
		if authWhitelisted(c.Request.Method, c.Request.URL.Path) {
			c.Next() // 登录入口无角色语义，跳过授权
			return
		}
		role := c.GetHeader("X-Role")
		if role == roleAdmin {
			c.Next()
			return
		}
		if matchRBACPattern(c.Request.Method, c.Request.URL.Path) {
			log.Warn().Str("role", role).Str("method", c.Request.Method).
				Str("path", c.Request.URL.Path).Msg("rbac denied: admin-only endpoint")
			abortJSON(c, http.StatusForbidden, http.StatusForbidden,
				"forbidden: role not allowed for this endpoint")
			return
		}
		c.Next()
	}
}
