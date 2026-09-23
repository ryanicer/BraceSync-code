// Package handler user-service HTTP 接入层（Gin）—— T030 admin 域端点
//
// 路由（对齐 docs/ T030 段）：
//
//	POST /api/v1/auth/login                              运营后台登录（签发 HS256 JWT）
//	GET  /api/v1/admin/patients                          管理端患者分页（团队/医生姓名 join）
//	GET  /api/v1/admin/patients/:patientId               患者详情（管理端）
//	GET  /api/v1/teams                                   团队概要
//	GET  /api/v1/teams/:teamId                           团队单条详情（T333 补，此前 404）
//	GET  /api/v1/teams/:teamId/members                   团队成员明细（医生+技师）
//	GET  /api/v1/doctors                                 医生列表（含患者计数）
//	GET  /api/v1/technicians                             技师分页列表
//	POST /api/v1/admin/technicians                       技师新建
//	PUT  /api/v1/admin/technicians/:techId               技师编辑
//	POST /api/v1/technicians/:techId/toggle              技师启用/禁用
//	GET  /api/v1/feedbacks                               反馈列表
//	POST /api/v1/feedbacks                               反馈创建（T311 患者端配网失败自动存档）
//	GET  /api/v1/feedbacks/stats                         反馈统计栏三项（T248 7.1）
//	POST /api/v1/feedbacks/:feedbackId/process           反馈处理（replyContent 落库）
//	PUT  /api/v1/admin/patients/:patientId               患者档案编辑（T248 4.3）
//	GET  /api/v1/patients/:patientId/orthosis-plans      矫形方案历史
//	POST /api/v1/patients/:patientId/orthosis-plans      保存新方案（版本递增）
//	GET  /api/v1/patients/:patientId/feeling-logs        佩戴感受日志
//	POST /api/v1/feeling-logs/:logId/reply               医生回复感受日志
//	GET  /api/v1/admin/roles                             RBAC 角色列表
//	GET  /api/v1/admin/roles/:roleId/permissions         权限矩阵读
//	PUT  /api/v1/admin/roles/:roleId/permissions         权限矩阵写
//	GET  /api/v1/admin/role-templates                   T252 11.4 角色模板下拉
//	POST /api/v1/admin/roles                            T252 11.2 新建角色
//	PUT  /api/v1/admin/roles/:roleId                    T252 11.2 改角色（预置改名 400）
//	DELETE /api/v1/admin/roles/:roleId                  T252 11.2 删除角色（被引用 409）
//	GET  /api/v1/admin/permissions/catalog              T257 11.5 子权限目录（9 组 23 项）
//	GET  /api/v1/admin/me/permissions                   T257 11.5 当前用户有效权限（前端渲染用）
//	GET  /api/v1/admin/settings                          系统参数读
//	PUT  /api/v1/admin/settings                          系统参数写
//	GET  /api/v1/admin/alert-rules                      T252 2.2 告警规则聚合视图
//	PUT  /api/v1/admin/alert-rules/points               T252 2.2 保存逐采集点阈值
//	POST /api/v1/admin/alert-rules/points/reset         T252 2.2 恢复默认
//	PUT  /api/v1/admin/alert-rules/global               T252 2.2 保存全局告警规则
//	GET  /api/v1/admin/audit-logs                       T252 12.3 操作日志分页查询
//	GET  /healthz                                        存活探针
//
// 统一响应体（架构 §3.5）：{ "code": 0, "message": "success", "data": {...} }
// 鉴权归 gateway（JWT + RBAC，/api/v1 路由组挂载点 Phase 1 落地）；
// 操作人取网关注入的 X-User-Id（架构 §5.2 内部信任链）。
package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/service"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
	"github.com/bracesync/bracesync/services/user-service/internal/wechat"
)

// headerUserID gateway 鉴权通过后注入的操作人身份头（架构 §5.2）
const headerUserID = "X-User-Id"

// headerRole gateway 鉴权通过后注入的角色头（架构 §5.2）
const headerRole = "X-Role"

const roleAdmin = "ROLE_ADMIN"

// staffRoles 内部 staff 角色集合（与 gateway / file-service / data-service 对齐；不含 patient）。
// T264：admin 全量跨患者；doctor/cs/technician 暂按 staff 放行（团队归属过滤待 T184 落地）。
var staffRoles = map[string]bool{
	"admin":       true,
	"ROLE_ADMIN":  true,
	"ROLE_DOCTOR": true,
	"ROLE_CS":     true,
	"technician":  true,
}

// isStaffRole 判断角色是否属于内部 staff
func isStaffRole(role string) bool { return staffRoles[role] }

// assertAdminOrSelf 水平鉴权（T264）：staff 放行；非 staff（patient）仅 X-User-Id == patientId。
// fail-closed：缺失 X-User-Id 视为无权限。
func assertAdminOrSelf(c *gin.Context, patientID string) bool {
	if isStaffRole(c.GetHeader(headerRole)) {
		return true
	}
	userID := c.GetHeader(headerUserID)
	if userID == "" || userID != patientID {
		fail(c, model.ErrForbidden("may only access your own data"))
		return false
	}
	return true
}

// scopeBindPrefix T159：绑定态 JWT sub 前缀（标记 scope=bind）。
// 与 services/gateway/cmd/server/scope_authz.go scopeBindPrefix 同名同值（双侧契约）。
// wxLogin 签发 bindToken 时把 openid 包成 "openid_<raw>"；bindPhone 消费侧用
// stripScopeBindPrefix 还原回 raw openid 后再与 DB wx_openid / phoneToken.openid 比较。
const scopeBindPrefix = "openid_"

// stripScopeBindPrefix 把绑定态 JWT sub 还原成 raw openid（DB / phoneToken 内部契约用 raw）。
// 入参若没有前缀，原样返回（防御 gateway 注入异常 / 旧 token 残留场景）。
func stripScopeBindPrefix(sub string) string {
	return strings.TrimPrefix(sub, scopeBindPrefix)
}

// 预置角色（PRD §7D.11，权限系统锁定标识）
// T262：Boss 2026-09-20 裁定登录角色只 3 个 —— 运营管理员 / 医生 / 客服；
// 主任医师 / 主治医师 / 康复师 / 护士是**医护职称**，走 doctors.title，不是角色
// （000016 误播的 5 条已由 migration 000017 删除）。
// 命中本表的锁定语义见 11.2：禁删除（403）、禁改名（400），描述与启停仍可改。
// 网关 RBAC / 登录签发链路按字面量匹配的也正是这 3 条（services/gateway/cmd/server/rbac.go:42-44）。
var presetRoles = map[string]struct{}{
	"ROLE_ADMIN":  {},
	"ROLE_DOCTOR": {},
	"ROLE_CS":     {},
}

// Handler HTTP 处理器（signer/phoneCipher 允许为 nil：对应登录/技师写入返回 500 配置错误；
// wxClient 允许为 nil：/patient/wx-login 返回 500，不影响其他登录端点）
type Handler struct {
	store            repo.Store
	signer           *token.Signer
	bindSigner       *token.Signer // T085：绑定态 JWT signer（同 secret，ttl=30min）
	phone            *phone.Cipher
	wxClient         wxClientI      // 接口化：单测注入内存 fake；生产为 *wechat.Client
	phoneTokenSecret string         // T085：phoneToken 签发/校验密钥（独立于 JWT_SECRET）
	fileSvc          fileSvcClientI // T130：file-service 客户端（获取报告文件元数据+下载URL）
}

// New 创建 Handler（保持三参签名兼容现有测试与调用方；main.go 通过 SetWXClient 注入真实客户端）。
// 缺失 signer：登录端点返回 500；缺失 phoneCipher：技师写入端点返回 500；
// wxClient 需显式 SetWXClient 注入，未注入时 /patient/wx-login 返回 500（不影响其他端点）。
// bindSigner 由 signer 派生（CloneWithTTL 30min）；若 signer 为 nil 则 bindSigner 亦为 nil。
func New(store repo.Store, signer *token.Signer, phoneCipher *phone.Cipher) *Handler {
	h := &Handler{store: store, signer: signer, phone: phoneCipher, wxClient: nil}
	if signer != nil {
		h.bindSigner = signer.CloneWithTTL(30 * time.Minute)
	}
	return h
}

// SetWXClient 注入微信登录客户端（nil 视为未配置）。
// 接 wxClientI 接口以便测试注入 wrapper（嵌入 testhelper.MockWechatClient）。
func (h *Handler) SetWXClient(wx wxClientI) {
	h.wxClient = wx
}

// SetPhoneTokenSecret T085：注入 phoneToken 签发/校验密钥（env PHONE_TOKEN_SECRET）。
func (h *Handler) SetPhoneTokenSecret(secret string) {
	h.phoneTokenSecret = secret
}

// SetPhoneCipher 注入手机号 AES-GCM 加密器（供测试注入；生产由 New 传入）。
func (h *Handler) SetPhoneCipher(c *phone.Cipher) {
	h.phone = c
}

// SetFileSvc T130：注入 file-service 客户端（nil 时复查记录列表不返回文件元数据/下载URL）。
func (h *Handler) SetFileSvc(c fileSvcClientI) {
	h.fileSvc = c
}

// wxClientI 微信客户端最小接口：
// handler 测试可用内存 fake/wrapper 轻量注入；生产 *wechat.Client 自然实现。
type wxClientI interface {
	DoCode2Session(ctx context.Context, code string) (*wechat.Code2SessionResult, error)
	// GetPhoneNumber T085：微信 phonenumber.getPhoneNumber（code 换手机号）。
	GetPhoneNumber(ctx context.Context, code string) (pureNumber, countryCode string, err error)
}

// fileSvcClientI T130：file-service 客户端最小接口（handler 测试可注入 fake）。
type fileSvcClientI interface {
	GetFileByID(ctx context.Context, fileID string) (*service.FileMetadata, error)
	GetDownloadURL(ctx context.Context, fileID string) (string, error)
}

