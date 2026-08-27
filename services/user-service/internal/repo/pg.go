// Package repo — PGStore：Store 接口的 PostgreSQL（pgx v5）实现
//
// 全部 SQL 使用 $n 占位符参数化；动态筛选仅拼接占位符序号，不拼接用户输入。
package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
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
// 患者写操作（T057 stub — 实现方转绿阶段补 SQL）
//
// 当前为占位实现，返回 not implemented 错误。仅保证 PGStore 满足扩展后的
// Store 接口编译；运行期不被 KNOWN_RED 单测触达（单测走 fakeStore）。
// ─────────────────────────────────────────────────────────────

// CreatePatient 创建患者（含重复判重）
func (s *PGStore) CreatePatient(ctx context.Context, in PatientInput) (*PatientRow, error) {
	return nil, errors.New("not implemented: T057 CreatePatient")
}

// AssignPatientTeam 分配/更改患者团队（幂等）
func (s *PGStore) AssignPatientTeam(ctx context.Context, patientID, teamID string) (*PatientRow, error) {
	return nil, errors.New("not implemented: T057 AssignPatientTeam")
}

// BatchBindPatients 批量绑定患者到团队（部分失败不回滚）
func (s *PGStore) BatchBindPatients(ctx context.Context, patientIDs []string, teamID string) (*BatchBindResult, error) {
	return nil, errors.New("not implemented: T057 BatchBindPatients")
}
