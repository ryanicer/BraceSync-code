//go:build integration
// +build integration

// Package repo 集成测试：T314 医护账号写通道（真实 PG）
//
// handler 单测的 fakeStore 证不到、必须在真库里证的三件事：
//  1. 一次创建确实写进 admins + doctors 两表且同事务（失败不留半行）；
//  2. 登录账号由 DB 序列发号 —— 🔴 不是「条数 + 1」：删掉中间行再建不得撞回已用序号，
//     历史人工账号占掉下一个序号时必须重发（真库唯一键 23505 才作数）；
//  3. 读侧 join 的 GROUP BY 在真库过得了（PG 不认 LEFT JOIN 可空侧函数依赖，42P10），
//     两层 status 各归各列。
//
// 独立 ID + 测后清场，口径同 feedback_create_t311_it_test.go（共享种子库）。
package repo

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t314Tracker 记下本文件建出来的行，测后按外键顺序清场（doctors → admins）
type t314Tracker struct {
	doctorIDs []string
	adminIDs  []string
}

func (tr *t314Tracker) addRow(t *testing.T, row *DoctorRow) {
	t.Helper()
	if row == nil {
		return
	}
	tr.doctorIDs = append(tr.doctorIDs, row.DoctorID)
	if row.AdminID != nil {
		tr.adminIDs = append(tr.adminIDs, *row.AdminID)
	}
}

func (tr *t314Tracker) addAdmin(t *testing.T, adminID string) {
	t.Helper()
	tr.adminIDs = append(tr.adminIDs, adminID)
}

func (tr *t314Tracker) cleanup(ctx context.Context, t *testing.T) {
	t.Helper()
	for _, id := range tr.doctorIDs {
		if _, err := itStore.pool.Exec(ctx, `DELETE FROM doctors WHERE doctor_id = $1`, id); err != nil {
			t.Errorf("t314 cleanup doctor %s: %v", id, err)
		}
	}
	for _, id := range tr.adminIDs {
		if _, err := itStore.pool.Exec(ctx, `DELETE FROM admins WHERE admin_id = $1`, id); err != nil {
			t.Errorf("t314 cleanup admin %s: %v", id, err)
		}
	}
}

func t314Input(name string) DoctorAccountInput {
	return DoctorAccountInput{
		Name: name, Title: "主治医师", Department: "骨科", TeamID: itTeam,
		PhoneEnc: []byte("t314-enc"), PhoneHash: "t314-hash-" + name,
		PasswordHash: "$2a$08$fakehashfortest", Status: "enabled",
	}
}

// ─────────────────────────────────────────────────────────────
// 创建：双表 + 发号
// ─────────────────────────────────────────────────────────────

// TestITCreateDoctorAccount_T314 正例：一次调用两表都落行，回读带出服务端发的登录账号
func TestITCreateDoctorAccount_T314(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	row, err := itStore.CreateDoctorAccount(ctx, t314Input("T314甲"))
	require.NoError(t, err)
	tr.addRow(t, row)

	require.NotNil(t, row.Username, "创建回读必须带出 username（handler 要一次性展示）")
	assert.Regexp(t, `^doc[0-9]{5}$`, *row.Username, "登录账号形态 = doc + 5 位序号")
	require.NotNil(t, row.AdminID)
	assert.Equal(t, "enabled", row.Status, "doctors.status（档案层）")
	require.NotNil(t, row.AccountStatus)
	assert.Equal(t, "enabled", *row.AccountStatus, "admins.status（登录层）")
	assert.False(t, row.AccountCreatedAt.IsZero(), "created_at 应由库默认 now() 填充")

	// 两表逐列对账（handler 侧看不到库，只能在这里证）
	var adminUser, adminRole, adminName, adminStatus string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT username, role_id, name, status FROM admins WHERE admin_id = $1`, *row.AdminID).
		Scan(&adminUser, &adminRole, &adminName, &adminStatus))
	assert.Equal(t, *row.Username, adminUser)
	assert.Equal(t, "ROLE_DOCTOR", adminRole, "角色固定为医护，不由入参决定（PRD（5））")
	assert.Equal(t, "T314甲", adminName, "admins.name 与档案同名")
	assert.Equal(t, "enabled", adminStatus)

	var docName string
	var title, dept, team, phoneHash, adminID *string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT name, title, department, team_id, phone_hash, admin_id FROM doctors WHERE doctor_id = $1`, row.DoctorID).
		Scan(&docName, &title, &dept, &team, &phoneHash, &adminID))
	assert.Equal(t, "T314甲", docName)
	require.NotNil(t, title)
	assert.Equal(t, "主治医师", *title)
	require.NotNil(t, dept)
	assert.Equal(t, "骨科", *dept)
	require.NotNil(t, team)
	assert.Equal(t, itTeam, *team)
	require.NotNil(t, adminID)
	assert.Equal(t, *row.AdminID, *adminID, "关联键落在 doctors.admin_id")

	// 🔴 登录链路零改动即可用：既有的 GetAdminByUsername 查得到新账号
	admin, err := itStore.GetAdminByUsername(ctx, adminUser)
	require.NoError(t, err)
	require.NotNil(t, admin, "新账号必须能被登录查询命中，否则建出来的是登不进去的号")
	assert.Equal(t, *row.AdminID, admin.AdminID)

	// 读侧列表（ListDoctors 的 join + GROUP BY）同样带出该账号
	list, err := itStore.ListDoctors(ctx)
	require.NoError(t, err)
	var found *DoctorRow
	for i := range list {
		if list[i].DoctorID == row.DoctorID {
			found = &list[i]
		}
	}
	require.NotNil(t, found, "新医护应出现在主诊列表里")
	require.NotNil(t, found.Username)
	assert.Equal(t, adminUser, *found.Username)
}