// Router 组装路由（可测试）
func (h *Handler) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	// captureLogger 将当前全局 log.Logger 注入 request context，使 handler 在请求生命周期内
	// 使用同一 logger（避免并行测试覆写全局 log.Logger 导致审计日志串台）。
	r.Use(captureLogger)
	r.Use(h.scopeGuard) // T085：scope 鉴权（bind 仅放行 bind-phone；full 禁 bind-phone）

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler())) // T234 Prometheus 采集端点

	v1 := r.Group("/api/v1")
	// T252 12.3 操作日志：表驱动埋点（auditRoutes 命中的路由在 HTTP<400 时写 audit_logs）
	v1.Use(h.auditTrail())
	{
		v1.POST("/auth/login", h.login)
		v1.POST("/tech/login", h.techLogin)         // T037 技师登录（免 JWT）
		v1.POST("/patient/login", h.patientLogin)   // T037 患者登录（免 JWT）
		v1.POST("/patient/wx-login", h.wxLogin)     // T069 患者端微信登录（免 JWT）
		v1.POST("/patient/bind-phone", h.bindPhone) // T085 患者微信绑定手机号（需 scope=bind JWT）

		v1.GET("/admin/patients", h.listPatients)
		v1.GET("/admin/patients/:patientId", h.getPatient)
		v1.POST("/admin/patients", h.createPatient)                    // T057 创建患者
		v1.POST("/admin/patients/batch-bind", h.batchBindPatients)     // T057 批量绑定
		v1.PUT("/admin/patients/:patientId/team", h.assignPatientTeam) // T057 分配团队
		// T085 Admin 档案维护
		v1.POST("/admin/patients/:patientId/unbind-wechat", h.unbindWechat) // 解绑微信
		v1.PUT("/admin/patients/:patientId/phone", h.updatePatientPhone)    // 改手机号
		v1.PUT("/admin/patients/:patientId", h.updatePatientAdmin)          // T248 4.3 档案编辑

		v1.GET("/teams", h.listTeams)
		v1.GET("/teams/:teamId", h.getTeam) // T333 单条读（此前契约已声明 leader/leaderName 却无读路由，实测 404）
		v1.GET("/teams/:teamId/members", h.getTeamMembers)
		v1.GET("/admin/teams/stats", h.getTeamStats) // T256 #1 团队管理统计卡
		// T059 团队/成员写操作（stub，统一返回 500；实现方转绿时填充逻辑）
		v1.POST("/teams", h.createTeam)
		v1.PUT("/teams/:teamId", h.updateTeam)
		v1.DELETE("/teams/:teamId", h.deleteTeam)
		v1.POST("/teams/:teamId/members", h.addTeamMember)
		v1.PUT("/teams/:teamId/members/:memberId", h.updateTeamMember)
		v1.DELETE("/teams/:teamId/members/:memberId", h.removeTeamMember)
		v1.GET("/doctors", h.listDoctors)
		// T314 医护账号管理写通道（PRD §7D.10）：🔴 无 DELETE，「删除」按设计稿无入口不入需求
		v1.POST("/admin/doctors", h.createDoctorAccount)
		v1.PUT("/admin/doctors/:doctorId", h.updateDoctorAccount)
		v1.POST("/admin/doctors/:doctorId/reset-password", h.resetDoctorAccountPassword)
		v1.POST("/admin/doctors/:doctorId/status", h.setDoctorAccountStatus)

		v1.GET("/technicians", h.listTechnicians)
		v1.POST("/admin/technicians", h.createTechnician)
		v1.PUT("/admin/technicians/:techId", h.updateTechnician)
		v1.POST("/technicians/:techId/toggle", h.toggleTechnician)

		v1.GET("/feedbacks", h.listFeedbacks)
		v1.POST("/feedbacks", h.createFeedback)     // T311 患者端配网失败自动存档
		v1.GET("/feedbacks/stats", h.feedbackStats) // T248 7.1 统计栏三项
		v1.POST("/feedbacks/:feedbackId/process", h.processFeedback)

		v1.GET("/patient/profile", h.getPatientProfile) // T186 患者本人只读档案（self-scope）
		// T226 患者自助改本人资料（白名单+限本人，handler 拦水平越权）。
		// 用 PUT 而非 PATCH：微信小程序 wx.request 不支持 PATCH（真机发不出），PUT 承载部分更新语义。
		v1.PUT("/patients/:patientId", h.updatePatientProfile)

		v1.GET("/patients/:patientId/orthosis-plans", h.listPlans)
		v1.POST("/patients/:patientId/orthosis-plans", h.savePlan)
		v1.GET("/patients/:patientId/feeling-logs", h.listFeelingLogs)
		v1.POST("/feeling-logs/:logId/reply", h.replyFeelingLog)
		v1.GET("/admin/feeling-logs", h.listFeelingLogsAdmin) // T256 #2 跨患者感受日志流

		v1.GET("/admin/roles", h.listRoles)
		v1.GET("/admin/roles/:roleId/permissions", h.getPermissions)
		v1.PUT("/admin/roles/:roleId/permissions", h.updatePermissions)
		// T252 11.2 角色增删改（权限页「新增/编辑/删除」按钮）+ 11.4 角色模板下拉
		v1.GET("/admin/role-templates", h.listRoleTemplates)
		v1.POST("/admin/roles", h.createAdminRole)
		v1.PUT("/admin/roles/:roleId", h.updateAdminRole)
		v1.DELETE("/admin/roles/:roleId", h.deleteAdminRole)
		// T257 11.5 子权限目录 + 当前用户有效权限（前端渲染菜单/按钮用，不参与鉴权）
		v1.GET("/admin/permissions/catalog", h.getPermissionCatalog)
		v1.GET("/admin/me/permissions", h.getMyPermissions)

		v1.GET("/admin/settings", h.getSettings)
		v1.PUT("/admin/settings", h.updateSettings)

		// T252 2.2 告警规则配置（告警页 Tab2：4×5 网格逐点阈值 + 全局规则）
		v1.GET("/admin/alert-rules", h.getAlertRules)
		v1.PUT("/admin/alert-rules/points", h.updateAlertPointRules)
		v1.POST("/admin/alert-rules/points/reset", h.resetAlertPointRules)
		v1.PUT("/admin/alert-rules/global", h.updateAlertGlobalRules)

		// T252 12.3 操作日志（系统配置页 Tab3）
		v1.GET("/admin/audit-logs", h.getAuditLogs)

		// T274 告警流程画布（2.4 设计器 = 模板 CRUD；2.3 运行态 = 实例/节点状态/操作/时间线）
		// 契约：docs/api/api-contracts.ts。RBAC 在 gateway rbac.go 登记，本处另有 handler 兜底。
		v1.GET("/admin/flow/templates", h.listFlowTemplates)
		v1.POST("/admin/flow/templates", h.createFlowTemplate)
		v1.GET("/admin/flow/templates/:templateId", h.getFlowTemplate)
		v1.PUT("/admin/flow/templates/:templateId", h.updateFlowTemplate)
		v1.DELETE("/admin/flow/templates/:templateId", h.deleteFlowTemplate)
		v1.POST("/admin/flow/instances", h.startFlowInstance)
		v1.GET("/admin/flow/instances", h.getFlowInstances)
		v1.GET("/admin/flow/instances/:instanceId/nodes", h.getFlowNodeStates)
		v1.POST("/admin/flow/instances/:instanceId/nodes/:nodeId/actions", h.submitFlowNodeAction)
		v1.GET("/admin/flow/instances/:instanceId/actions", h.getFlowInstanceActions)

		// T130 复查记录（合同患者端「复查管理」）
		v1.POST("/admin/review-records", h.createReviewRecord)
		v1.GET("/patients/:patientId/review-records", h.listReviewRecords)

		// T135 复查报告模板（合同运营后台「复查报告模板管理」；RBAC 限 admin+doctor）
		v1.POST("/admin/review-templates", h.createReviewTemplate)
		v1.POST("/admin/review-templates/:groupId/replace", h.replaceReviewTemplate)
		v1.GET("/admin/review-templates", h.listReviewTemplates)
		v1.GET("/admin/review-templates/:groupId/download", h.downloadReviewTemplate)
	}
	return r
}

// jsonResp 统一响应体
type jsonResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, jsonResp{Code: model.CodeOK, Message: "success", Data: data})
}

func fail(c *gin.Context, appErr *model.AppError) {
	c.JSON(appErr.HTTPStatus, jsonResp{Code: appErr.Code, Message: appErr.Message, Data: nil})
}

// captureLogger 将调用时刻的全局 log.Logger 注入 request context。
// handler 通过 ctxLogger(c) 取回，确保单个请求生命周期内使用同一 logger，
// 规避并行测试覆写全局 log.Logger 导致审计日志串台。
func captureLogger(c *gin.Context) {
	ctx := c.Request.Context()
	ctx = log.Logger.WithContext(ctx)
	c.Request = c.Request.WithContext(ctx)
	c.Next()
}

// ctxLogger 从 request context 取回 captureLogger 注入的 logger；
// 若 context 无 logger（非 HTTP 入口调用），回退到当前全局 log.Logger。
func ctxLogger(c *gin.Context) *zerolog.Logger {
	if l := zerolog.Ctx(c.Request.Context()); l != nil {
		return l
	}
	return &log.Logger
}

// operatorID 操作人：网关注入的 X-User-Id，缺省 fallback（一期 gateway JWT 未上线）
func operatorID(c *gin.Context, fallback string) string {
	if v := c.GetHeader(headerUserID); v != "" {
		return v
	}
	return fallback
}

// scopeGuard T085：scope 鉴权中间件。
// 无 Authorization 头时放行（生产由 gateway 注入 X-Scope；测试直连时由本中间件解析 JWT）。
// scope 判定：JWT sub 前缀 "openid_" → bind（仅可访问 /patient/bind-phone）；否则 full（禁 bind-phone）。
// 通过后将 subject/scope 写入 gin context 供 handler 使用。
func (h *Handler) scopeGuard(c *gin.Context) {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		c.Next()
		return
	}
	const bearer = "Bearer "
	if !strings.HasPrefix(auth, bearer) {
		fail(c, model.ErrUnauthorized("invalid authorization header"))
		c.Abort()
		return
	}
	tokenStr := strings.TrimPrefix(auth, bearer)
	if h.signer == nil {
		fail(c, model.ErrInternal("JWT_SECRET not configured"))
		c.Abort()
		return
	}
	claims, err := h.signer.Verify(tokenStr)
	if err != nil {
		fail(c, model.ErrUnauthorized("invalid token: %v", err))
		c.Abort()
		return
	}
	sub := claims.Subject
	scope := "full"
	if strings.HasPrefix(sub, scopeBindPrefix) {
		scope = "bind"
	}
	c.Set("subject", sub)
	c.Set("scope", scope)

	path := c.Request.URL.Path
	switch {
	case scope == "bind" && path != "/api/v1/patient/bind-phone":
		fail(c, model.ErrForbiddenScope("bind scope only allows /patient/bind-phone"))
		c.Abort()
		return
	case scope == "full" && path == "/api/v1/patient/bind-phone":
		fail(c, model.ErrForbiddenScope("full scope cannot call /patient/bind-phone"))
		c.Abort()
		return
	}
	c.Next()
}

// parsePaging 分页参数（架构 §3.5：page 1 起，pageSize 默认 20 上限 100，非法 400）
func parsePaging(c *gin.Context) (int, int, *model.AppError) {
	page, pageSize := 1, model.DefaultPageSize
	if v := c.Query("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return 0, 0, model.ErrInvalidParam("invalid page %q", v)
		}
		page = n
	}
	if v := c.Query("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > model.MaxPageSize {
			return 0, 0, model.ErrInvalidParam("invalid pageSize %q", v)
		}
		pageSize = n
	}
	return page, pageSize, nil
}

// ─────────────────────────────────────────────────────────────
// 登录（T030 #9 admin / T037 技师 / T037 患者）
// ─────────────────────────────────────────────────────────────

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type phoneLoginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

