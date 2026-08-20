// Package handler user-service HTTP 接入层（Gin）—— T030 admin 域端点
//
// 路由（对齐 docs/ T030 段）：
//
//	POST /api/v1/auth/login                              运营后台登录（签发 HS256 JWT）
//	GET  /api/v1/admin/patients                          管理端患者分页（团队/医生姓名 join）
//	GET  /api/v1/admin/patients/:patientId               患者详情（管理端）
//	GET  /api/v1/teams                                   团队概要
//	GET  /api/v1/teams/:teamId/members                   团队成员明细（医生+技师）
//	GET  /api/v1/doctors                                 医生列表（含患者计数）
//	GET  /api/v1/technicians                             技师分页列表
//	POST /api/v1/admin/technicians                       技师新建
//	PUT  /api/v1/admin/technicians/:techId               技师编辑
//	POST /api/v1/technicians/:techId/toggle              技师启用/禁用
//	GET  /api/v1/feedbacks                               反馈列表
//	POST /api/v1/feedbacks/:feedbackId/process           反馈处理（replyContent 落库）
//	GET  /api/v1/patients/:patientId/orthosis-plans      矫形方案历史
//	POST /api/v1/patients/:patientId/orthosis-plans      保存新方案（版本递增）
//	GET  /api/v1/patients/:patientId/feeling-logs        佩戴感受日志
//	POST /api/v1/feeling-logs/:logId/reply               医生回复感受日志
//	GET  /api/v1/admin/roles                             RBAC 角色列表
//	GET  /api/v1/admin/roles/:roleId/permissions         权限矩阵读
//	PUT  /api/v1/admin/roles/:roleId/permissions         权限矩阵写
//	GET  /api/v1/admin/settings                          系统参数读
//	PUT  /api/v1/admin/settings                          系统参数写
//	GET  /healthz                                        存活探针
//
// 统一响应体（架构 §3.5）：{ "code": 0, "message": "success", "data": {...} }
// 鉴权归 gateway（JWT + RBAC，/api/v1 路由组挂载点 Phase 1 落地）；
// 操作人取网关注入的 X-User-Id（架构 §5.2 内部信任链）。
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
)

// headerUserID gateway 鉴权通过后注入的操作人身份头（架构 §5.2）
const headerUserID = "X-User-Id"

// 预置角色（PRD §7D.11，权限系统锁定标识）
var presetRoles = map[string]struct{}{
	"ROLE_ADMIN":  {},
	"ROLE_DOCTOR": {},
	"ROLE_CS":     {},
}

// Handler HTTP 处理器（signer/phoneCipher 允许为 nil：对应登录/技师写入返回 500 配置错误）
type Handler struct {
	store  repo.Store
	signer *token.Signer
	phone  *phone.Cipher
}

// New 创建 Handler
func New(store repo.Store, signer *token.Signer, phoneCipher *phone.Cipher) *Handler {
	return &Handler{store: store, signer: signer, phone: phoneCipher}
}

