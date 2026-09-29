//go:build integration
// +build integration

// Package repo 集成测试：T487「后台账号可用手机号登录」的库侧证据
//
// handler 单测的 fakeStore 证不到、必须在真库里证的四件事：
//  1. 迁移 000032 落下来的形状：两列可空、phone_hash 走**部分**唯一索引
//     （NULL 彼此不算重复 ⇒ 存量账号继续用户名登录；两个非 NULL 同值必 23505），
//     down/up 往返后形状精确复原 —— 只测 up 等于没测 down；
//  2. 登录双凭证的第二支（GetAdminByPhoneHash）读得到刚录的号，且传空串**命中不到**
//     phone_hash 为 NULL 的行 —— 否则「没填手机号」会变成一条能撞开别人账号的凭据；
//  3. 撞号在两条写通道上的落点：创建 ⇒ ErrAdminPhoneTaken 且不留半行、
//     编辑 ⇒ sentinel 且**两张表的旧号都保持原值**（doctors 的 UPDATE 排在 admins 之前，
//     没有事务就是半改），清空 ⇒ 两表一起 NULL；
//  4. 手机号那份「登录凭据副本」只跟账号走：未绑账号的档案改手机号只落 doctors，
//     不给 admins 凭空造行；自助改密只碰 admins.password_hash。
//
// 独立 ID + 测后清场，口径同 doctor_accounts_t314_it_test.go（共享种子库）。
package repo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/phone"
)

const (
	t487UpFile   = "000032_t487_admin_phone_login_winner.up.sql"
	t487DownFile = "000032_t487_admin_phone_login_winner.down.sql"
)

// t487RestoreUp 作为 t.Cleanup 用：⚠ 清理函数里禁用 require / runMigrationFile
// （FailNow 只允许发生在测试函数体内，见 t368_integration_test.go 的同款注释），
// 失败只 t.Errorf —— 但必须把形状补回来，否则本包后面的 T314 用例会因
// CreateDoctorAccount 写 admins.phone_* 而全体连带红。
func t487RestoreUp(t *testing.T) {
	t.Helper()
	sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir(), t487UpFile))
	if err != nil {
		t.Errorf("t487 还原时读 %s 失败: %v", t487UpFile, err)
		return
	}
	if _, execErr := itStore.pool.Exec(context.Background(), string(sqlBytes)); execErr != nil {
		t.Errorf("t487 还原 admins 手机号形状失败: %v", execErr)
	}
}

// t487IndexShape 读 uk_admins_phone_hash 的唯一性与部分谓词；索引不在时 ok=false
func t487IndexShape(t *testing.T) (unique, partial bool, def string, ok bool) {
	t.Helper()
	err := itStore.pool.QueryRow(context.Background(),
		`SELECT i.indisunique, i.indpred IS NOT NULL, pg_get_indexdef(i.indexrelid)
		 FROM pg_class c JOIN pg_index i ON i.indexrelid = c.oid
		 WHERE c.relname = 'uk_admins_phone_hash'`).
		Scan(&unique, &partial, &def)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, "", false
	}
	require.NoError(t, err)
	return unique, partial, def, true
}

