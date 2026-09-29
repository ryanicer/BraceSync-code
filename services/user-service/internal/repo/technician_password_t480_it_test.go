//go:build integration
// +build integration

// Package repo — T480 技师登录口令的真库验证。
//
// 单测层（handler）已经锁住「服务端发号 + 一次性返回明文 + admin 判定」，这里补 SQL 本体那三格：
//  1. CreateTechnician 的 INSERT 必须真带上 password_hash —— 这一列不在 INSERT 里就是本卡的根因
//     （后台新建的技师 password_hash 恒 NULL，而技师端登录比对的就是它 ⇒ 永远登不进小程序）；
//     同时锁「未带口令 ⇒ 仍是 NULL」，不能把建档通路洗成空串口令；
//  2. 写入值能被登录查询（GetTechByPhoneHash，T037 技师密码登录读的就是它）原样取回并 bcrypt 验过，
//     否则「后台建号 → 技师端首登」这条闭环在库侧就是断的；
//  3. 重置只换 password_hash：同一行的 name/status/auth_status/team_id/install_count 不许被连带改动，
//     且 WHERE 必须锁在给定 tech_id 上（另一行的哈希不动）—— 否则一次重置会洗掉别人的口令。
//
// 另锁一格编辑通路：UpdateTechnician 的 SET 列表里没有 password_hash ⇒ 改资料不得换登录凭证。
//
// 自成行自删（TECH-T480-IT-*），不碰 TECH-USR-IT-1（TestITTechnicianLifecycle 按它断言列表总数）。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/phone"
)

const (
	itT480TechA = "TECH-T480-IT-1"
	itT480TechB = "TECH-T480-IT-2"
)

// t480Tech 占一行技师：注册删行 + 返回其 phone_hash。
// phone_hash 直接调 phone.Hash，与 handler 登录时算的是同一个键 ⇒ 两条腿在库侧对上。
func t480Tech(ctx context.Context, t *testing.T, techID, phoneNum string) string {
	t.Helper()
	phoneHash := phone.Hash(phoneNum)
	t.Cleanup(func() {
		if _, err := itStore.pool.Exec(ctx, `DELETE FROM technicians WHERE tech_id = $1`, techID); err != nil {
			t.Logf("t480 cleanup %s: %v", techID, err)
		}
	})
	return phoneHash
}

// t480Bcrypt 与 handler 侧 GenerateBcryptHash 同参数（成本 8；列宽 VARCHAR(60)，cost 8 的哈希正好 60 字符）
func t480Bcrypt(t *testing.T, pwd string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), 8)
	require.NoError(t, err)
	return string(hash)
}

