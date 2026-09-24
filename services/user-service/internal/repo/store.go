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
// store.AssignPatientTeam / store.CreateFeedback（feedbacks.patient_id 外键）返回此 sentinel，
// handler 映射为 404 CodeNotFound。
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
// T130 复查记录 sentinel 错误
// ─────────────────────────────────────────────────────────────

// ErrReviewRecordNotFound 复查记录不存在。
var ErrReviewRecordNotFound = errors.New("review record not found")

// ErrReviewPatientNotFound 复查关联的患者不存在。
var ErrReviewPatientNotFound = errors.New("patient not found for review record")

// ─────────────────────────────────────────────────────────────
// T135 复查报告模板 sentinel 错误
// ─────────────────────────────────────────────────────────────

// ErrTemplateNotFound 复查报告模板（组）不存在。
// store.GetReviewTemplateGroup 命中 0 行返回；handler 映射为 404 CodeNotFound。
var ErrTemplateNotFound = errors.New("review template not found")

// ErrTemplateNameExists 模板名已存在（新建 v1 时撞已有组名）。
// store.CreateReviewTemplateVersion 以 groupID=="" 但 name 已存在返回；handler 映射为 409 CodeConflict。
var ErrTemplateNameExists = errors.New("review template name already exists")

// ReviewRecordRow review_records 表投影
type ReviewRecordRow struct {
	ReviewID       string
	PatientID      string
	ReviewDate     time.Time
	ReviewType     *string
	Findings       *string
	NextReviewDate *time.Time
	DoctorID       *string
	ReportFileID   *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ReviewTemplateRow review_templates 表投影（T135）
type ReviewTemplateRow struct {
	TemplateID      string
	TemplateGroupID string
	Name            string
	Version         int
	FileID          string
	Status          string // active / retired
	UploadedBy      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ─────────────────────────────────────────────────────────────
// 行投影（repo 层出参；handler 层转 DTO）
// ─────────────────────────────────────────────────────────────

// PatientRow patients LEFT JOIN teams/doctors 投影（管理端列表/详情）
// DeviceID：当前绑定设备，来自 devices(patient_id 只读关联)；patients.device_id 已废弃(T151 方案1)。
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

	// T226 患者自助资料字段（迁移 000014；均 nullable）
	HeightCm                 *float64
	WeightKg                 *float64
	EmergencyContactName     *string
	EmergencyContactPhone    *string
	EmergencyContactRelation *string
}

// PatientProfileUpdate 资料更新入参（指针=nil=不改），两条通道共用：
//   - T226 患者自助 PUT：仅前 8 个键，phone 一律无通道（手机号由微信登录授权写入，PM 2026-09-16 裁定）；
//   - T248 4.3 admin「编辑患者」：额外可写 Diagnosis / CobbAngle。
//
// 即：本结构体是字段全集，各通道的白名单在 handler 层请求体解码处收口。
type PatientProfileUpdate struct {
	Name                     *string
	Gender                   *string
	Age                      *int
	HeightCm                 *float64
	WeightKg                 *float64
	EmergencyContactName     *string
	EmergencyContactPhone    *string
	EmergencyContactRelation *string
	// Diagnosis / CobbAngle 临床字段，仅 admin「编辑患者」通道写入（T248 4.3 · PRD §7D.3 编辑弹窗）。
	// 患者自助 PUT（T226）白名单请求体不含这两个键 ⇒ DisallowUnknownFields 直接 400。
	Diagnosis *string
	CobbAngle *float64
}

// TeamRow teams 表投影
type TeamRow struct {
	TeamID       string
	Name         string
	MemberCount  int
	PatientCount int
	Leader       string // 负责人 doctor_id，无负责人为空串（T333）
	LeaderName   string // 负责人姓名（join doctors.name），无负责人为空串（T333）
	Description  string // T337：与 TeamDetailRow 同列，列表此前漏带
	Status       string // T337："active"（一期固定；预留软删除字段）
	CreatedAt    time.Time
}

// TeamStatsRow 团队维度聚合投影（T256 5.1，设计稿 团队管理.html:87-90 四张统计卡）。
// 成员口径 = doctors/technicians 中 team_id 非空者，与 DeleteTeam 的引用计数、
// GET /teams/:teamId/members 成员列表三者同源，不用 teams.member_count 维护列（防漂移）。
type TeamStatsRow struct {
	TotalTeams         int // 团队总数（status='active'）
	TotalMembers       int // 成员总数（医生 + 技师，已挂团队）
	ManagedPatients    int // 管理患者（patients.team_id 非空）
	UnassignedPatients int // 待分配患者（patients.team_id 为空）
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
	PhoneEnc     []byte // AES-GCM 密文，handler 层脱敏并给三态（T361）
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

// DoctorRow doctors LEFT JOIN 患者计数 + LEFT JOIN admins（T314 一行跨两表）
type DoctorRow struct {
	DoctorID     string
	Name         string
	Title        *string
	Department   *string
	TeamID       *string
	PhoneEnc     []byte // AES-GCM 密文，出 service 层前解密脱敏
	Status       string // doctors.status：档案在册层（当前无消费方，见 T314 交件待裁）
	PatientCount int
	// ↓ admins 侧四列。未绑登录账号的存量档案（seed D0002/D0003）三者均为 nil
	AdminID          *string
	Username         *string // 登录账号 doc%05d（T314 服务端发号）
	AccountStatus    *string // admins.status：登录能力层，登录校验实际读的就是它
	AccountCreatedAt *time.Time
}

// TechnicianRow technicians 表投影（team_name 由 LEFT JOIN teams 带出，T278-②）
type TechnicianRow struct {
	TechID    string
	Name      string
	PhoneEnc  []byte // AES-GCM 密文
	PhoneHash string // SHA-256 hex；phone_hash 为 CHAR(64)，PG 返回带尾部空格，
	// 装配时经 TrimPhoneHash 去除（与 uk_technicians_phone_hash 查重口径一致）
	TeamID       *string
	TeamName     *string // NULL = 未入队（technicians.team_id 可空）
	InstallCount int
	Status       string
	AuthStatus   string
	CreatedAt    time.Time // T333-6：列表 SELECT 补带（此前列表接口带不出，页面创建时间列全空）
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

// FeedbackCreateInput 反馈创建入参（T311）。字段已过 handler 层校验，长度不超列宽。
type FeedbackCreateInput struct {
	PatientID string
	Type      string
	Content   string
	Status    string
}

// FeedbackStatsRow 患者沟通统计栏聚合投影（T248 7.1 · PRD §7D.7 统计条）
type FeedbackStatsRow struct {
	TodayCount   int64    // 今日（Asia/Shanghai 切日）提交数
	PendingCount int64    // status='pending'（待回复）
	AvgReplySec  *float64 // 已回复样本的 (reply_time - submit_time) 均值，单位秒；无样本为 nil
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
	PatientName     string // T256 #2：跨患者查询 join patients.name（单患者查询为空）
	LogDate         time.Time
	ComfortScore    *float64
	ComfortLevel    *string // T256 #3：fitted(贴合) | discomfort(不适)，NULL=未评
	DiscomfortAreas []string
	Notes           *string
	ReplyContent    *string
	ReplyTime       *time.Time
	CreatedAt       time.Time // T306：feeling_logs.created_at（提交时间），列 NOT NULL
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
	// TeamScoped T350：true 表示「调用者身份推导出必须限定团队」，此时 TeamID 为空是
	// 「无团队可看」的空集语义，不能像过去那样当成「不按团队过滤」。
	// 只应由 handler 层按 X-Role/X-User-Id 落值，绝不接受客户端自报。
	TeamScoped bool
}

// FeelingLogSaveInput T188 患者端录入入参（字段已过 handler 层校验，长度不超列宽）。
// LogDate 用 YYYY-MM-DD 文本传参：log_date 是 DATE 列，传 time.Time 会被按会话时区
// 做 timestamptz→date 转换，存在跨日偏移一位的风险。
type FeelingLogSaveInput struct {
	PatientID       string
	LogDate         string
	ComfortLevel    string // fitted | discomfort（方案 A 两档）
	DiscomfortAreas []string
	Notes           *string
}

// FeelingLogAdminFilter T256 #2：跨患者感受日志筛选条件
type FeelingLogAdminFilter struct {
	Keyword   string // 患者姓名 ILIKE
	StartDate string // YYYY-MM-DD（含）
	EndDate   string // YYYY-MM-DD（含）
	Feeling   string // fitted | discomfort（T256 #3 直接比对 comfort_level 列）
	Page      int
	PageSize  int
	// TeamID/TeamScoped T350：医护按所属团队过滤（PRD §7D.11 数据范围规则）。
	// TeamScoped 为真且 TeamID 为空 = 该医护无团队归属，返回空集而不是全量。
	TeamID     string
	TeamScoped bool
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
// T151：建档不再写入 device_id（患者-设备绑定以 devices.patient_id 为唯一事实源，建档无权绑定确诊设备）。
type PatientInput struct {
	Name      string
	PhoneEnc  *[]byte // AES-GCM 密文（handler.preparePhone 生成；为 nil=微信-only 无手机号）
	PhoneHash *string // SHA-256 hex（handler.preparePhone 生成；为 nil=微信-only 无手机号）
	Gender    *string
	Age       *int
	Diagnosis *string
	CobbAngle *float64
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
	// UpdatePatientProfile 患者自助改本人档案（T226 白名单动态 SET，见 PatientProfileUpdate）。
	UpdatePatientProfile(ctx context.Context, patientID string, in PatientProfileUpdate) error
	// PatientPhoneHashTaken phone_hash 是否已被其他患者占用（excludePatientID 排除自身）。
	PatientPhoneHashTaken(ctx context.Context, phoneHash, excludePatientID string) (bool, error)
	RoleScope(ctx context.Context, roleID string) (scope string, err error)
	DoctorIDByAdmin(ctx context.Context, adminID string) (doctorID string, ok bool, err error)
	// DoctorTeamByAdmin T350：admin_id → 所属团队 team_id（数据范围推导用）。
	// 无 doctor 行或 team_id 为空/NULL 一律 ok=false，由 handler 层按 fail-closed 处理。
	DoctorTeamByAdmin(ctx context.Context, adminID string) (teamID string, ok bool, err error)

	// 患者（管理端只读）
	ListPatients(ctx context.Context, f PatientFilter) ([]PatientRow, int64, error)
	GetPatient(ctx context.Context, patientID string) (*PatientRow, error)
	// GetPatientInTeam T350：带团队谓词的详情读，「不存在 / 跨团队 / 未分配团队」一律 (nil, nil)，
	// 由 handler 对受限身份统一回 403（防患者号存在性 oracle）。teamID 为空串同样恒不命中。
	GetPatientInTeam(ctx context.Context, patientID, teamID string) (*PatientRow, error)

	// 患者（管理端写，T057 写功能契约）
	CreatePatient(ctx context.Context, in PatientInput) (*PatientRow, error)
	AssignPatientTeam(ctx context.Context, patientID, teamID string) (*PatientRow, error)
	BatchBindPatients(ctx context.Context, patientIDs []string, teamID string) (*BatchBindResult, error)

	// 团队 / 医生
	ListTeams(ctx context.Context) ([]TeamRow, error)
	TeamExists(ctx context.Context, teamID string) (bool, error)
	ListDoctors(ctx context.Context) ([]DoctorRow, error)
	ListDoctorsByTeam(ctx context.Context, teamID string) ([]DoctorRow, error)
	// GetTeamStats T256 #1：团队管理 4 张统计卡（团队/成员/管理患者/待分配患者计数）
	GetTeamStats(ctx context.Context) (teamCount, memberCount, managedPatientCount, unassignedPatientCount int, err error)

	// 团队 / 成员写操作（T059 写功能契约）
	// 契约：docs/tasks/ella/T059-团队管理测试规格.md
	// sentinel：ErrTeamNotFound/ErrTeamNameExists/ErrLeaderNotFound/ErrMemberNotFound/ErrMemberInTeam/ErrTeamInUse
	CreateTeam(ctx context.Context, in TeamInput) (*TeamDetailRow, error)
	UpdateTeam(ctx context.Context, teamID string, in TeamInput) (*TeamDetailRow, error)
	// GetTeam T333：团队单条读（GET /api/v1/teams/:teamId），与写端点回读同一投影；不存在返回 ErrTeamNotFound
	GetTeam(ctx context.Context, teamID string) (*TeamDetailRow, error)
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

	// 医护账号写通道（T314，PRD §7D.10）：一行跨 admins + doctors 两表，创建同事务写两表。
	// sentinel：ErrDoctorNotFound(404) / ErrDoctorNoAccount(409，档案未绑登录账号) /
	//           ErrUsernameExhausted(500，发号序列连续撞已占用序号)
	// 单条读回 GetDoctorAccount 只在 PGStore 内部复用，不进接口（无调用方，避免死方法）。
	CreateDoctorAccount(ctx context.Context, in DoctorAccountInput) (*DoctorRow, error)
	UpdateDoctorAccount(ctx context.Context, doctorID string, in DoctorAccountUpdate) (*DoctorRow, error)
	SetDoctorAccountStatus(ctx context.Context, doctorID, status string) (*DoctorRow, error)
	SetDoctorAccountPassword(ctx context.Context, doctorID, passwordHash string) (*DoctorRow, error)

	// 反馈
	ListFeedbacks(ctx context.Context, keyword string) ([]FeedbackRow, error)
	// CreateFeedback T311 反馈创建端点（患者端配网失败自动存档）。
	// patient_id 外键不命中 → ErrPatientNotFound；返回自增 feedback_id。
	CreateFeedback(ctx context.Context, in FeedbackCreateInput) (int64, error)
	// FeedbackStats 患者沟通统计栏（T248 7.1）：今日区间由调用方按 Asia/Shanghai 切日传入
	FeedbackStats(ctx context.Context, todayStart, todayEnd time.Time) (FeedbackStatsRow, error)
	ProcessFeedback(ctx context.Context, feedbackID int64, handlerID string, replyContent *string) (bool, error)

	// 矫形方案
	ListPlans(ctx context.Context, patientID string) ([]OrthosisPlanRow, error)
	LatestPlanVersion(ctx context.Context, patientID string) (string, bool, error)
	CreatePlan(ctx context.Context, patientID, doctorID, content, version string) (*OrthosisPlanRow, error)

	// 感受日志
	ListFeelingLogs(ctx context.Context, patientID string) ([]FeelingLogRow, error)
	// SaveFeelingLog T188 患者端创建/覆盖当日感受日志（同患者同日覆盖，不清医生回复位）。
	// patient_id 外键不命中 → ErrPatientNotFound；返回落库后的整行（含 logId / createdAt）。
	SaveFeelingLog(ctx context.Context, in FeelingLogSaveInput) (FeelingLogRow, error)
	// FeelingLogInTeam T373：医生回复落库前的只读归属探测（团队谓词在同一条 SQL 里）。
	// 与 GetPatientInTeam 同语义：「日志不存在 / 患者属他团队 / 患者未分配团队」一律 false，
	// 由 handler 对受限身份统一回 403（否则 403 与 404 的差就是 logId 存在性 oracle）；
	// teamID 为空恒 false 且不下库。刻意无写副作用：越权要在触库之前判掉。
	FeelingLogInTeam(ctx context.Context, logID int64, teamID string) (bool, error)
	ReplyFeelingLog(ctx context.Context, logID int64, replyContent string) (bool, error)
	// ListFeelingLogsAdmin T256 #2：跨患者感受日志流（搜索/日期范围/感受筛选）
	ListFeelingLogsAdmin(ctx context.Context, f FeelingLogAdminFilter) ([]FeelingLogRow, int64, error)

	// 角色与权限矩阵
	ListRoles(ctx context.Context) ([]RoleRow, error)
	GetRole(ctx context.Context, roleID string) (*RoleRow, error)
	UpdateRolePermissions(ctx context.Context, roleID, permissionsJSON string) (bool, error)

	// 系统配置
	GetConfigs(ctx context.Context, keys []string) (map[string]string, error)
	UpsertConfigs(ctx context.Context, kvs []ConfigKV, updatedBy string) error

	// T252 2.2 逐采集点告警阈值（alert_point_rules，稀疏存放：未落库点由 handler 回默认）
	ListAlertPointRules(ctx context.Context) ([]AlertPointRuleRow, error)
	// SaveAlertRules 单事务写统一上下限（kvs → sys_configs）+ 逐点阈值 UPSERT。
	SaveAlertRules(ctx context.Context, kvs []ConfigKV, rules []AlertPointRuleRow, updatedBy string) error
	// ResetAlertRules 恢复默认：清空逐点表 + 统一/全局键写回默认值（同一事务）。
	ResetAlertRules(ctx context.Context, kvs []ConfigKV, updatedBy string) error

	// T252 11.2 角色增删改（重名由 handler 先调 RoleNameTaken 拦 409；
	// sentinel：ErrRoleNotFound / *ErrRoleInUse）
	RoleNameTaken(ctx context.Context, name, excludeRoleID string) (bool, error)
	CreateRole(ctx context.Context, name, description, permissionsJSON string) (*RoleRow, error)
	UpdateRole(ctx context.Context, roleID string, name, description, status *string) (*RoleRow, error)
	DeleteRole(ctx context.Context, roleID string) error

	// T252 12.3 操作日志（audit_logs；写入失败不得阻断主流程，handler 侧记 WARN）
	WriteAuditLog(ctx context.Context, in AuditInput) error
	QueryAuditLogs(ctx context.Context, f AuditFilter) ([]AuditLogRow, int64, error)

	// T130 复查记录
	CreateReviewRecord(ctx context.Context, row ReviewRecordRow) (*ReviewRecordRow, error)
	ListReviewRecordsByPatient(ctx context.Context, patientID string) ([]ReviewRecordRow, error)
	GetReviewRecord(ctx context.Context, reviewID string) (*ReviewRecordRow, error)

	// T135 复查报告模板
	// CreateReviewTemplateVersion 事务：新版本 active + 同组旧 active 标 retired（非删除）。
	// groupID=="" 表示新建组（version=1；name 已存在返回 ErrTemplateNameExists）；
	// groupID!= "" 表示版本替换（组内 version 递增，组不存在返回 ErrTemplateNotFound）。
	// uploadedBy 由 handler 从登录凭证 X-User-Id 传入（防伪造上传人）。
	CreateReviewTemplateVersion(ctx context.Context, groupID, name, fileID, uploadedBy string) (*ReviewTemplateRow, error)
	// ListActiveReviewTemplates 每模板组当前 active 版本（name 升序）。
	ListActiveReviewTemplates(ctx context.Context) ([]ReviewTemplateRow, error)
	// GetReviewTemplateGroup 按组查当前 active 版本；组不存在或无非active返回 ErrTemplateNotFound。
	GetReviewTemplateGroup(ctx context.Context, groupID string) (*ReviewTemplateRow, error)

	// T274 告警流程画布（flow_template / flow_instance / flow_node_state / flow_node_action）
	// sentinel：ErrFlowTemplateNotFound / *ErrFlowTemplateInUse / ErrFlowInstanceNotFound /
	// *ErrFlowInstanceExists / ErrFlowAlertNotFound / ErrFlowNodeNotFound /
	// *ErrFlowNodeNotCurrent / ErrFlowInstanceCompleted
	ListFlowTemplates(ctx context.Context, keyword string, page, pageSize int) ([]FlowTemplateRow, int64, error)
	GetFlowTemplate(ctx context.Context, templateID string) (*FlowTemplateRow, error)
	FlowTemplateNameTaken(ctx context.Context, name, excludeID string) (bool, error)
	CreateFlowTemplate(ctx context.Context, name, nodesJSON, edgesJSON, creator string) (*FlowTemplateRow, error)
	// UpdateFlowTemplate 全量覆盖语义：nil 字段不改；任一字段变更 → version+1。
	UpdateFlowTemplate(ctx context.Context, templateID string, name, nodesJSON, edgesJSON *string) (*FlowTemplateRow, error)
	DeleteFlowTemplate(ctx context.Context, templateID string) error

	CreateFlowInstance(ctx context.Context, templateID string, alertID int64, nodeIDs, entryNodeIDs []string) (*FlowInstanceRow, error)
	GetFlowInstance(ctx context.Context, instanceID string) (*FlowInstanceRow, error)
	ListFlowInstancesByAlert(ctx context.Context, alertID int64) ([]FlowInstanceRow, error)
	ListNodeStates(ctx context.Context, instanceID string) ([]FlowNodeStateRow, error)
	// ApplyFlowNodeAction 状态机唯一写入点（图遍历结果由 handler 传入，repo 不读模板 JSON）。
	ApplyFlowNodeAction(ctx context.Context, in FlowActionWrite) (*FlowNodeActionRow, error)
	ListFlowNodeActions(ctx context.Context, instanceID string) ([]FlowNodeActionRow, error)
}