// login 运营后台登录：bcrypt 校验 admins 表，签发 HS256 JWT（契约 adminLogin）
func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if req.Username == "" || req.Password == "" {
		fail(c, model.ErrInvalidParam("username and password are required"))
		return
	}
	if h.signer == nil {
		fail(c, model.ErrInternal("JWT_SECRET not configured"))
		return
	}

	admin, err := h.store.GetAdminByUsername(c.Request.Context(), req.Username)
	if err != nil {
		fail(c, model.ErrInternal("query admin failed"))
		return
	}
	// 统一 401 文案：不区分"用户不存在/密码错误"，防账号枚举
	if admin == nil || bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)) != nil {
		fail(c, model.ErrUnauthorized("invalid username or password"))
		return
	}
	if admin.Status != "enabled" {
		fail(c, model.ErrUnauthorized("account disabled"))
		return
	}

	// T040: 渐进式重哈希 - 检测到高成本 hash 则升级为 cost8
	if ShouldUpgradeToNewCost(admin.PasswordHash) {
		newHash, err := GenerateBcryptHash([]byte(req.Password))
		if err != nil {
			// 记录到 stderr 供运维排查，但不阻塞登录（降级策略）
			fmt.Fprintf(os.Stderr, "WARN: failed to generate new password hash: %v\n", err)
		} else if updateErr := h.store.UpdateAdminPasswordHash(c.Request.Context(), admin.AdminID, newHash); updateErr != nil {
			// 忽略更新失败，登录仍可继续（异步升级）
			fmt.Fprintf(os.Stderr, "WARN: failed to update admin password hash: %v\n", updateErr)
		}
	}

	scope, err := h.store.RoleScope(c.Request.Context(), admin.RoleID)
	if err != nil {
		fail(c, model.ErrInternal("query role scope failed"))
		return
	}
	tk, err := h.signer.Sign(admin.AdminID, admin.Username, admin.Name, admin.RoleID)
	if err != nil {
		fail(c, model.ErrInternal("sign token failed"))
		return
	}
	// T252 12.3：登录留痕（PRD §9.2a「谁在何时登录」）。必须在签发后写：
	// 操作人身份此处取 admin 行本身——登录接口在 gateway 免 JWT 白名单内，没有 X-User-Id 头。
	h.audit(c, repo.AuditInput{
		OperatorID:   admin.AdminID,
		OperatorRole: admin.RoleID,
		Action:       auditActionLogin,
		TargetType:   "admin",
		TargetID:     admin.AdminID,
		Description:  fmt.Sprintf("运营后台登录成功：%s（%s）", admin.Username, admin.RoleID),
	})
	ok(c, model.LoginResultDTO{
		Token:    tk,
		AdminID:  admin.AdminID,
		Username: admin.Username,
		Name:     admin.Name,
		RoleID:   admin.RoleID,
		Scope:    scope,
	})
}

// techLogin POST /api/v1/tech/login —— 技师手机号+密码登录（T037）
// 校验 status=enabled + auth_status=authorized → 签发 tech JWT；失败统一 401 防枚举
func (h *Handler) techLogin(c *gin.Context) {
	var req phoneLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if !validPhone(req.Phone) || req.Password == "" {
		fail(c, model.ErrInvalidParam("phone and password are required"))
		return
	}
	if h.signer == nil {
		fail(c, model.ErrInternal("JWT_SECRET not configured"))
		return
	}

	tech, err := h.store.GetTechByPhoneHash(c.Request.Context(), phone.Hash(req.Phone))
	if err != nil {
		fail(c, model.ErrInternal("query technician failed"))
		return
	}
	// 统一 401 文案：不区分"用户不存在/密码错误/禁用"，防账号枚举
	if tech == nil || bcrypt.CompareHashAndPassword([]byte(tech.PasswordHash), []byte(req.Password)) != nil {
		fail(c, model.ErrUnauthorized("invalid phone or password"))
		return
	}
	if tech.Status != "enabled" || tech.AuthStatus != "authorized" {
		fail(c, model.ErrUnauthorized("invalid phone or password"))
		return
	}

	tk, err := h.signer.SignWithTeam(tech.TechID, tech.Name, tech.TeamID, "technician")
	if err != nil {
		fail(c, model.ErrInternal("sign token failed"))
		return
	}
	ok(c, model.TechLoginResultDTO{
		Token:  tk,
		TechID: tech.TechID,
		Name:   tech.Name,
		TeamID: tech.TeamID,
		Role:   "technician",
	})
}

// patientLogin POST /api/v1/patient/login —— 患者手机号+密码登录（T037）
// 校验 status=active → 签发 patient JWT；失败统一 401 防枚举
func (h *Handler) patientLogin(c *gin.Context) {
	var req phoneLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if !validPhone(req.Phone) || req.Password == "" {
		fail(c, model.ErrInvalidParam("phone and password are required"))
		return
	}
	if h.signer == nil {
		fail(c, model.ErrInternal("JWT_SECRET not configured"))
		return
	}

	patient, err := h.store.GetPatientByPhoneHash(c.Request.Context(), phone.Hash(req.Phone))
	if err != nil {
		fail(c, model.ErrInternal("query patient failed"))
		return
	}
	// 统一 401 文案：不区分"用户不存在/密码错误/未激活"，防账号枚举
	if patient == nil || bcrypt.CompareHashAndPassword([]byte(patient.PasswordHash), []byte(req.Password)) != nil {
		fail(c, model.ErrUnauthorized("invalid phone or password"))
		return
	}
	if patient.Status != "active" {
		fail(c, model.ErrUnauthorized("invalid phone or password"))
		return
	}

	tk, err := h.signer.SignWithTeam(patient.PatientID, patient.Name, "", "patient")
	if err != nil {
		fail(c, model.ErrInternal("sign token failed"))
		return
	}
	ok(c, model.PatientLoginResultDTO{
		Token:     tk,
		PatientID: patient.PatientID,
		Name:      patient.Name,
		Role:      "patient",
	})
}

// ─────────────────────────────────────────────────────────────
// 微信登录（T069 患者端小程序）
//
// POST /api/v1/patient/wx-login — 入参 {code} → 调 jscode2session →
// 按 openid 查/建患者（status=active）→ 签发 patient JWT
// 失败统一 401/400/500/502 映射（见 T069 计划 §5 错误表）
// ─────────────────────────────────────────────────────────────

type wxLoginRequest struct {
	Code string `json:"code"`
}

// wxLogin 患者端微信登录（T069 + T085 改造）
// 三分支：
//   - 已绑定 + active → 200/0 + 正式 JWT（sub=patientID，8h）
//   - 已绑定 + inactive → 401 + 10001（统一文案防枚举）
//   - 未绑定 → 200 + 10601 + bindToken（scope=bind，30min，sub=openid）；不创建 patients 行
func (h *Handler) wxLogin(c *gin.Context) {
	var req wxLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if req.Code == "" {
		fail(c, model.ErrInvalidParam("code is required"))
		return
	}
	if h.wxClient == nil {
		fail(c, model.ErrInternal("WX_APPID/WX_APP_SECRET not configured; wechat login disabled"))
		return
	}

	sess, err := h.wxClient.DoCode2Session(c.Request.Context(), req.Code)
	if err != nil {
		var we *wechat.WechatError
		if errors.As(err, &we) {
			fail(c, model.ErrUnauthorized("wechat code invalid: errcode=%d errmsg=%s", we.ErrCode, we.ErrMsg))
			return
		}
		fail(c, model.NewWXServiceUnavailable("wechat service unavailable"))
		return
	}

	row, qErr := h.store.GetPatientByWXOpenID(c.Request.Context(), sess.OpenID)
	if qErr != nil {
		fail(c, model.ErrInternal("query patient by openid failed"))
		return
	}
	if row == nil {
		// T085：未绑定 → 返回 bindToken 引导绑定，不创建患者
		// T159：sub 加 scopeBindPrefix（openid_），网关 scopeAuthz 据此识别 scope=bind 放行 bind-phone。
		// 消费侧 bindPhone 用 stripScopeBindPrefix 还原为 raw openid 与 DB / phoneToken 对齐。
		if h.bindSigner == nil {
			fail(c, model.ErrInternal("JWT_SECRET not configured"))
			return
		}
		bindTok, bErr := h.bindSigner.SignWithTeam(scopeBindPrefix+sess.OpenID, "微信用户", "", "patient")
		if bErr != nil {
			fail(c, model.ErrInternal("sign bind token failed"))
			return
		}
		c.JSON(http.StatusOK, jsonResp{
			Code:    model.CodePatientNotBound,
			Message: "wechat openid not bound; bind phone required",
			Data:    gin.H{"token": bindTok},
		})
		return
	}
	if row.Status != "active" {
		// 不区分"禁用/不存在"统一 401 文案，防账号枚举；不返回 data（不泄露 token）
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    model.CodeInvalidCredentials,
			"message": "invalid_credentials",
		})
		return
	}
	if h.signer == nil {
		fail(c, model.ErrInternal("JWT_SECRET not configured"))
		return
	}
	tk, err := h.signer.SignWithTeam(row.PatientID, row.Name, "", "patient")
	if err != nil {
		fail(c, model.ErrInternal("sign token failed"))
		return
	}
	ok(c, model.PatientLoginResultDTO{
		Token:     tk,
		PatientID: row.PatientID,
		Name:      row.Name,
		Role:      "patient",
	})
}

// ─────────────────────────────────────────────────────────────
// 患者（T030 #1/#2）
// ─────────────────────────────────────────────────────────────

func toPatientDTO(r repo.PatientRow) model.AdminPatientDTO {
	return model.AdminPatientDTO{
		PatientID:  r.PatientID,
		Name:       r.Name,
		Gender:     r.Gender,
		Age:        r.Age,
		Diagnosis:  r.Diagnosis,
		CobbAngle:  r.CobbAngle,
		DeviceID:   r.DeviceID,
		TeamID:     r.TeamID,
		DoctorID:   r.DoctorID,
		Status:     r.Status,
		CreatedAt:  r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:  r.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		TeamName:   r.TeamName,
		DoctorName: r.DoctorName,

		HeightCm:                 r.HeightCm,
		WeightKg:                 r.WeightKg,
		EmergencyContactName:     r.EmergencyContactName,
		EmergencyContactPhone:    r.EmergencyContactPhone,
		EmergencyContactRelation: r.EmergencyContactRelation,
	}
}

// listPatients GET /api/v1/admin/patients —— 分页 + keyword/teamId 筛选（姓名 join）
func (h *Handler) listPatients(c *gin.Context) {
	page, pageSize, appErr := parsePaging(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	rows, total, err := h.store.ListPatients(c.Request.Context(), repo.PatientFilter{
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		TeamID:   c.Query("teamId"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		fail(c, model.ErrInternal("list patients failed"))
		return
	}
	list := make([]model.AdminPatientDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, toPatientDTO(r))
	}
	ok(c, model.PageData{List: list, Total: total, Page: page, PageSize: pageSize})
}

// getPatient GET /api/v1/admin/patients/:patientId —— 详情，不存在 404
func (h *Handler) getPatient(c *gin.Context) {
	row, err := h.store.GetPatient(c.Request.Context(), c.Param("patientId"))
	if err != nil {
		fail(c, model.ErrInternal("get patient failed"))
		return
	}
	if row == nil {
		fail(c, model.ErrNotFound("patient not found: %s", c.Param("patientId")))
		return
	}
	ok(c, toPatientDTO(*row))
}

// getPatientProfile GET /api/v1/patient/profile —— 患者本人只读档案（T186，C-PM-15 只读版）
// self-scope：查询对象只取网关注入的 X-User-Id（患者 JWT 的 sub），路径不携带患者 ID，
// 因此结构上无法请求他人档案；身份头缺失按 fail-closed 拒绝（403）。
func (h *Handler) getPatientProfile(c *gin.Context) {
	patientID := c.GetHeader(headerUserID)
	if patientID == "" {
		fail(c, model.ErrForbidden("patient identity required"))
		return
	}
	row, err := h.store.GetPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("get patient profile failed"))
		return
	}
	if row == nil {
		fail(c, model.ErrNotFound("patient not found: %s", patientID))
		return
	}
	ok(c, toPatientDTO(*row))
}

// ─────────────────────────────────────────────────────────────
// 团队 / 医生（T030 #10）
// ─────────────────────────────────────────────────────────────

// nilIfBlank 空串回 nil（T333：序列化成 JSON null，前端的 ?? 兜底只在 null/undefined 上生效）
func nilIfBlank(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// listTeams GET /api/v1/teams —— 团队概要
func (h *Handler) listTeams(c *gin.Context) {
	rows, err := h.store.ListTeams(c.Request.Context())
	if err != nil {
		fail(c, model.ErrInternal("list teams failed"))
		return
	}
	list := make([]model.TeamDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, model.TeamDTO{
			TeamID:       r.TeamID,
			Name:         r.Name,
			MemberCount:  r.MemberCount,
			PatientCount: r.PatientCount,
			Leader:       nilIfBlank(r.Leader),     // T333：负责人 doctor_id
			LeaderName:   nilIfBlank(r.LeaderName), // T333：join doctors.name
		})
	}
	ok(c, list)
}

