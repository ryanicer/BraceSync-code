// Package repo user-service 数据访问层
//
// 表归属（架构 §4.2 单一写入者）：patients / teams / doctors / technicians /
// orthosis_plans / feeling_logs / feedbacks / roles / admins / sys_configs 均为 user-service owner。
// SQL 全占位符参数化（防注入）；查询走 idx_patients_team / idx_patients_doctor 等既有索引。
package repo

import (
	"context"
	"errors"
	"time"
)

// ErrPatientExists 创建患者重复键冲突（name+age+diagnosis 完全相同）。
// store.CreatePatient 返回此 sentinel，handler 映射为 409 CodeConflict。
var ErrPatientExists = errors.New("patient already exists")

// ErrPatientNotFound 患者 ID 不存在。
// store.AssignPatientTeam 返回此 sentinel，handler 映射为 404 CodeNotFound。
var ErrPatientNotFound = errors.New("patient not found")

// ─────────────────────────────────────────────────────────────
// 行投影（repo 层出参；handler 层转 DTO）
// ─────────────────────────────────────────────────────────────

// PatientRow patients LEFT JOIN teams/doctors 投影（管理端列表/详情）
type PatientRow struct {
	PatientID  string
	Name       string
	Gender     *string
	Age        *int
	Diagnosis  *string
	CobbAngle  *float64
	DeviceID   *string
	TeamID     *string
	DoctorID   *string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	TeamName   *string
	DoctorName *string
}

// TeamRow teams 表投影
type TeamRow struct {
	TeamID       string
	Name         string
	MemberCount  int
	PatientCount int
}

// DoctorRow doctors LEFT JOIN 患者计数投影
type DoctorRow struct {
	DoctorID     string
	Name         string
	Title        *string
	Department   *string
	TeamID       *string
	PhoneEnc     []byte // AES-GCM 密文，出 service 层前解密脱敏
	Status       string
	PatientCount int
}

// TechnicianRow technicians 表投影
type TechnicianRow struct {
	TechID    string
	Name      string
	PhoneEnc  []byte // AES-GCM 密文
	PhoneHash string // SHA-256 hex；phone_hash 为 CHAR(64)，PG 返回带尾部空格，
	// 装配时经 TrimPhoneHash 去除（与 uk_technicians_phone_hash 查重口径一致）
	TeamID       *string
	InstallCount int
	Status       string
	AuthStatus   string
}

// TrimPhoneHash 去除 CHAR(64) 列的尾部空格填充（PG 定长字符列语义）
func TrimPhoneHash(h string) string {
	for len(h) > 0 && h[len(h)-1] == ' ' {
		h = h[:len(h)-1]
	}
	return h
}

// FeedbackRow feedbacks 表投影
type FeedbackRow struct {
	FeedbackID   int64
	PatientID    string
	Type         *string
	Content      string
	SubmitTime   time.Time
	Handler      *string
	ReplyContent *string
	ReplyTime    *time.Time
	Status       string
}

// OrthosisPlanRow orthosis_plans 表投影
type OrthosisPlanRow struct {
	PlanID    int64
	PatientID string
	DoctorID  string
	Content   string
	Version   string
	CreatedAt time.Time
}

// FeelingLogRow feeling_logs 表投影
type FeelingLogRow struct {
	LogID           int64
	PatientID       string
	LogDate         time.Time
	ComfortScore    *float64
	DiscomfortAreas []string
	Notes           *string
	ReplyContent    *string
	ReplyTime       *time.Time
}

// RoleRow roles LEFT JOIN admins 计数投影
type RoleRow struct {
	RoleID          string
	Name            string
	Description     *string
	PermissionsJSON string
	Status          string
	CreatedAt       time.Time
	MemberCount     int
}

// AdminRow admins 登录查询投影（password_hash 不出 repo 层以外，仅登录用）
type AdminRow struct {
	AdminID      string
	Username     string
	Name         string
	PasswordHash string
	RoleID       string
	Status       string
}

// TechLoginRow technicians 登录查询投影（T037 技师手机号+密码登录）
type TechLoginRow struct {
	TechID       string
	Name         string
	PasswordHash string
	TeamID       string // 可空（未分配团队的技师）
	Status       string
	AuthStatus   string
}

// PatientLoginRow patients 登录查询投影（T037 患者手机号+密码登录）
type PatientLoginRow struct {
	PatientID    string
	Name         string
	PasswordHash string
	Status       string
}

// ConfigKV sys_configs 键值
type ConfigKV struct {
	Key   string
	Value string
}

// PatientFilter 管理端患者列表筛选（keyword=姓名/患者ID ILIKE；teamId 精确）
type PatientFilter struct {
	Keyword  string
	TeamID   string
	Page     int
	PageSize int
}

