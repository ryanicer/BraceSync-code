// Package repo user-service 数据访问层
//
// 表归属（架构 §4.2 单一写入者）：patients / teams / doctors / technicians /
// orthosis_plans / feeling_logs / feedbacks / roles / admins / sys_configs 均为 user-service owner。
// SQL 全占位符参数化（防注入）；查询走 idx_patients_team / idx_patients_doctor 等既有索引。
package repo

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrPatientExists 创建患者手机号重复（phone_hash 已存在）。
// store.CreatePatient 内部按 PhoneHash 查重命中时返回此 sentinel，handler 映射为 409 CodeConflict。
var ErrPatientExists = errors.New("patient already exists")

// ErrPatientNotFound 患者 ID 不存在。
// store.AssignPatientTeam 返回此 sentinel，handler 映射为 404 CodeNotFound。
var ErrPatientNotFound = errors.New("patient not found")

// ErrWXOpenIDExists 创建微信患者 openid 冲突（并发竞态下 idx_patients_wx_openid
// 命中 23505；handler 据此回退 GetPatientByWXOpenID 重试 1 次实现幂等 upsert）。
var ErrWXOpenIDExists = errors.New("patient wx_openid already exists")

// ErrAlreadyBound T085：患者 wx_openid 已绑定其他微信（并发绑定竞态下
// UPDATE ... WHERE wx_openid IS NULL 命中 0 行；handler 映射为 10603）。
var ErrAlreadyBound = errors.New("patient already bound to another wechat openid")

// ─────────────────────────────────────────────────────────────
// T059 团队/成员写操作 sentinel 错误（handler 据此映射 HTTP code）
// ─────────────────────────────────────────────────────────────

// ErrTeamNotFound 团队 ID 不存在。
// store.UpdateTeam/DeleteTeam/AddTeamMember/UpdateTeamMember/RemoveTeamMember 返回此 sentinel，
// handler 映射为 404 CodeNotFound。
var ErrTeamNotFound = errors.New("team not found")

// ErrTeamNameExists 团队名重复。
// store.CreateTeam/UpdateTeam 按 name 查重命中返回此 sentinel，handler 映射为 409 CodeConflict。
var ErrTeamNameExists = errors.New("team name already exists")

// ErrLeaderNotFound 负责人 doctorId 不存在。
// store.CreateTeam/UpdateTeam 校验 leader 存在性失败返回此 sentinel，handler 映射为 400 CodeInvalidParam。
var ErrLeaderNotFound = errors.New("leader not found")

// ErrMemberNotFound 成员不存在或不属本团队。
// store.AddTeamMember（memberId 查无）/UpdateTeamMember（memberId 不属本团队）返回此 sentinel，
// handler 映射为 404 CodeNotFound。
var ErrMemberNotFound = errors.New("member not found")

// ErrMemberInTeam 成员已属本团队（重复添加）。
// store.AddTeamMember 检测到 member.team_id 已等于目标 teamID 返回此 sentinel，
// handler 映射为 409 CodeConflict。
var ErrMemberInTeam = errors.New("member already in team")

// ErrTeamInUse 团队被引用（patients/members 命中），不可删除。
// store.DeleteTeam 统计引用计数命中返回此结构化错误，handler 据 Counts 拼装 409 文案。
// 删除约束策略 A（reject-if-referenced，Ella 推荐，待 Boss 评审）。
type ErrTeamInUse struct {
	PatientCount int
	MemberCount  int
}

func (e *ErrTeamInUse) Error() string {
	return fmt.Sprintf("team in use: %d patients, %d members", e.PatientCount, e.MemberCount)
}

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
	PhoneEnc   []byte // AES-GCM 密文（T057：创建患者含手机号；出参 handler 脱敏）
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

// TeamDetailRow teams 详情投影（T059 写功能返回；扩展 leader/description/status/createdAt）
type TeamDetailRow struct {
	TeamID       string
	Name         string
	Leader       string // 负责人 doctor_id
	LeaderName   string // 负责人姓名（join doctors.name）
	MemberCount  int
	PatientCount int
	Description  string
	Status       string // "active"（一期固定；预留软删除字段）
	CreatedAt    time.Time
}