func (h *Handler) toDoctorDTO(r repo.DoctorRow) model.DoctorDTO {
	masked := ""
	if h.phone != nil {
		masked = h.phone.Masked(r.PhoneEnc)
	}
	return model.DoctorDTO{
		DoctorID:     r.DoctorID,
		Name:         r.Name,
		Title:        strOr(r.Title, ""),
		Department:   strOr(r.Department, ""),
		TeamID:       r.TeamID,
		PhoneMasked:  masked,
		PatientCount: r.PatientCount,
		Status:       r.Status,
		// T314：admins 侧三列指针原样透出（未绑账号 = nil = JSON null，不得填成空串冒充有值）
		Username:      r.Username,
		AccountStatus: r.AccountStatus,
		CreatedAt:     timePtrRFC3339(r.AccountCreatedAt),
	}
}

// timePtrRFC3339 *time.Time → RFC3339（UTC，带 Z 标记）指针；nil 原样回 nil。
// 与既有 CreatedAt 字段同格式（handler.go 内其它 DTO 用同一 layout 串）。
func timePtrRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format("2006-01-02T15:04:05Z07:00")
	return &s
}

func (h *Handler) toTechDTO(r repo.TechnicianRow) model.TechnicianDTO {
	masked := ""
	if h.phone != nil {
		masked = h.phone.Masked(r.PhoneEnc)
	}
	return model.TechnicianDTO{
		TechID:       r.TechID,
		Name:         r.Name,
		PhoneMasked:  masked,
		TeamID:       strOr(r.TeamID, ""),
		TeamName:     r.TeamName,
		InstallCount: r.InstallCount,
		Status:       r.Status,
		AuthStatus:   r.AuthStatus,
	}
}

func strOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// getTeam GET /api/v1/teams/:teamId —— 团队单条详情（T333）
// 与 POST / PUT 复用同一投影 SQL；团队不存在 404。
func (h *Handler) getTeam(c *gin.Context) {
	teamID := c.Param("teamId")
	row, err := h.store.GetTeam(c.Request.Context(), teamID)
	if err != nil {
		if errors.Is(err, repo.ErrTeamNotFound) {
			fail(c, model.ErrNotFound("team not found: %s", teamID))
			return
		}
		fail(c, model.ErrInternal("get team failed"))
		return
	}
	ok(c, toTeamDetailDTO(*row))
}

// getTeamMembers GET /api/v1/teams/:teamId/members —— 成员明细（医生+技师），团队不存在 404
func (h *Handler) getTeamMembers(c *gin.Context) {
	teamID := c.Param("teamId")
	exists, err := h.store.TeamExists(c.Request.Context(), teamID)
	if err != nil {
		fail(c, model.ErrInternal("query team failed"))
		return
	}
	if !exists {
		fail(c, model.ErrNotFound("team not found: %s", teamID))
		return
	}
	doctors, err := h.store.ListDoctorsByTeam(c.Request.Context(), teamID)
	if err != nil {
		fail(c, model.ErrInternal("list team doctors failed"))
		return
	}
	techs, err := h.store.ListTechniciansByTeam(c.Request.Context(), teamID)
	if err != nil {
		fail(c, model.ErrInternal("list team technicians failed"))
		return
	}
	doctorList := make([]model.DoctorDTO, 0, len(doctors))
	for _, d := range doctors {
		doctorList = append(doctorList, h.toDoctorDTO(d))
	}
	techList := make([]model.TechnicianDTO, 0, len(techs))
	for _, t := range techs {
		techList = append(techList, h.toTechDTO(t))
	}
	ok(c, model.TeamMembersDTO{Doctors: doctorList, Technicians: techList})
}

// listDoctors GET /api/v1/doctors —— 医生列表（含患者计数）
func (h *Handler) listDoctors(c *gin.Context) {
	rows, err := h.store.ListDoctors(c.Request.Context())
	if err != nil {
		fail(c, model.ErrInternal("list doctors failed"))
		return
	}
	list := make([]model.DoctorDTO, 0, len(rows))
	for _, d := range rows {
		list = append(list, h.toDoctorDTO(d))
	}
	ok(c, list)
}

// ─────────────────────────────────────────────────────────────
// 技师（T030 #4：新建/编辑 + 列表/启停）
// ─────────────────────────────────────────────────────────────

// listTechnicians GET /api/v1/technicians —— 分页列表
func (h *Handler) listTechnicians(c *gin.Context) {
	page, pageSize, appErr := parsePaging(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	rows, total, err := h.store.ListTechnicians(c.Request.Context(), page, pageSize)
	if err != nil {
		fail(c, model.ErrInternal("list technicians failed"))
		return
	}
	list := make([]model.TechnicianDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, h.toTechDTO(r))
	}
	ok(c, model.PageData{List: list, Total: total, Page: page, PageSize: pageSize})
}

type techRequest struct {
	Name   string  `json:"name"`
	Phone  string  `json:"phone"`
	TeamID *string `json:"teamId"`
}

// validPhone 手机号格式（大陆 11 位 1 开头；与患者端/技师端注册口径一致）
func validPhone(p string) bool {
	if len(p) != 11 || p[0] != '1' {
		return false
	}
	for _, ch := range p {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// newTechID 生成技师 ID（TECH + 12 位随机 hex，VARCHAR(32) 内）
func newTechID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return "TECH" + hex.EncodeToString(buf)
}

// preparePhone 加密+哈希准备；加密器未配置返回 500 配置错误
func (h *Handler) preparePhone(plain string) (enc []byte, hash string, appErr *model.AppError) {
	if h.phone == nil {
		return nil, "", model.ErrInternal("phone encryption key not configured")
	}
	enc, err := h.phone.Encrypt(plain)
	if err != nil {
		return nil, "", model.ErrInternal("encrypt phone failed")
	}
	return enc, phone.Hash(plain), nil
}

// validateTechTeam teamId 传入时校验存在性（FK 前置，友好 400 替代 DB 违约）
func (h *Handler) validateTechTeam(c *gin.Context, teamID *string) *model.AppError {
	if teamID == nil || *teamID == "" {
		return nil
	}
	exists, err := h.store.TeamExists(c.Request.Context(), *teamID)
	if err != nil {
		return model.ErrInternal("query team failed")
	}
	if !exists {
		return model.ErrInvalidParam("team not found: %s", *teamID)
	}
	return nil
}

// createTechnician POST /api/v1/admin/technicians —— 新建（手机号查重 409）
func (h *Handler) createTechnician(c *gin.Context) {
	var req techRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		fail(c, model.ErrInvalidParam("name is required"))
		return
	}
	if !validPhone(req.Phone) {
		fail(c, model.ErrInvalidParam("invalid phone: must be 11 digits starting with 1"))
		return
	}
	if appErr := h.validateTechTeam(c, req.TeamID); appErr != nil {
		fail(c, appErr)
		return
	}
	enc, hash, appErr := h.preparePhone(req.Phone)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	taken, err := h.store.TechPhoneHashTaken(c.Request.Context(), hash, "")
	if err != nil {
		fail(c, model.ErrInternal("check phone hash failed"))
		return
	}
	if taken {
		fail(c, model.ErrConflict("phone already registered"))
		return
	}
	row, err := h.store.CreateTechnician(c.Request.Context(), repo.TechInput{
		TechID: newTechID(), Name: strings.TrimSpace(req.Name),
		PhoneEnc: enc, PhoneHash: hash, TeamID: req.TeamID,
	})
	if err != nil {
		fail(c, model.ErrInternal("create technician failed"))
		return
	}
	ok(c, h.toTechDTO(*row))
}

// updateTechnician PUT /api/v1/admin/technicians/:techId —— 编辑（phone 缺省保留原值）
func (h *Handler) updateTechnician(c *gin.Context) {
	techID := c.Param("techId")
	var req techRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	existing, err := h.store.GetTechnician(c.Request.Context(), techID)
	if err != nil {
		fail(c, model.ErrInternal("get technician failed"))
		return
	}
	if existing == nil {
		fail(c, model.ErrNotFound("technician not found: %s", techID))
		return
	}

	name := existing.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	teamID := existing.TeamID
	if req.TeamID != nil {
		teamID = req.TeamID
	}
	if appErr := h.validateTechTeam(c, teamID); appErr != nil {
		fail(c, appErr)
		return
	}

	enc, hash := existing.PhoneEnc, existing.PhoneHash
	if req.Phone != "" {
		if !validPhone(req.Phone) {
			fail(c, model.ErrInvalidParam("invalid phone: must be 11 digits starting with 1"))
			return
		}
		newEnc, newHash, appErr := h.preparePhone(req.Phone)
		if appErr != nil {
			fail(c, appErr)
			return
		}
		taken, takenErr := h.store.TechPhoneHashTaken(c.Request.Context(), newHash, techID)
		if takenErr != nil {
			fail(c, model.ErrInternal("check phone hash failed"))
			return
		}
		if taken {
			fail(c, model.ErrConflict("phone already registered"))
			return
		}
		enc, hash = newEnc, newHash
	}

	row, err := h.store.UpdateTechnician(c.Request.Context(), techID, repo.TechInput{
		Name: name, PhoneEnc: enc, PhoneHash: hash, TeamID: teamID,
	})
	if err != nil {
		fail(c, model.ErrInternal("update technician failed"))
		return
	}
	if row == nil {
		fail(c, model.ErrNotFound("technician not found: %s", techID))
		return
	}
	ok(c, h.toTechDTO(*row))
}

type toggleRequest struct {
	Action string `json:"action"`
}

// toggleTechnician POST /api/v1/technicians/:techId/toggle —— 启用/禁用（幂等）
func (h *Handler) toggleTechnician(c *gin.Context) {
	var req toggleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	var status string
	switch req.Action {
	case "enable":
		status = "enabled"
	case "disable":
		status = "disabled"
	default:
		fail(c, model.ErrInvalidParam("invalid action: %s (enable|disable)", req.Action))
		return
	}
	exists, err := h.store.ToggleTechnician(c.Request.Context(), c.Param("techId"), status)
	if err != nil {
		fail(c, model.ErrInternal("toggle technician failed"))
		return
	}
	if !exists {
		fail(c, model.ErrNotFound("technician not found: %s", c.Param("techId")))
		return
	}
	ok(c, nil)
}

// ─────────────────────────────────────────────────────────────
// 反馈（T030 #5：replyContent 落库）
// ─────────────────────────────────────────────────────────────

func toFeedbackDTO(r repo.FeedbackRow) model.FeedbackDTO {
	replyTime := (*string)(nil)
	if r.ReplyTime != nil {
		s := r.ReplyTime.UTC().Format("2006-01-02T15:04:05Z07:00")
		replyTime = &s
	}
	return model.FeedbackDTO{
		FeedbackID:   strconv.FormatInt(r.FeedbackID, 10),
		PatientID:    r.PatientID,
		Type:         strOr(r.Type, ""),
		Content:      r.Content,
		SubmitTime:   r.SubmitTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Handler:      r.Handler,
		ReplyContent: r.ReplyContent,
		ReplyTime:    replyTime,
		Status:       r.Status,
	}
}

// feedbacks 列宽/CHECK（scripts/db/migrations/000001_init_schema.up.sql:241）
const (
	feedbackTypeMaxLen    = 32  // feedbacks.type VARCHAR(32)
	feedbackContentMaxLen = 500 // feedbacks.content VARCHAR(500)
)

// feedbackStatuses feedbacks.status CHECK 枚举（含 processFeedback 会写入的 replied）
var feedbackStatuses = map[string]bool{"pending": true, "replied": true, "resolved": true}

