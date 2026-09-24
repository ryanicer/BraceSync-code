// Package repo — PGStore：Store 接口的 PostgreSQL（pgx v5）实现
//
// 全部 SQL 使用 $n 占位符参数化；动态筛选仅拼接占位符序号，不拼接用户输入。
package repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore Store 的 pgx 实现
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore 组装 PGStore
func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

// feedbackListLimit 反馈列表单次返回上限（契约一期数组返回，防大结果集）
const feedbackListLimit = 200

// ─────────────────────────────────────────────────────────────
// 登录与身份
// ─────────────────────────────────────────────────────────────

// GetAdminByUsername 按登录用户名查账号；不存在返回 (nil, nil)
func (s *PGStore) GetAdminByUsername(ctx context.Context, username string) (*AdminRow, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT admin_id, username, name, password_hash, role_id, status FROM admins WHERE username = $1`, username)
	var a AdminRow
	err := row.Scan(&a.AdminID, &a.Username, &a.Name, &a.PasswordHash, &a.RoleID, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// UpdateAdminPasswordHash 更新 admins 密码哈希（渐进式重哈希：T040）
func (s *PGStore) UpdateAdminPasswordHash(ctx context.Context, adminID string, newHash string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE admins SET password_hash = $1 WHERE admin_id = $2`, newHash, adminID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("admin not found")
	}
	return nil
}

// GetTechByPhoneHash 按手机号哈希查技师登录信息；不存在返回 (nil, nil)（T037）
func (s *PGStore) GetTechByPhoneHash(ctx context.Context, phoneHash string) (*TechLoginRow, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT tech_id, name, password_hash, team_id, status, auth_status
		 FROM technicians WHERE phone_hash = $1`, phoneHash)
	var t TechLoginRow
	var teamID *string
	err := row.Scan(&t.TechID, &t.Name, &t.PasswordHash, &teamID, &t.Status, &t.AuthStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if teamID != nil {
		t.TeamID = *teamID
	}
	return &t, nil
}

// GetPatientByPhoneHash 按手机号哈希查患者登录信息；不存在返回 (nil, nil)（T037）
func (s *PGStore) GetPatientByPhoneHash(ctx context.Context, phoneHash string) (*PatientLoginRow, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT patient_id, name, password_hash, status
		 FROM patients WHERE phone_hash = $1`, phoneHash)
	var p PatientLoginRow
	err := row.Scan(&p.PatientID, &p.Name, &p.PasswordHash, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPatientByWXOpenID T069：按微信 openid 查患者登录行；不存在返回 (nil, nil)
func (s *PGStore) GetPatientByWXOpenID(ctx context.Context, openid string) (*PatientLoginRow, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT patient_id, name, password_hash, status
		 FROM patients WHERE wx_openid = $1`, openid)
	var p PatientLoginRow
	err := row.Scan(&p.PatientID, &p.Name, &p.PasswordHash, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePatientByWXOpenID T069：按 openid 创建微信-only 新患者。
// 默认 name="微信用户"、status="active"，其余字段 NULL；并发唯一冲突返回
// ErrWXOpenIDExists（handler 据此重试 Get 实现幂等 upsert）。
func (s *PGStore) CreatePatientByWXOpenID(ctx context.Context, openid string) (*PatientLoginRow, error) {
	patientID, err := newPatientID()
	if err != nil {
		return nil, err
	}
	_, execErr := s.pool.Exec(ctx,
		`INSERT INTO patients (patient_id, name, wx_openid, status)
		 VALUES ($1, '微信用户', $2, 'active')`,
		patientID, openid)
	if execErr != nil {
		var pgErr *pgconn.PgError
		if errors.As(execErr, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrWXOpenIDExists
		}
		return nil, execErr
	}
	return s.GetPatientByWXOpenID(ctx, openid)
}

// GetPatientWXOpenID T085：查患者当前绑定的 wx_openid；未绑定返回空串。
func (s *PGStore) GetPatientWXOpenID(ctx context.Context, patientID string) (string, error) {
	row := s.pool.QueryRow(ctx, `SELECT wx_openid FROM patients WHERE patient_id = $1`, patientID)
	var openID *string
	if err := row.Scan(&openID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrPatientNotFound
		}
		return "", err
	}
	if openID == nil {
		return "", nil
	}
	return *openID, nil
}

// BindPatientOpenid T085：原子绑定 openid。UPDATE ... WHERE wx_openid IS NULL
// 命中 0 行 → ErrAlreadyBound（已绑定其他 openid 或被并发抢占）。
func (s *PGStore) BindPatientOpenid(ctx context.Context, patientID, openid string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE patients SET wx_openid = $1, updated_at = NOW()
		 WHERE patient_id = $2 AND wx_openid IS NULL`,
		openid, patientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyBound
	}
	return nil
}

// UnbindWechat T085：解绑微信（wx_openid 置 NULL）。
func (s *PGStore) UnbindWechat(ctx context.Context, patientID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE patients SET wx_openid = NULL, updated_at = NOW() WHERE patient_id = $1`,
		patientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPatientNotFound
	}
	return nil
}

// UpdatePatientPhone T085：改手机号，phone_enc + phone_hash 同步更新。
func (s *PGStore) UpdatePatientPhone(ctx context.Context, patientID string, phoneEnc []byte, phoneHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE patients SET phone_enc = $1, phone_hash = $2, updated_at = NOW()
		 WHERE patient_id = $3`,
		phoneEnc, phoneHash, patientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPatientNotFound
	}
	return nil
}

// UpdatePatientProfile T226 患者自助改本人档案：白名单字段动态 SET（nil=不改），
// updated_at 应用层刷新；不命中返回 ErrPatientNotFound。phone 不在白名单（微信授权写入）。
func (s *PGStore) UpdatePatientProfile(ctx context.Context, patientID string, in PatientProfileUpdate) error {
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if in.Name != nil {
		add("name", *in.Name)
	}
	if in.Gender != nil {
		add("gender", *in.Gender)
	}
	if in.Age != nil {
		add("age", *in.Age)
	}
	if in.HeightCm != nil {
		add("height_cm", *in.HeightCm)
	}
	if in.WeightKg != nil {
		add("weight_kg", *in.WeightKg)
	}
	if in.EmergencyContactName != nil {
		add("emergency_contact_name", *in.EmergencyContactName)
	}
	if in.EmergencyContactPhone != nil {
		add("emergency_contact_phone", *in.EmergencyContactPhone)
	}
	if in.EmergencyContactRelation != nil {
		add("emergency_contact_relation", *in.EmergencyContactRelation)
	}
	if in.Diagnosis != nil {
		add("diagnosis", *in.Diagnosis)
	}
	if in.CobbAngle != nil {
		add("cobb_angle", *in.CobbAngle)
	}
	if len(sets) == 1 { // 仅 updated_at：无白名单字段可写（handler 已先拒 400，此处兜底防空 SET）
		return nil
	}
	args = append(args, patientID)
	tag, err := s.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE patients SET %s WHERE patient_id = $%d`, strings.Join(sets, ", "), len(args)),
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPatientNotFound
	}
	return nil
}

// PatientPhoneHashTaken T085：phone_hash 是否已被其他患者占用（排除自身）。
func (s *PGStore) PatientPhoneHashTaken(ctx context.Context, phoneHash, excludePatientID string) (bool, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM patients WHERE phone_hash = $1 AND patient_id <> $2)`,
		phoneHash, excludePatientID)
	var taken bool
	if err := row.Scan(&taken); err != nil {
		return false, err
	}
	return taken, nil
}

// RoleScope 读角色数据范围（permissions_json->>'scope'）；角色不存在返回空串
func (s *PGStore) RoleScope(ctx context.Context, roleID string) (string, error) {
	row := s.pool.QueryRow(ctx, `SELECT permissions_json->>'scope' FROM roles WHERE role_id = $1`, roleID)
	var scope *string
	err := row.Scan(&scope)
	if errors.Is(err, pgx.ErrNoRows) || scope == nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return *scope, nil
}

