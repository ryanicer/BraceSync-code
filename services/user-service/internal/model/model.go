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
	CodeInternal     = 90001 // 系统内部错误（DB/加密配置等）
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
