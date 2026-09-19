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
//	/patients 写操作    → /api/v1/admin/patients(POST) · /admin/patients/batch-bind ·
//	                      /admin/patients/:id/team · /admin/patients/:id/unbind-wechat ·
//	                      /admin/patients/:id/phone（T190，账号接管风险面）
//
// T190 另立 staffOnlyPatterns：/admin/patients 与 /admin/dashboard/* 的「读」端点医生/客服/
// 技师确有调用方（doctor 的 monitor/orthosis-log/review-records 页、tech-miniapp 绑定步骤、
// doctor 的 dashboard 页），故不归 admin 专属，但患者角色一律 403。
//
// 其余业务端点（alerts/feedbacks/患者域 /patients/:id/* 等）仍为多角色可用：
// 数据范围收敛（医生仅本团队 / 客服仅沟通域）为后端 RBAC 细化职责（Phase 2，
// user-service/internal/rbac），患者域水平鉴权在 handler 层 self-scope。
// 前端 ROLE_PAGE_MATRIX 为 UX 层守卫，本网关矩阵是安全控制，两者须同步变更。
//
// ⚠️ roleAuthz 仍是「默认放行」：不在任一矩阵内的路径对所有已认证角色开放。
// 新增 /admin/* 端点必须同时登记矩阵，否则等于零防护（T184 排查清单 P0-1 建议的
// 默认拒绝尚未实施，会打断现存的 48 条未登记路径）。
package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// 预置角色常量（对齐 user-service/internal/rbac.Role*，跨模块不直接依赖）
const (
	roleAdmin   = "ROLE_ADMIN"
	roleDoctor  = "ROLE_DOCTOR"
	roleCS      = "ROLE_CS"
	roleTech    = "technician" // 技师登录签发 role="technician"（user-service handler.go techLogin）
	rolePatient = "patient"    // 患者登录签发 role="patient"（user-service patientLogin / wxLogin / bindPhone，均走 SignWithTeam）
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
	rbacOf(http.MethodGet, "/api/v1/admin/teams/stats"), // T256 #1 团队统计卡

	// T185 订阅额度授予：权益写操作。患者域读端点（wear-reminder / subscription-quota /
	// notifications）不进本矩阵——患者需自查本人，水平越权由 msg-service handler 层
	// requireSelfScope 拦截；唯 grant 若放行 self-scope 等于患者可自行加额，故收敛为 admin-only。
	rbacOf(http.MethodPost, "/api/v1/patients/:patientId/subscription-quota/grant"),

	// T190 表 B：后台患者管理写端点。患者 token 原先可 PUT /admin/patients/:id/phone
	// 改他人手机号 → 用新号登录对方账号（账号接管）。逐条核实前端调用方后收口为 admin-only：
	//   创建/批量绑定/改团队 = admin 专属 /patients 页；解绑微信/改手机号 = 全仓零前端调用。
	// 同 5 个 handler 内部另有角色判定（user-service admin_patient.go / handler.go），双层防御。
	rbacOf(http.MethodPost, "/api/v1/admin/patients"),
	rbacOf(http.MethodPost, "/api/v1/admin/patients/batch-bind"),
	rbacOf(http.MethodPut, "/api/v1/admin/patients/:patientId/team"),
	rbacOf(http.MethodPost, "/api/v1/admin/patients/:patientId/unbind-wechat"),
	rbacOf(http.MethodPut, "/api/v1/admin/patients/:patientId/phone"),
	// T248 4.3 档案编辑（姓名/性别/年龄/诊断/Cobb）。不登记则 default-allow 放行任意角色。
	rbacOf(http.MethodPut, "/api/v1/admin/patients/:patientId"),
}

// techAdminOnlyPatterns 仅技师+管理员可访问端点矩阵（T122）：
// 安装记录元数据回填——仅安装技师（technician）与管理员（ROLE_ADMIN）可写，
// 患者/医生/客服等角色 → 403。T089 技师端调用时本就携带技师登录 JWT，前端零改动。
//
// T193：配网密钥领卡端点已迁出本矩阵，改由 provisionKeyPatterns + provisionKeyRoles
// 单独放行患者（归属校验见 device-service，非本层职责）。
//
// 注意口径不一致（已知，见独立安全加固任务）：
//   - PUT /api/v1/install-records/:id 已收紧为 tech+admin（签名=责任归属，医生/客服不得代签）
//   - POST /api/v1/install-records 仍为全 full-scope 角色开放（创建端点，收紧需另评）
var techAdminOnlyPatterns = []rbacPattern{
	rbacOf(http.MethodPut, "/api/v1/install-records/:id"), // T122 安装记录元数据回填（技师+管理员）
}