// t487AdminPhoneColumns 直查 admins 的两列，确保断言打在库里的真实字节上
func t487AdminPhoneColumns(t *testing.T, adminID string) (enc []byte, hash *string) {
	t.Helper()
	var encArg any
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT phone_enc, phone_hash FROM admins WHERE admin_id = $1`, adminID).
		Scan(&encArg, &hash))
	switch v := encArg.(type) {
	case nil:
		return nil, hash
	case []byte:
		return v, hash
	case string:
		return []byte(v), hash
	default:
		t.Fatalf("admins.phone_enc 意外类型 %T", encArg)
		return nil, nil
	}
}

// t487DeleteAdmins 清场：先按 admin_id 摘掉可能挂着的档案，再删账号
func t487DeleteAdmins(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := itStore.pool.Exec(context.Background(),
			`DELETE FROM doctors WHERE admin_id = $1`, id); err != nil {
			t.Errorf("t487 cleanup doctors by admin %s: %v", id, err)
		}
		if _, err := itStore.pool.Exec(context.Background(),
			`DELETE FROM admins WHERE admin_id = $1`, id); err != nil {
			t.Errorf("t487 cleanup admin %s: %v", id, err)
		}
	}
}

// ─────────────────────────────────────────────────────────────
// 迁移形状：可空 + 部分唯一
// ─────────────────────────────────────────────────────────────

// TestITMigration_T487_AdminPhoneShape 证 000032 up 在真库里的形状与索引语义。
func TestITMigration_T487_AdminPhoneShape(t *testing.T) {
	ctx := context.Background()

	// ① 列形状：两列都在、都可空；phone_hash 定长 64（SHA-256 hex）
	var colCount int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_name = 'admins' AND column_name IN ('phone_enc','phone_hash')
		   AND is_nullable = 'YES'`).Scan(&colCount))
	assert.Equal(t, 2, colCount, "🔴 两列都必须可空 ⇒ 存量账号不回填也能继续用户名登录（本卡不代做数据变更）")

	var encType, hashType string
	var hashLen *int32
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT data_type, character_maximum_length FROM information_schema.columns
		 WHERE table_name = 'admins' AND column_name = 'phone_hash'`).Scan(&hashType, &hashLen))
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT data_type FROM information_schema.columns
		 WHERE table_name = 'admins' AND column_name = 'phone_enc'`).Scan(&encType))
	assert.Equal(t, "bytea", encType, "密文 BYTEA，口径同 technicians/patients")
	// ⚠ information_schema 把 CHAR 报成 SQL 标准名 "character"（不是 PG 内部名 bpchar）——
	//	本机 PG14 真库实测：character|64。写 bpchar 这条断言必假红。
	assert.Equal(t, "character", hashType, "哈希 CHAR(64)")
	require.NotNil(t, hashLen)
	assert.Equal(t, int32(64), *hashLen, "定长 64 = SHA-256 hex 长度")

	// ② 索引形状：唯一 + 带部分谓词
	// ⚠ 变异实测（raw-06 / raw-07）：去掉 UNIQUE ⇒ ③④ 两格同时红；
	//	去掉 WHERE 谓词 ⇒ 只有这两条结构断言红 —— 因为 PG 的唯一索引本来就允许多个 NULL，
	//	谓词换不来「NULL 不限」这条语义（那是 PG 默认行为），换来的是索引不装无号行。
	//	所以这里锁形状、③ 锁行为，两条各证一件事，不合并成一条。
	unique, partial, def, ok := t487IndexShape(t)
	require.True(t, ok, "uk_admins_phone_hash 应存在")
	assert.True(t, unique, "必须是唯一索引")
	assert.True(t, partial, "必须是部分索引（WHERE phone_hash IS NOT NULL）")
	assert.Contains(t, def, "phone_hash IS NOT NULL", "索引定义里要带谓词，实得：%s", def)

	// ③ NULL 彼此不限：seed 的 it_admin 没手机号，再放两个没手机号的账号进来都不得撞
	const noPhoneA = "ADM-T487-NOPH-A"
	const noPhoneB = "ADM-T487-NOPH-B"
	for _, pair := range [][2]string{{noPhoneA, "t487_noph_a"}, {noPhoneB, "t487_noph_b"}} {
		_, err := itStore.pool.Exec(ctx,
			`INSERT INTO admins (admin_id, username, name, password_hash, role_id)
			 VALUES ($1, $2, 'T487无号账号', '$2a$08$fakehashfortest', 'ROLE_DOCTOR')`,
			pair[0], pair[1])
		require.NoError(t, err, "🔴 两个 phone_hash 均为 NULL 的账号必须能共存（管理员与存量账号的面）")
	}
	t.Cleanup(func() { t487DeleteAdmins(t, noPhoneA, noPhoneB) })

	// ④ 非 NULL 同值必撞，且撞的是这条索引（不是 username / 主键）
	const dupA = "ADM-T487-DUP-A"
	const dupB = "ADM-T487-DUP-B"
	const dupC = "ADM-T487-DUP-C"
	dupHash := phone.Hash("13911110000")
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO admins (admin_id, username, name, password_hash, role_id, phone_enc, phone_hash)
		 VALUES ($1, 't487_dup_a', 'T487占号甲', '$2a$08$fakehashfortest', 'ROLE_DOCTOR', $2, $3)`,
		dupA, []byte{0x01}, dupHash)
	require.NoError(t, err)
	t.Cleanup(func() { t487DeleteAdmins(t, dupA, dupB, dupC) })

	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO admins (admin_id, username, name, password_hash, role_id, phone_enc, phone_hash)
		 VALUES ($1, 't487_dup_b', 'T487占号乙', '$2a$08$fakehashfortest', 'ROLE_DOCTOR', $2, $3)`,
		dupB, []byte{0x02}, dupHash)
	require.Error(t, err, "同一手机号被两个后台账号占作凭据 ⇒ 库里必须拦住")
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "应为 PG 约束错误，实得 %v", err)
	assert.Equal(t, "23505", pgErr.Code)
	assert.Equal(t, "uk_admins_phone_hash", pgErr.ConstraintName,
		"🔴 约束名是 repo 侧 isPhoneCollision 的唯一判据，改名等于把撞号漏成 500")

	// ⑤ 不同号不撞（否则医护录号直接不可用）
	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO admins (admin_id, username, name, password_hash, role_id, phone_enc, phone_hash)
		 VALUES ($1, 't487_dup_c', 'T487占号丙', '$2a$08$fakehashfortest', 'ROLE_DOCTOR', $2, $3)`,
		dupC, []byte{0x03}, phone.Hash("13911110001"))
	require.NoError(t, err)

	// ⑥ 不许顺带改掉 username 唯一键（本卡只加一条索引，不重写既有约束）
	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO admins (admin_id, username, name, password_hash, role_id)
		 VALUES ('ADM-T487-REUSER', 'it_admin', 'T487重名', '$2a$08$fakehashfortest', 'ROLE_DOCTOR')`)
	require.Error(t, err, "username 仍应唯一")
	t.Cleanup(func() { t487DeleteAdmins(t, "ADM-T487-REUSER") })
	require.True(t, errors.As(err, &pgErr))
	assert.Equal(t, "admins_username_key", pgErr.ConstraintName, "撞的应是既有的 username 唯一键")

	// ⑦ 存量账号（seed）没被这张索引卡住，也没被本卡顺手动过数据
	var seedHash *string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT phone_hash FROM admins WHERE admin_id = $1`, itAdmin).Scan(&seedHash))
	assert.Nil(t, seedHash, "🔴 本卡不回填 ⇒ 存量 admins 行仍为 NULL（继续 username 登录）")
}

// TestITMigration_T487_AdminPhoneRoundTrip down 撤干净、up 精确回来。
// 只测 up 等于没测 down：运维回滚 000032 时若留下半截列，下一条医护创建就会撞未知列。
func TestITMigration_T487_AdminPhoneRoundTrip(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { t487RestoreUp(t) })

	runMigrationFile(t, t487DownFile)

	var colCount int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_name = 'admins' AND column_name IN ('phone_enc','phone_hash')`).Scan(&colCount))
	assert.Zero(t, colCount, "down 后两列都应消失")
	_, _, _, ok := t487IndexShape(t)
	assert.False(t, ok, "down 后索引应被撤掉")

	// down 不许伤及既有数据：账号还在、用户名登录腿照旧读得到
	admin, err := itStore.GetAdminByUsername(ctx, "it_admin")
	require.NoError(t, err)
	require.NotNil(t, admin, "回滚只删列，不该把账号删掉")

	runMigrationFile(t, t487UpFile)

	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_name = 'admins' AND column_name IN ('phone_enc','phone_hash')`).Scan(&colCount))
	assert.Equal(t, 2, colCount, "再 up 应回到两列都在")
	unique, partial, _, ok := t487IndexShape(t)
	require.True(t, ok, "再 up 索引应重建")
	assert.True(t, unique)
	assert.True(t, partial)

	// 幂等：已建库重放（applyMigrations 每次全量前滚走的就是这条）
	runMigrationFile(t, t487UpFile)
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_name = 'admins' AND column_name IN ('phone_enc','phone_hash')`).Scan(&colCount))
	assert.Equal(t, 2, colCount, "重复执行 up 应幂等")
}

