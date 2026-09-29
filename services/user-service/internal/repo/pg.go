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

// GetAdminByPhoneHash T487：/auth/login 双凭证的第二支——按 SHA-256(明文手机号) hex 命中 admins。
// 不存在返回 (nil, nil)（与同族 GetAdminByUsername / GetTechByPhoneHash 一致，handler 靠它统一 401 防枚举）。
// 投影列与 GetAdminByUsername 逐字相同 ⇒ 两支拿到的是同一个行结构，后续 bcrypt/status/重哈希链路不分叉。
// phone_hash 走 000032 的部分唯一索引 uk_admins_phone_hash；NULL（管理员与未录号的存量账号）不在索引内，
// 因此传 64 位 hex 永远命中不到它们——空手机号不可能被当成凭据。
func (s *PGStore) GetAdminByPhoneHash(ctx context.Context, phoneHash string) (*AdminRow, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT admin_id, username, name, password_hash, role_id, status FROM admins WHERE phone_hash = $1`, phoneHash)
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

// GetAdminByID T487 自助改密用：按主键取当前哈希与状态（改密要先验旧密码，必须读回 password_hash）。
// 不存在返回 (nil, nil)（与同族一致，handler 据此统一回「账号不存在」而不是 500）。
// 投影列与 GetAdminByUsername / GetAdminByPhoneHash 逐字相同 ⇒ 三条登录/改密读法拿到同一个行结构。
// 🔴 只按 admin_id 查：该值来自网关注入的 X-User-Id（jwtAuth 从 JWT claims 重签，外部同名头已被删除）。
func (s *PGStore) GetAdminByID(ctx context.Context, adminID string) (*AdminRow, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT admin_id, username, name, password_hash, role_id, status FROM admins WHERE admin_id = $1`, adminID)
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

