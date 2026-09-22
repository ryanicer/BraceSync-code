// T314 医护账号写通道（PRD §7D.10「医护账号管理」·合同《功能清单》:48）
//
// 一页一行跨两张表（PRD（5））：doctors（档案：姓名/职称/科室/团队/手机号/status）
// + admins（登录：username/password_hash/role_id/status/created_at），
// 关联键 doctors.admin_id → admins.admin_id。
//
// 🔴 为什么复用 admins 而不新建认证链路：POST /api/v1/auth/login 已经是
// GetAdminByUsername → bcrypt → admins.status → roles.permissions_json.scope 一条链，
// 医护账号本来就是「role_id = ROLE_DOCTOR 的 admins 行」。新建第二套认证会让登录、
// gateway JWT、RBAC 全部要分叉 ⇒ 创建 = 同事务写两表，登录侧零改动。
//
// 🔴 发号不用 count+1：admins.username 带 UNIQUE NOT NULL（000001:30），
//
//	「现有条数 + 1」在并发/连点下必然撞唯一键（PRD（3）与 T316 交件均明令）。
//	序号由 DB 序列 doctor_username_seq 发（迁移 000024），nextval 跨事务唯一；
//	仍保留 23505 有限重试，兜「历史人工账号已占掉某个序号」的窗口——
//	本地真库复现过：序列 START 早于存量 doc00004 建立时，nextval 会发出撞号的 doc00001。
package repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDoctorNotFound 医护档案 ID 不存在。handler 映射 404。
var ErrDoctorNotFound = errors.New("doctor not found")

// ErrDoctorNoAccount 医护档案在，但 admin_id 为空或指向已不存在的 admins 行 ⇒ 无登录凭据可改。
// 存量库里确有这类行（seed.sql 的 D0002/D0003 admin_id 为 NULL）。
// handler 映射 409 而非 404：档案在列表里看得见、操作却无从下手，得让运维看懂差在哪。
var ErrDoctorNoAccount = errors.New("doctor profile has no linked admin account")

// ErrUsernameExhausted 连续撞已占用序号超过上限（发号器被历史数据推到不可用）。
var ErrUsernameExhausted = errors.New("doctor login account sequence exhausted on unique username")

// doctorAccountRole 医护账号固定登录角色（PRD（5）：本页不提供角色选择控件）。
const doctorAccountRole = "ROLE_DOCTOR"

// usernameCollideRetries 撞唯一键后的重发次数上限。
// 5 次够用：正常库里最多撞上历史人工账号占掉的少数序号；仍撞 = 数据异常，
// 回错让运维查，不在一次请求里无限打库。
const usernameCollideRetries = 5

// isUsernameCollision 判定 admins.username 唯一约束违约（SQLSTATE 23505）。
// 只认这一条约束：doctors 侧主键不会撞（DOC+随机 hex），FK/CHECK 违约都是入参问题，重试无意义。
func isUsernameCollision(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "admins_username_key"
}

// randID 前缀 + 12 位大写随机 hex（对齐 newRoleID 的抗并发口径，VARCHAR(32) 内）
func randID(prefix string) (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + strings.ToUpper(hex.EncodeToString(buf)), nil
}

// DoctorAccountInput 创建入参（手机号已加密、密码已 bcrypt，repo 不经手明文）。
type DoctorAccountInput struct {
	Name         string
	Title        string
	Department   string
	TeamID       string
	PhoneEnc     []byte
	PhoneHash    string // 空串 = 未填手机号（选填，落 NULL 而不是 sha256("")）
	PasswordHash string
	Status       string // enabled | disabled；两层同写，见 SetDoctorAccountStatus
}