// provisionKeyPatterns 配网密钥领卡端点（T067；T091 收紧为 tech+admin；T193 放开患者）
var provisionKeyPatterns = []rbacPattern{
	rbacOf(http.MethodPost, "/api/v1/devices/:deviceId/provision-key"),
}

// provisionKeyRoles 可领配网密钥的角色白名单（T193，Boss 裁决 D4「患者需要这把钥匙」）：
// 技师/管理员口径不变，新增患者以支撑 PRD §7A.9 患者自助配网。
//
// 用 allow-list 而非「仅拒非 patient」：X-Role 缺失（鉴权链路异常）或将来新增角色默认 403，
// 口径同 T185 subscription-quota/grant 与 T190 staffOnlyPatterns。
// 患者「只能领自己已绑定设备」不在本层实现——gateway 无 device→patient 视图，
// 归属校验落在 device-service provisionKey handler（见 requireDeviceBoundToCaller）。
var provisionKeyRoles = map[string]bool{roleAdmin: true, roleTech: true, rolePatient: true}

// matchProvisionKeyPattern 判断 method+path 是否为配网密钥领卡端点
func matchProvisionKeyPattern(method, path string) bool {
	return matchPatterns(method, path, provisionKeyPatterns)
}

// doctorAdminOnlyPatterns 仅医生+管理员可访问端点矩阵（T130 / T135）：
// 复查记录创建、复查报告模板管理——仅医生（ROLE_DOCTOR）与管理员（ROLE_ADMIN）可访问，
// 患者/客服等 → 403。
var doctorAdminOnlyPatterns = []rbacPattern{
	rbacOf(http.MethodPost, "/api/v1/admin/review-records"), // T130 创建复查记录
	// T135 复查报告模板管理（合同运营后台「复查报告模板管理」；admin+doctor 均需：
	//   admin 后台上传/替换/列表/下载；doctor 列表/下载空白模板线下填写）
	rbacOf(http.MethodPost, "/api/v1/admin/review-templates"),                  // 上传/创建模板
	rbacOf(http.MethodPost, "/api/v1/admin/review-templates/:groupId/replace"), // 版本替换
	rbacOf(http.MethodGet, "/api/v1/admin/review-templates"),                   // 模板列表
	rbacOf(http.MethodGet, "/api/v1/admin/review-templates/:groupId/download"), // 模板下载
}

// staffOnlyPatterns T190 表 B 剩余行：后台管理域「读」端点——仅限内部 staff 角色，
// 患者 token 一律 403。不并入 adminOnlyPatterns 的理由是实测调用方（非推断）：
//   - GET /admin/patients        → doctor 的 monitor / orthosis-log / review-records 页共用
//   - GET /admin/patients/:id    → tech-miniapp 绑定步骤用 technician JWT 调用
//   - GET /admin/dashboard/*     → /dashboard 页在 doctor 的角色矩阵内（permissions.ts）
//
// 用 staff allow-list 而非「deny patient」：X-Role 缺失或将来新增角色时默认 403（fail-closed）。
var staffOnlyPatterns = []rbacPattern{
	rbacOf(http.MethodGet, "/api/v1/admin/patients"),
	rbacOf(http.MethodGet, "/api/v1/admin/patients/:patientId"),
	rbacOf(http.MethodGet, "/api/v1/admin/feeling-logs"), // T256 #2 跨患者感受日志流
	rbacOf(http.MethodGet, "/api/v1/admin/dashboard/kpi"),
	rbacOf(http.MethodGet, "/api/v1/admin/dashboard/wear-trend"),
	rbacOf(http.MethodGet, "/api/v1/admin/dashboard/wear-distribution"),
	rbacOf(http.MethodGet, "/api/v1/admin/dashboard/alert-trend"),
	rbacOf(http.MethodGet, "/api/v1/admin/dashboard/team-ranking"),
	rbacOf(http.MethodGet, "/api/v1/admin/dashboard/doctor-ranking"),
	// T248 7.1 患者沟通统计栏：全院聚合计数（今日咨询/待回复/平均响应），后台工作台专用。
	// 不登记即 default-allow ⇒ 患者 token 也能读全院计数。
	rbacOf(http.MethodGet, "/api/v1/feedbacks/stats"),
}

