//go:build integration
// +build integration

// Package repo — T477 SetPatientPassword 的真库验证。
//
// 单测层已经锁住「admin 判定 + 只写一次 + 落 bcrypt 哈希」，这里补的是 SQL 本体那三格：
//  1. 只换 password_hash，同一行的其他列不许被这条 UPDATE 连带改动；
//  2. 写进去的值能被登录查询（GetPatientByPhoneHash，T037 患者密码登录读的就是它）原样取回，
//     否则 CI 的「自建患者 + 手机号密码登录」闭环在库侧就断了；
//  3. 未知患者号回 ErrPatientNotFound sentinel（handler 据此出 404）。
//
// 自成一行一删，不碰 seedITData 的 P-USR-IT-1/2（别的用例按那两行做断言）。
package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/phone"
)

const itT477Patient = "P-T477-IT-1"

// seedT477Patient 造一行「API 建档后应有的样子」：password_hash 恒 NULL（那就是本卡要修的洞）。
// phone_hash 直接调 phone.Hash，与 handler 登录查库时算的是同一个键 ⇒ 两条腿在库侧对上。
func seedT477Patient(ctx context.Context, t *testing.T) string {
	t.Helper()
	phoneHash := phone.Hash("13700137000")
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, status)
		 VALUES ($1, 'T477集成患者', '\x00'::bytea, $2, 'male', 12, 'active')
		 ON CONFLICT (patient_id) DO NOTHING`,
		itT477Patient, phoneHash)
	require.NoError(t, err)
	t.Cleanup(func() {
		if _, err := itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, itT477Patient); err != nil {
			t.Logf("t477 cleanup: %v", err)
		}
	})
	return phoneHash
}

func TestITSetPatientPassword_T477(t *testing.T) {
	ctx := context.Background()
	phoneHash := seedT477Patient(ctx, t)

	var beforeName, beforeStatus string
	var beforeHash *string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT name, status, password_hash FROM patients WHERE patient_id = $1`, itT477Patient).
		Scan(&beforeName, &beforeStatus, &beforeHash))
	require.Nil(t, beforeHash, "建档通路不写 password_hash，起点必须是 NULL")

	// 成本 8 与 handler 侧 GenerateBcryptHash 一致（列宽 VARCHAR(60)，cost 8 的哈希正好 60 字符）
	hash, err := bcrypt.GenerateFromPassword([]byte("BrT477samplepwd#7"), 8)
	require.NoError(t, err)
	require.NoError(t, itStore.SetPatientPassword(ctx, itT477Patient, string(hash)))

	var afterName, afterStatus, gotHash string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT name, status, password_hash FROM patients WHERE patient_id = $1`, itT477Patient).
		Scan(&afterName, &afterStatus, &gotHash))
	assert.Equal(t, string(hash), gotHash, "写入值必须原样落库")
	assert.Equal(t, beforeName, afterName, "只换 password_hash，姓名不得被连带改动")
	assert.Equal(t, beforeStatus, afterStatus, "状态不得被连带改动")

	// 登录查询投影必须能看到新哈希（患者端密码登录读的就是这一列）
	row, err := itStore.GetPatientByPhoneHash(ctx, phoneHash)
	require.NoError(t, err)
	require.NotNil(t, row, "登录查询按 phone_hash 必须命中本行")
	assert.Equal(t, itT477Patient, row.PatientID)
	assert.Equal(t, string(hash), row.PasswordHash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("BrT477samplepwd#7")),
		"登录侧用该哈希比对口令必须通过")

	// 重设覆盖旧哈希 ⇒ 旧口令即时失效
	require.NoError(t, itStore.SetPatientPassword(ctx, itT477Patient, "$2a$08$secondhashfort477"))
	var second string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT password_hash FROM patients WHERE patient_id = $1`, itT477Patient).Scan(&second))
	assert.Equal(t, "$2a$08$secondhashfort477", second, "重设必须覆盖旧哈希")
}

func TestITSetPatientPassword_UnknownPatient_T477(t *testing.T) {
	ctx := context.Background()

	err := itStore.SetPatientPassword(ctx, "P-T477-NOPE", "$2a$08$whateverhashfort477")

	assert.True(t, errors.Is(err, ErrPatientNotFound), "不存在应回 sentinel：%v", err)
}