// ─────────────────────────────────────────────────────────────
// 登录双凭证的第二支
// ─────────────────────────────────────────────────────────────

// TestITGetAdminByPhoneHash_T487 命中 / 未命中 / 🔴 空串不得命中 NULL 行。
func TestITGetAdminByPhoneHash_T487(t *testing.T) {
	ctx := context.Background()
	const acct = "ADM-T487-LOGIN"
	t.Cleanup(func() { t487DeleteAdmins(t, acct) })

	num := "13800000487"
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO admins (admin_id, username, name, password_hash, role_id, status, phone_enc, phone_hash)
		 VALUES ($1, 't487_login_a', 'T487可登录号', '$2a$08$fakehashfortest', 'ROLE_DOCTOR', 'enabled', $2, $3)`,
		acct, []byte("t487-enc"), phone.Hash(num))
	require.NoError(t, err)

	got, err := itStore.GetAdminByPhoneHash(ctx, phone.Hash(num))
	require.NoError(t, err)
	require.NotNil(t, got, "录过手机号的账号应能按 phone_hash 命中")
	assert.Equal(t, acct, got.AdminID)
	assert.Equal(t, "t487_login_a", got.Username, "投影要带出 username ⇒ 两支登录拿到同一个行结构")
	assert.Equal(t, "ROLE_DOCTOR", got.RoleID)
	assert.Equal(t, "enabled", got.Status)

	miss, err := itStore.GetAdminByPhoneHash(ctx, phone.Hash("13800000000"))
	require.NoError(t, err)
	assert.Nil(t, miss, "未注册过的号回 (nil, nil)，handler 据此统一 401 而不是 500")

	// 🔴 空串（前端「没填手机号」）不得撞开任何一行：NULL 不参与等值比较
	empty, err := itStore.GetAdminByPhoneHash(ctx, "")
	require.NoError(t, err)
	assert.Nil(t, empty, "🔴 空手机号绝不能命中 phone_hash 为 NULL 的账号（否则无号账号被人用空串登进）")

	// seed 的无号账号也证一遍：它 phone_hash 为 NULL，任何哈希都取不到它
	legacy, err := itStore.GetAdminByPhoneHash(ctx, phone.Hash("13700000000"))
	require.NoError(t, err)
	assert.Nil(t, legacy)
}

// t487InputWithPhone 复用 T314 的构造，只把手机号换成真 SHA-256（撞号要撞在真实键上）
func t487InputWithPhone(name, num string) DoctorAccountInput {
	in := t314Input(name)
	in.PhoneEnc = []byte("t487-enc-" + num)
	in.PhoneHash = phone.Hash(num)
	return in
}

// ─────────────────────────────────────────────────────────────
// 医护通道上的撞号与镜像
// ─────────────────────────────────────────────────────────────

// TestITCreateDoctorAccount_T487_PairWrittenOnCreate 创建时就该两表成对落号：
// 🔴 admins 那份是登录凭据副本，缺它则「录了号却登不进」；doctors 那份是列表展示源。
func TestITCreateDoctorAccount_T487_PairWrittenOnCreate(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	num := "13500000487"
	row, err := itStore.CreateDoctorAccount(ctx, t487InputWithPhone("T487建档带号", num))
	require.NoError(t, err)
	tr.addRow(t, row)
	require.NotNil(t, row.AdminID)

	adminEnc, adminHash := t487AdminPhoneColumns(t, *row.AdminID)
	assert.Equal(t, []byte("t487-enc-"+num), adminEnc, "密文原样落 admins")
	require.NotNil(t, adminHash)
	assert.Equal(t, phone.Hash(num), *adminHash)

	docEnc, docHash := t361PhoneColumns(t, row.DoctorID)
	assert.Equal(t, []byte("t487-enc-"+num), docEnc, "档案侧同一份密文（列表掩码列的数据源）")
	require.NotNil(t, docHash)
	assert.Equal(t, phone.Hash(num), *docHash, "档案侧与凭据侧同源一个号，不分叉")

	// 未填手机号 ⇒ 两列一起 NULL（不许出现「有密文无哈希」的半对）。
	// ⚠ t314Input 默认就带一组手机号字节（T314/T361 的用例靠它），这里要显式清干净才是「未录号」
	//	—— 本机 PG14 真库跑过才发现：不清就直接把成对写入的密文读回来，断言假红。
	bareIn := t314Input("T487建档不带号")
	bareIn.PhoneEnc = nil
	bareIn.PhoneHash = ""
	bare, err := itStore.CreateDoctorAccount(ctx, bareIn)
	require.NoError(t, err)
	tr.addRow(t, bare)
	require.NotNil(t, bare.AdminID)
	bEnc, bHash := t487AdminPhoneColumns(t, *bare.AdminID)
	assert.Nil(t, bEnc, "未录号 ⇒ admins.phone_enc 为 NULL")
	assert.Nil(t, bHash, "未录号 ⇒ admins.phone_hash 为 NULL（部分索引不管它）")
	bDocEnc, bDocHash := t361PhoneColumns(t, bare.DoctorID)
	assert.Nil(t, bDocEnc, "未录号 ⇒ doctors.phone_enc 也为 NULL（两表同生同灭）")
	assert.Nil(t, bDocHash, "未录号 ⇒ doctors.phone_hash 也为 NULL")
}

// TestITCreateDoctorAccount_T487_PhoneCollision 撞号回 sentinel，
// 🔴 且不留「有登录账号无档案」或「有档案无登录账号」的半行。
func TestITCreateDoctorAccount_T487_PhoneCollision(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	shared := "13600000487"
	first, err := itStore.CreateDoctorAccount(ctx, t487InputWithPhone("T487同号甲", shared))
	require.NoError(t, err)
	tr.addRow(t, first)
	require.NotNil(t, first.AdminID)

	second, err := itStore.CreateDoctorAccount(ctx, t487InputWithPhone("T487同号乙", shared))
	require.ErrorIs(t, err, ErrAdminPhoneTaken, "撞号要回业务 sentinel，handler 才折得成 409")
	assert.Nil(t, second)

	// 这一轮 admins 先写、撞在自己的索引上 ⇒ 事务整体回滚，两表都不该有痕迹
	var docCount, adminCount int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT count(*) FROM doctors WHERE name = 'T487同号乙'`).Scan(&docCount))
	assert.Zero(t, docCount, "🔴 撞号失败不得留下医护档案")
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT count(*) FROM admins WHERE name = 'T487同号乙'`).Scan(&adminCount))
	assert.Zero(t, adminCount, "🔴 撞号那一发整体回滚，不得留下 admins 行")

	// 第一个账号不受牵连：号仍是它的，登录腿照旧命中
	owner, err := itStore.GetAdminByPhoneHash(ctx, phone.Hash(shared))
	require.NoError(t, err)
	require.NotNil(t, owner)
	assert.Equal(t, *first.AdminID, owner.AdminID)
}

// TestITUpdateDoctorAccount_T487_PhoneMirror 编辑通道四格：
// 不带手机号保持原值 / 改号两表同步 / 🔴 撞号两表都保持旧值 / 清空两表一起 NULL。
func TestITUpdateDoctorAccount_T487_PhoneMirror(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	oldNum := "13400000487"
	row, err := itStore.CreateDoctorAccount(ctx, t487InputWithPhone("T487编辑镜像", oldNum))
	require.NoError(t, err)
	tr.addRow(t, row)
	require.NotNil(t, row.AdminID)

	oldEnc, oldHash := t487AdminPhoneColumns(t, *row.AdminID)
	require.NotNil(t, oldHash)

	// ① 只改名的编辑不动手机号（前端「没碰手机号」通道）
	_, err = itStore.UpdateDoctorAccount(ctx, row.DoctorID, DoctorAccountUpdate{Name: strptrT314("T487编辑镜像改")})
	require.NoError(t, err)
	enc1, hash1 := t487AdminPhoneColumns(t, *row.AdminID)
	assert.Equal(t, oldEnc, enc1, "🔴 未下发手机号不得动 admins 的凭据副本")
	require.NotNil(t, hash1)
	assert.Equal(t, *oldHash, *hash1)

	// ② 改成一个没人占的新号：两表同步换成同一份
	newNum := "13300000487"
	newHash := phone.Hash(newNum)
	_, err = itStore.UpdateDoctorAccount(ctx, row.DoctorID, DoctorAccountUpdate{
		PhoneEnc: []byte("t487-enc-" + newNum), PhoneHash: &newHash,
	})
	require.NoError(t, err)
	enc2, hash2 := t487AdminPhoneColumns(t, *row.AdminID)
	assert.Equal(t, []byte("t487-enc-"+newNum), enc2)
	require.NotNil(t, hash2)
	assert.Equal(t, newHash, *hash2)
	hit, err := itStore.GetAdminByPhoneHash(ctx, newHash)
	require.NoError(t, err)
	require.NotNil(t, hit, "🔴 编辑后的新号必须真能当凭据用")
	assert.Equal(t, *row.AdminID, hit.AdminID)
	_, docHash2 := t361PhoneColumns(t, row.DoctorID)
	require.NotNil(t, docHash2)
	assert.Equal(t, newHash, *docHash2, "档案侧跟着同步，两边不指不同的号")

	// ③ 撞号回滚：别人占了这个号 ⇒ sentinel，且**两张表都保持第 ② 步的值**。
	//    repo 里 doctors 的 UPDATE 排在 admins 之前，没有事务就会把档案改成别人的号、
	//    凭据却还是旧号 —— 列表显示的号与能登录的号不是同一个。
	takenNum := "13200000487"
	taken, err := itStore.CreateDoctorAccount(ctx, t487InputWithPhone("T487先占号", takenNum))
	require.NoError(t, err)
	tr.addRow(t, taken)
	takenHash := phone.Hash(takenNum)
	_, err = itStore.UpdateDoctorAccount(ctx, row.DoctorID, DoctorAccountUpdate{
		PhoneEnc: []byte("t487-enc-" + takenNum), PhoneHash: &takenHash,
	})
	require.ErrorIs(t, err, ErrAdminPhoneTaken)
	enc3, hash3 := t487AdminPhoneColumns(t, *row.AdminID)
	assert.Equal(t, []byte("t487-enc-"+newNum), enc3, "撞号后凭据侧仍是第 ② 步的号")
	require.NotNil(t, hash3)
	assert.Equal(t, newHash, *hash3)
	_, docHash3 := t361PhoneColumns(t, row.DoctorID)
	require.NotNil(t, docHash3)
	assert.Equal(t, newHash, *docHash3, "🔴 撞号后档案侧也不许被改成别人的号（同事务回滚）")

	// ④ 明确清空（PhoneHash 指向空串）⇒ 两表一起 NULL，该账号退回用户名登录
	empty := ""
	_, err = itStore.UpdateDoctorAccount(ctx, row.DoctorID, DoctorAccountUpdate{PhoneHash: &empty})
	require.NoError(t, err)
	enc4, hash4 := t487AdminPhoneColumns(t, *row.AdminID)
	assert.Nil(t, enc4, "清空后凭据侧密文为 NULL")
	assert.Nil(t, hash4, "清空后凭据侧哈希为 NULL")
	docEnc4, docHash4 := t361PhoneColumns(t, row.DoctorID)
	assert.Nil(t, docEnc4, "清空后档案侧密文为 NULL")
	assert.Nil(t, docHash4, "清空后档案侧哈希为 NULL（不留半清空态）")
	gone, err := itStore.GetAdminByPhoneHash(ctx, newHash)
	require.NoError(t, err)
	assert.Nil(t, gone, "清空后旧号再也取不到账号")
}

// TestITDoctorPhoneWithoutAccount_T487 存量形态（admin_id 为 NULL）改手机号：
// 只落 doctors，🔴 不给 admins 凭空造行（造了就是一个没有用户名、却能被哈希命中的幽灵凭据）。
func TestITDoctorPhoneWithoutAccount_T487(t *testing.T) {
	ctx := context.Background()
	const orphanDoc = "DOC-T487-ORPHAN"
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO doctors (doctor_id, name, title, department, team_id, admin_id, status)
		 VALUES ($1, 'T487未绑账号', '医师', '康复科', $2, NULL, 'enabled')
		 ON CONFLICT (doctor_id) DO NOTHING`, orphanDoc, itTeam)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM doctors WHERE doctor_id = $1`, orphanDoc)
	})

	num := "13100000487"
	hash := phone.Hash(num)
	updated, err := itStore.UpdateDoctorAccount(ctx, orphanDoc, DoctorAccountUpdate{
		PhoneEnc: []byte("t487-enc-" + num), PhoneHash: &hash,
	})
	require.NoError(t, err, "档案侧改号不该被「没账号」挡住")
	assert.Nil(t, updated.AdminID)

	docEnc, docHash := t361PhoneColumns(t, orphanDoc)
	assert.Equal(t, []byte("t487-enc-"+num), docEnc, "档案侧照常落号")
	require.NotNil(t, docHash)
	assert.Equal(t, hash, *docHash)

	byPhone, err := itStore.GetAdminByPhoneHash(ctx, hash)
	require.NoError(t, err)
	assert.Nil(t, byPhone, "🔴 未绑账号的档案不得因此产出一个可登录的凭据")

	var ghostAdmin int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT count(*) FROM admins WHERE phone_hash = $1`, hash).Scan(&ghostAdmin))
	assert.Zero(t, ghostAdmin, "🔴 不许为无账号的档案新建 admins 行")

	// 清空走同一条通道：档案侧落 NULL，仍不报错
	empty := ""
	_, err = itStore.UpdateDoctorAccount(ctx, orphanDoc, DoctorAccountUpdate{PhoneHash: &empty})
	require.NoError(t, err)
	_, docHashAfter := t361PhoneColumns(t, orphanDoc)
	assert.Nil(t, docHashAfter)
}