// createFeedback POST /api/v1/feedbacks —— 反馈创建（T311 患者端配网失败自动存档）
//
// 鉴权沿用 assertAdminOrSelf（T264 同族，不新造中间件）：患者只能为本人提交。
// 列宽/CHECK/外键在此预拦为 400 或 404，非法入参一律不落到 500。
func (h *Handler) createFeedback(c *gin.Context) {
	var req model.CreateFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	patientID := trimStr(req.PatientID)
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}
	if !assertAdminOrSelf(c, patientID) {
		return
	}
	content := trimStr(req.Content)
	if content == "" {
		fail(c, model.ErrInvalidParam("content is required"))
		return
	}
	if runeLen(content) > feedbackContentMaxLen {
		fail(c, model.ErrInvalidParam("content exceeds %d characters", feedbackContentMaxLen))
		return
	}
	feedbackType := trimStr(req.Type)
	if runeLen(feedbackType) > feedbackTypeMaxLen {
		fail(c, model.ErrInvalidParam("type exceeds %d characters", feedbackTypeMaxLen))
		return
	}
	status := "pending" // 建表 DEFAULT 'pending'，入参缺省同值
	if req.Status != nil && trimStr(*req.Status) != "" {
		status = trimStr(*req.Status)
	}
	if !feedbackStatuses[status] {
		fail(c, model.ErrInvalidParam("status must be pending / replied / resolved"))
		return
	}

	id, err := h.store.CreateFeedback(c.Request.Context(), repo.FeedbackCreateInput{
		PatientID: patientID,
		Type:      feedbackType,
		Content:   content,
		Status:    status,
	})
	if err != nil {
		if errors.Is(err, repo.ErrPatientNotFound) {
			fail(c, model.ErrNotFound("patient not found: %s", patientID))
			return
		}
		fail(c, model.ErrInternal("create feedback failed"))
		return
	}
	ok(c, model.FeedbackCreatedDTO{FeedbackID: strconv.FormatInt(id, 10)})
}

// listFeedbacks GET /api/v1/feedbacks —— keyword 过滤，提交时间倒序
func (h *Handler) listFeedbacks(c *gin.Context) {
	rows, err := h.store.ListFeedbacks(c.Request.Context(), strings.TrimSpace(c.Query("keyword")))
	if err != nil {
		fail(c, model.ErrInternal("list feedbacks failed"))
		return
	}
	list := make([]model.FeedbackDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, toFeedbackDTO(r))
	}
	ok(c, list)
}

// cstLoc 业务切日时区（架构 §3.5）。用 FixedZone 而非 LoadLocation，与 data-service
// model.CSTZone 同口径 —— 容器内可能无 tzdata。
var cstLoc = time.FixedZone("Asia/Shanghai", 8*3600)

// feedbackStats GET /api/v1/feedbacks/stats —— 统计栏三项（T248 7.1 · PRD §7D.7 统计条）
//
// 口径：仅「今日咨询」按 Asia/Shanghai 切日；「待回复」为全量 pending；
// 「平均响应」为全量已回复样本均值（设计稿标签「平均响应」无期限词，限定今日会在
// 多数时段无样本而空栏）。🔴 设计稿第四项「满意度」无数据模型字段（feedbacks 无评分列），
// 本端点不返回，已作为待裁项上报。
func (h *Handler) feedbackStats(c *gin.Context) {
	now := time.Now().In(cstLoc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, cstLoc)
	row, err := h.store.FeedbackStats(c.Request.Context(), todayStart, todayStart.AddDate(0, 0, 1))
	if err != nil {
		fail(c, model.ErrInternal("feedback stats failed"))
		return
	}
	ok(c, model.FeedbackStatsDTO{
		TodayCount:         row.TodayCount,
		PendingCount:       row.PendingCount,
		AvgResponseSeconds: row.AvgReplySec,
	})
}

type processFeedbackRequest struct {
	ReplyContent *string `json:"replyContent"`
}

// processFeedback POST /api/v1/feedbacks/:feedbackId/process —— 回复落库 + 标记处理
func (h *Handler) processFeedback(c *gin.Context) {
	feedbackID, err := strconv.ParseInt(c.Param("feedbackId"), 10, 64)
	if err != nil || feedbackID < 1 {
		fail(c, model.ErrInvalidParam("invalid feedbackId: %s", c.Param("feedbackId")))
		return
	}
	var req processFeedbackRequest
	if c.Request.ContentLength > 0 {
		if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
			fail(c, model.ErrInvalidParam("invalid request body: %v", bindErr))
			return
		}
	}
	if req.ReplyContent != nil && len(*req.ReplyContent) > 500 {
		fail(c, model.ErrInvalidParam("replyContent exceeds 500 chars"))
		return
	}
	exists, err := h.store.ProcessFeedback(c.Request.Context(), feedbackID, operatorID(c, "ops"), req.ReplyContent)
	if err != nil {
		fail(c, model.ErrInternal("process feedback failed"))
		return
	}
	if !exists {
		fail(c, model.ErrNotFound("feedback not found: %s", c.Param("feedbackId")))
		return
	}
	ok(c, nil)
}

// ─────────────────────────────────────────────────────────────
// 矫形方案 / 感受日志（T030 #6）
// ─────────────────────────────────────────────────────────────

func toPlanDTO(r repo.OrthosisPlanRow) model.OrthosisPlanDTO {
	return model.OrthosisPlanDTO{
		PlanID:    strconv.FormatInt(r.PlanID, 10),
		PatientID: r.PatientID,
		DoctorID:  r.DoctorID,
		Content:   r.Content,
		Version:   r.Version,
		CreatedAt: r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// listPlans GET /api/v1/patients/:patientId/orthosis-plans
func (h *Handler) listPlans(c *gin.Context) {
	patientID := c.Param("patientId")
	if !assertAdminOrSelf(c, patientID) { // T264：水平鉴权
		return
	}
	rows, err := h.store.ListPlans(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("list orthosis plans failed"))
		return
	}
	list := make([]model.OrthosisPlanDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, toPlanDTO(r))
	}
	ok(c, list)
}

// nextPlanVersion 版本号递增：v{主}.{次} 次位 +1；无历史/解析失败 → v1.0
func nextPlanVersion(latest string, ok bool) string {
	if !ok {
		return "v1.0"
	}
	trimmed := strings.TrimPrefix(latest, "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 2 {
		return "v1.0"
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || major < 1 || minor < 0 {
		return "v1.0"
	}
	return "v" + strconv.Itoa(major) + "." + strconv.Itoa(minor+1)
}

type savePlanRequest struct {
	Content string `json:"content"`
}

// savePlan POST /api/v1/patients/:patientId/orthosis-plans —— 医生身份（X-User-Id → doctors）+ 版本递增
func (h *Handler) savePlan(c *gin.Context) {
	patientID := c.Param("patientId")
	var req savePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		fail(c, model.ErrInvalidParam("content is required"))
		return
	}
	if len(content) > 2000 {
		fail(c, model.ErrInvalidParam("content exceeds 2000 chars"))
		return
	}

	patient, err := h.store.GetPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("get patient failed"))
		return
	}
	if patient == nil {
		fail(c, model.ErrNotFound("patient not found: %s", patientID))
		return
	}

	doctorID, found, err := h.store.DoctorIDByAdmin(c.Request.Context(), operatorID(c, ""))
	if err != nil {
		fail(c, model.ErrInternal("resolve doctor identity failed"))
		return
	}
	if !found {
		fail(c, model.ErrForbidden("doctor identity required to save orthosis plan"))
		return
	}

	latest, hasLatest, err := h.store.LatestPlanVersion(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("query latest plan version failed"))
		return
	}
	row, err := h.store.CreatePlan(c.Request.Context(), patientID, doctorID, content, nextPlanVersion(latest, hasLatest))
	if err != nil {
		fail(c, model.ErrInternal("create orthosis plan failed"))
		return
	}
	ok(c, toPlanDTO(*row))
}

func toFeelingDTO(r repo.FeelingLogRow) model.FeelingLogDTO {
	areas := r.DiscomfortAreas
	if areas == nil {
		areas = []string{}
	}
	replyTime := (*string)(nil)
	if r.ReplyTime != nil {
		s := r.ReplyTime.UTC().Format("2006-01-02T15:04:05Z07:00")
		replyTime = &s
	}
	// T256 #3：feeling 直接来自 comfort_level 列（fitted=贴合 / discomfort=不适），不再从 comfort_score 派生。
	var patientName *string
	if r.PatientName != "" {
		patientName = &r.PatientName
	}
	return model.FeelingLogDTO{
		LogID:           strconv.FormatInt(r.LogID, 10),
		PatientID:       r.PatientID,
		LogDate:         r.LogDate.Format("2006-01-02"),
		ComfortScore:    r.ComfortScore,
		Feeling:         r.ComfortLevel,
		PatientName:     patientName,
		DiscomfortAreas: areas,
		Notes:           r.Notes,
		ReplyContent:    r.ReplyContent,
		ReplyTime:       replyTime,
		// T306：提交时间取 created_at 列（NOT NULL），RFC3339 口径同 replyTime。
		CreatedAt: r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// listFeelingLogs GET /api/v1/patients/:patientId/feeling-logs
// 水平鉴权（T184）：ROLE_ADMIN 可查任意患者；其他角色仅 X-User-Id == patientId 可查。
func (h *Handler) listFeelingLogs(c *gin.Context) {
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}

	// 水平鉴权（fail-closed：缺失头视为无权限）
	role := c.GetHeader(headerRole)
	userID := c.GetHeader(headerUserID)
	if role != roleAdmin {
		if userID == "" || userID != patientID {
			fail(c, model.ErrForbidden("may only query your own feeling logs"))
			return
		}
	}

	rows, err := h.store.ListFeelingLogs(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("list feeling logs failed"))
		return
	}
	list := make([]model.FeelingLogDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, toFeelingDTO(r))
	}
	ok(c, list)
}

type replyRequest struct {
	ReplyContent string `json:"replyContent"`
}

// replyFeelingLog POST /api/v1/feeling-logs/:logId/reply —— 医生回复写入（T030 #6）
func (h *Handler) replyFeelingLog(c *gin.Context) {
	logID, err := strconv.ParseInt(c.Param("logId"), 10, 64)
	if err != nil || logID < 1 {
		fail(c, model.ErrInvalidParam("invalid logId: %s", c.Param("logId")))
		return
	}
	var req replyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	reply := strings.TrimSpace(req.ReplyContent)
	if reply == "" {
		fail(c, model.ErrInvalidParam("replyContent is required"))
		return
	}
	if len(reply) > 200 {
		fail(c, model.ErrInvalidParam("replyContent exceeds 200 chars"))
		return
	}
	exists, err := h.store.ReplyFeelingLog(c.Request.Context(), logID, reply)
	if err != nil {
		fail(c, model.ErrInternal("reply feeling log failed"))
		return
	}
	if !exists {
		fail(c, model.ErrNotFound("feeling log not found: %s", c.Param("logId")))
		return
	}
	ok(c, nil)
}

// ─────────────────────────────────────────────────────────────
// T256 #1 团队统计卡 + #2 跨患者感受日志
// ─────────────────────────────────────────────────────────────

// getTeamStats GET /api/v1/admin/teams/stats —— 团队管理 4 张统计卡（T256 #1）
func (h *Handler) getTeamStats(c *gin.Context) {
	teamCount, memberCount, managed, unassigned, err := h.store.GetTeamStats(c.Request.Context())
	if err != nil {
		fail(c, model.ErrInternal("get team stats failed"))
		return
	}
	ok(c, model.TeamStatsDTO{
		TeamCount:              teamCount,
		MemberCount:            memberCount,
		ManagedPatientCount:    managed,
		UnassignedPatientCount: unassigned,
	})
}