// TestITCreateDoctorAccount_T314_NotCountPlusOne 连建 5 个：序号严格递增且互不相同。
// 再删掉中间一个后重建 —— 若发号是「doctors 条数 + 1」，这里必然产出与已存在账号重复的序号。
func TestITCreateDoctorAccount_T314_NotCountPlusOne(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	var users []string
	var rows []*DoctorRow
	for i := 0; i < 5; i++ {
		row, err := itStore.CreateDoctorAccount(ctx, t314Input(fmt.Sprintf("T314连建%d", i)))
		require.NoError(t, err)
		tr.addRow(t, row)
		require.NotNil(t, row.Username)
		users = append(users, *row.Username)
		rows = append(rows, row)
	}
	uniq := make(map[string]bool, len(users))
	for _, u := range users {
		assert.False(t, uniq[u], "连建发出重复序号 %s：%v", u, users)
		uniq[u] = true
	}
	for i := 1; i < len(users); i++ {
		assert.Greater(t, users[i], users[i-1], "序号应严格递增（nextval），实得 %v", users)
	}

	// 删掉第 2 个 ⇒ 存量条数变 4；下一条若按「条数+1」发号就会撞上 users[3]/users[4]
	_, err := itStore.pool.Exec(ctx, `DELETE FROM doctors WHERE doctor_id = $1`, rows[1].DoctorID)
	require.NoError(t, err)
	_, err = itStore.pool.Exec(ctx, `DELETE FROM admins WHERE admin_id = $1`, *rows[1].AdminID)
	require.NoError(t, err)

	next, err := itStore.CreateDoctorAccount(ctx, t314Input("T314删后重建"))
	require.NoError(t, err)
	tr.addRow(t, next)
	require.NotNil(t, next.Username)
	for _, u := range users {
		assert.NotEqual(t, u, *next.Username, "新序号不得回用到仍被占用的 %s", u)
	}
	assert.Greater(t, *next.Username, users[4], "nextval 只前进不回头")
}

// TestITCreateDoctorAccount_T314_RetriesOnTakenUsername 🔴 撞号必须重发而不是 500：
// 手动占掉「下一个」序号（模拟历史人工账号），再走创建 —— 应跳过它拿到再下一个。
// 本地真库复现过：序列 START 早于存量 doc 行建立时，nextval 就是会发出撞号。
func TestITCreateDoctorAccount_T314_RetriesOnTakenUsername(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	var nextSeq int64
	require.NoError(t, itStore.pool.QueryRow(ctx, `SELECT nextval('doctor_username_seq')`).Scan(&nextSeq))
	// 此刻序列下一个值就是 nextSeq+1，先把它占掉
	occupied := fmt.Sprintf("doc%05d", nextSeq+1)
	const squatter = "ADM-USR-T314-SQUAT"
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO admins (admin_id, username, name, password_hash, role_id)
		 VALUES ($1, $2, 'T314占号账号', '$2a$08$fakehashfortest', 'ROLE_DOCTOR')
		 ON CONFLICT (admin_id) DO NOTHING`, squatter, occupied)
	require.NoError(t, err)
	tr.addAdmin(t, squatter)

	row, err := itStore.CreateDoctorAccount(ctx, t314Input("T314跳号"))
	require.NoError(t, err, "撞号应重发而非上抛唯一键错误")
	tr.addRow(t, row)
	require.NotNil(t, row.Username)
	assert.NotEqual(t, occupied, *row.Username, "不得把已占用序号再发一次")
	assert.Equal(t, fmt.Sprintf("doc%05d", nextSeq+2), *row.Username)
}

// TestITCreateDoctorAccount_T314_TransactionAtomicity 第二步（写 doctors）失败 ⇒ 第一步的 admins 行必须回滚。
// 用不存在的 team_id 触发 FK 违约（23503），它不是可重发的冲突，应当原样上抛。
func TestITCreateDoctorAccount_T314_TransactionAtomicity(t *testing.T) {
	ctx := context.Background()

	in := t314Input("T314半行")
	in.TeamID = "TEAM-T314-NOT-EXIST"

	_, err := itStore.CreateDoctorAccount(ctx, in)
	require.Error(t, err, "团队 FK 不命中应报错")
	assert.False(t, errors.Is(err, ErrDoctorNotFound), "库约束错误不该被误映射成业务 sentinel")

	var n int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT count(*) FROM admins WHERE name = 'T314半行' AND role_id = 'ROLE_DOCTOR'`).Scan(&n))
	assert.Zero(t, n, "🔴 不得留下「有登录账号、无医护档案」的半行（能被登录却不在主诊列表）")
}