// Router 组装路由（可测试）
func (h *Handler) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/v1")
	{
		v1.POST("/auth/login", h.login)
		v1.POST("/tech/login", h.techLogin)       // T037 技师登录（免 JWT）
		v1.POST("/patient/login", h.patientLogin) // T037 患者登录（免 JWT）

		v1.GET("/admin/patients", h.listPatients)
		v1.GET("/admin/patients/:patientId", h.getPatient)

		v1.GET("/teams", h.listTeams)
		v1.GET("/teams/:teamId/members", h.getTeamMembers)
		v1.GET("/doctors", h.listDoctors)

		v1.GET("/technicians", h.listTechnicians)
		v1.POST("/admin/technicians", h.createTechnician)
		v1.PUT("/admin/technicians/:techId", h.updateTechnician)
		v1.POST("/technicians/:techId/toggle", h.toggleTechnician)

		v1.GET("/feedbacks", h.listFeedbacks)
		v1.POST("/feedbacks/:feedbackId/process", h.processFeedback)

		v1.GET("/patients/:patientId/orthosis-plans", h.listPlans)
		v1.POST("/patients/:patientId/orthosis-plans", h.savePlan)
		v1.GET("/patients/:patientId/feeling-logs", h.listFeelingLogs)
		v1.POST("/feeling-logs/:logId/reply", h.replyFeelingLog)

		v1.GET("/admin/roles", h.listRoles)
		v1.GET("/admin/roles/:roleId/permissions", h.getPermissions)
		v1.PUT("/admin/roles/:roleId/permissions", h.updatePermissions)

		v1.GET("/admin/settings", h.getSettings)
		v1.PUT("/admin/settings", h.updateSettings)
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

// operatorID 操作人：网关注入的 X-User-Id，缺省 fallback（一期 gateway JWT 未上线）
func operatorID(c *gin.Context, fallback string) string {
	if v := c.GetHeader(headerUserID); v != "" {
		return v
	}
	return fallback
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

// ─────────────────────────────────────────────────────────────
// 团队 / 医生（T030 #10）
// ─────────────────────────────────────────────────────────────

// listTeams GET /api/v1/teams —— 团队概要
func (h *Handler) listTeams(c *gin.Context) {
	rows, err := h.store.ListTeams(c.Request.Context())
	if err != nil {
		fail(c, model.ErrInternal("list teams failed"))
		return
	}
	list := make([]model.TeamDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, model.TeamDTO{TeamID: r.TeamID, Name: r.Name, MemberCount: r.MemberCount, PatientCount: r.PatientCount})
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
	}
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
	rows, err := h.store.ListPlans(c.Request.Context(), c.Param("patientId"))
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
	return model.FeelingLogDTO{
		LogID:           strconv.FormatInt(r.LogID, 10),
		PatientID:       r.PatientID,
		LogDate:         r.LogDate.Format("2006-01-02"),
		ComfortScore:    r.ComfortScore,
		DiscomfortAreas: areas,
		Notes:           r.Notes,
		ReplyContent:    r.ReplyContent,
		ReplyTime:       replyTime,
	}
}

// listFeelingLogs GET /api/v1/patients/:patientId/feeling-logs
func (h *Handler) listFeelingLogs(c *gin.Context) {
	rows, err := h.store.ListFeelingLogs(c.Request.Context(), c.Param("patientId"))
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
	keyWearTarget      = "wear_target_hours"
	keyPressureHigh    = "threshold_pressure_high"
	keyFluctuationPct  = "threshold_pressure_fluctuation_pct"
	keyWearInterrupt   = "threshold_wear_interrupt_minutes"
	keySensorDrift     = "threshold_sensor_drift"
	keyWifiPresets     = "wifi_presets"
	keyCollectInterval = "collect_interval_minutes"
)

// 缺失键默认值（PRD §7D.12，与 @bracesync/constants DEFAULT_THRESHOLDS 对齐）
var settingsDefaults = model.SystemSettingsDTO{
	DailyWearTargetHours:   22,
	PressureHighThresholdN: 45,
	PressureFluctuationPct: 30,
	WearInterruptMinutes:   60,
	SensorDriftN:           2.8,
	WifiPresets:            []model.WifiPresetDTO{},
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
func (h *Handler) getSettings(c *gin.Context) {
	keys := []string{keyWearTarget, keyPressureHigh, keyFluctuationPct, keyWearInterrupt, keySensorDrift, keyWifiPresets}
	kvs, err := h.store.GetConfigs(c.Request.Context(), keys)
	if err != nil {
		fail(c, model.ErrInternal("read settings failed"))
		return
	}
	dto := model.SystemSettingsDTO{
		DailyWearTargetHours:   numOr(kvs[keyWearTarget], settingsDefaults.DailyWearTargetHours),
		PressureHighThresholdN: numOr(kvs[keyPressureHigh], settingsDefaults.PressureHighThresholdN),
		PressureFluctuationPct: numOr(kvs[keyFluctuationPct], settingsDefaults.PressureFluctuationPct),
		WearInterruptMinutes:   numOr(kvs[keyWearInterrupt], settingsDefaults.WearInterruptMinutes),
		SensorDriftN:           numOr(kvs[keySensorDrift], settingsDefaults.SensorDriftN),
		WifiPresets:            maskWifiPasswords(parseWifiPresets(kvs[keyWifiPresets])),
	}
	ok(c, dto)
}

// validateSettings 参数范围校验（对齐前端表单 min/max 与 T009 阈值联动口径）
func validateSettings(s model.SystemSettingsDTO, collectInterval float64) *model.AppError {
	switch {
	case s.DailyWearTargetHours < 1 || s.DailyWearTargetHours > 24:
		return model.ErrInvalidParam("dailyWearTargetHours must be in [1,24]")
	case s.PressureHighThresholdN < 1 || s.PressureHighThresholdN > 200:
		return model.ErrInvalidParam("pressureHighThresholdN must be in [1,200]")
	case s.PressureFluctuationPct < 1 || s.PressureFluctuationPct > 100:
		return model.ErrInvalidParam("pressureFluctuationPct must be in [1,100]")
	case s.WearInterruptMinutes < 10 || s.WearInterruptMinutes > 720:
		return model.ErrInvalidParam("wearInterruptMinutes must be in [10,720]")
	case s.WearInterruptMinutes < 2*collectInterval:
		return model.ErrInvalidParam("wearInterruptMinutes must be >= 2x collect interval (%.0f)", 2*collectInterval)
	case s.SensorDriftN < 0.1 || s.SensorDriftN > 20:
		return model.ErrInvalidParam("sensorDriftN must be in [0.1,20]")
	case len(s.WifiPresets) > 32:
		return model.ErrInvalidParam("wifiPresets exceeds 32 entries")
	}
	for _, p := range s.WifiPresets {
		if strings.TrimSpace(p.Ssid) == "" {
			return model.ErrInvalidParam("wifiPreset ssid must not be empty")
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
func (h *Handler) updateSettings(c *gin.Context) {
	var req model.SystemSettingsDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}

	// 采集间隔（中断阈值联动校验用；缺失回默认 30）
	intervalKVs, err := h.store.GetConfigs(c.Request.Context(), []string{keyCollectInterval})
	if err != nil {
		fail(c, model.ErrInternal("read collect interval failed"))
		return
	}
	interval := numOr(intervalKVs[keyCollectInterval], 30)

	if appErr := validateSettings(req, interval); appErr != nil {
		fail(c, appErr)
		return
	}

	storedPresets := []model.WifiPresetDTO{}
	presetKVs, err := h.store.GetConfigs(c.Request.Context(), []string{keyWifiPresets})
	if err == nil {
		storedPresets = parseWifiPresets(presetKVs[keyWifiPresets])
	}
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
	}
	if err := h.store.UpsertConfigs(c.Request.Context(), kvs, operatorID(c, "ops")); err != nil {
		fail(c, model.ErrInternal("save settings failed"))
		return
	}
	req.WifiPresets = maskWifiPasswords(merged)
	ok(c, req)
}