// listFeelingLogsAdmin GET /api/v1/admin/feeling-logs —— 跨患者感受日志流（T256 #2）
// 支持 keyword / startDate / endDate / feeling 筛选 + 分页。
func (h *Handler) listFeelingLogsAdmin(c *gin.Context) {
	page, pageSize, appErr := parsePaging(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	feeling := strings.TrimSpace(c.Query("feeling"))
	if feeling != "" && feeling != "fitted" && feeling != "discomfort" {
		fail(c, model.ErrInvalidParam("invalid feeling: %s (fitted|discomfort)", feeling))
		return
	}
	rows, total, err := h.store.ListFeelingLogsAdmin(c.Request.Context(), repo.FeelingLogAdminFilter{
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		StartDate: strings.TrimSpace(c.Query("startDate")),
		EndDate:   strings.TrimSpace(c.Query("endDate")),
		Feeling:   feeling,
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		fail(c, model.ErrInternal("list feeling logs failed"))
		return
	}
	list := make([]model.FeelingLogDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, toFeelingDTO(r))
	}
	ok(c, model.PageData{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ─────────────────────────────────────────────────────────────
// RBAC 角色与权限矩阵（T030 #7）
// ─────────────────────────────────────────────────────────────

func toRoleDTO(r repo.RoleRow) model.AdminRoleDTO {
	_, preset := presetRoles[r.RoleID]
	desc := ""
	if r.Description != nil {
		desc = *r.Description
	}
	return model.AdminRoleDTO{
		RoleID:      r.RoleID,
		Name:        r.Name,
		Description: desc,
		MemberCount: r.MemberCount,
		CreatedAt:   r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Status:      r.Status,
		Preset:      preset,
	}
}

// listRoles GET /api/v1/admin/roles —— 角色列表（含成员计数）
func (h *Handler) listRoles(c *gin.Context) {
	rows, err := h.store.ListRoles(c.Request.Context())
	if err != nil {
		fail(c, model.ErrInternal("list roles failed"))
		return
	}
	list := make([]model.AdminRoleDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, toRoleDTO(r))
	}
	ok(c, list)
}

// validScopes permissions_json.scope 白名单（对齐 seed 预置角色）
var validScopes = map[string]struct{}{
	"all":          {},
	"team":         {},
	"all_patients": {},
}

// getPermissions GET /api/v1/admin/roles/:roleId/permissions
func (h *Handler) getPermissions(c *gin.Context) {
	row, err := h.store.GetRole(c.Request.Context(), c.Param("roleId"))
	if err != nil {
		fail(c, model.ErrInternal("get role failed"))
		return
	}
	if row == nil {
		fail(c, model.ErrNotFound("role not found: %s", c.Param("roleId")))
		return
	}
	var perms model.RolePermissionsDTO
	if err := json.Unmarshal([]byte(row.PermissionsJSON), &perms); err != nil {
		fail(c, model.ErrInternal("invalid permissions_json for role %s", row.RoleID))
		return
	}
	// T257 11.5：items 缺省（老角色 / seed 预置三个）⇒ 按目录物化为「modules 下全部子权限」，
	// 前端只有一条规则：照 items 渲染勾选，不用自己判 null
	if perms.Items == nil {
		perms.Items = materializeItems(perms.Modules)
	}
	ok(c, perms)
}

// updatePermissions PUT /api/v1/admin/roles/:roleId/permissions —— 校验 scope/modules 后整体替换
func (h *Handler) updatePermissions(c *gin.Context) {
	roleID := c.Param("roleId")
	var req model.RolePermissionsDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if _, scopeOK := validScopes[req.Scope]; !scopeOK {
		fail(c, model.ErrInvalidParam("invalid scope: %s (all|team|all_patients)", req.Scope))
		return
	}
	if len(req.Modules) == 0 {
		fail(c, model.ErrInvalidParam("modules must not be empty"))
		return
	}
	// T257 11.5：items 为 nil = 不细化（读时按目录物化）；给了就必须在目录内且模块已勾
	if req.Items != nil {
		if appErr := validatePermissionItems(req.Items, req.Modules); appErr != nil {
			fail(c, appErr)
			return
		}
	}
	payload, err := json.Marshal(req)
	if err != nil {
		fail(c, model.ErrInternal("marshal permissions failed"))
		return
	}
	exists, err := h.store.UpdateRolePermissions(c.Request.Context(), roleID, string(payload))
	if err != nil {
		fail(c, model.ErrInternal("update permissions failed"))
		return
	}
	if !exists {
		fail(c, model.ErrNotFound("role not found: %s", roleID))
		return
	}
	ok(c, req)
}

// ─────────────────────────────────────────────────────────────
// 系统参数（T030 #8，PRD §7D.12，sys_configs KV 映射）
// ─────────────────────────────────────────────────────────────

// 配置键（对齐 scripts/db/seed/seed.sql sys_configs）
const (
	keyWearTarget             = "wear_target_hours"
	keyPressureHigh           = "threshold_pressure_high"
	keyFluctuationPct         = "threshold_pressure_fluctuation_pct"
	keyWearInterrupt          = "threshold_wear_interrupt_minutes"
	keySensorDrift            = "threshold_sensor_drift"
	keyWifiPresets            = "wifi_presets"
	keyCollectInterval        = "collect_interval_minutes" // 内部分钟（device/alert 服务依赖）
	keyCollectIntervalSeconds = "collect_interval_seconds" // T256 #4：API 秒口径（设计稿）
	keyRetentionDays          = "data_retention_days"      // T256 #4：数据保留天数
	keyMaxPatients            = "max_patients"             // T256 #4：最大患者数
	// T302 F1 后端半边：PRD §7D.12 已有语义、此前只有 seed 行没有读写口的三项
	keyCalibrationOffset = "threshold_calibration_offset" // 空载校准偏差上限（N），000019 已 ÷10 到 0.05
	keyWechatTemplateID  = "notify_wechat_template_id"    // 微信模板消息 ID（PRD §7D.12 通知模板配置）
	keySmsTemplateID     = "notify_sms_template_id"       // 短信模板 ID（同上）
)

// defaultCalibrationOffsetN 缺行兜底，与 seed.sql:354 / 000019 ÷10 后同值
// （🔴 T173 口径：阈值是配置参数，此处只是「库里没行时显示什么」，不是硬编码判定值）。
const defaultCalibrationOffsetN = 0.05

// 模板 ID 形状守卫：只挡控制字符与超长，不校验厂商格式
// （微信模板 ID 是 URL-safe 串、阿里云短信是 SMS_ 前缀，且各家不同 —— 写死前缀 = 换厂商就得改后端）。
// 上限 128 < sys_configs.config_value VARCHAR(255)，留余量给两侧空格被 trim 的情况。
const maxTemplateIDLen = 128

// 缺失键默认值（PRD §7D.12 + T203 ÷10，与 @bracesync/constants DEFAULT_THRESHOLDS 对齐）
var settingsDefaults = model.SystemSettingsDTO{
	DailyWearTargetHours:   22,
	PressureHighThresholdN: 5, // T203: 45 → 5
	PressureFluctuationPct: 30,
	WearInterruptMinutes:   60,
	SensorDriftN:           0.3, // T203: 2.8 → 0.3
	WifiPresets:            []model.WifiPresetDTO{},
	CollectIntervalSeconds: 1800,  // 默认 30 分钟 = 1800 秒
	RetentionDays:          365,   // 默认保留 365 天
	MaxPatients:            10000, // 默认最大 10000 患者（对齐 seed.sql）
}

func numOr(raw string, def float64) float64 {
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	return v
}

// parseWifiPresets 解析 sys_configs.wifi_presets JSON 数组；非法/空 → 空列表
func parseWifiPresets(raw string) []model.WifiPresetDTO {
	if raw == "" {
		return []model.WifiPresetDTO{}
	}
	var list []model.WifiPresetDTO
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return []model.WifiPresetDTO{}
	}
	return list
}

// maskWifiPasswords GET 出参：非空密码脱敏为 ********
func maskWifiPasswords(list []model.WifiPresetDTO) []model.WifiPresetDTO {
	out := make([]model.WifiPresetDTO, 0, len(list))
	for _, p := range list {
		if p.Password != "" {
			p.Password = "********"
		}
		out = append(out, p)
	}
	return out
}

// getSettings GET /api/v1/admin/settings —— sys_configs 映射（缺失键回默认值）
// T256 #4：collectIntervalSeconds 直接读 collect_interval_seconds（设计稿秒口径）；
// 内部分钟键 collect_interval_minutes 由写入端点同步维护，供 device/alert 服务消费。
func (h *Handler) getSettings(c *gin.Context) {
	keys := []string{keyWearTarget, keyPressureHigh, keyPressureLow, keyFluctuationPct, keyWearInterrupt, keySensorDrift, keyWifiPresets, keyCollectIntervalSeconds, keyRetentionDays, keyMaxPatients, keyCalibrationOffset, keyWechatTemplateID, keySmsTemplateID}
	kvs, err := h.store.GetConfigs(c.Request.Context(), keys)
	if err != nil {
		fail(c, model.ErrInternal("read settings failed"))
		return
	}
	pressureLow := numOr(kvs[keyPressureLow], defaultUnifiedLowerN)
	// T302：三项 GET 恒回值（缺行按默认 / 空串），指针只为让 PUT 能表达「本次不改」
	calibrationOffset := numOr(kvs[keyCalibrationOffset], defaultCalibrationOffsetN)
	wechatTemplateID := strings.TrimSpace(kvs[keyWechatTemplateID]) // 缺键 = 未配置 = 空串
	smsTemplateID := strings.TrimSpace(kvs[keySmsTemplateID])
	dto := model.SystemSettingsDTO{
		DailyWearTargetHours:   numOr(kvs[keyWearTarget], settingsDefaults.DailyWearTargetHours),
		PressureHighThresholdN: numOr(kvs[keyPressureHigh], settingsDefaults.PressureHighThresholdN),
		PressureLowThresholdN:  &pressureLow, // T257 12.4：GET 恒回数值，前端不用判缺失
		PressureFluctuationPct: numOr(kvs[keyFluctuationPct], settingsDefaults.PressureFluctuationPct),
		WearInterruptMinutes:   numOr(kvs[keyWearInterrupt], settingsDefaults.WearInterruptMinutes),
		SensorDriftN:           numOr(kvs[keySensorDrift], settingsDefaults.SensorDriftN),
		WifiPresets:            maskWifiPasswords(parseWifiPresets(kvs[keyWifiPresets])),
		CollectIntervalSeconds: int(numOr(kvs[keyCollectIntervalSeconds], float64(settingsDefaults.CollectIntervalSeconds))),
		RetentionDays:          int(numOr(kvs[keyRetentionDays], float64(settingsDefaults.RetentionDays))),
		MaxPatients:            int(numOr(kvs[keyMaxPatients], float64(settingsDefaults.MaxPatients))),
		CalibrationOffsetN:     &calibrationOffset,
		WechatTemplateID:       &wechatTemplateID,
		SmsTemplateID:          &smsTemplateID,
	}
	ok(c, dto)
}

// validateSettings 参数范围校验（对齐前端表单 min/max 与 T009 阈值联动口径）
// T256 #4：collectIntervalSeconds 需为 60 的整数倍（内部分钟存储）；retentionDays/maxPatients 正数。
//
// T257 12.4（三档合两键）：pressureLow 为**本次生效的**统一压力下限
// （请求给了就用请求值，没给则取库里现值 threshold_pressure_low，该键由告警管理页 Tab2 维护）——
// 上限必须严格大于下限，否则同一份配置在两个页面自相矛盾（压力偏高告警恒不触发）。
func validateSettings(s model.SystemSettingsDTO, collectInterval, pressureLow float64) *model.AppError {
	switch {
	case s.DailyWearTargetHours < 1 || s.DailyWearTargetHours > 24:
		return model.ErrInvalidParam("dailyWearTargetHours must be in [1,24]")
	case s.PressureHighThresholdN < 1 || s.PressureHighThresholdN > 200:
		return model.ErrInvalidParam("pressureHighThresholdN must be in [1,200]")
	case s.PressureLowThresholdN != nil && (*s.PressureLowThresholdN < 0 || *s.PressureLowThresholdN > 200):
		return model.ErrInvalidParam("pressureLowThresholdN must be in [0,200]")
	case s.PressureHighThresholdN <= pressureLow:
		return model.ErrInvalidParam("pressureHighThresholdN (%g) must be greater than threshold_pressure_low (%g)",
			s.PressureHighThresholdN, pressureLow)
	case s.PressureFluctuationPct < 1 || s.PressureFluctuationPct > 100:
		return model.ErrInvalidParam("pressureFluctuationPct must be in [1,100]")
	case s.WearInterruptMinutes < 10 || s.WearInterruptMinutes > 720:
		return model.ErrInvalidParam("wearInterruptMinutes must be in [10,720]")
	case s.WearInterruptMinutes < 2*collectInterval:
		return model.ErrInvalidParam("wearInterruptMinutes must be >= 2x collect interval (%.0f)", 2*collectInterval)
	case s.SensorDriftN < 0.1 || s.SensorDriftN > 20:
		return model.ErrInvalidParam("sensorDriftN must be in [0.1,20]")
	case s.CalibrationOffsetN != nil && (*s.CalibrationOffsetN < 0.01 || *s.CalibrationOffsetN > 20):
		// 上限与 sensorDriftN 同档（都是「偏差类」阈值，N 量纲，T203 ÷10 后量级）；
		// 下限不给 0：0 容差会让每次空载校准必判失败。区间是形状守卫，不代表量纲已定
		// （🔴 T173：现值仍是占位，待按 mN/÷1000 量级重定后随配置调整，不改代码）。
		return model.ErrInvalidParam("calibrationOffsetN must be in [0.01,20]")
	case s.CollectIntervalSeconds < 60 || s.CollectIntervalSeconds%60 != 0:
		return model.ErrInvalidParam("collectIntervalSeconds must be a positive multiple of 60 (seconds)")
	case s.RetentionDays < 1:
		return model.ErrInvalidParam("retentionDays must be >= 1")
	case s.MaxPatients < 1:
		return model.ErrInvalidParam("maxPatients must be >= 1")
	case len(s.WifiPresets) > 32:
		return model.ErrInvalidParam("wifiPresets exceeds 32 entries")
	}
	for _, p := range s.WifiPresets {
		if strings.TrimSpace(p.Ssid) == "" {
			return model.ErrInvalidParam("wifiPreset ssid must not be empty")
		}
	}
	// T302：模板 ID 只在请求给出时校验（nil = 本次不改该键，沿用库里现值）
	if s.WechatTemplateID != nil {
		if appErr := validateTemplateID("wechatTemplateId", *s.WechatTemplateID); appErr != nil {
			return appErr
		}
	}
	if s.SmsTemplateID != nil {
		if appErr := validateTemplateID("smsTemplateId", *s.SmsTemplateID); appErr != nil {
			return appErr
		}
	}
	return nil
}

// validateTemplateID 通知模板 ID 形状守卫：允许空（= 未配置，回滚到不用模板直发），
// 拒绝控制字符（CR/LF 会被拼进下游消息与查询串 ⇒ 分隔符注入）与超长（列宽 VARCHAR(255)）。
// 🔴 不校验厂商格式：微信是 URL-safe 串、短信服务商各家前缀不同，写死即把配置项变成绑定。
func validateTemplateID(field, v string) *model.AppError {
	if len(v) > maxTemplateIDLen {
		return model.ErrInvalidParam("%s exceeds %d characters", field, maxTemplateIDLen)
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] == 0x7F {
			return model.ErrInvalidParam("%s must not contain control characters", field)
		}
	}
	return nil
}

// mergeWifiPasswords PUT 入参：密码为 ******** 或空时保留同 ssid 既有密码（防脱敏值回写）
func mergeWifiPasswords(incoming []model.WifiPresetDTO, stored []model.WifiPresetDTO) []model.WifiPresetDTO {
	storedBySsid := make(map[string]string, len(stored))
	for _, p := range stored {
		storedBySsid[p.Ssid] = p.Password
	}
	out := make([]model.WifiPresetDTO, 0, len(incoming))
	for _, p := range incoming {
		if p.Password == "" || p.Password == "********" {
			p.Password = storedBySsid[p.Ssid]
		}
		out = append(out, p)
	}
	return out
}

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// updateSettings PUT /api/v1/admin/settings —— 校验 + UPSERT sys_configs
// T256 #4：collectIntervalSeconds（秒）换算为 collect_interval_minutes（分钟）写入，兼容 device/alert 服务；
// retentionDays / maxPatients 写入新键。
// T257 12.4：pressureLowThresholdN 与告警页共用 threshold_pressure_low（三档合两键，不加第三键）；
// 上下限做联动校验（上限必须严格大于下限）。
func (h *Handler) updateSettings(c *gin.Context) {
	var req model.SystemSettingsDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}

	// 采集间隔（中断阈值联动校验用）：用请求体的秒值换算分钟
	intervalMinutes := float64(req.CollectIntervalSeconds) / 60.0

	// T257 12.4：统一压力下限与告警管理页 Tab2 同键（threshold_pressure_low）；
	// 请求未给（nil）= 不改该键，校验时沿用库里现值
	// T302：同一条规则扩展到空载校准上限与两个模板 ID（三项都是后加的，老前端不带字段）
	currentKVs, err := h.store.GetConfigs(c.Request.Context(), []string{
		keyWifiPresets, keyPressureLow, keyCalibrationOffset, keyWechatTemplateID, keySmsTemplateID,
	})
	if err != nil {
		fail(c, model.ErrInternal("read existing settings failed"))
		return
	}
	pressureLow := numOr(currentKVs[keyPressureLow], defaultUnifiedLowerN)
	if req.PressureLowThresholdN != nil {
		pressureLow = *req.PressureLowThresholdN
	}
	// 模板 ID 入库前 trim（首尾空白在厂商后台复制时常见，留着会让下次 GET 与本次校验口径不一致）
	wechatTemplateID := strings.TrimSpace(currentKVs[keyWechatTemplateID])
	if req.WechatTemplateID != nil {
		trimmed := strings.TrimSpace(*req.WechatTemplateID)
		req.WechatTemplateID = &trimmed
		wechatTemplateID = trimmed
	}
	smsTemplateID := strings.TrimSpace(currentKVs[keySmsTemplateID])
	if req.SmsTemplateID != nil {
		trimmed := strings.TrimSpace(*req.SmsTemplateID)
		req.SmsTemplateID = &trimmed
		smsTemplateID = trimmed
	}
	calibrationOffset := numOr(currentKVs[keyCalibrationOffset], defaultCalibrationOffsetN)
	if req.CalibrationOffsetN != nil {
		calibrationOffset = *req.CalibrationOffsetN
	}

	if appErr := validateSettings(req, intervalMinutes, pressureLow); appErr != nil {
		fail(c, appErr)
		return
	}

	storedPresets := parseWifiPresets(currentKVs[keyWifiPresets])
	merged := mergeWifiPasswords(req.WifiPresets, storedPresets)
	wifiJSON, err := json.Marshal(merged)
	if err != nil {
		fail(c, model.ErrInternal("marshal wifi presets failed"))
		return
	}

	kvs := []repo.ConfigKV{
		{Key: keyWearTarget, Value: fmtNum(req.DailyWearTargetHours)},
		{Key: keyPressureHigh, Value: fmtNum(req.PressureHighThresholdN)},
		{Key: keyFluctuationPct, Value: fmtNum(req.PressureFluctuationPct)},
		{Key: keyWearInterrupt, Value: fmtNum(req.WearInterruptMinutes)},
		{Key: keySensorDrift, Value: fmtNum(req.SensorDriftN)},
		{Key: keyWifiPresets, Value: string(wifiJSON)},
		{Key: keyCollectIntervalSeconds, Value: strconv.Itoa(req.CollectIntervalSeconds)}, // API 秒口径
		{Key: keyCollectInterval, Value: fmtNum(intervalMinutes)},                         // 内部分钟（device/alert）
		{Key: keyRetentionDays, Value: strconv.Itoa(req.RetentionDays)},
		{Key: keyMaxPatients, Value: strconv.Itoa(req.MaxPatients)},
	}
	// 下限只在请求给出时写：nil = 保持现值（老前端不带该字段，一次保存不该把下限抹掉）
	if req.PressureLowThresholdN != nil {
		kvs = append(kvs, repo.ConfigKV{Key: keyPressureLow, Value: fmtNum(pressureLow)})
	}
	// T302 三项同规则：给出才写
	if req.CalibrationOffsetN != nil {
		kvs = append(kvs, repo.ConfigKV{Key: keyCalibrationOffset, Value: fmtNum(calibrationOffset)})
	}
	if req.WechatTemplateID != nil {
		kvs = append(kvs, repo.ConfigKV{Key: keyWechatTemplateID, Value: wechatTemplateID})
	}
	if req.SmsTemplateID != nil {
		kvs = append(kvs, repo.ConfigKV{Key: keySmsTemplateID, Value: smsTemplateID})
	}
	if err := h.store.UpsertConfigs(c.Request.Context(), kvs, operatorID(c, "ops")); err != nil {
		fail(c, model.ErrInternal("save settings failed"))
		return
	}
	req.WifiPresets = maskWifiPasswords(merged)
	req.PressureLowThresholdN = &pressureLow    // 响应与 GET 同形（恒回数值）
	req.CalibrationOffsetN = &calibrationOffset // T302 同上
	req.WechatTemplateID = &wechatTemplateID    // T302 同上
	req.SmsTemplateID = &smsTemplateID          // T302 同上
	ok(c, req)
}