// DoctorIDByAdmin admin_id → doctors.doctor_id（医生工作台身份解析）
func (s *PGStore) DoctorIDByAdmin(ctx context.Context, adminID string) (string, bool, error) {
	row := s.pool.QueryRow(ctx, `SELECT doctor_id FROM doctors WHERE admin_id = $1`, adminID)
	var doctorID string
	err := row.Scan(&doctorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return doctorID, true, nil
}

// DoctorTeamByAdmin T350：admin_id → 医护所属团队 team_id。
// 登录身份是 admins.admin_id（JWT sub），团队只挂在 doctors 行上，故必须走这一跳；
// 无 doctor 行 / team_id 为 NULL 或空串都返回 ok=false —— 调用方据此收紧为空集，
// 绝不退化成「不过滤」（那正是 T350 要修的口子）。
func (s *PGStore) DoctorTeamByAdmin(ctx context.Context, adminID string) (string, bool, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT COALESCE(team_id, '') FROM doctors WHERE admin_id = $1`, adminID)
	var teamID string
	err := row.Scan(&teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return teamID, teamID != "", nil
}

// ─────────────────────────────────────────────────────────────
// 患者（管理端只读，含 teams/doctors 姓名 join）
// ─────────────────────────────────────────────────────────────

// patientSelect 患者列表/详情投影。
// T151(方案C)：当前绑定设备取自 devices（patient_id 只读关联；跨服务只读，写归属 device-service），
// 依赖迁移 000012 的 uk_devices_active_patient 部分唯一索引保证一个患者至多一行；patients.device_id 已废弃、不再读取。
const patientSelect = `
SELECT p.patient_id, p.name, p.gender, p.age, p.diagnosis, p.cobb_angle,
       dev.device_id, p.team_id, p.primary_doctor_id, p.status, p.created_at, p.updated_at,
       t.name AS team_name, d.name AS doctor_name,
       p.height_cm, p.weight_kg, p.emergency_contact_name, p.emergency_contact_phone, p.emergency_contact_relation
FROM patients p
LEFT JOIN devices dev ON dev.patient_id = p.patient_id
LEFT JOIN teams t ON t.team_id = p.team_id
LEFT JOIN doctors d ON d.doctor_id = p.primary_doctor_id`

// patientWhere 组装筛选 WHERE 与参数（keyword=姓名/患者ID ILIKE；teamId 精确）
//
// T350 数据范围：TeamScoped 为真表示「按身份必须限定团队」——此时 TeamID 为空是
// 「无团队可看」，必须落空集，不能沿用下面「空串 = 不过滤」的运营侧语义。
func patientWhere(f PatientFilter) (string, []any) {
	var conds []string
	var args []any
	if f.Keyword != "" {
		args = append(args, "%"+f.Keyword+"%")
		conds = append(conds, fmt.Sprintf(`(p.name ILIKE $%[1]d OR p.patient_id ILIKE $%[1]d)`, len(args)))
	}
	if f.TeamScoped && f.TeamID == "" {
		conds = append(conds, "false")
	} else if f.TeamID != "" {
		args = append(args, f.TeamID)
		conds = append(conds, fmt.Sprintf(`p.team_id = $%d`, len(args)))
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func scanPatient(row pgx.Row) (*PatientRow, error) {
	var p PatientRow
	err := row.Scan(&p.PatientID, &p.Name, &p.Gender, &p.Age, &p.Diagnosis, &p.CobbAngle,
		&p.DeviceID, &p.TeamID, &p.DoctorID, &p.Status, &p.CreatedAt, &p.UpdatedAt,
		&p.TeamName, &p.DoctorName,
		&p.HeightCm, &p.WeightKg, &p.EmergencyContactName, &p.EmergencyContactPhone, &p.EmergencyContactRelation)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListPatients 管理端患者分页列表（走 idx_patients_team；keyword 小表全扫可接受，一期数据量）
func (s *PGStore) ListPatients(ctx context.Context, f PatientFilter) ([]PatientRow, int64, error) {
	where, args := patientWhere(f)

	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM patients p`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (f.Page - 1) * f.PageSize
	listArgs := append(append([]any{}, args...), f.PageSize, offset)
	query := patientSelect + where +
		fmt.Sprintf(` ORDER BY p.created_at DESC, p.patient_id LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)

	rows, err := s.pool.Query(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]PatientRow, 0, f.PageSize)
	for rows.Next() {
		p, scanErr := scanPatient(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		list = append(list, *p)
	}
	return list, total, rows.Err()
}

// GetPatient 患者详情（管理端）；不存在返回 (nil, nil)
//
// 不带团队谓词 ⇒ 受限身份（仅本团队患者）请用 GetPatientInTeam，否则 403 与 404 的差会泄露患者号是否存在。
func (s *PGStore) GetPatient(ctx context.Context, patientID string) (*PatientRow, error) {
	row := s.pool.QueryRow(ctx, patientSelect+` WHERE p.patient_id = $1`, patientID)
	p, err := scanPatient(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetPatientInTeam T350：只按「患者 + 团队」取档案行，无命中返回 (nil, nil)。
//
// 与 GetPatient 的分工是给单资源读端点一条「越权与查无此人同结果」的读法：
// 团队谓词进同一条 SQL，三种不可见（无此患者 / 患者在他团队 / 患者未分配团队）
// 一律 (nil, nil)，handler 据此统一回 403，受限身份因此拿不到患者号存在性 oracle。
//
// teamID 为空串（医护无团队归属）同样恒不命中，且不下库；patients.team_id 可为 NULL，
// 但 NULL 与空串做等值比较恒为未知，空团队这一侧靠本函数的短路保证，不依赖比较语义。
func (s *PGStore) GetPatientInTeam(ctx context.Context, patientID, teamID string) (*PatientRow, error) {
	if teamID == "" {
		return nil, nil
	}
	row := s.pool.QueryRow(ctx, patientSelect+` WHERE p.patient_id = $1 AND p.team_id = $2`, patientID, teamID)
	p, err := scanPatient(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ─────────────────────────────────────────────────────────────
// 团队 / 医生
// ─────────────────────────────────────────────────────────────

// teamPatientCountExpr T371-B1：团队「患者数」实时按 patients.team_id 数，不再读 teams.patient_count。
// 该列运行期无人写（全仓只有迁移/seed 写过，删除守卫按 team_id 现场数完也不写回），
// 现网读到的是建库快照 ⇒ 列表显示 0 患者的团队点删除会得 409，两个数同名互相打脸。
// 谓词与 data-service 团队排行（dashboard_repo.go teamRankingSQL）、本仓删除守卫同源。
const teamPatientCountExpr = `(SELECT COUNT(*) FROM patients p WHERE p.team_id = t.team_id)`

// ListTeams 团队概要（member_count 为 teams 表维护列；patient_count 见 teamPatientCountExpr）
// T333：负责人两列同 teamDetailSelect 的 LEFT JOIN doctors 口径——
// 列表页「负责人」列与编辑弹窗回显都直接读列表行，缺这两列就是结构上带不出来。
// T333-5：created_at 同为该页表格列（T335 探测证据 filled=0），列在库里非空，纯 SELECT 漏带。
// T337：description / status 契约（shared-types Team）已声明、详情接口已带出，列表仍未带 ⇒ 列同 teamDetailSelect 口径。
func (s *PGStore) ListTeams(ctx context.Context) ([]TeamRow, error) {
	rows, err := s.pool.Query(ctx, `
SELECT t.team_id, t.name, t.member_count, `+teamPatientCountExpr+` AS patient_count,
       COALESCE(t.leader, ''), COALESCE(d.name, ''), t.created_at,
       COALESCE(t.description, ''), t.status
FROM teams t
LEFT JOIN doctors d ON d.doctor_id = t.leader
ORDER BY t.team_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []TeamRow
	for rows.Next() {
		var t TeamRow
		if scanErr := rows.Scan(&t.TeamID, &t.Name, &t.MemberCount, &t.PatientCount,
			&t.Leader, &t.LeaderName, &t.CreatedAt, &t.Description, &t.Status); scanErr != nil {
			return nil, scanErr
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// TeamExists 团队存在性（技师新建/编辑的 FK 前置校验）
func (s *PGStore) TeamExists(ctx context.Context, teamID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM teams WHERE team_id = $1)`, teamID).Scan(&exists)
	return exists, err
}

// doctorColumns 医护读侧投影：doctors 6 列 + 主诊患者数 + admins 侧 4 列
// （T314：PRD §7D.10（1）的「登录账号 / 创建时间」两列在 admins 侧，一行跨两张表）
const doctorColumns = `d.doctor_id, d.name, d.title, d.department, d.team_id, d.phone_enc, d.status,
       COUNT(p.patient_id) AS patient_count,
       d.admin_id, a.username, a.status AS account_status, a.created_at AS account_created_at`

const doctorSelect = `
SELECT ` + doctorColumns + `
FROM doctors d
LEFT JOIN patients p ON p.primary_doctor_id = d.doctor_id
` + doctorAdminJoin

func (s *PGStore) scanDoctors(rows pgx.Rows) ([]DoctorRow, error) {
	defer rows.Close()
	var list []DoctorRow
	for rows.Next() {
		var d DoctorRow
		if err := rows.Scan(&d.DoctorID, &d.Name, &d.Title, &d.Department, &d.TeamID, &d.PhoneEnc,
			&d.Status, &d.PatientCount, &d.AdminID, &d.Username, &d.AccountStatus, &d.AccountCreatedAt); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

// ListDoctors 全量医生（含主诊患者计数 + admins 侧登录账号/状态/创建时间，T314）
func (s *PGStore) ListDoctors(ctx context.Context) ([]DoctorRow, error) {
	rows, err := s.pool.Query(ctx, doctorSelect+` `+doctorGroupBy+` ORDER BY d.doctor_id`)
	if err != nil {
		return nil, err
	}
	return s.scanDoctors(rows)
}

// ListDoctorsByTeam 团队内医生（团队成员明细）
func (s *PGStore) ListDoctorsByTeam(ctx context.Context, teamID string) ([]DoctorRow, error) {
	rows, err := s.pool.Query(ctx,
		doctorSelect+` WHERE d.team_id = $1 `+doctorGroupBy+` ORDER BY d.doctor_id`, teamID)
	if err != nil {
		return nil, err
	}
	return s.scanDoctors(rows)
}

// ─────────────────────────────────────────────────────────────
// 技师
// ─────────────────────────────────────────────────────────────

// techColumns 技师投影；末尾 team_name = T278-② LEFT JOIN teams 带出的团队名
// （设计稿技师列表显示团队名，前端分页拿不到全量团队字典 ⇒ 与患者列表 D1 同源，后端 join）
// created_at = T333-6：技师管理页「创建时间」列此前恒空（列在库里非空、列表也按它排序，只是没 SELECT）
const techColumns = `technicians.tech_id, technicians.name, technicians.phone_enc, technicians.phone_hash,
	technicians.team_id, technicians.install_count, technicians.status, technicians.auth_status,
	teams.name AS team_name, technicians.created_at`

// techFrom 统一 FROM 子句（三处技师查询共用，别名 teams 不与 technicians 列冲突）
const techFrom = ` FROM technicians LEFT JOIN teams ON teams.team_id = technicians.team_id`

func scanTech(row pgx.Row) (*TechnicianRow, error) {
	var t TechnicianRow
	err := row.Scan(&t.TechID, &t.Name, &t.PhoneEnc, &t.PhoneHash, &t.TeamID, &t.InstallCount,
		&t.Status, &t.AuthStatus, &t.TeamName, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	t.PhoneHash = TrimPhoneHash(t.PhoneHash) // CHAR(64) 尾空格
	return &t, nil
}

// ListTechnicians 技师分页列表
func (s *PGStore) ListTechnicians(ctx context.Context, page, pageSize int) ([]TechnicianRow, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM technicians`).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	rows, err := s.pool.Query(ctx,
		`SELECT `+techColumns+techFrom+` ORDER BY technicians.created_at DESC, technicians.tech_id LIMIT $1 OFFSET $2`,
		pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]TechnicianRow, 0, pageSize)
	for rows.Next() {
		t, scanErr := scanTech(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		list = append(list, *t)
	}
	return list, total, rows.Err()
}

// ListTechniciansByTeam 团队内技师（团队成员明细）
func (s *PGStore) ListTechniciansByTeam(ctx context.Context, teamID string) ([]TechnicianRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+techColumns+techFrom+` WHERE technicians.team_id = $1 ORDER BY technicians.tech_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []TechnicianRow
	for rows.Next() {
		t, scanErr := scanTech(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		list = append(list, *t)
	}
	return list, rows.Err()
}

// GetTechnician 技师详情；不存在返回 (nil, nil)
func (s *PGStore) GetTechnician(ctx context.Context, techID string) (*TechnicianRow, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+techColumns+techFrom+` WHERE technicians.tech_id = $1`, techID)
	t, err := scanTech(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// CreateTechnician 新建技师（phone_hash 唯一约束由 uk_technicians_phone_hash 兜底）
func (s *PGStore) CreateTechnician(ctx context.Context, in TechInput) (*TechnicianRow, error) {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO technicians (tech_id, name, phone_enc, phone_hash, team_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		in.TechID, in.Name, in.PhoneEnc, in.PhoneHash, in.TeamID)
	if err != nil {
		return nil, err
	}
	return s.GetTechnician(ctx, in.TechID)
}

// UpdateTechnician 编辑技师（全字段覆盖：handler 已合并既有值）
func (s *PGStore) UpdateTechnician(ctx context.Context, techID string, in TechInput) (*TechnicianRow, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE technicians SET name = $2, phone_enc = $3, phone_hash = $4, team_id = $5 WHERE tech_id = $1`,
		techID, in.Name, in.PhoneEnc, in.PhoneHash, in.TeamID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}
	return s.GetTechnician(ctx, techID)
}

// ToggleTechnician 启用/禁用（幂等）；返回技师是否存在
func (s *PGStore) ToggleTechnician(ctx context.Context, techID, status string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE technicians SET status = $2 WHERE tech_id = $1`, techID, status)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// TechPhoneHashTaken 手机号哈希查重（excludeTechID 编辑时排除自身；新建传空串）
func (s *PGStore) TechPhoneHashTaken(ctx context.Context, phoneHash, excludeTechID string) (bool, error) {
	var taken bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM technicians WHERE phone_hash = $1 AND tech_id <> $2)`,
		phoneHash, excludeTechID).Scan(&taken)
	return taken, err
}

// ─────────────────────────────────────────────────────────────
// 反馈
// ─────────────────────────────────────────────────────────────

// feedbackTeamCond T378：反馈按所属团队过滤的谓词片段（含前导 " AND "，空串 = 不加条件）。
//
// 字段名与 PatientFilter.TeamScoped / FeelingLogAdminFilter 的 T350 约定一致：
// TeamScoped 为真而 TeamID 为空 = 该医护无团队归属 ⇒ 恒假（空集），绝不退化成「不过滤」。
// feedbacks 自身不带团队列，归属只能经 patient_id 一跳落到 patients.team_id。
// argN 是该条件要用的占位符序号（前面已挂了几个入参），故与 keyword 的先后无关。
func feedbackTeamCond(scope FeedbackScope, argN int) (string, []any) {
	if !scope.TeamScoped {
		return "", nil
	}
	if scope.TeamID == "" {
		return " AND false", nil
	}
	return fmt.Sprintf(` AND EXISTS (SELECT 1 FROM patients pt WHERE pt.patient_id = f.patient_id AND pt.team_id = $%d)`, argN),
		[]any{scope.TeamID}
}

// ListFeedbacks 反馈列表（keyword=内容/患者ID/患者姓名 ILIKE；按提交时间倒序，上限 200）
func (s *PGStore) ListFeedbacks(ctx context.Context, keyword string, scope FeedbackScope) ([]FeedbackRow, error) {
	query := `SELECT f.feedback_id, f.patient_id, f.type, f.content, f.submit_time,
	                 f.handler, f.reply_content, f.reply_time, f.status
	          FROM feedbacks f`
	var args []any
	var conds []string
	if keyword != "" {
		query += ` LEFT JOIN patients p ON p.patient_id = f.patient_id`
		args = append(args, "%"+keyword+"%")
		conds = append(conds, fmt.Sprintf(`(f.content ILIKE $%[1]d OR f.patient_id ILIKE $%[1]d OR p.name ILIKE $%[1]d)`, 1))
	}
	// 团队谓词自带 EXISTS 子查询，不依赖 keyword 分支的 p join ⇒ 与是否带 keyword 无关
	teamCond, teamArgs := feedbackTeamCond(scope, len(args)+1)
	if teamCond != "" {
		args = append(args, teamArgs...)
		conds = append(conds, strings.TrimPrefix(teamCond, " AND "))
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += fmt.Sprintf(` ORDER BY f.submit_time DESC, f.feedback_id DESC LIMIT %d`, feedbackListLimit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []FeedbackRow
	for rows.Next() {
		var f FeedbackRow
		if scanErr := rows.Scan(&f.FeedbackID, &f.PatientID, &f.Type, &f.Content, &f.SubmitTime,
			&f.Handler, &f.ReplyContent, &f.ReplyTime, &f.Status); scanErr != nil {
			return nil, scanErr
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

// CreateFeedback T311 反馈落库（患者端配网失败自动存档）。
// submit_time 用库默认 now()，handler/reply_content/reply_time 留空待客服回复时回填。
// patient_id 外键不命中（含校验通过后患者被删的竞态）→ ErrPatientNotFound，不裸抛 500。
func (s *PGStore) CreateFeedback(ctx context.Context, in FeedbackCreateInput) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO feedbacks (patient_id, type, content, status)
		 VALUES ($1, $2, $3, $4)
		 RETURNING feedback_id`,
		in.PatientID, in.Type, in.Content, in.Status).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return 0, ErrPatientNotFound
		}
		return 0, err
	}
	return id, nil
}

// FeedbackStats 患者沟通统计栏（T248 7.1）：一次聚合出今日咨询 / 待回复 / 平均响应
//
// T378：三项计数原先恒为全院口径，医护身份进来也数全院。团队谓词挂在同一条聚合的
// WHERE 上（与 ListFeedbacks 同一个 feedbackTeamCond），让统计条与列表页落在同一个
// 患者集合里 —— 分开减项会让两处数字对不上，正是 T371 那族「同名列不同取数」的坑。
func (s *PGStore) FeedbackStats(ctx context.Context, todayStart, todayEnd time.Time, scope FeedbackScope) (FeedbackStatsRow, error) {
	var out FeedbackStatsRow
	teamCond, teamArgs := feedbackTeamCond(scope, 3) // $1/$2 是今日区间，团队恒取 $3
	args := append([]any{todayStart, todayEnd}, teamArgs...)
	sql := `SELECT COUNT(*) FILTER (WHERE f.submit_time >= $1 AND f.submit_time < $2),
		        COUNT(*) FILTER (WHERE f.status = 'pending'),
		        AVG(EXTRACT(EPOCH FROM (f.reply_time - f.submit_time)))
		            FILTER (WHERE f.reply_time IS NOT NULL AND f.submit_time IS NOT NULL)
		 FROM feedbacks f`
	if teamCond != "" {
		sql += " WHERE " + strings.TrimPrefix(teamCond, " AND ")
	}
	err := s.pool.QueryRow(ctx, sql, args...).
		Scan(&out.TodayCount, &out.PendingCount, &out.AvgReplySec)
	if err != nil {
		return FeedbackStatsRow{}, err
	}
	return out, nil
}

// FeedbackInTeam T378：处理反馈落库前的只读归属探测（口径同 FeelingLogInTeam）。
// 归属经 feedbacks.patient_id → patients.team_id 一跳；「反馈不存在 / 患者属他团队 /
// 患者未分配团队」一律 false，由 handler 对受限身份统一回 403，免得 403 与 404 之差
// 成为 feedbackId 存在性 oracle。teamID 为空（医护无团队归属）不下库直接 false。
func (s *PGStore) FeedbackInTeam(ctx context.Context, feedbackID int64, teamID string) (bool, error) {
	if teamID == "" {
		return false, nil
	}
	var one int
	err := s.pool.QueryRow(ctx,
		`SELECT 1 FROM feedbacks f
		 JOIN patients p ON p.patient_id = f.patient_id
		 WHERE f.feedback_id = $1 AND p.team_id = $2`, feedbackID, teamID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ProcessFeedback 客服「处理备注」与「标记已处理」两个动作（T374，PRD V3.26 §7D.7 状态映射）：
// markResolved 为真落 resolved，否则 pending → replied；resolved 不回退。
// replyContent 为 nil 时保留原备注与原 reply_time（仅标记不得洗掉已存备注）。
// 返回反馈是否存在。
func (s *PGStore) ProcessFeedback(
	ctx context.Context, feedbackID int64, handlerID string, replyContent *string, markResolved bool,
) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE feedbacks
		 SET handler = $2,
		     reply_content = CASE WHEN $3::text IS NULL THEN reply_content ELSE $3::text END,
		     reply_time = CASE WHEN $3::text IS NULL THEN reply_time ELSE now() END,
		     status = CASE WHEN $4 THEN 'resolved' WHEN status = 'resolved' THEN 'resolved' ELSE 'replied' END
		 WHERE feedback_id = $1`,
		feedbackID, handlerID, replyContent, markResolved)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ─────────────────────────────────────────────────────────────
// 矫形方案
// ─────────────────────────────────────────────────────────────

// ListPlans 患者方案历史（按创建倒序）
func (s *PGStore) ListPlans(ctx context.Context, patientID string) ([]OrthosisPlanRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT plan_id, patient_id, doctor_id, content, version, created_at
		 FROM orthosis_plans WHERE patient_id = $1 ORDER BY created_at DESC, plan_id DESC`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []OrthosisPlanRow
	for rows.Next() {
		var p OrthosisPlanRow
		if scanErr := rows.Scan(&p.PlanID, &p.PatientID, &p.DoctorID, &p.Content, &p.Version, &p.CreatedAt); scanErr != nil {
			return nil, scanErr
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// LatestPlanVersion 患者最新方案版本号；无方案返回 ok=false
func (s *PGStore) LatestPlanVersion(ctx context.Context, patientID string) (string, bool, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT version FROM orthosis_plans WHERE patient_id = $1 ORDER BY created_at DESC, plan_id DESC LIMIT 1`, patientID)
	var version string
	err := row.Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return version, true, nil
}

// CreatePlan 保存新方案（version 由 handler 递增计算）
func (s *PGStore) CreatePlan(ctx context.Context, patientID, doctorID, content, version string) (*OrthosisPlanRow, error) {
	var p OrthosisPlanRow
	err := s.pool.QueryRow(ctx,
		`INSERT INTO orthosis_plans (patient_id, doctor_id, content, version)
		 VALUES ($1, $2, $3, $4)
		 RETURNING plan_id, patient_id, doctor_id, content, version, created_at`,
		patientID, doctorID, content, version).
		Scan(&p.PlanID, &p.PatientID, &p.DoctorID, &p.Content, &p.Version, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ─────────────────────────────────────────────────────────────
// 感受日志
// ─────────────────────────────────────────────────────────────

// feelingLogColumns 感受日志统一投影（单患者读 + 写端点回读共用；T333 同型思路：
// 读写两处共用一份列清单，避免写侧回读与列表少列）。patient_name 占位空串，
// 跨患者流的姓名列由 ListFeelingLogsAdmin 自己的 join 投影负责。
const feelingLogColumns = `log_id, patient_id, '' AS patient_name, log_date, comfort_score, comfort_level, discomfort_areas, notes, reply_content, reply_time, created_at`

func scanFeelingLog(scanner interface{ Scan(dest ...any) error }) (FeelingLogRow, error) {
	var f FeelingLogRow
	err := scanner.Scan(&f.LogID, &f.PatientID, &f.PatientName, &f.LogDate, &f.ComfortScore,
		&f.ComfortLevel, &f.DiscomfortAreas, &f.Notes, &f.ReplyContent, &f.ReplyTime, &f.CreatedAt)
	return f, err
}

// ListFeelingLogs 患者感受日志（按日期倒序）
func (s *PGStore) ListFeelingLogs(ctx context.Context, patientID string) ([]FeelingLogRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+feelingLogColumns+`
		 FROM feeling_logs WHERE patient_id = $1 ORDER BY log_date DESC, log_id DESC`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []FeelingLogRow
	for rows.Next() {
		f, scanErr := scanFeelingLog(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

// SaveFeelingLog T188 患者端录入（方案 A，Boss 2026-09-23 18:55 裁定两档）。
// uk（patient_id, log_date）⇒ 同患者同日覆盖更新 comfort_level / discomfort_areas / notes，
// 显式不写 reply_content 与 reply_time（PM 采纳的 Q3 口径：覆盖当日行但保留医生已写的回复）。
// comfort_score 不写（PRD V3.17 已注明旧星级口径作废，写入口径以 comfort_level 为准）。
// patient_id 外键不命中 → ErrPatientNotFound，与 T311 建反馈同口径，不裸抛 500。
func (s *PGStore) SaveFeelingLog(ctx context.Context, in FeelingLogSaveInput) (FeelingLogRow, error) {
	areas := in.DiscomfortAreas
	if areas == nil {
		areas = []string{}
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO feeling_logs (patient_id, log_date, comfort_level, discomfort_areas, notes)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (patient_id, log_date) DO UPDATE
		 SET comfort_level    = EXCLUDED.comfort_level,
		     discomfort_areas = EXCLUDED.discomfort_areas,
		     notes            = EXCLUDED.notes
		 RETURNING `+feelingLogColumns,
		in.PatientID, in.LogDate, in.ComfortLevel, areas, in.Notes)
	out, err := scanFeelingLog(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return FeelingLogRow{}, ErrPatientNotFound
		}
		return FeelingLogRow{}, err
	}
	return out, nil
}

// FeelingLogInTeam T373：医生回复落库前的只读归属探测。
// feeling_logs 本身不带团队列，归属由患者的 team_id 决定，故与 patients 内连接
// （口径同 ListFeelingLogsAdmin 的 T350 团队过滤）。患者未分配团队时 p.team_id IS NULL，
// 等值比较恒不命中，与「日志不存在 / 他团队」同回 false，由 handler 统一成 403。
// teamID 为空（医护账号无团队归属）不下库直接 false，走 fail-closed。
func (s *PGStore) FeelingLogInTeam(ctx context.Context, logID int64, teamID string) (bool, error) {
	if teamID == "" {
		return false, nil
	}
	var one int
	err := s.pool.QueryRow(ctx,
		`SELECT 1 FROM feeling_logs fl
		 JOIN patients p ON p.patient_id = fl.patient_id
		 WHERE fl.log_id = $1 AND p.team_id = $2`, logID, teamID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ReplyFeelingLog 医生回复写入（重复回复覆盖）；返回日志是否存在
func (s *PGStore) ReplyFeelingLog(ctx context.Context, logID int64, replyContent string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE feeling_logs SET reply_content = $2, reply_time = now() WHERE log_id = $1`,
		logID, replyContent)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// GetTeamStats T256 #1：团队管理 4 张统计卡。
// 团队总数=COUNT(teams)；成员总数=SUM(teams.member_count)；
// 管理患者=COUNT(patients WHERE team_id IS NOT NULL)；待分配=COUNT(patients WHERE team_id IS NULL)。
func (s *PGStore) GetTeamStats(ctx context.Context) (int, int, int, int, error) {
	var teamCount, memberCount, managedCount, unassignedCount int
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM teams) AS team_count,
			COALESCE((SELECT SUM(member_count) FROM teams), 0) AS member_count,
			(SELECT COUNT(*) FROM patients WHERE team_id IS NOT NULL) AS managed_count,
			(SELECT COUNT(*) FROM patients WHERE team_id IS NULL) AS unassigned_count
	`).Scan(&teamCount, &memberCount, &managedCount, &unassignedCount)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return teamCount, memberCount, managedCount, unassignedCount, nil
}

// ListFeelingLogsAdmin T256 #2：跨患者感受日志流。
// 支持 keyword（p.name ILIKE）、startDate/endDate（log_date 范围）、feeling（fitted|discomfort，
// 直接比对 comfort_level 列，T256 #3 起不再由 comfort_score 派生）。按 log_date DESC 分页。
// 与 patients 内连接 ⇒ p.name 必然带出（T290-A 在 DTO 回填 patientName）。
func (s *PGStore) ListFeelingLogsAdmin(ctx context.Context, f FeelingLogAdminFilter) ([]FeelingLogRow, int64, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if f.Keyword != "" {
		where = append(where, fmt.Sprintf("p.name ILIKE $%d", idx))
		args = append(args, "%"+f.Keyword+"%")
		idx++
	}
	if f.StartDate != "" {
		where = append(where, fmt.Sprintf("fl.log_date >= $%d", idx))
		args = append(args, f.StartDate)
		idx++
	}
	if f.EndDate != "" {
		where = append(where, fmt.Sprintf("fl.log_date <= $%d", idx))
		args = append(args, f.EndDate)
		idx++
	}
	switch f.Feeling {
	case "fitted":
		where = append(where, fmt.Sprintf("fl.comfort_level = $%d", idx))
		args = append(args, "fitted")
		idx++
	case "discomfort":
		where = append(where, fmt.Sprintf("fl.comfort_level = $%d", idx))
		args = append(args, "discomfort")
		idx++
	}
	// T350 数据范围：医护只能看本团队患者的感受日志（patients 已内连接，直接落 p.team_id）；
	// TeamScoped 且无团队 → 空集。
	if f.TeamScoped {
		if f.TeamID == "" {
			where = append(where, "false")
		} else {
			where = append(where, fmt.Sprintf("p.team_id = $%d", idx))
			args = append(args, f.TeamID)
			idx++
		}
	}
	whereSQL := strings.Join(where, " AND ")

	// count
	var total int64
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM feeling_logs fl JOIN patients p ON p.patient_id = fl.patient_id WHERE %s`, whereSQL)
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (f.Page - 1) * f.PageSize
	listSQL := fmt.Sprintf(`
		SELECT fl.log_id, fl.patient_id, p.name, fl.log_date, fl.comfort_score, fl.comfort_level, fl.discomfort_areas, fl.notes, fl.reply_content, fl.reply_time, fl.created_at
		FROM feeling_logs fl JOIN patients p ON p.patient_id = fl.patient_id
		WHERE %s ORDER BY fl.log_date DESC, fl.log_id DESC LIMIT $%d OFFSET $%d`, whereSQL, idx, idx+1)
	args = append(args, f.PageSize, offset)

	rows, err := s.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []FeelingLogRow
	for rows.Next() {
		var f FeelingLogRow
		if scanErr := rows.Scan(&f.LogID, &f.PatientID, &f.PatientName, &f.LogDate, &f.ComfortScore,
			&f.ComfortLevel, &f.DiscomfortAreas, &f.Notes, &f.ReplyContent, &f.ReplyTime, &f.CreatedAt); scanErr != nil {
			return nil, 0, scanErr
		}
		list = append(list, f)
	}
	return list, total, rows.Err()
}

// ─────────────────────────────────────────────────────────────
// 角色与权限矩阵
// ─────────────────────────────────────────────────────────────

const roleSelect = `
SELECT r.role_id, r.name, r.description, r.permissions_json::text, r.status, r.created_at,
       COUNT(a.admin_id) AS member_count
FROM roles r
LEFT JOIN admins a ON a.role_id = r.role_id`

func scanRole(row pgx.Row) (*RoleRow, error) {
	var r RoleRow
	err := row.Scan(&r.RoleID, &r.Name, &r.Description, &r.PermissionsJSON, &r.Status, &r.CreatedAt, &r.MemberCount)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListRoles 角色列表（含 admins 计数）
func (s *PGStore) ListRoles(ctx context.Context) ([]RoleRow, error) {
	rows, err := s.pool.Query(ctx, roleSelect+` GROUP BY r.role_id ORDER BY r.role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []RoleRow
	for rows.Next() {
		r, scanErr := scanRole(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		list = append(list, *r)
	}
	return list, rows.Err()
}

// GetRole 单角色；不存在返回 (nil, nil)
func (s *PGStore) GetRole(ctx context.Context, roleID string) (*RoleRow, error) {
	row := s.pool.QueryRow(ctx, roleSelect+` WHERE r.role_id = $1 GROUP BY r.role_id`, roleID)
	r, err := scanRole(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// UpdateRolePermissions 权限矩阵写入（permissions_json 整体替换）；返回角色是否存在
func (s *PGStore) UpdateRolePermissions(ctx context.Context, roleID, permissionsJSON string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE roles SET permissions_json = $2::jsonb WHERE role_id = $1`, roleID, permissionsJSON)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ─────────────────────────────────────────────────────────────
// 系统配置
// ─────────────────────────────────────────────────────────────

// GetConfigs 批量读 sys_configs；不存在的键不出现在结果 map
func (s *PGStore) GetConfigs(ctx context.Context, keys []string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT config_key, config_value FROM sys_configs WHERE config_key = ANY($1)`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string, len(keys))
	for rows.Next() {
		var kv ConfigKV
		if scanErr := rows.Scan(&kv.Key, &kv.Value); scanErr != nil {
			return nil, scanErr
		}
		out[kv.Key] = kv.Value
	}
	return out, rows.Err()
}

// UpsertConfigs 批量写 sys_configs（单事务 UPSERT，updated_at/updated_by 审计）
func (s *PGStore) UpsertConfigs(ctx context.Context, kvs []ConfigKV, updatedBy string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, kv := range kvs {
		if _, execErr := tx.Exec(ctx,
			`INSERT INTO sys_configs (config_key, config_value, updated_by, updated_at)
			 VALUES ($1, $2, $3, now())
			 ON CONFLICT (config_key) DO UPDATE
			   SET config_value = EXCLUDED.config_value,
			       updated_by = EXCLUDED.updated_by,
			       updated_at = now()`,
			kv.Key, kv.Value, updatedBy); execErr != nil {
			return execErr
		}
	}
	return tx.Commit(ctx)
}

// ─────────────────────────────────────────────────────────────
// 患者写操作（T057：创建患者 / 分配团队 / 批量绑定）
//
// phone_hash 唯一键（idx_patients_phone_hash）：先查重 + INSERT 兜底 unique violation → ErrPatientExists。
// patient_id 生成：P + 年份 + 12 位随机 hex（VARCHAR(32) 内，规避并发序号竞争）。
// ─────────────────────────────────────────────────────────────

// newPatientID 生成患者 ID：P + 当前年份 + 12 位随机 hex
func newPatientID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "P" + time.Now().Format("2006") + hex.EncodeToString(buf), nil
}

// CreatePatient 创建患者（phone_hash 查重 → INSERT → 回读 join 行）。
// T069 扩展：PhoneEnc/PhoneHash 为 nil 表示微信-only 无手机号用户，
// 查重步骤跳过（phone_hash IS NULL 不参与 uk 冲突语义，INSERT 直接写 NULL）。
func (s *PGStore) CreatePatient(ctx context.Context, in PatientInput) (*PatientRow, error) {
	// phone_hash 非空时走原有查重（T057 旧语义）；为 nil（微信-only）跳过查重
	if in.PhoneHash != nil {
		var taken bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM patients WHERE phone_hash = $1)`, *in.PhoneHash).Scan(&taken); err != nil {
			return nil, err
		}
		if taken {
			return nil, ErrPatientExists
		}
	}
	patientID, err := newPatientID()
	if err != nil {
		return nil, err
	}
	_, execErr := s.pool.Exec(ctx,
		`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, diagnosis, cobb_angle,
		                       team_id, primary_doctor_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'active')`,
		patientID, in.Name, in.PhoneEnc, in.PhoneHash, in.Gender, in.Age, in.Diagnosis, in.CobbAngle,
		in.TeamID, in.DoctorID)
	if execErr != nil {
		// 并发兜底：unique violation(phone_hash) → ErrPatientExists
		var pgErr *pgconn.PgError
		if errors.As(execErr, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrPatientExists
		}
		return nil, execErr
	}
	return s.GetPatient(ctx, patientID)
}

// AssignPatientTeam 分配/更改患者团队（幂等：同 teamId no-op，不变更 updated_at）
func (s *PGStore) AssignPatientTeam(ctx context.Context, patientID, teamID string) (*PatientRow, error) {
	existing, err := s.GetPatient(ctx, patientID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrPatientNotFound
	}
	// 幂等：当前已绑定同一 teamId → 直接返回，不更新 updated_at
	if existing.TeamID != nil && *existing.TeamID == teamID {
		return existing, nil
	}
	if _, err = s.pool.Exec(ctx,
		`UPDATE patients SET team_id = $2, updated_at = now() WHERE patient_id = $1`,
		patientID, teamID); err != nil {
		return nil, err
	}
	return s.GetPatient(ctx, patientID)
}

// BatchBindPatients 批量绑定患者到团队（逐条 UPDATE；部分失败不回滚，HTTP 仍 200）
func (s *PGStore) BatchBindPatients(ctx context.Context, patientIDs []string, teamID string) (*BatchBindResult, error) {
	result := &BatchBindResult{}
	for _, pid := range patientIDs {
		tag, err := s.pool.Exec(ctx,
			`UPDATE patients SET team_id = $2, updated_at = now() WHERE patient_id = $1`,
			pid, teamID)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() > 0 {
			result.Success = append(result.Success, pid)
		} else {
			result.Failed = append(result.Failed, BatchBindFailure{
				PatientID: pid,
				Reason:    "patient not found",
			})
		}
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────
// T059 团队 / 成员写操作（reject-if-referenced 删除策略）
//
// 契约：docs/tasks/ella/T059-团队管理测试规格.md
// sentinel 映射（handler 层）：
//   - ErrTeamNotFound  → 404
//   - ErrTeamNameExists → 409
//   - ErrLeaderNotFound → 400
//   - ErrMemberNotFound → 404
//   - ErrMemberInTeam   → 409
//   - ErrTeamInUse{PatientCount, MemberCount} → 409（携带计数）
// ─────────────────────────────────────────────────────────────

// teamDetailSelect teams LEFT JOIN doctors 负责人姓名投影（T059 写功能返回）
// 患者数列 T371-B1 起走 teamPatientCountExpr 实时计数（列表与详情同一条表达式，不漂口径）
const teamDetailSelect = `
SELECT t.team_id, t.name, COALESCE(t.leader, ''), COALESCE(d.name, ''),
       t.member_count, ` + teamPatientCountExpr + ` AS patient_count,
       COALESCE(t.description, ''), t.status, t.created_at
FROM teams t
LEFT JOIN doctors d ON d.doctor_id = t.leader
WHERE t.team_id = $1`

// getTeamDetail 回读团队详情（含 leader_name join）；不存在返回 ErrTeamNotFound
func (s *PGStore) getTeamDetail(ctx context.Context, teamID string) (*TeamDetailRow, error) {
	row := s.pool.QueryRow(ctx, teamDetailSelect, teamID)
	var t TeamDetailRow
	err := row.Scan(&t.TeamID, &t.Name, &t.Leader, &t.LeaderName,
		&t.MemberCount, &t.PatientCount, &t.Description, &t.Status, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTeamNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTeam 团队单条读（T333：GET /api/v1/teams/:teamId）
// 复用写端点回读的同一 SQL，避免详情字段两处口径漂移；不存在返回 ErrTeamNotFound。
func (s *PGStore) GetTeam(ctx context.Context, teamID string) (*TeamDetailRow, error) {
	return s.getTeamDetail(ctx, teamID)
}

// newTeamID 生成团队 ID：TEAM + 年份后两位 + 随机 hex（VARCHAR(32) 内，规避并发序号竞争）
func newTeamID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "TEAM" + time.Now().Format("06") + hex.EncodeToString(buf), nil
}

// CreateTeam 创建团队（name 唯一性 + leader 存在性校验 → INSERT → 回读 join doctors.leader_name）
func (s *PGStore) CreateTeam(ctx context.Context, in TeamInput) (*TeamDetailRow, error) {
	// 1. name 查重
	var nameTaken bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM teams WHERE name = $1)`, in.Name).Scan(&nameTaken); err != nil {
		return nil, err
	}
	if nameTaken {
		return nil, ErrTeamNameExists
	}
	// 2. leader 存在性校验
	var leaderExists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM doctors WHERE doctor_id = $1)`, in.Leader).Scan(&leaderExists); err != nil {
		return nil, err
	}
	if !leaderExists {
		return nil, ErrLeaderNotFound
	}
	// 3. 生成 team_id + INSERT
	teamID, err := newTeamID()
	if err != nil {
		return nil, err
	}
	if _, err = s.pool.Exec(ctx,
		`INSERT INTO teams (team_id, name, leader, description, status) VALUES ($1, $2, $3, $4, 'active')`,
		teamID, in.Name, in.Leader, in.Description); err != nil {
		return nil, err
	}
	// 4. 回读 join doctors.leader_name
	return s.getTeamDetail(ctx, teamID)
}

// UpdateTeam 编辑团队（团队存在 + name 查重排除自身 + leader 校验 + UPDATE）
func (s *PGStore) UpdateTeam(ctx context.Context, teamID string, in TeamInput) (*TeamDetailRow, error) {
	// 1. name 查重排除自身
	var nameTaken bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM teams WHERE name = $1 AND team_id <> $2)`, in.Name, teamID).Scan(&nameTaken); err != nil {
		return nil, err
	}
	if nameTaken {
		return nil, ErrTeamNameExists
	}
	// 2. leader 存在性校验
	var leaderExists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM doctors WHERE doctor_id = $1)`, in.Leader).Scan(&leaderExists); err != nil {
		return nil, err
	}
	if !leaderExists {
		return nil, ErrLeaderNotFound
	}
	// 3. UPDATE（0 行 → 团队不存在）
	tag, err := s.pool.Exec(ctx,
		`UPDATE teams SET name = $2, leader = $3, description = $4 WHERE team_id = $1`,
		teamID, in.Name, in.Leader, in.Description)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrTeamNotFound
	}
	// 4. 回读
	return s.getTeamDetail(ctx, teamID)
}

// DeleteTeam 删除团队（拒绝被引用：patients.team_id / doctors.team_id / technicians.team_id 命中 → ErrTeamInUse）
func (s *PGStore) DeleteTeam(ctx context.Context, teamID string) error {
	// 1. 团队存在性
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM teams WHERE team_id = $1)`, teamID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrTeamNotFound
	}
	// 2. 统计引用计数（patients + doctors + technicians）
	var patientCount, doctorCount, techCount int
	if err := s.pool.QueryRow(ctx,
		`SELECT (SELECT COUNT(*) FROM patients WHERE team_id = $1),
		        (SELECT COUNT(*) FROM doctors WHERE team_id = $1),
		        (SELECT COUNT(*) FROM technicians WHERE team_id = $1)`,
		teamID).Scan(&patientCount, &doctorCount, &techCount); err != nil {
		return err
	}
	memberCount := doctorCount + techCount
	if patientCount > 0 || memberCount > 0 {
		return &ErrTeamInUse{PatientCount: patientCount, MemberCount: memberCount}
	}
	// 3. DELETE
	_, err := s.pool.Exec(ctx, `DELETE FROM teams WHERE team_id = $1`, teamID)
	return err
}

// AddTeamMember 添加成员（doctor/technician.team_id 置为本 teamId；重复 → ErrMemberInTeam）
func (s *PGStore) AddTeamMember(ctx context.Context, teamID string, in MemberInput) (*TeamMemberRow, error) {
	// 1. 团队存在性
	var teamOK bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM teams WHERE team_id = $1)`, teamID).Scan(&teamOK); err != nil {
		return nil, err
	}
	if !teamOK {
		return nil, ErrTeamNotFound
	}
	// 2. 按 memberType 查成员 + 更新 team_id
	if in.MemberType == "doctor" {
		return s.addDoctorToTeam(ctx, teamID, in)
	}
	return s.addTechToTeam(ctx, teamID, in)
}

// addDoctorToTeam 添加医生到团队（重复 → ErrMemberInTeam；memberId 查无 → ErrMemberNotFound）
func (s *PGStore) addDoctorToTeam(ctx context.Context, teamID string, in MemberInput) (*TeamMemberRow, error) {
	var name, title, dept string
	var currentTeamID *string
	var phoneEnc []byte
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT name, COALESCE(title, ''), COALESCE(department, ''), team_id, phone_enc, status
		 FROM doctors WHERE doctor_id = $1`, in.MemberID).
		Scan(&name, &title, &dept, &currentTeamID, &phoneEnc, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMemberNotFound
	}
	if err != nil {
		return nil, err
	}
	// 已在本团队
	if currentTeamID != nil && *currentTeamID == teamID {
		return nil, ErrMemberInTeam
	}
	// UPDATE team_id + 可选 title
	if in.Role != "" {
		_, err = s.pool.Exec(ctx,
			`UPDATE doctors SET team_id = $2, title = $3 WHERE doctor_id = $1`,
			in.MemberID, teamID, in.Role)
	} else {
		_, err = s.pool.Exec(ctx,
			`UPDATE doctors SET team_id = $2 WHERE doctor_id = $1`,
			in.MemberID, teamID)
	}
	if err != nil {
		return nil, err
	}
	role := title
	if in.Role != "" {
		role = in.Role
	}
	var patientCount int
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM patients WHERE primary_doctor_id = $1`, in.MemberID).Scan(&patientCount)
	return &TeamMemberRow{
		MemberID:     in.MemberID,
		MemberType:   "doctor",
		Name:         name,
		Role:         role,
		Title:        dept,
		PhoneEnc:     phoneEnc,
		PatientCount: patientCount,
		JoinTime:     time.Now().UTC(),
		Status:       status,
	}, nil
}

// addTechToTeam 添加技师到团队（重复 → ErrMemberInTeam；techId 查无 → ErrMemberNotFound）
func (s *PGStore) addTechToTeam(ctx context.Context, teamID string, in MemberInput) (*TeamMemberRow, error) {
	var name string
	var currentTeamID *string
	var phoneEnc []byte
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT name, team_id, phone_enc, status FROM technicians WHERE tech_id = $1`, in.MemberID).
		Scan(&name, &currentTeamID, &phoneEnc, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMemberNotFound
	}
	if err != nil {
		return nil, err
	}
	if currentTeamID != nil && *currentTeamID == teamID {
		return nil, ErrMemberInTeam
	}
	if _, err = s.pool.Exec(ctx,
		`UPDATE technicians SET team_id = $2 WHERE tech_id = $1`, in.MemberID, teamID); err != nil {
		return nil, err
	}
	return &TeamMemberRow{
		MemberID:   in.MemberID,
		MemberType: "technician",
		Name:       name,
		PhoneEnc:   phoneEnc,
		JoinTime:   time.Now().UTC(),
		Status:     status,
	}, nil
}

// UpdateTeamMember 编辑成员（更新 doctor.title；member 不属本团队 → ErrMemberNotFound）
func (s *PGStore) UpdateTeamMember(ctx context.Context, teamID, memberID string, in MemberInput) (*TeamMemberRow, error) {
	// 1. 团队存在性
	var teamOK bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM teams WHERE team_id = $1)`, teamID).Scan(&teamOK); err != nil {
		return nil, err
	}
	if !teamOK {
		return nil, ErrTeamNotFound
	}
	// 2. 按 memberType 更新
	if in.MemberType == "doctor" {
		return s.updateDoctorMember(ctx, teamID, memberID, in)
	}
	return s.updateTechMember(ctx, teamID, memberID, in)
}

// updateDoctorMember 编辑医生成员（须属本团队；role 可选更新 doctor.title）
func (s *PGStore) updateDoctorMember(ctx context.Context, teamID, memberID string, in MemberInput) (*TeamMemberRow, error) {
	var name, title, dept string
	var phoneEnc []byte
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT name, COALESCE(title, ''), COALESCE(department, ''), phone_enc, status
		 FROM doctors WHERE doctor_id = $1 AND team_id = $2`, memberID, teamID).
		Scan(&name, &title, &dept, &phoneEnc, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMemberNotFound
	}
	if err != nil {
		return nil, err
	}
	if in.Role != "" {
		if _, err = s.pool.Exec(ctx,
			`UPDATE doctors SET title = $2 WHERE doctor_id = $1`, memberID, in.Role); err != nil {
			return nil, err
		}
		title = in.Role
	}
	var patientCount int
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM patients WHERE primary_doctor_id = $1`, memberID).Scan(&patientCount)
	return &TeamMemberRow{
		MemberID:     memberID,
		MemberType:   "doctor",
		Name:         name,
		Role:         title,
		Title:        dept,
		PhoneEnc:     phoneEnc,
		PatientCount: patientCount,
		JoinTime:     time.Now().UTC(),
		Status:       status,
	}, nil
}

// updateTechMember 编辑技师成员（technician 无 title 字段，role 忽略）
func (s *PGStore) updateTechMember(ctx context.Context, teamID, memberID string, _ MemberInput) (*TeamMemberRow, error) {
	var name string
	var phoneEnc []byte
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT name, phone_enc, status FROM technicians WHERE tech_id = $1 AND team_id = $2`, memberID, teamID).
		Scan(&name, &phoneEnc, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMemberNotFound
	}
	if err != nil {
		return nil, err
	}
	return &TeamMemberRow{
		MemberID:   memberID,
		MemberType: "technician",
		Name:       name,
		PhoneEnc:   phoneEnc,
		JoinTime:   time.Now().UTC(),
		Status:     status,
	}, nil
}

// RemoveTeamMember 移除成员（doctor/technician.team_id 置 NULL；幂等：已 NULL no-op）
func (s *PGStore) RemoveTeamMember(ctx context.Context, teamID, memberID, memberType string) error {
	// 1. 团队存在性
	var teamOK bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM teams WHERE team_id = $1)`, teamID).Scan(&teamOK); err != nil {
		return err
	}
	if !teamOK {
		return ErrTeamNotFound
	}
	// 2. 置 NULL（幂等：已 NULL 或 member 不属本团队 → 0 行 no-op）
	if memberType == "doctor" {
		_, err := s.pool.Exec(ctx,
			`UPDATE doctors SET team_id = NULL WHERE doctor_id = $1 AND team_id = $2`, memberID, teamID)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE technicians SET team_id = NULL WHERE tech_id = $1 AND team_id = $2`, memberID, teamID)
	return err
}

// ─────────────────────────────────────────────────────────────
// T130 复查记录
// ─────────────────────────────────────────────────────────────

const reviewColumns = `review_id, patient_id, review_date, review_type, findings,
	next_review_date, doctor_id, report_file_id, created_at, updated_at`

func scanReviewRecord(row pgx.Row) (*ReviewRecordRow, error) {
	var r ReviewRecordRow
	err := row.Scan(&r.ReviewID, &r.PatientID, &r.ReviewDate, &r.ReviewType, &r.Findings,
		&r.NextReviewDate, &r.DoctorID, &r.ReportFileID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateReviewRecord 创建复查记录（review_id 由调用方生成）
func (s *PGStore) CreateReviewRecord(ctx context.Context, row ReviewRecordRow) (*ReviewRecordRow, error) {
	// 患者存在性校验
	var patientOK bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM patients WHERE patient_id = $1)`, row.PatientID).Scan(&patientOK); err != nil {
		return nil, err
	}
	if !patientOK {
		return nil, ErrReviewPatientNotFound
	}
	query := `INSERT INTO review_records (` + reviewColumns + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now(),now())
		RETURNING ` + reviewColumns
	r, err := scanReviewRecord(s.pool.QueryRow(ctx, query,
		row.ReviewID, row.PatientID, row.ReviewDate, row.ReviewType, row.Findings,
		row.NextReviewDate, row.DoctorID, row.ReportFileID))
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ListReviewRecordsByPatient 按患者列出复查记录（review_date 倒序）
func (s *PGStore) ListReviewRecordsByPatient(ctx context.Context, patientID string) ([]ReviewRecordRow, error) {
	query := `SELECT ` + reviewColumns + ` FROM review_records
		WHERE patient_id = $1 ORDER BY review_date DESC, created_at DESC`
	rows, err := s.pool.Query(ctx, query, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ReviewRecordRow
	for rows.Next() {
		r, err := scanReviewRecord(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *r)
	}
	return list, rows.Err()
}

// GetReviewRecord 按 ID 查复查记录
func (s *PGStore) GetReviewRecord(ctx context.Context, reviewID string) (*ReviewRecordRow, error) {
	query := `SELECT ` + reviewColumns + ` FROM review_records WHERE review_id = $1`
	r, err := scanReviewRecord(s.pool.QueryRow(ctx, query, reviewID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrReviewRecordNotFound
		}
		return nil, err
	}
	return r, nil
}

// ─────────────────────────────────────────────────────────────
// T135 复查报告模板
// ─────────────────────────────────────────────────────────────

const reviewTemplateColumns = `template_id, template_group_id, name, version, file_id,
	status, uploaded_by, created_at, updated_at`

func scanReviewTemplate(row pgx.Row) (*ReviewTemplateRow, error) {
	var r ReviewTemplateRow
	err := row.Scan(&r.TemplateID, &r.TemplateGroupID, &r.Name, &r.Version, &r.FileID,
		&r.Status, &r.UploadedBy, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateReviewTemplateVersion 事务创建/替换模板版本（T135）。
//
//	groupID==""  → 新建组：name 存在返回 ErrTemplateNameExists，否则 version=1。
//	groupID!=""  → 版本替换：组不存在返回 ErrTemplateNotFound；
//	                新版本 = MAX(version)+1，同组旧 active 置 retired（非删除）。
//
// 已上传的填写报告(review_records)只引用各自 file_id，与模板文件互不影响，
// 版本替换不改动历史文件对象，「已填报告不受影响」由数据模型天然保证。
func (s *PGStore) CreateReviewTemplateVersion(ctx context.Context, templateGroupID, name, fileID, uploadedBy string) (*ReviewTemplateRow, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var groupID string
	if templateGroupID == "" {
		// 新建组：name 查重
		var nameExists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM review_templates WHERE name = $1)`, name).Scan(&nameExists); err != nil {
			return nil, err
		}
		if nameExists {
			return nil, ErrTemplateNameExists
		}
		groupID, err = newTemplateGroupID()
		if err != nil {
			return nil, err
		}
	} else {
		// 版本替换：确认组存在（按组 or 按名）
		var existed bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM review_templates WHERE template_group_id = $1 OR name = $1)`,
			templateGroupID).Scan(&existed); err != nil {
			return nil, err
		}
		if !existed {
			return nil, ErrTemplateNotFound
		}
		groupID = templateGroupID
		// 同组旧 active 置 retired（非物理删除）
		if _, err := tx.Exec(ctx,
			`UPDATE review_templates SET status = 'retired', updated_at = now()
			 WHERE template_group_id = $1 AND status = 'active'`, groupID); err != nil {
			return nil, err
		}
	}

	// 新版本号 = 组内 max(version)+1（无历史则 1）
	var nextVersion int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(version),0) + 1 FROM review_templates WHERE template_group_id = $1`,
		groupID).Scan(&nextVersion); err != nil {
		return nil, err
	}

	templateID, err := newTemplateID()
	if err != nil {
		return nil, err
	}

	row, scanErr := scanReviewTemplate(tx.QueryRow(ctx,
		`INSERT INTO review_templates (template_id, template_group_id, name, version, file_id, status, uploaded_by, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,'active',$6,now(),now())
		 RETURNING `+reviewTemplateColumns,
		templateID, groupID, name, nextVersion, fileID, uploadedBy))
	if scanErr != nil {
		return nil, scanErr
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return row, nil
}