// CreateDoctorAccount 一次事务写 admins + doctors 两表（PRD（6）「创建须一次写两表」）。
// 回读走 admins join ⇒ 返回体带服务端生成的 username，供 handler 一次性展示。
func (s *PGStore) CreateDoctorAccount(ctx context.Context, in DoctorAccountInput) (*DoctorRow, error) {
	adminID, err := randID("ADM")
	if err != nil {
		return nil, err
	}
	doctorID, err := randID("DOC")
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < usernameCollideRetries; attempt++ {
		row, createErr := s.createDoctorAccountOnce(ctx, doctorID, adminID, in)
		if createErr == nil {
			return row, nil
		}
		if !isUsernameCollision(createErr) {
			return nil, createErr
		}
		lastErr = createErr
	}
	return nil, fmt.Errorf("%w after %d attempts: %v", ErrUsernameExhausted, usernameCollideRetries, lastErr)
}

// createDoctorAccountOnce 单轮事务：nextval 取号 → 写 admins → 写 doctors → commit。
// 任一步失败整体回滚 ⇒ 不留「有登录账号、无医护档案」的半行（反之亦然）。
func (s *PGStore) createDoctorAccountOnce(ctx context.Context, doctorID, adminID string, in DoctorAccountInput) (*DoctorRow, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var seq int64
	// 42P01（序列不存在）= 迁移 000024 未跑，原样上抛，500 文案里带得到 driver 错误
	if err := tx.QueryRow(ctx, `SELECT nextval('doctor_username_seq')`).Scan(&seq); err != nil {
		return nil, err
	}
	username := fmt.Sprintf("doc%05d", seq)

	if _, err := tx.Exec(ctx,
		`INSERT INTO admins (admin_id, username, name, password_hash, role_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		adminID, username, in.Name, in.PasswordHash, doctorAccountRole, in.Status); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO doctors (doctor_id, name, title, department, team_id, phone_enc, phone_hash, admin_id, status)
		 VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, NULLIF($7, ''), $8, $9)`,
		doctorID, in.Name, in.Title, in.Department, in.TeamID,
		in.PhoneEnc, in.PhoneHash, adminID, in.Status); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetDoctorAccount(ctx, doctorID)
}

// DoctorAccountUpdate 编辑入参：nil = 该字段不改（指针语义，与 T302 settings 一致）。
// PhoneHash 特例：nil = 保持原号；指向空串 = 清空手机号（选填字段要能撤回，PhoneEnc 同步置空）。
type DoctorAccountUpdate struct {
	Name       *string
	Title      *string
	Department *string
	TeamID     *string
	PhoneEnc   []byte
	PhoneHash  *string
}

// UpdateDoctorAccount 编辑医护档案（不改密码、不改登录账号，PRD（4）编辑态密码分组隐藏）。
//
// 🔴 admins.name 跟着 doctors.name 同步：admins.name 是登录与操作日志里的显示名，
//
//	只改档案不改账号会让操作日志和后台顶栏继续显示旧姓名。
func (s *PGStore) UpdateDoctorAccount(ctx context.Context, doctorID string, in DoctorAccountUpdate) (*DoctorRow, error) {
	cur, err := s.GetDoctorAccount(ctx, doctorID)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, ErrDoctorNotFound
	}

	args := []any{doctorID}
	var sets []string
	set := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if in.Name != nil {
		set("name", *in.Name)
	}
	if in.Title != nil {
		set("title", nullable(*in.Title))
	}
	if in.Department != nil {
		set("department", nullable(*in.Department))
	}
	if in.TeamID != nil {
		set("team_id", nullable(*in.TeamID))
	}
	if in.PhoneHash != nil {
		// enc 与 hash 成对改（handler 保证），清空时两者都是零值 → 落 NULL
		set("phone_enc", in.PhoneEnc)
		set("phone_hash", nullable(*in.PhoneHash))
	}
	if len(sets) == 0 {
		return cur, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		"UPDATE doctors SET "+strings.Join(sets, ", ")+" WHERE doctor_id = $1", args...); err != nil {
		return nil, err
	}
	if in.Name != nil {
		// 未绑账号的档案这里 0 行受影响，不报错：档案侧改动已经成功
		if _, err := tx.Exec(ctx,
			`UPDATE admins SET name = $2
			 FROM doctors WHERE admins.admin_id = doctors.admin_id AND doctors.doctor_id = $1`,
			doctorID, *in.Name); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetDoctorAccount(ctx, doctorID)
}