// ─────────────────────────────────────────────────────────────
// 患者写操作（T057：创建患者 / 分配团队 / 批量绑定）
//
// 契约：docs/tasks/ella/T057-患者管理测试规格.md
// 手机号唯一键：preparePhone 生成 PhoneEnc(AES-GCM) + PhoneHash(SHA-256)，
// store 按 PhoneHash 查重命中返回 ErrPatientExists → handler 映射 409。
// ─────────────────────────────────────────────────────────────

// createPatient POST /api/v1/admin/patients —— 创建患者（手机号必填，phone_hash 查重）
func (h *Handler) createPatient(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can create patient"))
		return
	}
	var req model.CreatePatientRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		fail(c, model.ErrInvalidParam("name is required"))
		return
	}
	if req.Phone == "" {
		fail(c, model.ErrInvalidParam("phone is required"))
		return
	}
	if !validPhone(req.Phone) {
		fail(c, model.ErrInvalidParam("invalid phone format: must be 11 digits starting with 1"))
		return
	}
	if req.Gender != nil && *req.Gender != "male" && *req.Gender != "female" {
		fail(c, model.ErrInvalidParam("invalid gender: male|female"))
		return
	}
	if req.Age != nil && (*req.Age < 0 || *req.Age > 150) {
		fail(c, model.ErrInvalidParam("invalid age range: [0,150]"))
		return
	}
	if req.CobbAngle != nil && (*req.CobbAngle < 0 || *req.CobbAngle > 180) {
		fail(c, model.ErrInvalidParam("invalid cobbAngle range: [0,180]"))
		return
	}
	enc, hash, appErr := h.preparePhone(req.Phone)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	row, err := h.store.CreatePatient(c.Request.Context(), repo.PatientInput{
		Name:      name,
		PhoneEnc:  &enc,
		PhoneHash: &hash,
		Gender:    req.Gender,
		Age:       req.Age,
		Diagnosis: req.Diagnosis,
		CobbAngle: req.CobbAngle,
		TeamID:    req.TeamID,
		DoctorID:  req.DoctorID,
	})
	if err != nil {
		if errors.Is(err, repo.ErrPatientExists) {
			fail(c, model.ErrConflict("patient already exists"))
			return
		}
		fail(c, model.ErrInternal("create patient failed"))
		return
	}
	// 用 preparePhone 生成的 enc 脱敏（store 返回行可能未回填 PhoneEnc）
	dto := toPatientDTO(*row)
	if h.phone != nil {
		dto.Phone = h.phone.Masked(enc)
	}
	ok(c, dto)
}