// staffRoles 内部 staff 角色集合（患者端 patient-miniapp 全仓零 /admin/* 调用，故不含 rolePatient）
var staffRoles = []string{roleAdmin, roleDoctor, roleCS, roleTech}

// matchStaffOnlyPattern 判断 method+path 是否命中「仅 staff 可读」端点矩阵
func matchStaffOnlyPattern(method, path string) bool {
	return matchPatterns(method, path, staffOnlyPatterns)
}

// isStaffRole 判断角色是否属于内部 staff（admin / doctor / cs / technician）
func isStaffRole(role string) bool {
	for _, r := range staffRoles {
		if r == role {
			return true
		}
	}
	return false
}

// matchDoctorAdminPattern 判断 method+path 是否命中 doctor+admin 专属端点矩阵
func matchDoctorAdminPattern(method, path string) bool {
	return matchPatterns(method, path, doctorAdminOnlyPatterns)
}

// matchTechAdminPattern 判断 method+path 是否命中 tech+admin 专属端点矩阵
func matchTechAdminPattern(method, path string) bool {
	return matchPatterns(method, path, techAdminOnlyPatterns)
}

// rbacOf 构造 admin 专属端点模式（路径按 "/" 切段存储）
func rbacOf(method, path string) rbacPattern {
	return rbacPattern{method: method, segments: strings.Split(path, "/")}
}

// matchPatterns 判断 method+path 是否命中给定端点模式列表
func matchPatterns(method, path string, patterns []rbacPattern) bool {
	segments := strings.Split(path, "/")
	for _, p := range patterns {
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

// matchRBACPattern 判断 method+path 是否命中 admin 专属端点矩阵
func matchRBACPattern(method, path string) bool {
	return matchPatterns(method, path, adminOnlyPatterns)
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
		// T193：配网密钥领卡——技师/管理员/患者放行（患者限本人已绑定设备，由 device-service 校验），
		// 医生/客服/角色缺失 → 403（与 T091 收紧后的口径一致）
		if matchProvisionKeyPattern(c.Request.Method, c.Request.URL.Path) && !provisionKeyRoles[role] {
			log.Warn().Str("role", role).Str("method", c.Request.Method).
				Str("path", c.Request.URL.Path).Msg("rbac denied: provision-key role not in allow-list")
			abortJSON(c, http.StatusForbidden, http.StatusForbidden,
				"forbidden: role not allowed for this endpoint")
			return
		}
		// T091：tech+admin 专属端点（如安装记录元数据回填）——仅 technician 与 ROLE_ADMIN 可访问
		if matchTechAdminPattern(c.Request.Method, c.Request.URL.Path) && role != roleTech {
			log.Warn().Str("role", role).Str("method", c.Request.Method).
				Str("path", c.Request.URL.Path).Msg("rbac denied: tech-or-admin-only endpoint")
			abortJSON(c, http.StatusForbidden, http.StatusForbidden,
				"forbidden: role not allowed for this endpoint")
			return
		}
		// T130：doctor+admin 专属端点（如复查记录创建）——仅 ROLE_DOCTOR 与 ROLE_ADMIN 可访问
		if matchDoctorAdminPattern(c.Request.Method, c.Request.URL.Path) && role != roleDoctor {
			log.Warn().Str("role", role).Str("method", c.Request.Method).
				Str("path", c.Request.URL.Path).Msg("rbac denied: doctor-or-admin-only endpoint")
			abortJSON(c, http.StatusForbidden, http.StatusForbidden,
				"forbidden: role not allowed for this endpoint")
			return
		}
		// T190：后台管理域读端点（患者档案/全院聚合）——患者及未知角色 403，staff 放行
		if matchStaffOnlyPattern(c.Request.Method, c.Request.URL.Path) && !isStaffRole(role) {
			log.Warn().Str("role", role).Str("method", c.Request.Method).
				Str("path", c.Request.URL.Path).Msg("rbac denied: staff-only endpoint")
			abortJSON(c, http.StatusForbidden, http.StatusForbidden,
				"forbidden: role not allowed for this endpoint")
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