// SetDoctorAccountStatus 启用/禁用。
//
// 🔴 两层 status 一起写（PRD（6）待 Boss 裁 #6「禁哪一层」）：admins.status 是唯一有
//
//	消费方的一层（登录读它，disabled 直接 10401），doctors.status 当前零消费方。
//	只写 admins ⇒ 停用的医护仍会出现在患者管理主诊下拉，正是 PRD 点名的合规事故；
//	两层同写则将来按裁定拆成两个开关时属纯增加粒度、不反转任何既有语义。
//	若 Boss 裁定「只禁登录」，删掉 doctors 那条 Exec 即可，读侧两列已经拆开。
func (s *PGStore) SetDoctorAccountStatus(ctx context.Context, doctorID, status string) (*DoctorRow, error) {
	cur, err := s.GetDoctorAccount(ctx, doctorID)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, ErrDoctorNotFound
	}
	if cur.AdminID == nil {
		return nil, ErrDoctorNoAccount
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE admins SET status = $2 WHERE admin_id = $1`,
		*cur.AdminID, status); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE doctors SET status = $2 WHERE doctor_id = $1`,
		doctorID, status); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetDoctorAccount(ctx, doctorID)
}

// SetDoctorAccountPassword 重置密码：只换 admins.password_hash，旧密码即时失效（PRD（3））。
// 新密码由 handler 随机生成并 bcrypt，repo 不经手明文。
func (s *PGStore) SetDoctorAccountPassword(ctx context.Context, doctorID, passwordHash string) (*DoctorRow, error) {
	cur, err := s.GetDoctorAccount(ctx, doctorID)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, ErrDoctorNotFound
	}
	if cur.AdminID == nil {
		return nil, ErrDoctorNoAccount
	}
	tag, err := s.pool.Exec(ctx, `UPDATE admins SET password_hash = $2 WHERE admin_id = $1`,
		*cur.AdminID, passwordHash)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		// 刚读到 admin_id、写时账号已不在：同口径回错，不假装重置成功
		return nil, ErrDoctorNoAccount
	}
	return s.GetDoctorAccount(ctx, doctorID)
}

// GetDoctorAccount 单个医护账号（含 admins 侧四列）；不存在返回 (nil, nil)
func (s *PGStore) GetDoctorAccount(ctx context.Context, doctorID string) (*DoctorRow, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+doctorColumns+`
		 FROM doctors d
		 LEFT JOIN patients p ON p.primary_doctor_id = d.doctor_id
		 `+doctorAdminJoin+`
		 WHERE d.doctor_id = $1
		 `+doctorGroupBy, doctorID)
	var d DoctorRow
	err := row.Scan(&d.DoctorID, &d.Name, &d.Title, &d.Department, &d.TeamID, &d.PhoneEnc,
		&d.Status, &d.PatientCount, &d.AdminID, &d.Username, &d.AccountStatus, &d.AccountCreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// doctorAdminJoin 医护读侧的 admins join（一行跨两表，PRD（5））。
const doctorAdminJoin = `LEFT JOIN admins a ON a.admin_id = d.admin_id`

// doctorGroupBy 🔴 必须显式列出 a.* 列：PG 不把 LEFT JOIN 可空侧的列认作函数依赖于
// d.doctor_id（真库实测 42P10）。admin_id 是 admins 主键 ⇒ 加进 GROUP BY 不改行数。
const doctorGroupBy = `GROUP BY d.doctor_id, a.username, a.status, a.created_at`

// nullable 空串按「未填」落 NULL（可空列统一口径，避免库里同时存在空串与 NULL 两种「没有」）
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