// TeamMemberRow 团队成员投影（T059 写功能返回；统一 doctor/technician 两类）
type TeamMemberRow struct {
	MemberID     string
	MemberType   string // "doctor" | "technician"
	Name         string
	Role         string // doctor.title（technician 无 title 字段则空）
	Title        string // 保留字段：科室/职称（与 Role 字段语义对齐设计源成员表角色）
	PhoneEnc     []byte // AES-GCM 密文，handler 层脱敏
	PhoneMasked  string // handler 装配时脱敏（避免在 repo 层依赖 phone.Cipher）
	PatientCount int    // 仅 doctor 有意义（technician 用 InstallCount，预留位）
	JoinTime     time.Time
	Status       string
}

// TeamInput 创建/编辑团队入参（T059 写功能契约）
type TeamInput struct {
	Name        string // 必填，trim 后 ≥1 字符 ≤50
	Leader      string // 必填，doctor_id 存在性校验
	Description string // 可选，≤200 字符
}

// MemberInput 成员管理入参（T059 写功能契约）
type MemberInput struct {
	MemberType string // "doctor" | "technician"
	MemberID   string // doctor_id / tech_id
	Role       string // 可选，更新 doctor.title（technician 无 title 字段则忽略）
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

// PatientInput 创建患者入参（T057 写功能契约；T069 扩展可空 phone 支持微信-only 用户）。
// Name 必填；PhoneEnc/PhoneHash 为 nil 表示微信-only 用户（对应 DB 列 NULL，
// 迁移 000008 已解除 NOT NULL 约束）；其余可空指针。
type PatientInput struct {
	Name      string
	PhoneEnc  *[]byte // AES-GCM 密文（handler.preparePhone 生成；为 nil=微信-only 无手机号）
	PhoneHash *string // SHA-256 hex（handler.preparePhone 生成；为 nil=微信-only 无手机号）
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
	// GetPatientByWXOpenID T069：按微信 openid 查患者登录行；不存在返回 (nil, nil)
	GetPatientByWXOpenID(ctx context.Context, openid string) (*PatientLoginRow, error)
	// CreatePatientByWXOpenID T069：按 openid 创建微信-only 患者。
	// 默认 name="微信用户" status="active" 其余字段 NULL；并发下 openid 唯一冲突返回
	// ErrWXOpenIDExists（handler 据此回退 Get 1 次实现幂等 upsert）
	CreatePatientByWXOpenID(ctx context.Context, openid string) (*PatientLoginRow, error)
	// T085 患者微信绑定与档案维护
	GetPatientWXOpenID(ctx context.Context, patientID string) (openID string, err error)
	// BindPatientOpenid 原子绑定 openid：UPDATE ... WHERE wx_openid IS NULL。
	// 命中 0 行表示已绑定（或被并发抢占）→ ErrAlreadyBound；命中 1 行成功。
	BindPatientOpenid(ctx context.Context, patientID, openid string) error
	// UnbindWechat 解绑微信：wx_openid 置 NULL（admin 维护）。
	UnbindWechat(ctx context.Context, patientID string) error
	// UpdatePatientPhone 改手机号：phone_enc + phone_hash 同步更新（admin 维护）。
	UpdatePatientPhone(ctx context.Context, patientID string, phoneEnc []byte, phoneHash string) error
	// PatientPhoneHashTaken phone_hash 是否已被其他患者占用（excludePatientID 排除自身）。
	PatientPhoneHashTaken(ctx context.Context, phoneHash, excludePatientID string) (bool, error)
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

	// 团队 / 成员写操作（T059 写功能契约）
	// 契约：docs/tasks/ella/T059-团队管理测试规格.md
	// sentinel：ErrTeamNotFound/ErrTeamNameExists/ErrLeaderNotFound/ErrMemberNotFound/ErrMemberInTeam/ErrTeamInUse
	CreateTeam(ctx context.Context, in TeamInput) (*TeamDetailRow, error)
	UpdateTeam(ctx context.Context, teamID string, in TeamInput) (*TeamDetailRow, error)
	DeleteTeam(ctx context.Context, teamID string) error // 返回 ErrTeamNotFound / ErrTeamInUse
	AddTeamMember(ctx context.Context, teamID string, in MemberInput) (*TeamMemberRow, error)
	UpdateTeamMember(ctx context.Context, teamID, memberID string, in MemberInput) (*TeamMemberRow, error)
	RemoveTeamMember(ctx context.Context, teamID, memberID, memberType string) error // 幂等：已移除 no-op

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
