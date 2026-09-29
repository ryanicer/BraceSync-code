//go:build integration
// +build integration

// Package repo — T483 技师 NULL 口令的读侧真库验证。
//
// 缺陷本体在 SQL 扫描这一步，不在 handler：technicians.password_hash 自迁移 000005 起可空，
// 而 GetTechByPhoneHash 早把该列直接扫进 Go 的 string。pgx 对 NULL 目标写的是
// "can't scan into dest[2]: cannot scan NULL into *string"，它不是 pgx.ErrNoRows，
// 于是读函数把它当错误往上抛 ⇒ handler 的 if err != nil 分支给 ErrInternal ⇒ HTTP 500。
// 「无口令」是数据态、不是查询失败，口径上该走统一 401。
//
// 这里锁三格：
//  1. NULL 口令行必须 (非 nil 行, nil error) —— 报错就是本卡的 500；
//  2. 抹平出来的 PasswordHash 是空串，不是别的哨兵值（handler 按空串判凭据无效）；
//  3. 同行其余投影（team_id 可空、status/auth_status）不受牵连，
//     且同一 phone_hash 上有口令的技师仍能原样取回哈希。
//
// 自成行自删（TECH-T483-IT-*），不碰 TECH-USR-IT-1（TestITTechnicianLifecycle 按它断言列表总数）。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/phone"
)

const (
	itT483TechNull = "TECH-T483-IT-1"
	itT483TechHash = "TECH-T483-IT-2"
)

// t483Tech 注册删行并返回 phone_hash（与 handler 登录时算的是同一个键）。
func t483Tech(ctx context.Context, t *testing.T, techID, phoneNum string) string {
	t.Helper()
	phoneHash := phone.Hash(phoneNum)
	t.Cleanup(func() {
		if _, err := itStore.pool.Exec(ctx, `DELETE FROM technicians WHERE tech_id = $1`, techID); err != nil {
			t.Logf("t483 cleanup %s: %v", techID, err)
		}
	})
	return phoneHash
}

func TestITGetTechByPhoneHashAllowsNullPassword_T483(t *testing.T) {
	ctx := context.Background()
	phoneHash := t483Tech(ctx, t, itT483TechNull, "13700137011")

	// 未传口令的建档通路 ⇒ 库里该列落 NULL（T480 已锁「不得洗成空串」）
	created, err := itStore.CreateTechnician(ctx, TechInput{
		TechID: itT483TechNull, Name: "T483无口令技师", PhoneEnc: []byte("\x00"),
		PhoneHash: phoneHash,
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Nil(t, t480ReadHash(ctx, t, itT483TechNull), "造数据起点必须是 NULL，否则这一格空对空")

	// 本卡判据：NULL 口令不是查询错误
	row, err := itStore.GetTechByPhoneHash(ctx, phoneHash)
	require.NoError(t, err, "password_hash 为 NULL 不得让登录读侧报错 ⇒ handler 才不会回 500")
	require.NotNil(t, row, "行确实存在，不能被读成「不存在」")
	assert.Equal(t, itT483TechNull, row.TechID)
	assert.Equal(t, "", row.PasswordHash, "NULL 抹平成空串（handler 按空串判凭据无效）")

	// 同列其余投影不受牵连
	var teamID *string
	var status, authStatus string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT team_id, status, auth_status FROM technicians WHERE tech_id = $1`, itT483TechNull).
		Scan(&teamID, &status, &authStatus))
	assert.Nil(t, teamID, "未分配团队仍是 NULL")
	assert.Equal(t, status, row.Status)
	assert.Equal(t, authStatus, row.AuthStatus)
}

func TestITGetTechByPhoneHashStillReadsHash_T483(t *testing.T) {
	ctx := context.Background()
	pwd := "BrT483hashtest#7"
	phoneHash := t483Tech(ctx, t, itT483TechHash, "13700137012")

	_, err := itStore.CreateTechnician(ctx, TechInput{
		TechID: itT483TechHash, Name: "T483有口令技师", PhoneEnc: []byte("\x00"),
		PhoneHash: phoneHash, PasswordHash: t480Bcrypt(t, pwd),
	})
	require.NoError(t, err)

	row, err := itStore.GetTechByPhoneHash(ctx, phoneHash)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.NotEqual(t, "", row.PasswordHash, "有口令的技师不能被抹平成空串 ⇒ 首登闭环仍通")
	stored := t480ReadHash(ctx, t, itT483TechHash)
	require.NotNil(t, stored)
	assert.Equal(t, *stored, row.PasswordHash)
}