// assignPatientTeam PUT /api/v1/admin/patients/:patientId/team —— 分配/更改团队（幂等）
func (h *Handler) assignPatientTeam(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can assign patient team"))
		return
	}
	patientID := c.Param("patientId")
	var req model.AssignTeamRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if req.TeamID == "" {
		fail(c, model.ErrInvalidParam("teamId is required"))
		return
	}
	row, err := h.store.AssignPatientTeam(c.Request.Context(), patientID, req.TeamID)
	if err != nil {
		if errors.Is(err, repo.ErrPatientNotFound) {
			fail(c, model.ErrNotFound("patient not found: %s", patientID))
			return
		}
		fail(c, model.ErrInternal("assign team failed"))
		return
	}
	dto := toPatientDTO(*row)
	if h.phone != nil {
		dto.Phone = h.phone.Masked(row.PhoneEnc)
	}
	ok(c, dto)
}

// batchBindPatients POST /api/v1/admin/patients/batch-bind —— 批量绑定（部分失败不回滚，HTTP 仍 200）
func (h *Handler) batchBindPatients(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can batch bind patients"))
		return
	}
	var req model.BatchBindRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if len(req.PatientIDs) == 0 {
		fail(c, model.ErrInvalidParam("patientIds must not be empty"))
		return
	}
	if req.TeamID == "" {
		fail(c, model.ErrInvalidParam("teamId is required"))
		return
	}
	result, err := h.store.BatchBindPatients(c.Request.Context(), req.PatientIDs, req.TeamID)
	if err != nil {
		fail(c, model.ErrInternal("batch bind failed"))
		return
	}
	failures := make([]model.BatchBindFailureDTO, 0, len(result.Failed))
	for _, f := range result.Failed {
		failures = append(failures, model.BatchBindFailureDTO{
			PatientID: f.PatientID,
			Reason:    f.Reason,
		})
	}
	ok(c, model.BatchBindResultDTO{
		SuccessCount: len(result.Success),
		FailedCount:  len(result.Failed),
		Failures:     failures,
	})
}

// ─────────────────────────────────────────────────────────────
// T059 团队 / 成员写操作
//
// 契约：docs/tasks/ella/T059-团队管理测试规格.md
// 错误映射：ErrTeamNameExists→409、ErrTeamNotFound→404、ErrLeaderNotFound→400、
//           ErrTeamInUse{计数}→409、ErrMemberNotFound→404、ErrMemberInTeam→409
// ─────────────────────────────────────────────────────────────

// validMemberType 校验成员类型枚举（doctor|technician）
func validMemberType(t string) bool { return t == "doctor" || t == "technician" }

// toTeamDetailDTO 将 TeamDetailRow 转为 TeamDetailDTO
func toTeamDetailDTO(r repo.TeamDetailRow) model.TeamDetailDTO {
	return model.TeamDetailDTO{
		TeamID:       r.TeamID,
		Name:         r.Name,
		Leader:       r.Leader,
		LeaderName:   r.LeaderName,
		MemberCount:  r.MemberCount,
		PatientCount: r.PatientCount,
		Description:  r.Description,
		Status:       r.Status,
		CreatedAt:    r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// toTeamMemberDTO 将 TeamMemberRow 转为 TeamMemberDTO（phone 脱敏由 handler 补充）
func toTeamMemberDTO(r repo.TeamMemberRow) model.TeamMemberDTO {
	return model.TeamMemberDTO{
		MemberID:     r.MemberID,
		MemberType:   r.MemberType,
		Name:         r.Name,
		Role:         r.Role,
		Title:        r.Title,
		PhoneMasked:  r.PhoneMasked,
		PatientCount: r.PatientCount,
		JoinTime:     r.JoinTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Status:       r.Status,
	}
}

// createTeam POST /api/v1/teams —— 创建团队（name 唯一 + leader 校验）
func (h *Handler) createTeam(c *gin.Context) {
	var req model.CreateTeamRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		fail(c, model.ErrInvalidParam("name is required"))
		return
	}
	if len([]rune(name)) > 50 {
		fail(c, model.ErrInvalidParam("name exceeds 50 chars"))
		return
	}
	if req.Leader == "" {
		fail(c, model.ErrInvalidParam("leader is required"))
		return
	}
	if len([]rune(req.Description)) > 200 {
		fail(c, model.ErrInvalidParam("description exceeds 200 chars"))
		return
	}
	row, err := h.store.CreateTeam(c.Request.Context(), repo.TeamInput{
		Name:        name,
		Leader:      req.Leader,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, repo.ErrTeamNameExists) {
			fail(c, model.ErrConflict("team name already exists"))
			return
		}
		if errors.Is(err, repo.ErrLeaderNotFound) {
			fail(c, model.ErrInvalidParam("leader not found"))
			return
		}
		fail(c, model.ErrInternal("create team failed"))
		return
	}
	ok(c, toTeamDetailDTO(*row))
}

// updateTeam PUT /api/v1/teams/:teamId —— 编辑团队（团队存在 + name 查重排除自身）
func (h *Handler) updateTeam(c *gin.Context) {
	teamID := c.Param("teamId")
	var req model.UpdateTeamRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		fail(c, model.ErrInvalidParam("name is required"))
		return
	}
	if len([]rune(name)) > 50 {
		fail(c, model.ErrInvalidParam("name exceeds 50 chars"))
		return
	}
	if req.Leader == "" {
		fail(c, model.ErrInvalidParam("leader is required"))
		return
	}
	if len([]rune(req.Description)) > 200 {
		fail(c, model.ErrInvalidParam("description exceeds 200 chars"))
		return
	}
	row, err := h.store.UpdateTeam(c.Request.Context(), teamID, repo.TeamInput{
		Name:        name,
		Leader:      req.Leader,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, repo.ErrTeamNotFound) {
			fail(c, model.ErrNotFound("team not found: %s", teamID))
			return
		}
		if errors.Is(err, repo.ErrTeamNameExists) {
			fail(c, model.ErrConflict("team name already exists"))
			return
		}
		if errors.Is(err, repo.ErrLeaderNotFound) {
			fail(c, model.ErrInvalidParam("leader not found"))
			return
		}
		fail(c, model.ErrInternal("update team failed"))
		return
	}
	ok(c, toTeamDetailDTO(*row))
}

// deleteTeam DELETE /api/v1/teams/:teamId —— 删除团队（被引用 409 带计数）
func (h *Handler) deleteTeam(c *gin.Context) {
	teamID := c.Param("teamId")
	err := h.store.DeleteTeam(c.Request.Context(), teamID)
	if err != nil {
		if errors.Is(err, repo.ErrTeamNotFound) {
			fail(c, model.ErrNotFound("team not found: %s", teamID))
			return
		}
		var inUse *repo.ErrTeamInUse
		if errors.As(err, &inUse) {
			fail(c, model.ErrConflict("%s", inUse.Error()))
			return
		}
		fail(c, model.ErrInternal("delete team failed"))
		return
	}
	ok(c, nil)
}

// addTeamMember POST /api/v1/teams/:teamId/members —— 添加成员
func (h *Handler) addTeamMember(c *gin.Context) {
	teamID := c.Param("teamId")
	var req model.AddMemberRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if !validMemberType(req.MemberType) {
		fail(c, model.ErrInvalidParam("invalid memberType: doctor|technician"))
		return
	}
	if req.MemberID == "" {
		fail(c, model.ErrInvalidParam("memberId is required"))
		return
	}
	row, err := h.store.AddTeamMember(c.Request.Context(), teamID, repo.MemberInput{
		MemberType: req.MemberType,
		MemberID:   req.MemberID,
		Role:       req.Role,
	})
	if err != nil {
		if errors.Is(err, repo.ErrTeamNotFound) {
			fail(c, model.ErrNotFound("team not found: %s", teamID))
			return
		}
		if errors.Is(err, repo.ErrMemberNotFound) {
			fail(c, model.ErrNotFound("member not found: %s", req.MemberID))
			return
		}
		if errors.Is(err, repo.ErrMemberInTeam) {
			fail(c, model.ErrConflict("member already in team"))
			return
		}
		fail(c, model.ErrInternal("add member failed"))
		return
	}
	dto := toTeamMemberDTO(*row)
	if h.phone != nil && row.PhoneEnc != nil {
		dto.PhoneMasked = h.phone.Masked(row.PhoneEnc)
	}
	ok(c, dto)
}

// updateTeamMember PUT /api/v1/teams/:teamId/members/:memberId —— 编辑成员
func (h *Handler) updateTeamMember(c *gin.Context) {
	teamID := c.Param("teamId")
	memberID := c.Param("memberId")
	var req model.UpdateMemberRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if !validMemberType(req.MemberType) {
		fail(c, model.ErrInvalidParam("invalid memberType: doctor|technician"))
		return
	}
	row, err := h.store.UpdateTeamMember(c.Request.Context(), teamID, memberID, repo.MemberInput{
		MemberType: req.MemberType,
		MemberID:   memberID,
		Role:       req.Role,
	})
	if err != nil {
		if errors.Is(err, repo.ErrTeamNotFound) {
			fail(c, model.ErrNotFound("team not found: %s", teamID))
			return
		}
		if errors.Is(err, repo.ErrMemberNotFound) {
			fail(c, model.ErrNotFound("member not found: %s", memberID))
			return
		}
		fail(c, model.ErrInternal("update member failed"))
		return
	}
	dto := toTeamMemberDTO(*row)
	if h.phone != nil && row.PhoneEnc != nil {
		dto.PhoneMasked = h.phone.Masked(row.PhoneEnc)
	}
	ok(c, dto)
}

// removeTeamMember DELETE /api/v1/teams/:teamId/members/:memberId?memberType=doctor —— 移除成员（幂等）
func (h *Handler) removeTeamMember(c *gin.Context) {
	teamID := c.Param("teamId")
	memberID := c.Param("memberId")
	memberType := c.Query("memberType")
	if memberType == "" {
		fail(c, model.ErrInvalidParam("memberType is required"))
		return
	}
	if !validMemberType(memberType) {
		fail(c, model.ErrInvalidParam("invalid memberType: doctor|technician"))
		return
	}
	err := h.store.RemoveTeamMember(c.Request.Context(), teamID, memberID, memberType)
	if err != nil {
		if errors.Is(err, repo.ErrTeamNotFound) {
			fail(c, model.ErrNotFound("team not found: %s", teamID))
			return
		}
		fail(c, model.ErrInternal("remove member failed"))
		return
	}
	ok(c, nil)
}
