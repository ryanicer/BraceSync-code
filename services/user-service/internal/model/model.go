// Package model user-service 领域错误码与 API DTO 定义
//
// 对齐：docs/ §3.5（错误码分段：1xxxx 用户域 / 9xxxx 系统级）
//
//	docs/ admin 域端点）
//	shared-types（AdminPatient / Technician / Feedback / OrthosisPlan / FeelingLog / AdminRole / SystemSettings）
package model

import (
	"fmt"
)

// ─────────────────────────────────────────────────────────────
// 错误码（架构 §3.5：1xxxx 用户/权限域；9xxxx 系统级）
// ─────────────────────────────────────────────────────────────

const (
	CodeOK           = 0
	CodeInvalidParam = 10400 // 参数非法（分页/枚举/范围校验）
	CodeUnauthorized = 10401 // 登录凭据错误 / 账号禁用
	CodeForbidden    = 10403 // 无权限（如非医生保存矫形方案）
	CodeNotFound     = 10404 // 用户域资源不存在（patient/technician/feedback/role…）
	CodeConflict     = 10409 // 状态冲突（手机号重复等）
	CodeWXUnavail    = 10502 // 微信 jscode2session 下游不可用（HTTP 502 语义）
	CodeInvalidPhone = 10604 // 微信 phonenumber.getPhoneNumber 业务错误（code 非法/已用）
	CodeInternal     = 90001 // 系统内部错误（DB/加密配置等）

	// T085 患者微信登录绑定域错误码（设计源 T088-V2 §5）
	CodeInvalidCredentials = 10001 // 凭据无效（status!=active 统一文案防枚举）
	CodePatientNotBound    = 10601 // openid 未绑定（需走 bind-phone 流程）
	CodePatientNotFound    = 10602 // phone_hash 无匹配或 status!=active（同码防枚举）
	CodePhoneAlreadyBound  = 10603 // 手机号档案已绑定其他微信 openid
	CodeInvalidPhoneToken  = 10605 // phoneToken 校验失败（签名/用途/openid/过期）
	CodeForbiddenScope     = 40301 // scope 越权（如 full JWT 调用 bind-phone）
)

// AppError 业务错误：携带统一响应 code 与建议 HTTP 状态
type AppError struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *AppError) Error() string { return fmt.Sprintf("code=%d: %s", e.Code, e.Message) }

func newAppError(code, httpStatus int, format string, args ...any) *AppError {
	return &AppError{Code: code, HTTPStatus: httpStatus, Message: fmt.Sprintf(format, args...)}
}

func ErrInvalidParam(format string, args ...any) *AppError {
	return newAppError(CodeInvalidParam, 400, format, args...)
}

func ErrUnauthorized(format string, args ...any) *AppError {
	return newAppError(CodeUnauthorized, 401, format, args...)
}

func ErrForbidden(format string, args ...any) *AppError {
	return newAppError(CodeForbidden, 403, format, args...)
}

func ErrNotFound(format string, args ...any) *AppError {
	return newAppError(CodeNotFound, 404, format, args...)
}

func ErrConflict(format string, args ...any) *AppError {
	return newAppError(CodeConflict, 409, format, args...)
}

func ErrInternal(format string, args ...any) *AppError {
	return newAppError(CodeInternal, 500, format, args...)
}

// NewWXServiceUnavailable 微信服务端不可用（jscode2session 网络/HTTP 错误）
func NewWXServiceUnavailable(format string, args ...any) *AppError {
	return newAppError(CodeWXUnavail, 502, format, args...)
}

// T085 患者微信登录绑定域错误构造器

// ErrInvalidCredentials 凭据无效（wx-login: status!=active 统一文案防枚举；HTTP 401）
func ErrInvalidCredentials(format string, args ...any) *AppError {
	return newAppError(CodeInvalidCredentials, 401, format, args...)
}

// ErrPatientNotBound openid 未绑定（wx-login: 返回 bindToken 引导绑定；HTTP 200）
func ErrPatientNotBound(format string, args ...any) *AppError {
	return newAppError(CodePatientNotBound, 200, format, args...)
}

