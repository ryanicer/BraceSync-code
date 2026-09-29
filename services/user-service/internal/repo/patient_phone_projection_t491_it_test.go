//go:build integration
// +build integration

// Package repo 集成测试：T491「患者列表投影与详情投影一致性修复」的库侧证据
//
// 缺陷本体（T462 待裁一，PM 2026-09-29 裁 1-A 立本卡）：patientSelect 没投影 patients.phone_enc，
// scanPatient 也没有对应入参 ⇒ 三条读函数（ListPatients / GetPatient / GetPatientInTeam）回行的
// PatientRow.PhoneEnc 恒为空切片。消费方是 handler 侧改号审计的「改前快照」：空密文落 phone.View
// 的 absent 分支，于是留痕永远写「无手机号（absent）」，而库里存着能解开的真号 ——
// Joe 在 e2e-real 链A 用例里把这条钉成了红。
//
// 为什么必须在真库里证（也是本卡的存在理由）：handler 单测注入的 fakeStore 直接往行结构体塞
// PhoneEnc（audit_t450_test.go 用 t450CipherPhone 造好密文），投影缺列它照样绿。也就是说
// 「服务内脱敏逻辑」早就测透了，瞎的是「投影有没有把列带回服务内」这一段，只有真库读一次才看得见。
//
// 锁四格：
//  1. 建档写入的 AES-GCM 密文，GetPatient 必须原样带回、能解回明文，且三态判成 masked
//     （改前恒为空切片 ⇒ View 判 absent，就是缺陷现形处）；
//  2. 带回的字节与库内该列逐字节相同，且列表 / 详情 / 团队内详情三条读法值级一致
//     ——卡面目标「列表投影与详情投影对齐」按字节判，不按「都非空」判；
//  3. 密文列为 NULL 的行（微信-only，迁移 000008 解除两列 NOT NULL）必须仍是 (非 nil 行, nil error)：
//     新扫一列就把这类行读崩，正是 T483/T484 那一族「非指针目标扫 NULL」的复发，
//     这一格证明 []byte 这一支安全（pgx 把 NULL 写成 nil 切片，不报错）；
//  4. 解不开的占位密文（与种子 '\x00'::bytea 同形）原样透传并判成 unreadable，
//     证明补投影没有把「有号但读不出」顺手洗成「没填手机号」（T361 的三态口径是本域既有约定）。
//
// 独立 ID + 测后清场（口径同 patient_null_password_t484_it_test.go）。
// ⚠ 本包 repo_integration_test.go 的列表用例断言种子患者总数与团队筛选总数 ⇒ 残行会连坐。
// 清场函数里禁用 require / FailNow（FailNow 只允许发生在测试函数体内），失败只 t.Errorf。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/phone"
)

const (
	// t491TestPhoneKey 测试专用假密钥（64 位 hex，不是任何环境的 PHONE_ENC_KEY）。
	// IT 只需要「同一把 key 加密再解密」这条自洽性，不需要与部署密钥一致。
	t491TestPhoneKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	// 三个号分属三个用例：patients.phone_hash 是唯一键，同值会让第二次建档直接判重。
	t491PhoneMasked = "13911112222"
	t491PhoneAgree  = "13911113333"
	t491PhoneJunk   = "13911114444" // 只用来占一个不冲突的 hash，密文是解不开的占位
)