// newTemplateID 生成模板版本 ID（TPL + 12 位随机 hex，VARCHAR(32) 内）
func newTemplateID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "TPL_" + hex.EncodeToString(buf), nil
}

// newTemplateGroupID 生成模板组 ID（GRP + 12 位随机 hex，VARCHAR(32) 内）
func newTemplateGroupID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "GRP_" + hex.EncodeToString(buf), nil
}

// ListActiveReviewTemplates 每模板组当前 active 版本（name 升序）。
func (s *PGStore) ListActiveReviewTemplates(ctx context.Context) ([]ReviewTemplateRow, error) {
	query := `SELECT ` + reviewTemplateColumns + ` FROM review_templates
		WHERE status = 'active' ORDER BY name, version DESC`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ReviewTemplateRow
	for rows.Next() {
		r, err := scanReviewTemplate(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *r)
	}
	return list, rows.Err()
}

// GetReviewTemplateGroup 按组查当前 active 版本；无匹配返回 ErrTemplateNotFound。
func (s *PGStore) GetReviewTemplateGroup(ctx context.Context, groupID string) (*ReviewTemplateRow, error) {
	r, err := scanReviewTemplate(s.pool.QueryRow(ctx,
		`SELECT `+reviewTemplateColumns+` FROM review_templates
		 WHERE template_group_id = $1 AND status = 'active'
		 ORDER BY version DESC LIMIT 1`, groupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTemplateNotFound
		}
		return nil, err
	}
	return r, nil
}