// ErrPatientNotFound phone_hash 无匹配或档案非 active（bind-phone；与无匹配同码防枚举；HTTP 200）
func ErrPatientNotFound(format string, args ...any) *AppError {
	return newAppError(CodePatientNotFound, 200, format, args...)
}

// ErrPhoneAlreadyBound 手机号档案已绑定其他微信 openid（bind-phone；HTTP 200）
func ErrPhoneAlreadyBound(format string, args ...any) *AppError {
	return newAppError(CodePhoneAlreadyBound, 200, format, args...)
}

// ErrInvalidPhoneCode 微信 phonenumber.getPhoneNumber 业务错误（code 非法/已用；HTTP 200）
func ErrInvalidPhoneCode(format string, args ...any) *AppError {
	return newAppError(CodeInvalidPhone, 200, format, args...)
}

// ErrInvalidPhoneToken phoneToken 校验失败（签名/用途/openid/过期；HTTP 200）
func ErrInvalidPhoneToken(format string, args ...any) *AppError {
	return newAppError(CodeInvalidPhoneToken, 200, format, args...)
}

// ErrForbiddenScope scope 越权（如 full JWT 调用 bind-phone；HTTP 403）
func ErrForbiddenScope(format string, args ...any) *AppError {
	return newAppError(CodeForbiddenScope, 403, format, args...)
}

// ─────────────────────────────────────────────────────────────
// 分页（架构 §3.5：page 1 起，pageSize 默认 20 上限 100）
// ─────────────────────────────────────────────────────────────

// DefaultPageSize 分页默认值
const DefaultPageSize = 20

// MaxPageSize 分页上限（防大页扫描）
const MaxPageSize = 100