// TechInput 技师新建/编辑入参（PhoneEnc/PhoneHash 由 service/handler 层准备）
type TechInput struct {
	TechID    string // 新建时由 handler 生成；编辑时忽略
	Name      string
	PhoneEnc  []byte
	PhoneHash string
	TeamID    *string
}

// PatientInput 创建患者入参（T057 写功能契约）。Name 必填；其余可空指针。
type PatientInput struct {
	Name      string
	Gender    *string
	Age       *int
	Diagnosis *string
	CobbAngle *float64
	DeviceID  *string
	TeamID    *string
	DoctorID  *string
}

// BatchBindFailure 批量绑定单条失败记录
type BatchBindFailure struct {
	PatientID string
	Reason    string
}

// BatchBindResult 批量绑定结果：成功 ID 列表 + 失败明细（部分失败策略，不整体回滚）
type BatchBindResult struct {
	Success []string
	Failed  []BatchBindFailure
}

// ─────────────────────────────────────────────────────────────
// Store 数据访问接口（handler 依赖注入点；单测用 fake，集成测试用 PGStore）
// ─────────────────────────────────────────────────────────────

// Store user-service 全量数据访问接口
type Store interface {
	// 登录与身份
	GetAdminByUsername(ctx context.Context, username string) (*AdminRow, error)
	UpdateAdminPasswordHash(ctx context.Context, adminID string, newHash string) error
	GetTechByPhoneHash(ctx context.Context, phoneHash string) (*TechLoginRow, error)
	GetPatientByPhoneHash(ctx context.Context, phoneHash string) (*PatientLoginRow, error)
	RoleScope(ctx context.Context, roleID string) (scope string, err error)
	DoctorIDByAdmin(ctx context.Context, adminID string) (doctorID string, ok bool, err error)

	// 患者（管理端只读）
	ListPatients(ctx context.Context, f PatientFilter) ([]PatientRow, int64, error)
	GetPatient(ctx context.Context, patientID string) (*PatientRow, error)

	// 患者（管理端写，T057 写功能契约）
	CreatePatient(ctx context.Context, in PatientInput) (*PatientRow, error)
	AssignPatientTeam(ctx context.Context, patientID, teamID string) (*PatientRow, error)
	BatchBindPatients(ctx context.Context, patientIDs []string, teamID string) (*BatchBindResult, error)

	// 团队 / 医生
	ListTeams(ctx context.Context) ([]TeamRow, error)
	TeamExists(ctx context.Context, teamID string) (bool, error)
	ListDoctors(ctx context.Context) ([]DoctorRow, error)
	ListDoctorsByTeam(ctx context.Context, teamID string) ([]DoctorRow, error)

	// 技师
	ListTechnicians(ctx context.Context, page, pageSize int) ([]TechnicianRow, int64, error)
	ListTechniciansByTeam(ctx context.Context, teamID string) ([]TechnicianRow, error)
	GetTechnician(ctx context.Context, techID string) (*TechnicianRow, error)
	CreateTechnician(ctx context.Context, in TechInput) (*TechnicianRow, error)
	UpdateTechnician(ctx context.Context, techID string, in TechInput) (*TechnicianRow, error)
	ToggleTechnician(ctx context.Context, techID, status string) (bool, error)
	TechPhoneHashTaken(ctx context.Context, phoneHash, excludeTechID string) (bool, error)

	// 反馈
	ListFeedbacks(ctx context.Context, keyword string) ([]FeedbackRow, error)
	ProcessFeedback(ctx context.Context, feedbackID int64, handlerID string, replyContent *string) (bool, error)

	// 矫形方案
	ListPlans(ctx context.Context, patientID string) ([]OrthosisPlanRow, error)
	LatestPlanVersion(ctx context.Context, patientID string) (string, bool, error)
	CreatePlan(ctx context.Context, patientID, doctorID, content, version string) (*OrthosisPlanRow, error)

	// 感受日志
	ListFeelingLogs(ctx context.Context, patientID string) ([]FeelingLogRow, error)
	ReplyFeelingLog(ctx context.Context, logID int64, replyContent string) (bool, error)

	// 角色与权限矩阵
	ListRoles(ctx context.Context) ([]RoleRow, error)
	GetRole(ctx context.Context, roleID string) (*RoleRow, error)
	UpdateRolePermissions(ctx context.Context, roleID, permissionsJSON string) (bool, error)

	// 系统配置
	GetConfigs(ctx context.Context, keys []string) (map[string]string, error)
	UpsertConfigs(ctx context.Context, kvs []ConfigKV, updatedBy string) error
}