// t491DBPhoneEnc 直查 patients.phone_enc，绕开投影，确保「投影带回的」与「库里那份」是两个可比的量
func t491DBPhoneEnc(t *testing.T, patientID string) []byte {
	t.Helper()
	var arg any
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT phone_enc FROM patients WHERE patient_id = $1`, patientID).Scan(&arg))
	switch v := arg.(type) {
	case nil:
		return nil
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		t.Fatalf("phone_enc unexpected type %T", v)
		return nil
	}
}

// t491Patient 用建档通路写一行患者（登记清场），密文由调用方给定
func t491Patient(t *testing.T, name string, enc []byte, plainForHash string) string {
	t.Helper()
	ctx := context.Background()
	in := PatientInput{Name: name}
	if enc != nil {
		in.PhoneEnc = &enc
		hash := phone.Hash(plainForHash)
		in.PhoneHash = &hash
	}
	created, err := itStore.CreatePatient(ctx, in)
	require.NoError(t, err)
	require.NotNil(t, created)
	t.Cleanup(func() {
		if _, delErr := itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, created.PatientID); delErr != nil {
			t.Errorf("t491 cleanup %s: %v", created.PatientID, delErr)
		}
	})
	return created.PatientID
}

// TestITPatientPhoneProjectionT491_DetailCarriesDecryptableCiphertext 判据 1 + 2 前半：
// 详情读法必须把密文带回服务内，并能解回建档时那个号（缺陷下这一格是空切片，View 判 absent）。
func TestITPatientPhoneProjectionT491_DetailCarriesDecryptableCiphertext(t *testing.T) {
	ctx := context.Background()
	cipher, err := phone.NewCipher(t491TestPhoneKey)
	require.NoError(t, err)
	enc, err := cipher.Encrypt(t491PhoneMasked)
	require.NoError(t, err)
	patientID := t491Patient(t, "T491带号患者", enc, t491PhoneMasked)

	row, err := itStore.GetPatient(ctx, patientID)
	require.NoError(t, err)
	require.NotNil(t, row)

	require.NotEmpty(t, row.PhoneEnc, "读侧投影必须带回 phone_enc（改前恒空 = 本卡缺陷本体）")
	assert.Equal(t, enc, row.PhoneEnc, "建档响应回读的那一行同样要带密文（写侧口径不受影响）")
	assert.Equal(t, t491DBPhoneEnc(t, patientID), row.PhoneEnc,
		"投影带回的字节要与库内该列逐字节相同（证明取的正是这一列，不是别的列）")

	plain, decErr := cipher.Decrypt(row.PhoneEnc)
	require.NoError(t, decErr, "带回的密文必须解得开，否则改号审计的改前快照仍然读不出值")
	assert.Equal(t, t491PhoneMasked, plain)

	// 缺陷消费方判据：改前快照走的就是 phone.View —— 改前判 absent，本卡后要判 masked
	view := phone.View(cipher, row.PhoneEnc)
	assert.Equal(t, phone.PhoneStateMasked, view.State, "真库读回的密文要能被判成 masked")
	assert.Equal(t, phone.Mask(t491PhoneMasked), view.Masked)
}

// TestITPatientPhoneProjectionT491_ThreeReadPathsAgree 卡面目标「列表投影与详情投影对齐」：
// 三条读函数共用 patientSelect + scanPatient，本格按字节判一致（不是「都非空」）。
// 「只补 SELECT 不补扫描」这类半成品改动到不了这里 —— 占位符数与入参数不等，Scan 先报错。
func TestITPatientPhoneProjectionT491_ThreeReadPathsAgree(t *testing.T) {
	ctx := context.Background()
	cipher, err := phone.NewCipher(t491TestPhoneKey)
	require.NoError(t, err)
	enc, err := cipher.Encrypt(t491PhoneAgree)
	require.NoError(t, err)
	patientID := t491Patient(t, "T491三读一致", enc, t491PhoneAgree)

	// 团队谓词那一条得有团队：走既有写通道，不自己 UPDATE
	assigned, err := itStore.AssignPatientTeam(ctx, patientID, itTeam)
	require.NoError(t, err)
	require.NotNil(t, assigned)

	dbEnc := t491DBPhoneEnc(t, patientID)

	detail, err := itStore.GetPatient(ctx, patientID)
	require.NoError(t, err)
	require.NotNil(t, detail)

	inTeam, err := itStore.GetPatientInTeam(ctx, patientID, itTeam)
	require.NoError(t, err)
	require.NotNil(t, inTeam, "本团队患者必须读得到")

	list, _, err := itStore.ListPatients(ctx, PatientFilter{Keyword: "T491三读一致", Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, list, 1)

	assert.Equal(t, dbEnc, detail.PhoneEnc, "详情投影")
	assert.Equal(t, dbEnc, inTeam.PhoneEnc, "团队内详情投影")
	assert.Equal(t, dbEnc, list[0].PhoneEnc, "列表投影")
	assert.Equal(t, detail.PhoneEnc, list[0].PhoneEnc, "列表与详情必须值级一致（卡面目标）")

	// 补扫一列最容易错在「插错位置」⇒ 其余投影逐列对拍，防列序错位
	assert.Equal(t, detail.PatientID, list[0].PatientID)
	assert.Equal(t, detail.Name, list[0].Name)
	assert.Equal(t, detail.Status, list[0].Status)
	assert.Equal(t, detail.CreatedAt, list[0].CreatedAt)
	assert.Equal(t, detail.UpdatedAt, inTeam.UpdatedAt)
	require.NotNil(t, inTeam.TeamName)
	assert.Equal(t, "集成团队", *inTeam.TeamName)
}

// TestITPatientPhoneProjectionT491_NullPhoneStillReads NULL 密文行（微信-only）不得被新扫的一列读崩。
// 参照 T483/T484：patients.password_hash 的 NULL 扫进非指针 string 就是那条 500 链路；
// 这一格证明 phone_enc 走 []byte 没有同类问题，且三态判成 absent（既不是报错也不是「读不出」）。
func TestITPatientPhoneProjectionT491_NullPhoneStillReads(t *testing.T) {
	ctx := context.Background()

	// 建档通道 PhoneEnc/PhoneHash 双 nil = 微信-only（两列落 NULL，查重步骤跳过）
	patientID := t491Patient(t, "T491无号患者", nil, "")
	require.Nil(t, t491DBPhoneEnc(t, patientID), "起点必须是 NULL，否则这一格空对空")

	row, err := itStore.GetPatient(ctx, patientID)
	require.NoError(t, err, "phone_enc 为 NULL 不得让患者详情读侧报错（报错就是一条新的 500）")
	require.NotNil(t, row)
	assert.Empty(t, row.PhoneEnc)

	// 另外两条读法同样不能崩
	assigned, err := itStore.AssignPatientTeam(ctx, patientID, itTeam)
	require.NoError(t, err)
	require.NotNil(t, assigned)
	assert.Empty(t, assigned.PhoneEnc, "分配团队的写响应读的是同一套投影")

	inTeam, err := itStore.GetPatientInTeam(ctx, patientID, itTeam)
	require.NoError(t, err)
	require.NotNil(t, inTeam)
	assert.Empty(t, inTeam.PhoneEnc)

	list, _, err := itStore.ListPatients(ctx, PatientFilter{Keyword: "T491无号患者", Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Empty(t, list[0].PhoneEnc)

	assert.Equal(t, phone.PhoneStateAbsent, phone.View(nil, row.PhoneEnc).State,
		"库里确实没存号才判 absent")
}

// TestITPatientPhoneProjectionT491_PlaceholderReadsUnreadable 占位密文（与种子 '\x00'::bytea 同形）
// 补投影后仍原样透传：1 字节短于 AES-GCM nonce(12B)，任何密钥都解不开 ⇒ 三态必须是 unreadable，
// 不能被补投影这件事洗成 absent。
func TestITPatientPhoneProjectionT491_PlaceholderReadsUnreadable(t *testing.T) {
	ctx := context.Background()
	patientID := t491Patient(t, "T491占位密文", []byte{0x00}, t491PhoneJunk)

	row, err := itStore.GetPatient(ctx, patientID)
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Len(t, row.PhoneEnc, 1, "占位密文要原样带回（1 字节，短于 nonce）")
	assert.Equal(t, t491DBPhoneEnc(t, patientID), row.PhoneEnc)

	cipher, err := phone.NewCipher(t491TestPhoneKey)
	require.NoError(t, err)
	assert.Equal(t, phone.PhoneStateUnreadable, phone.View(cipher, row.PhoneEnc).State,
		"解不开必须判 unreadable，不是 absent")
}