// PageData PaginatedResponse<T>（契约统一分页响应 data 字段）
type PageData struct {
	List     any   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// ─────────────────────────────────────────────────────────────
// API DTO（camelCase，对齐 shared-types；时间 RFC3339，可空字段 null）
// ─────────────────────────────────────────────────────────────

// AdminPatientDTO 管理端患者视图（Patient + teamName/doctorName join，契约 getPatients）
type AdminPatientDTO struct {
	PatientID  string   `json:"patientId"`
	Name       string   `json:"name"`
	Gender     *string  `json:"gender"`
	Age        *int     `json:"age"`
	Diagnosis  *string  `json:"diagnosis"`
	CobbAngle  *float64 `json:"cobbAngle"`
	DeviceID   *string  `json:"deviceId"`
	TeamID     *string  `json:"teamId"`
	DoctorID   *string  `json:"doctorId"`
	Phone      string   `json:"phone"` // 脱敏手机号（138****8000），由 handler.Masked(PhoneEnc) 生成
	Status     string   `json:"status"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
	TeamName   *string  `json:"teamName"`
	DoctorName *string  `json:"doctorName"`
}

// TeamDTO 团队概要（契约 getTeams，对齐 shared-types Team）
type TeamDTO struct {
	TeamID       string `json:"teamId"`
	Name         string `json:"name"`
	MemberCount  int    `json:"memberCount"`
	PatientCount int    `json:"patientCount"`
}

// DoctorDTO 医生（契约 getDoctors，对齐 shared-types Doctor）
type DoctorDTO struct {
	DoctorID     string  `json:"doctorId"`
	Name         string  `json:"name"`
	Title        string  `json:"title"`
	Department   string  `json:"department"`
	TeamID       *string `json:"teamId"`
	PhoneMasked  string  `json:"phoneMasked"`
	PatientCount int     `json:"patientCount"`
	Status       string  `json:"status"`
}

// TechnicianDTO 技师（契约 getTechnicians，对齐 shared-types Technician）
type TechnicianDTO struct {
	TechID       string `json:"techId"`
	Name         string `json:"name"`
	PhoneMasked  string `json:"phoneMasked"`
	TeamID       string `json:"teamId"`
	InstallCount int    `json:"installCount"`
	Status       string `json:"status"`
	AuthStatus   string `json:"authStatus"`
}

// TeamMembersDTO 团队成员明细（契约 getTeamMembers，T030 #10）
type TeamMembersDTO struct {
	Doctors     []DoctorDTO     `json:"doctors"`
	Technicians []TechnicianDTO `json:"technicians"`
}

// FeedbackDTO 患者反馈（契约 getFeedbacks，对齐 shared-types Feedback）
type FeedbackDTO struct {
	FeedbackID   string  `json:"feedbackId"`
	PatientID    string  `json:"patientId"`
	Type         string  `json:"type"`
	Content      string  `json:"content"`
	SubmitTime   string  `json:"submitTime"`
	Handler      *string `json:"handler"`
	ReplyContent *string `json:"replyContent"`
	ReplyTime    *string `json:"replyTime"`
	Status       string  `json:"status"`
}

// OrthosisPlanDTO 矫形方案（契约 getOrthosisPlans，对齐 shared-types OrthosisPlan）
type OrthosisPlanDTO struct {
	PlanID    string `json:"planId"`
	PatientID string `json:"patientId"`
	DoctorID  string `json:"doctorId"`
	Content   string `json:"content"`
	Version   string `json:"version"`
	CreatedAt string `json:"createdAt"`
}

// FeelingLogDTO 佩戴感受日志（契约 getFeelingLogs，对齐 shared-types FeelingLog）
type FeelingLogDTO struct {
	LogID           string   `json:"logId"`
	PatientID       string   `json:"patientId"`
	LogDate         string   `json:"logDate"`
	ComfortScore    *float64 `json:"comfortScore"`
	DiscomfortAreas []string `json:"discomfortAreas"`
	Notes           *string  `json:"notes"`
	ReplyContent    *string  `json:"replyContent"`
	ReplyTime       *string  `json:"replyTime"`
}

// AdminRoleDTO RBAC 角色行（契约 getAdminRoles，对齐 shared-types AdminRole）
type AdminRoleDTO struct {
	RoleID      string `json:"roleId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MemberCount int    `json:"memberCount"`
	CreatedAt   string `json:"createdAt"`
	Status      string `json:"status"`
	Preset      bool   `json:"preset"`
}

// RolePermissionsDTO 权限矩阵（契约 getRolePermissions，对齐 roles.permissions_json）
type RolePermissionsDTO struct {
	Scope   string   `json:"scope"`
	Modules []string `json:"modules"`
}

// WifiPresetDTO WiFi 预设条目（sys_configs.wifi_presets JSON 元素）
type WifiPresetDTO struct {
	Ssid     string `json:"ssid"`
	Password string `json:"password,omitempty"`
}

// SystemSettingsDTO 系统参数（契约 getSystemSettings，PRD §7D.12，映射 sys_configs）
type SystemSettingsDTO struct {
	DailyWearTargetHours   float64         `json:"dailyWearTargetHours"`
	PressureHighThresholdN float64         `json:"pressureHighThresholdN"`
	PressureFluctuationPct float64         `json:"pressureFluctuationPct"`
	WearInterruptMinutes   float64         `json:"wearInterruptMinutes"`
	SensorDriftN           float64         `json:"sensorDriftN"`
	WifiPresets            []WifiPresetDTO `json:"wifiPresets"`
}

// LoginResultDTO 登录响应（契约 adminLogin，T030 #9）
type LoginResultDTO struct {
	Token    string `json:"token"`
	AdminID  string `json:"adminId"`
	Username string `json:"username"`
	Name     string `json:"name"`
	RoleID   string `json:"roleId"`
	Scope    string `json:"scope"`
}

// TechLoginResultDTO 技师登录响应（契约 techLogin，T037）
type TechLoginResultDTO struct {
	Token  string `json:"token"`
	TechID string `json:"techId"`
	Name   string `json:"name"`
	TeamID string `json:"teamId"`
	Role   string `json:"role"` // 固定 "technician"
}

// PatientLoginResultDTO 患者登录响应（契约 patientLogin，T037）
type PatientLoginResultDTO struct {
	Token     string `json:"token"`
	PatientID string `json:"patientId"`
	Name      string `json:"name"`
	Role      string `json:"role"` // 固定 "patient"
}

// ─────────────────────────────────────────────────────────────
// 患者写操作请求/响应 DTO（T057 写功能契约）
// ─────────────────────────────────────────────────────────────

// CreatePatientRequestDTO 创建患者请求（name + phone 必填，其余可空）
type CreatePatientRequestDTO struct {
	Name      string   `json:"name"`
	Phone     string   `json:"phone"` // 必填，11 位 1 开头手机号（validPhone 校验）
	Gender    *string  `json:"gender"`
	Age       *int     `json:"age"`
	Diagnosis *string  `json:"diagnosis"`
	CobbAngle *float64 `json:"cobbAngle"`
	DeviceID  *string  `json:"deviceId"`
	TeamID    *string  `json:"teamId"`
	DoctorID  *string  `json:"doctorId"`
}

// AssignTeamRequestDTO 分配团队请求（teamId 必填）
type AssignTeamRequestDTO struct {
	TeamID string `json:"teamId"`
}

// BatchBindRequestDTO 批量绑定请求（patientIds 非空、teamId 必填）
type BatchBindRequestDTO struct {
	PatientIDs []string `json:"patientIds"`
	TeamID     string   `json:"teamId"`
}

// BatchBindFailureDTO 批量绑定单条失败
type BatchBindFailureDTO struct {
	PatientID string `json:"patientId"`
	Reason    string `json:"reason"`
}

// BatchBindResultDTO 批量绑定响应（部分失败不回滚，HTTP 仍 200）
type BatchBindResultDTO struct {
	SuccessCount int                   `json:"successCount"`
	FailedCount  int                   `json:"failedCount"`
	Failures     []BatchBindFailureDTO `json:"failures"`
}

// ─────────────────────────────────────────────────────────────
// 团队 / 成员写操作请求/响应 DTO（T059 写功能契约）
// 契约：docs/tasks/ella/T059-团队管理测试规格.md
// ─────────────────────────────────────────────────────────────

// CreateTeamRequestDTO 创建团队请求（name + leader 必填，description 可选）
type CreateTeamRequestDTO struct {
	Name        string `json:"name"`        // 必填，trim 后 ≥1 字符 ≤50
	Leader      string `json:"leader"`      // 必填，doctor_id 存在性校验
	Description string `json:"description"` // 可选，≤200 字符
}

// UpdateTeamRequestDTO 编辑团队请求（与创建同字段，全量替换语义）
type UpdateTeamRequestDTO struct {
	Name        string `json:"name"`
	Leader      string `json:"leader"`
	Description string `json:"description"`
}

// TeamDetailDTO 团队详情响应（创建/编辑返回；扩展 TeamDTO 增 leader/description/status/createdAt）
type TeamDetailDTO struct {
	TeamID       string `json:"teamId"`
	Name         string `json:"name"`
	Leader       string `json:"leader"`
	LeaderName   string `json:"leaderName"`
	MemberCount  int    `json:"memberCount"`
	PatientCount int    `json:"patientCount"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	CreatedAt    string `json:"createdAt"`
}

// AddMemberRequestDTO 添加成员请求
type AddMemberRequestDTO struct {
	MemberType string `json:"memberType"` // 必填，枚举 {doctor, technician}
	MemberID   string `json:"memberId"`   // 必填，doctor_id / tech_id
	Role       string `json:"role"`       // 可选，更新 doctor.title
}

// UpdateMemberRequestDTO 编辑成员请求（memberType 定位表，role 可选更新）
type UpdateMemberRequestDTO struct {
	MemberType string `json:"memberType"` // 必填，{doctor, technician}
	Role       string `json:"role"`       // 可选，更新 doctor.title
}

// TeamMemberDTO 团队成员响应（统一 doctor/technician 两类）
type TeamMemberDTO struct {
	MemberID     string `json:"memberId"`
	MemberType   string `json:"memberType"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	Title        string `json:"title"`
	PhoneMasked  string `json:"phoneMasked"`
	PatientCount int    `json:"patientCount"`
	JoinTime     string `json:"joinTime"`
	Status       string `json:"status"`
}

// ─────────────────────────────────────────────────────────────
// 微信登录（T069 患者端小程序）
// ─────────────────────────────────────────────────────────────

// WXLoginRequestDTO 患者端微信登录请求（小程序 wx.login 返回的 code）
type WXLoginRequestDTO struct {
	Code string `json:"code"` // 必填，wx.login() 签发的临时登录凭证
}
