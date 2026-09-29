//go:build integration
// +build integration

// Package repo — T484 患者 NULL 口令的读侧真库验证（三支链路的共同根因）。
//
// 缺陷本体在 SQL 扫描这一步，不在 handler：patients.password_hash 自迁移 000005 起可空，
// 而 GetPatientByPhoneHash / GetPatientByWXOpenID 把该列直接扫进 Go 的 string。
// pgx 对 NULL 目标写的是 "can't scan into dest[2]: cannot scan NULL into *string"，
// 它不是 pgx.ErrNoRows ⇒ 读函数原有的「查无此行」收口接不住，上抛后被三条调用链
// （患者密码登录 handler.go、微信 openid 登录、绑定手机 bind_phone.go）的错误分支吃掉成 500。
//
// 现网该列为空的来源是既定语义：后台建号通路 CreatePatient 的列清单不含口令列，
// 落库即 NULL（患者要靠 T477 的设密端点或微信侧链路才有口令）。本卡不改这一条。
//
// 锁四格：
//  1. 建档通路造出的 NULL 口令行，手机号读侧必须 (非 nil 行, nil error) —— 报错就是本卡的 500；
//  2. 抹平出来的 PasswordHash 是空串（handler 按空串判凭据无效，回 401）；
//  3. openid 读侧同款 —— 绑定手机链路与微信登录走的是它；
//  4. 正对照：有口令的患者必须仍原样取回哈希，不能被抹平成空串，否则 T477 的设密闭环断在这里。
//
// 自成行自删（P-T484-IT-*），不碰 seedITData 的行（别的用例按那些行断言列表总数）。
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
	itT484WxOpenid  = "oT484NullPwd"
	itT484WxPatient = "P-T484-IT-2"
)

// t484ReadPwdHash 库侧直查该列，返回指针以区分 NULL 与空串。
func t484ReadPwdHash(ctx context.Context, t *testing.T, patientID string) *string {
	t.Helper()
	var h *string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT password_hash FROM patients WHERE patient_id = $1`, patientID).Scan(&h))
	return h
}

// t484Patient 走后台建档通路（列清单不含口令列）建一行患者，登记清场，返回患者号与 phone_hash。
func t484Patient(ctx context.Context, t *testing.T, phoneNum string) (string, string) {
	t.Helper()
	phoneHash := phone.Hash(phoneNum)
	enc := []byte{0x00}
	created, err := itStore.CreatePatient(ctx, PatientInput{
		Name: "T484无口令患者", PhoneEnc: &enc, PhoneHash: &phoneHash,
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	t.Cleanup(func() {
		if _, err := itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, created.PatientID); err != nil {
			t.Logf("t484 cleanup %s: %v", created.PatientID, err)
		}
	})
	return created.PatientID, phoneHash
}

func TestITGetPatientByPhoneHashAllowsNullPassword_T484(t *testing.T) {
	ctx := context.Background()
	patientID, phoneHash := t484Patient(ctx, t, "13700137013")

	// 造数据起点：建档通路确实把该列留成 NULL，否则这一格空对空
	require.Nil(t, t484ReadPwdHash(ctx, t, patientID), "后台建号不写 password_hash，起点必须是 NULL")

	// 本卡判据：NULL 口令不是查询错误
	row, err := itStore.GetPatientByPhoneHash(ctx, phoneHash)
	require.NoError(t, err, "password_hash 为 NULL 不得让患者登录读侧报错 ⇒ handler 才不会回 500")
	require.NotNil(t, row, "行确实存在，不能被读成「不存在」")
	assert.Equal(t, patientID, row.PatientID)
	assert.Equal(t, "", row.PasswordHash, "NULL 抹平成空串（handler 按空串判凭据无效）")

	// 同行其余投影不受牵连
	var name, status string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT name, status FROM patients WHERE patient_id = $1`, patientID).Scan(&name, &status))
	assert.Equal(t, name, row.Name)
	assert.Equal(t, status, row.Status)
}

func TestITGetPatientByWXOpenIDAllowsNullPassword_T484(t *testing.T) {
	ctx := context.Background()

	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO patients (patient_id, name, wx_openid, status)
		 VALUES ($1, 'T484微信无口令患者', $2, 'active')
		 ON CONFLICT (patient_id) DO NOTHING`,
		itT484WxPatient, itT484WxOpenid)
	require.NoError(t, err)
	t.Cleanup(func() {
		if _, err := itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, itT484WxPatient); err != nil {
			t.Logf("t484 cleanup openid: %v", err)
		}
	})
	require.Nil(t, t484ReadPwdHash(ctx, t, itT484WxPatient), "起点必须是 NULL")

	// 微信 openid 登录与绑定手机链路读的是这个函数
	row, err := itStore.GetPatientByWXOpenID(ctx, itT484WxOpenid)
	require.NoError(t, err, "openid 侧同族：NULL 口令不得报错")
	require.NotNil(t, row)
	assert.Equal(t, itT484WxPatient, row.PatientID)
	assert.Equal(t, "", row.PasswordHash)
	assert.Equal(t, "active", row.Status)
}

func TestITGetPatientByPhoneHashStillReadsHash_T484(t *testing.T) {
	ctx := context.Background()
	pwd := "BrT484hashtest#7"
	patientID, phoneHash := t484Patient(ctx, t, "13700137014")

	// T477 的设密通路（本卡不动写侧，用它把该列填上做正对照）
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), 8)
	require.NoError(t, err)
	require.NoError(t, itStore.SetPatientPassword(ctx, patientID, string(hash)))

	row, err := itStore.GetPatientByPhoneHash(ctx, phoneHash)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.NotEqual(t, "", row.PasswordHash, "有口令的患者不能被抹平成空串")
	stored := t484ReadPwdHash(ctx, t, patientID)
	require.NotNil(t, stored)
	assert.Equal(t, *stored, row.PasswordHash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(pwd)),
		"登录侧用该哈希比对口令必须通过")
}