// t480ReadHash 直读口令列（TechnicianRow 的投影故意不含 password_hash ⇒ 只能走 pool）
func t480ReadHash(ctx context.Context, t *testing.T, techID string) *string {
	t.Helper()
	var h *string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT password_hash FROM technicians WHERE tech_id = $1`, techID).Scan(&h))
	return h
}

func TestITCreateTechnicianWritesPasswordHash_T480(t *testing.T) {
	ctx := context.Background()
	pwd := "BrT480createpwd#7"
	phoneHash := t480Tech(ctx, t, itT480TechA, "13700137001")

	// 带口令建档：INSERT 必须把哈希写进去
	created, err := itStore.CreateTechnician(ctx, TechInput{
		TechID: itT480TechA, Name: "T480集成技师", PhoneEnc: []byte("\x00"),
		PhoneHash: phoneHash, PasswordHash: t480Bcrypt(t, pwd),
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	stored := t480ReadHash(ctx, t, itT480TechA)
	require.NotNil(t, stored, "INSERT 必须带 password_hash，落库不能还是 NULL")

	// 登录查询投影必须能看到该哈希（技师端密码登录读的就是这一列）
	row, err := itStore.GetTechByPhoneHash(ctx, phoneHash)
	require.NoError(t, err)
	require.NotNil(t, row, "登录查询按 phone_hash 必须命中本行")
	assert.Equal(t, itT480TechA, row.TechID)
	assert.Equal(t, *stored, row.PasswordHash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(pwd)),
		"登录侧用该哈希比对口令必须通过 ⇒「后台建号 → 技师端首登」在库侧闭合")

	// 未带口令的通路（既有集成测试、任何绕过发号的调用）必须仍是 NULL，不是空串
	teamID := itTeam
	barePhoneHash := t480Tech(ctx, t, itT480TechB, "13700137002")
	bare, err := itStore.CreateTechnician(ctx, TechInput{
		TechID: itT480TechB, Name: "T480无口令技师", PhoneEnc: []byte("\x00"),
		PhoneHash: barePhoneHash, TeamID: &teamID,
	})
	require.NoError(t, err)
	require.NotNil(t, bare)
	assert.Nil(t, t480ReadHash(ctx, t, itT480TechB), "未传口令 ⇒ 写 NULL，不得洗成空串")
}

func TestITSetTechnicianPassword_T480(t *testing.T) {
	ctx := context.Background()
	oldPwd := "BrT480firstpwd#7"
	phoneHash := t480Tech(ctx, t, itT480TechA, "13700137001")
	_, err := itStore.CreateTechnician(ctx, TechInput{
		TechID: itT480TechA, Name: "T480集成技师", PhoneEnc: []byte("\x00"),
		PhoneHash: phoneHash, PasswordHash: t480Bcrypt(t, oldPwd),
	})
	require.NoError(t, err)

	// B 行是「别人」：重置 A 时它的哈希必须原样不动（证明 UPDATE 的 WHERE 锁在 tech_id 上）
	otherPhoneHash := t480Tech(ctx, t, itT480TechB, "13700137002")
	_, err = itStore.CreateTechnician(ctx, TechInput{
		TechID: itT480TechB, Name: "T480他人技师", PhoneEnc: []byte("\x00"),
		PhoneHash: otherPhoneHash, PasswordHash: t480Bcrypt(t, "BrT480otherpwd#7"),
	})
	require.NoError(t, err)
	otherHash := t480ReadHash(ctx, t, itT480TechB)
	require.NotNil(t, otherHash, "B 行起点必须已带口令，否则「不受牵连」那格是空对空")

	var beforeName, beforeStatus, beforeAuth string
	var beforeTeam *string
	var beforeInstall int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT name, status, auth_status, team_id, install_count
		 FROM technicians WHERE tech_id = $1`, itT480TechA).
		Scan(&beforeName, &beforeStatus, &beforeAuth, &beforeTeam, &beforeInstall))

	newHash := t480Bcrypt(t, "BrT480resetpwd#7")
	require.NoError(t, itStore.SetTechnicianPassword(ctx, itT480TechA, newHash))

	var afterName, afterStatus, afterAuth, gotHash string
	var afterTeam *string
	var afterInstall int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT name, status, auth_status, team_id, install_count, password_hash
		 FROM technicians WHERE tech_id = $1`, itT480TechA).
		Scan(&afterName, &afterStatus, &afterAuth, &afterTeam, &afterInstall, &gotHash))
	assert.Equal(t, newHash, gotHash, "重置值必须原样落库")
	assert.Equal(t, beforeName, afterName, "只换 password_hash，姓名不得被连带改动")
	assert.Equal(t, beforeStatus, afterStatus, "启停状态不得被连带改动")
	assert.Equal(t, beforeAuth, afterAuth, "授权状态不得被连带改动")
	assert.Equal(t, beforeTeam, afterTeam, "团队归属不得被连带改动")
	assert.Equal(t, beforeInstall, afterInstall, "装机数不得被连带改动")

	assert.Equal(t, *otherHash, *t480ReadHash(ctx, t, itT480TechB), "重置 A 不得动到 B 的口令")

	// 登录侧：旧口令即时失效、新口令可登
	row, err := itStore.GetTechByPhoneHash(ctx, phoneHash)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, newHash, row.PasswordHash)
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(oldPwd)),
		"重置后旧口令必须登不进")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("BrT480resetpwd#7")))
}

// 编辑资料不得换登录凭证：UpdateTechnician 的 SET 列表里没有 password_hash。
// 这一格防的是「以后有人给编辑通路补口令字段」把技师口令洗掉。
func TestITUpdateTechnicianKeepsPasswordHash_T480(t *testing.T) {
	ctx := context.Background()
	pwd := "BrT480editpwd#7"
	phoneHash := t480Tech(ctx, t, itT480TechA, "13700137001")
	_, err := itStore.CreateTechnician(ctx, TechInput{
		TechID: itT480TechA, Name: "T480集成技师", PhoneEnc: []byte("\x00"),
		PhoneHash: phoneHash, PasswordHash: t480Bcrypt(t, pwd),
	})
	require.NoError(t, err)
	before := t480ReadHash(ctx, t, itT480TechA)
	require.NotNil(t, before)

	teamID := itTeam
	updated, err := itStore.UpdateTechnician(ctx, itT480TechA, TechInput{
		Name: "T480集成技师改", PhoneEnc: []byte("\x01"), PhoneHash: phoneHash, TeamID: &teamID,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "T480集成技师改", updated.Name)

	after := t480ReadHash(ctx, t, itT480TechA)
	require.NotNil(t, after, "编辑后口令不得被置空")
	assert.Equal(t, *before, *after, "改资料不得动口令")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(*after), []byte(pwd)),
		"原口令仍能比对 ⇒ 编辑通路没把凭证换掉")
}