// ─────────────────────────────────────────────────────────────
// 自助改密（POST /api/v1/auth/change-password 的 repo 半边）
// ─────────────────────────────────────────────────────────────

// TestITAdminSelfPassword_T487 网关注入的 X-User-Id 走 GetAdminByID 取回当前哈希，
// UpdateAdminPasswordHash 只换 password_hash —— 🔴 手机号、用户名、角色、状态都不许被连带改动，
// 否则「改个密码」会把人的登录凭据洗没。0 行受影响必须报错，不许假成功。
func TestITAdminSelfPassword_T487(t *testing.T) {
	ctx := context.Background()
	const acct = "ADM-T487-SELF"
	num := "13000000487"
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO admins (admin_id, username, name, password_hash, role_id, status, phone_enc, phone_hash)
		 VALUES ($1, 't487_self_a', 'T487自助改密', '$2a$08$oldhash', 'ROLE_DOCTOR', 'enabled', $2, $3)
		 ON CONFLICT (admin_id) DO NOTHING`, acct, []byte("t487-enc"), phone.Hash(num))
	require.NoError(t, err)
	t.Cleanup(func() { t487DeleteAdmins(t, acct) })

	cur, err := itStore.GetAdminByID(ctx, acct)
	require.NoError(t, err)
	require.NotNil(t, cur, "改密前按主键必须取回账号（X-User-Id 读的就是这一行）")
	assert.Equal(t, "$2a$08$oldhash", cur.PasswordHash)

	newHash := "$2a$08$newhashfort487"
	require.NoError(t, itStore.UpdateAdminPasswordHash(ctx, acct, newHash))

	after, err := itStore.GetAdminByID(ctx, acct)
	require.NoError(t, err)
	assert.Equal(t, newHash, after.PasswordHash, "新哈希原样落库")
	assert.Equal(t, cur.Username, after.Username, "改密不换登录用户名")
	assert.Equal(t, cur.RoleID, after.RoleID, "🔴 改密不得动角色（越权面）")
	assert.Equal(t, cur.Status, after.Status, "改密不得顺带启停账号")

	encAfter, hashAfter := t487AdminPhoneColumns(t, acct)
	assert.Equal(t, []byte("t487-enc"), encAfter, "🔴 改密不得动手机号密文")
	require.NotNil(t, hashAfter)
	assert.Equal(t, phone.Hash(num), *hashAfter, "改密后手机号仍是同一份凭据")

	// 按手机号仍能取到该账号，且带的是新哈希（两支登录指向同一行）
	byPhone, err := itStore.GetAdminByPhoneHash(ctx, phone.Hash(num))
	require.NoError(t, err)
	require.NotNil(t, byPhone)
	assert.Equal(t, newHash, byPhone.PasswordHash)

	miss, err := itStore.GetAdminByID(ctx, "ADM-T487-NOPE")
	require.NoError(t, err)
	assert.Nil(t, miss, "不存在回 (nil, nil)，handler 才不会把「查无此人」报成 500")

	require.Error(t, itStore.UpdateAdminPasswordHash(ctx, "ADM-T487-NOPE", newHash),
		"0 行受影响必须报错，不许假装重置成功")
}