// ─────────────────────────────────────────────────────────────
// 编辑 / 重置密码 / 启停
// ─────────────────────────────────────────────────────────────

// TestITUpdateDoctorAccount_T314 改名要同步 admins.name（操作日志与顶栏显示名同源），
// 且🔴 不碰密码哈希；未给的字段保持原值。
func TestITUpdateDoctorAccount_T314(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	row, err := itStore.CreateDoctorAccount(ctx, t314Input("T314编辑前"))
	require.NoError(t, err)
	tr.addRow(t, row)

	var beforeHash string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT password_hash FROM admins WHERE admin_id = $1`, *row.AdminID).Scan(&beforeHash))

	team2 := "TEAM-USR-IT-B"
	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO teams (team_id, name, member_count, patient_count) VALUES ($1, 'T314新团队', 0, 0)
		 ON CONFLICT (team_id) DO NOTHING`, team2)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM teams WHERE team_id = $1`, team2)
	})

	dept := "" // 空串按未填落 NULL，库里不留空串与 NULL 两种「没有」
	updated, err := itStore.UpdateDoctorAccount(ctx, row.DoctorID, DoctorAccountUpdate{
		Name: strptrT314("T314编辑后"), Department: &dept, TeamID: &team2,
	})
	require.NoError(t, err)
	assert.Equal(t, "T314编辑后", updated.Name, "回读走同一 join，改名当次即生效")
	require.NotNil(t, updated.Username, "已绑账号的档案回读必须带 username")
	assert.Regexp(t, `^doc[0-9]{5}$`, *updated.Username)

	var adminName, adminHash string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT name, password_hash FROM admins WHERE admin_id = $1`, *row.AdminID).
		Scan(&adminName, &adminHash))
	assert.Equal(t, "T314编辑后", adminName, "改名必须同步到 admins.name")
	assert.Equal(t, beforeHash, adminHash, "🔴 编辑端点无密码通道，哈希不得被动")

	var title, team *string
	var department *string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT title, department, team_id FROM doctors WHERE doctor_id = $1`, row.DoctorID).
		Scan(&title, &department, &team))
	require.NotNil(t, title)
	assert.Equal(t, "主治医师", *title, "未下发的职称保持原值")
	assert.Nil(t, department, "空串应落 NULL")
	require.NotNil(t, team)
	assert.Equal(t, team2, *team)

	// 不存在的档案 → ErrDoctorNotFound（handler 映射 404）
	_, err = itStore.UpdateDoctorAccount(ctx, "DOC-T314-NOPE", DoctorAccountUpdate{Name: strptrT314("x")})
	assert.True(t, errors.Is(err, ErrDoctorNotFound), "应映射 sentinel：%v", err)

	// 全空入参 = 一行不改，直接回读当前值
	same, err := itStore.UpdateDoctorAccount(ctx, row.DoctorID, DoctorAccountUpdate{})
	require.NoError(t, err)
	assert.Equal(t, "T314编辑后", same.Name)
}

// TestITSetDoctorAccountStatus_T314 两层 status 同写；读侧两列各归各列不串味。
func TestITSetDoctorAccountStatus_T314(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	row, err := itStore.CreateDoctorAccount(ctx, t314Input("T314启停"))
	require.NoError(t, err)
	tr.addRow(t, row)

	after, err := itStore.SetDoctorAccountStatus(ctx, row.DoctorID, "disabled")
	require.NoError(t, err)
	require.NotNil(t, after.AccountStatus)
	assert.Equal(t, "disabled", *after.AccountStatus, "登录层")
	assert.Equal(t, "disabled", after.Status, "档案层")

	// 幂等：再禁一次不报错、值不变
	again, err := itStore.SetDoctorAccountStatus(ctx, row.DoctorID, "disabled")
	require.NoError(t, err)
	assert.Equal(t, "disabled", *again.AccountStatus)

	// 回到 enabled，确认开关可逆
	back, err := itStore.SetDoctorAccountStatus(ctx, row.DoctorID, "enabled")
	require.NoError(t, err)
	assert.Equal(t, "enabled", *back.AccountStatus)

	_, err = itStore.SetDoctorAccountStatus(ctx, "DOC-T314-NOPE", "disabled")
	assert.True(t, errors.Is(err, ErrDoctorNotFound), "不存在应 404 sentinel：%v", err)
}

// TestITDoctorAccount_T314_ProfileWithoutAccount 存量形态（seed 的 D0002/D0003：档案在、admin_id 为 NULL）
// ⇒ 启停/重置回 ErrDoctorNoAccount（handler 映射 409），编辑仍可改档案。
func TestITDoctorAccount_T314_ProfileWithoutAccount(t *testing.T) {
	ctx := context.Background()
	const orphanDoc = "DOC-T314-ORPHAN"
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO doctors (doctor_id, name, title, department, team_id, admin_id, status)
		 VALUES ($1, 'T314未绑账号', '医师', '康复科', $2, NULL, 'enabled')
		 ON CONFLICT (doctor_id) DO NOTHING`, orphanDoc, itTeam)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM doctors WHERE doctor_id = $1`, orphanDoc)
	})

	_, err = itStore.SetDoctorAccountStatus(ctx, orphanDoc, "disabled")
	assert.True(t, errors.Is(err, ErrDoctorNoAccount), "无凭据可停 ⇒ sentinel 409，不是假成功：%v", err)

	_, err = itStore.SetDoctorAccountPassword(ctx, orphanDoc, "$2a$08$newhash")
	assert.True(t, errors.Is(err, ErrDoctorNoAccount), "无凭据可改密 ⇒ sentinel 409：%v", err)

	// 编辑档案不该被「没账号」挡住（PRD（4）：编辑态只是没有密码分组）
	updated, err := itStore.UpdateDoctorAccount(ctx, orphanDoc, DoctorAccountUpdate{Name: strptrT314("T314未绑账号改")})
	require.NoError(t, err)
	assert.Equal(t, "T314未绑账号改", updated.Name)
	assert.Nil(t, updated.Username, "未绑账号 ⇒ username 为 null，不得伪造")

	// 读侧：未绑账号的档案仍要在列表里，且 admins 三列为 nil
	list, err := itStore.ListDoctors(ctx)
	require.NoError(t, err)
	var found *DoctorRow
	for i := range list {
		if list[i].DoctorID == orphanDoc {
			found = &list[i]
		}
	}
	require.NotNil(t, found, "未绑账号的医护不能从主诊列表里消失")
	assert.Nil(t, found.AdminID)
	assert.Nil(t, found.Username)
	assert.Nil(t, found.AccountStatus)
	assert.Nil(t, found.AccountCreatedAt)
}

// TestITSetDoctorAccountPassword_T314 重置只换哈希；旧哈希取不回、账号其他列不动。
func TestITSetDoctorAccountPassword_T314(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	row, err := itStore.CreateDoctorAccount(ctx, t314Input("T314改密"))
	require.NoError(t, err)
	tr.addRow(t, row)

	var beforeHash, beforeUser string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT password_hash, username FROM admins WHERE admin_id = $1`, *row.AdminID).
		Scan(&beforeHash, &beforeUser))

	after, err := itStore.SetDoctorAccountPassword(ctx, row.DoctorID, "$2a$08$brandnewhashfort314")
	require.NoError(t, err)
	require.NotNil(t, after.Username)
	assert.Equal(t, beforeUser, *after.Username, "🔴 重置密码不换登录账号")

	var gotHash string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT password_hash FROM admins WHERE admin_id = $1`, *row.AdminID).Scan(&gotHash))
	assert.Equal(t, "$2a$08$brandnewhashfort314", gotHash)
	assert.NotEqual(t, beforeHash, gotHash, "旧哈希必须被覆盖（旧密码即时失效）")

	_, err = itStore.SetDoctorAccountPassword(ctx, "DOC-T314-NOPE", "x")
	assert.True(t, errors.Is(err, ErrDoctorNotFound), "不存在应 404 sentinel：%v", err)
}

func strptrT314(s string) *string { return &s }
