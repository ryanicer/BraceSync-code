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

// ─────────────────────────────────────────────────────────────
// 患者（管理端只读，含 teams/doctors 姓名 join）
// ─────────────────────────────────────────────────────────────

const patientSelect = `
SELECT p.patient_id, p.name, p.gender, p.age, p.diagnosis, p.cobb_angle,
       p.device_id, p.team_id, p.primary_doctor_id, p.status, p.created_at, p.updated_at,
       t.name AS team_name, d.name AS doctor_name
FROM patients p
LEFT JOIN teams t ON t.team_id = p.team_id
LEFT JOIN doctors d ON d.doctor_id = p.primary_doctor_id`

// patientWhere 组装筛选 WHERE 与参数（keyword=姓名/患者ID ILIKE；teamId 精确）
func patientWhere(f PatientFilter) (string, []any) {
	var conds []string
	var args []any
	if f.Keyword != "" {
		args = append(args, "%"+f.Keyword+"%")
		conds = append(conds, fmt.Sprintf(`(p.name ILIKE $%[1]d OR p.patient_id ILIKE $%[1]d)`, len(args)))
	}
	if f.TeamID != "" {
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
		&p.TeamName, &p.DoctorName)
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

// ─────────────────────────────────────────────────────────────
// 团队 / 医生
// ─────────────────────────────────────────────────────────────

// ListTeams 团队概要（member_count/patient_count 为 teams 表维护列）
func (s *PGStore) ListTeams(ctx context.Context) ([]TeamRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT team_id, name, member_count, patient_count FROM teams ORDER BY team_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []TeamRow
	for rows.Next() {
		var t TeamRow
		if scanErr := rows.Scan(&t.TeamID, &t.Name, &t.MemberCount, &t.PatientCount); scanErr != nil {
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

const doctorSelect = `
SELECT d.doctor_id, d.name, d.title, d.department, d.team_id, d.phone_enc, d.status,
       COUNT(p.patient_id) AS patient_count
FROM doctors d
LEFT JOIN patients p ON p.primary_doctor_id = d.doctor_id`

func (s *PGStore) scanDoctors(rows pgx.Rows) ([]DoctorRow, error) {
	defer rows.Close()
	var list []DoctorRow
	for rows.Next() {
		var d DoctorRow
		if err := rows.Scan(&d.DoctorID, &d.Name, &d.Title, &d.Department, &d.TeamID, &d.PhoneEnc, &d.Status, &d.PatientCount); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

// ListDoctors 全量医生（含患者计数）
func (s *PGStore) ListDoctors(ctx context.Context) ([]DoctorRow, error) {
	rows, err := s.pool.Query(ctx, doctorSelect+` GROUP BY d.doctor_id ORDER BY d.doctor_id`)
	if err != nil {
		return nil, err
	}
	return s.scanDoctors(rows)
}

// ListDoctorsByTeam 团队内医生（团队成员明细）
func (s *PGStore) ListDoctorsByTeam(ctx context.Context, teamID string) ([]DoctorRow, error) {
	rows, err := s.pool.Query(ctx, doctorSelect+` WHERE d.team_id = $1 GROUP BY d.doctor_id ORDER BY d.doctor_id`, teamID)
	if err != nil {
		return nil, err
	}
	return s.scanDoctors(rows)
}

// ─────────────────────────────────────────────────────────────
// 技师
// ─────────────────────────────────────────────────────────────

const techColumns = `tech_id, name, phone_enc, phone_hash, team_id, install_count, status, auth_status`

func scanTech(row pgx.Row) (*TechnicianRow, error) {
	var t TechnicianRow
	err := row.Scan(&t.TechID, &t.Name, &t.PhoneEnc, &t.PhoneHash, &t.TeamID, &t.InstallCount, &t.Status, &t.AuthStatus)
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
		`SELECT `+techColumns+` FROM technicians ORDER BY created_at DESC, tech_id LIMIT $1 OFFSET $2`,
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
		`SELECT `+techColumns+` FROM technicians WHERE team_id = $1 ORDER BY tech_id`, teamID)
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
	row := s.pool.QueryRow(ctx, `SELECT `+techColumns+` FROM technicians WHERE tech_id = $1`, techID)
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

// ListFeedbacks 反馈列表（keyword=内容/患者ID/患者姓名 ILIKE；按提交时间倒序，上限 200）
func (s *PGStore) ListFeedbacks(ctx context.Context, keyword string) ([]FeedbackRow, error) {
	query := `SELECT f.feedback_id, f.patient_id, f.type, f.content, f.submit_time,
	                 f.handler, f.reply_content, f.reply_time, f.status
	          FROM feedbacks f`
	var args []any
	if keyword != "" {
		query += ` LEFT JOIN patients p ON p.patient_id = f.patient_id`
		args = append(args, "%"+keyword+"%")
		query += fmt.Sprintf(` WHERE (f.content ILIKE $%[1]d OR f.patient_id ILIKE $%[1]d OR p.name ILIKE $%[1]d)`, 1)
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

// ProcessFeedback 回复落库 + 标记处理（resolved 不回退）；返回反馈是否存在
func (s *PGStore) ProcessFeedback(ctx context.Context, feedbackID int64, handlerID string, replyContent *string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE feedbacks
		 SET handler = $2, reply_content = $3, reply_time = now(),
		     status = CASE WHEN status = 'resolved' THEN 'resolved' ELSE 'replied' END
		 WHERE feedback_id = $1`,
		feedbackID, handlerID, replyContent)
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

// ListFeelingLogs 患者感受日志（按日期倒序）
func (s *PGStore) ListFeelingLogs(ctx context.Context, patientID string) ([]FeelingLogRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT log_id, patient_id, log_date, comfort_score, discomfort_areas, notes, reply_content, reply_time
		 FROM feeling_logs WHERE patient_id = $1 ORDER BY log_date DESC, log_id DESC`, patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []FeelingLogRow
	for rows.Next() {
		var f FeelingLogRow
		if scanErr := rows.Scan(&f.LogID, &f.PatientID, &f.LogDate, &f.ComfortScore,
			&f.DiscomfortAreas, &f.Notes, &f.ReplyContent, &f.ReplyTime); scanErr != nil {
			return nil, scanErr
		}
		list = append(list, f)
	}
	return list, rows.Err()
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

// CreatePatient 创建患者（phone_hash 查重 → INSERT → 回读 join 行）
func (s *PGStore) CreatePatient(ctx context.Context, in PatientInput) (*PatientRow, error) {
	var taken bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM patients WHERE phone_hash = $1)`, in.PhoneHash).Scan(&taken); err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrPatientExists
	}
	patientID, err := newPatientID()
	if err != nil {
		return nil, err
	}
	_, execErr := s.pool.Exec(ctx,
		`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, diagnosis, cobb_angle,
		                       device_id, team_id, primary_doctor_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'active')`,
		patientID, in.Name, in.PhoneEnc, in.PhoneHash, in.Gender, in.Age, in.Diagnosis, in.CobbAngle,
		in.DeviceID, in.TeamID, in.DoctorID)
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
const teamDetailSelect = `
SELECT t.team_id, t.name, COALESCE(t.leader, ''), COALESCE(d.name, ''),
       t.member_count, t.patient_count, COALESCE(t.description, ''), t.status, t.created_at
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