// UpdateAdminPasswordHash 更新 admins 密码哈希（渐进式重哈希：T040；T487 自助改密同用这一条）
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
	// password_hash 自 000005 起可空；直接扫进 string 会让 NULL 行报错（T483），
	// 同 team_id 一样走指针再抹平为空串。
	var pwdHash *string
	err := row.Scan(&t.TechID, &t.Name, &pwdHash, &teamID, &t.Status, &t.AuthStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pwdHash != nil {
		t.PasswordHash = *pwdHash
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
	// password_hash 自 000005 起可空；直接扫进 string 会让 NULL 行报普通错误
	// （不是 ErrNoRows，上面的收口接不住），上抛后被调用方的错误分支吃掉成 500（T484）。
	var pwdHash *string
	err := row.Scan(&p.PatientID, &p.Name, &pwdHash, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pwdHash != nil {
		p.PasswordHash = *pwdHash
	}
	return &p, nil
}

// GetPatientByWXOpenID T069：按微信 openid 查患者登录行；不存在返回 (nil, nil)
func (s *PGStore) GetPatientByWXOpenID(ctx context.Context, openid string) (*PatientLoginRow, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT patient_id, name, password_hash, status
		 FROM patients WHERE wx_openid = $1`, openid)
	var p PatientLoginRow
	// 同 GetPatientByPhoneHash：可空口令列走指针再抹平为空串（T484）
	var pwdHash *string
	err := row.Scan(&p.PatientID, &p.Name, &pwdHash, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pwdHash != nil {
		p.PasswordHash = *pwdHash
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

// SetPatientPassword T477：admin 通道给患者设登录口令，只写 bcrypt 哈希列。
// 患者端手机号+密码登录（T037，handler 侧读 patients.password_hash）此前在本仓库
// 没有任何 Go 写点：API 建档的 INSERT 不带该列，落库恒为 NULL，非 seed 患者登录必 401。
// 口令明文由 handler 生成、只在 HTTP 响应里一次性返回，不进库、不进日志、不进审计。
// 不命中返回 ErrPatientNotFound（与 UnbindWechat / UpdatePatientPhone 同口径）。
func (s *PGStore) SetPatientPassword(ctx context.Context, patientID, passwordHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE patients SET password_hash = $2, updated_at = NOW() WHERE patient_id = $1`,
		patientID, passwordHash)
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
// T450-②b 起同一函数还承接 admin 通道的显式置空（in.ClearColumns ⇒ SET col = NULL）。
func (s *PGStore) UpdatePatientProfile(ctx context.Context, patientID string, in PatientProfileUpdate) error {
	sqlText, args, err := buildPatientProfileUpdateSQL(patientID, in)
	if err != nil {
		return err
	}
	if sqlText == "" { // 无白名单字段也无置空列：handler 已先判 400，此处兜底不空跑 UPDATE
		return nil
	}
	tag, err := s.pool.Exec(ctx, sqlText, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPatientNotFound
	}
	return nil
}

// buildPatientProfileUpdateSQL 动态 SET 的纯函数（无库依赖 ⇒ 单测能逐字看 SQL 与占位符编号）。
// 置空列走字面 NULL、不占参数位 ⇒ 参数序号只由 add 推进，这正是「写死占位符」类失真唯一能被抓住的形状。
// 列名一律取自 PatientProfileClearColumns 的值集：请求体里的字符串永不进 SQL 文本，
// 表外列名（含注入串）在此报错返回，不静默丢弃。
func buildPatientProfileUpdateSQL(patientID string, in PatientProfileUpdate) (string, []any, error) {
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
	for _, col := range in.ClearColumns {
		if !isPatientProfileClearColumn(col) {
			return "", nil, fmt.Errorf("column is not clearable: %s", col)
		}
		sets = append(sets, col+" = NULL")
	}
	if len(sets) == 1 { // 仅 updated_at：无白名单字段可写（handler 已先拒 400，此处兜底防空 SET）
		return "", nil, nil
	}
	args = append(args, patientID)
	return fmt.Sprintf(`UPDATE patients SET %s WHERE patient_id = $%d`, strings.Join(sets, ", "), len(args)), args, nil
}

// isPatientProfileClearColumn 列名是否落在 PatientProfileClearColumns 的值集内。
func isPatientProfileClearColumn(col string) bool {
	for _, v := range PatientProfileClearColumns {
		if v == col {
			return true
		}
	}
	return false
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

// teamMemberCountExpr T385：团队「成员数」实时按 doctors.team_id / technicians.team_id 数，不再读 teams.member_count。
// 该列与 patient_count 同族：全仓无应用写路径（CreateTeam 的 INSERT 不带它，UpdateTeam 只改 name/leader/description），
// 现网读到的是建库初值 —— seed 自身即不自洽（三行写 3/2/2，同份 seed 挂团队的医生+技师只有 6 人）。
// 列表 / 详情 / 统计卡三处读点复用同一条表达式（T385 立的就是这条复用，见下面两个 select 常量）。
// 当时连带写下「与成员明细两条腿、DeleteTeam 引用计数同源」—— 那句话到 T429 已不成立，
// 收口后这三者数的是两个量，理由见下方 T429 段（不是把复用取消，而是把同源范围收窄到三处读点）。
//
// T429（Boss 09-27 17:3x 拍「禁用的不算总数」）：两条腿各加 status='enabled'，本表达式承载的是
// 「在职成员数」这一个维度，列表 / 详情 / 统计卡三处读点因复用而同时改口，不存在半张页改半张页不改。
// 同源关系按新口径重述（不是取消）：
//   - 成员明细两条腿（ListDoctorsByTeam / ListTechniciansByTeam）**不看** status ⇒ 面板仍列禁用者，
//     因为「编辑 / 移除」要能落到禁用行上；页面 teams/index.vue:96-102 本就有一列「状态」标签输出
//     启用/禁用，所以「成员数 3 / 面板 4 行」在同一页是可解释的，不是 T385 修的那种两数打脸。
//     新恒等式：成员数 == 明细里 status=enabled 的行数（集成用例钉住）。
//   - DeleteTeam 的引用计数**不看** status ⇒ 禁用者仍占着 team_id 外键，若守卫跟着收口，
//     一个只剩禁用成员的团队会被判「无引用」删掉，把人挂进悬空状态。守卫数的是「引用数」，
//     与本表达式的「在职人数」是两个量，故意不同源。
//
// 患者数一列（teamPatientCountExpr）不在本次裁定范围内，一字未动。
const teamMemberCountExpr = `(SELECT COUNT(*) FROM doctors dm WHERE dm.team_id = t.team_id AND dm.status = 'enabled')` +
	` + (SELECT COUNT(*) FROM technicians tc WHERE tc.team_id = t.team_id AND tc.status = 'enabled')`

// listTeamsSelect 团队列表查询。抽出成常量是为了让单测能直接盯住「成员数一列不许读维护列」
// （同 teamDetailSelect）—— 集成层要 Docker，回归时未必跑得到。
const listTeamsSelect = `
SELECT t.team_id, t.name, ` + teamMemberCountExpr + ` AS member_count, ` + teamPatientCountExpr + ` AS patient_count,
       COALESCE(t.leader, ''), COALESCE(d.name, ''), t.created_at,
       COALESCE(t.description, ''), t.status
FROM teams t
LEFT JOIN doctors d ON d.doctor_id = t.leader
ORDER BY t.team_id`

// teamStatsSelect 团队管理页四张统计卡。成员一项 T385 起实时，且直接对 teamMemberCountExpr 按团队求和：
// 修前这里是 SUM(teams.member_count) —— 一条没人维护的列的汇总，于是同页四张卡与列表/明细/删除守卫四处互相打脸
// （Joe 现网：卡 6 对真实 7）。复用列表那一条表达式后「卡数等于各行之和」由构造保证，不靠两条 SQL 碰巧一致。
// 另：doctors.team_id / technicians.team_id 都带 REFERENCES teams(team_id) 外键
// （000001_init_schema.up.sql:44、:58），人挂不上不存在的团队，故求和与「全库已挂团队的在职人数」等价；
// 卡面第四节子口径 2 原设想的「直连 SQL 造孤儿」因此不成立，集成用例改钉这条恒等式。
// T429 起「在职」二字是真的：被求和的那条表达式已排除禁用账号，所以这张卡与实际可登录人数同口径。
const teamStatsSelect = `
		SELECT
			(SELECT COUNT(*) FROM teams) AS team_count,
			COALESCE((SELECT SUM(` + teamMemberCountExpr + `) FROM teams t), 0) AS member_count,
			(SELECT COUNT(*) FROM patients WHERE team_id IS NOT NULL) AS managed_count,
			(SELECT COUNT(*) FROM patients WHERE team_id IS NULL) AS unassigned_count
	`

// ListTeams 团队概要（member_count 见 teamMemberCountExpr，patient_count 见 teamPatientCountExpr；两列均实时）
// T333：负责人两列同 teamDetailSelect 的 LEFT JOIN doctors 口径——
// 列表页「负责人」列与编辑弹窗回显都直接读列表行，缺这两列就是结构上带不出来。
// T333-5：created_at 同为该页表格列（T335 探测证据 filled=0），列在库里非空，纯 SELECT 漏带。
// T337：description / status 契约（shared-types Team）已声明、详情接口已带出，列表仍未带 ⇒ 列同 teamDetailSelect 口径。
func (s *PGStore) ListTeams(ctx context.Context) ([]TeamRow, error) {
	rows, err := s.pool.Query(ctx, listTeamsSelect)
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

// CreateTechnician 新建技师（phone_hash 唯一约束由 uk_technicians_phone_hash 兜底）。
// T480：写入 password_hash —— 此前这一列不在 INSERT 里、落库恒为 NULL，
// 而 techLogin 比对的就是它 ⇒ 后台新建的技师登不进小程序（只有 seed 那几行能登）。
// 不带口令的调用（如既有集成测试）仍写 NULL，不把「未设口令」洗成「空串口令」。
func (s *PGStore) CreateTechnician(ctx context.Context, in TechInput) (*TechnicianRow, error) {
	var pwdHash any
	if in.PasswordHash != "" {
		pwdHash = in.PasswordHash
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO technicians (tech_id, name, phone_enc, phone_hash, team_id, password_hash)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		in.TechID, in.Name, in.PhoneEnc, in.PhoneHash, in.TeamID, pwdHash)
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

// SetTechnicianPassword T480 重置登录口令（POST /api/v1/admin/technicians/:techId/reset-password）：
// 只换 password_hash 这一列，其余列不动（启停/认证状态/团队归属都不该被重置密码顺带改掉）。
// 新哈希一写，旧哈希即取不回 ⇒ 旧口令当场失效。存在性判定排在调用之前（handler 先 GetTechnician）。
func (s *PGStore) SetTechnicianPassword(ctx context.Context, techID, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE technicians SET password_hash = $2 WHERE tech_id = $1`, techID, passwordHash)
	return err
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

// GetTeamStats T256 #1：团队管理 4 张统计卡（团队/成员/管理患者/待分配患者四项计数）。
// 成员一项的口径见 teamStatsSelect —— T385 起实时，不读 teams.member_count。
func (s *PGStore) GetTeamStats(ctx context.Context) (int, int, int, int, error) {
	var teamCount, memberCount, managedCount, unassignedCount int
	err := s.pool.QueryRow(ctx, teamStatsSelect).Scan(&teamCount, &memberCount, &managedCount, &unassignedCount)
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
// T467 患者档案删除（reject-if-referenced，镜像 T059 DeleteTeam 策略）
// ─────────────────────────────────────────────────────────────

// patientRefTable 一张「按 patient_id 引用患者」的关联表。
type patientRefTable struct {
	name  string // 表名，同时是技术日志（409 那条）里的机读键
	hasFK bool   // patients(patient_id) 外键是否存在（false 时数据库不会拦删除）
}

// ErrPatientInUse 患者仍被关联面引用，不可删除（T467）。
//
// DeletePatient 逐表计数命中时返回此结构化错误，handler 用 errors.As 认出并映射 409。
// 逐表计数只进技术日志通道：T464 双通道后 fail() 把响应体 message 换成 model.UserText(code)
// 的中文短句、data 恒为 null，所以计数既不进 message 也不进 data（要机读结构化计数得再动
// 信封面，属另立卡）。
type ErrPatientInUse struct {
	Refs map[string]int // 表名（并发兜底时为 FK 约束名）→ 命中行数，只收录非零项
}

// Error 按 patientRefTables 声明序渲染（文案可比对）；兜底路径的约束名排在末尾。
func (e *ErrPatientInUse) Error() string {
	pairs := make([]string, 0, len(e.Refs))
	known := make(map[string]bool, len(e.Refs))
	for _, t := range patientRefTables {
		if n, hit := e.Refs[t.name]; hit {
			pairs = append(pairs, fmt.Sprintf("%s=%d", t.name, n))
			known[t.name] = true
		}
	}
	for name, n := range e.Refs {
		if !known[name] {
			pairs = append(pairs, fmt.Sprintf("%s=%d", name, n))
		}
	}
	return "patient in use: " + strings.Join(pairs, ", ")
}

// patientRefTables 患者关联面全集（scripts/db/migrations 逐条实测，非推断）：
//
//	有外键 12 张：000001:97/123/189/219/229/243/258/272/284、000003:13/48、000009:10
//	无外键 3 张：pressure_records(000001:147)、daily_wear_stats(000001:173)、device_bindings(000002:14)
//
// 🔴 后三张必须一起数：它们只有 patient_id 列、没有 REFERENCES patients，数据库不会拦删除，
// 少数一张就等于「删患者顺手留下一堆无主体的佩戴明细/日聚合/绑定历史」——
// 派发单「禁止级联误删业务数据」点名的正是这一面。
// 前十二张有外键，靠数据库拦会直接 23503 变 500，所以在服务端先判成 409。
var patientRefTables = []patientRefTable{
	{name: "devices", hasFK: true},
	{name: "install_records", hasFK: true},
	{name: "alerts", hasFK: true},
	{name: "orthosis_plans", hasFK: true},
	{name: "feeling_logs", hasFK: true},
	{name: "feedbacks", hasFK: true},
	{name: "health_reports", hasFK: true},
	{name: "patient_preferences", hasFK: true},
	{name: "consents", hasFK: true},
	{name: "notification_records", hasFK: true},
	{name: "quota_grants", hasFK: true},
	{name: "review_records", hasFK: true},
	{name: "pressure_records", hasFK: false},
	{name: "daily_wear_stats", hasFK: false},
	{name: "device_bindings", hasFK: false},
}

// patientRefCountSQL 一次往返数出全部关联表行数（逐表一条 COUNT(*)，UNION ALL 汇成 rows）。
// 表名取自包内常量表 patientRefTables，不含任何入参拼接。
func patientRefCountSQL() string {
	var b strings.Builder
	for i, t := range patientRefTables {
		if i > 0 {
			b.WriteString(" UNION ALL ")
		}
		fmt.Fprintf(&b, "SELECT '%s', COUNT(*) FROM %s WHERE patient_id = $1", t.name, t.name)
	}
	return b.String()
}

// countPatientRefs 返回 表名 → 命中行数（只收录非零项）。
func (s *PGStore) countPatientRefs(ctx context.Context, patientID string) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, patientRefCountSQL(), patientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := make(map[string]int)
	for rows.Next() {
		var table string
		var n int
		if err := rows.Scan(&table, &n); err != nil {
			return nil, err
		}
		if n > 0 {
			refs[table] = n
		}
	}
	return refs, rows.Err()
}

// DeletePatient 删除患者档案（T467）。
//
// 判定序（与 DeleteTeam 同族）：存在性 → 关联面计数 → 删行。
//   - 无行 → ErrPatientNotFound（重复删除回 404，不采「已经没了也算成功」的伪幂等）
//   - 任一关联表非空 → *ErrPatientInUse（409，携带逐表计数）
//   - 并发下刚被写入关联行：外键 23503 兜底为同一条 *ErrPatientInUse
//
// 硬删，不级联、不软删：patients 无软删列，加列属新迁移（超本卡范围，待裁项已落卡）。
func (s *PGStore) DeletePatient(ctx context.Context, patientID string) error {
	row, err := s.GetPatient(ctx, patientID)
	if err != nil {
		return err
	}
	if row == nil {
		return ErrPatientNotFound
	}
	refs, err := s.countPatientRefs(ctx, patientID)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return &ErrPatientInUse{Refs: refs}
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, patientID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			// 计数与删除之间刚被并发写入关联行：constraint 名形如 devices_patient_id_fkey
			name := pgErr.ConstraintName
			if name == "" {
				name = "foreign key"
			}
			return &ErrPatientInUse{Refs: map[string]int{name: 1}}
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPatientNotFound
	}
	return nil
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
// 患者数列 T371-B1 起走 teamPatientCountExpr 实时计数，成员数列 T385 起走 teamMemberCountExpr 实时计数
// （列表与详情共用同两条表达式，不漂口径）
const teamDetailSelect = `
SELECT t.team_id, t.name, COALESCE(t.leader, ''), COALESCE(d.name, ''),
       ` + teamMemberCountExpr + ` AS member_count, ` + teamPatientCountExpr + ` AS patient_count,
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
	// 2. 统计引用计数（patients + doctors + technicians）。T429 裁定只收口「在职成员数」这一个展示维度，
	//    这里刻意继续看全量：禁用医护的 team_id 外键照样存在，跟着收口会把「名下只剩禁用者」的团队判成可删。
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
