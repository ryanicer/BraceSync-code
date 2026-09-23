//go:build integration
// +build integration

// Package repo 集成测试：T361「编辑手机号不得洗号」的库侧证据
//
// 卡面回归要求：编辑弹窗在手机号为「空」或「脱敏态」时保存，不得把库内真号洗成 NULL。
// 在真库里只有两条通道能碰到手机号，必须分别钉住：
//  1. PhoneHash=nil（前端「用户没碰过手机号」）⇒ phone_enc / phone_hash 逐字节保持原值；
//  2. PhoneHash 指向空串（前端「用户明确清空」）⇒ 两列一起落 NULL —— 这是唯一允许洗号的入口。
//
// 另附 seed 占位形态（scripts/db/seed/seed.sql 写 '\x00'::bytea，1 字节）读回后的原样透传，
// 它比 12 字节 GCM nonce 还短、任何密钥都解不开 ⇒ handler 侧必须报 unreadable 而不是「没填」。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t361PhoneColumns 直查两列，绕开 join，确保断言打在库里的真实字节上
func t361PhoneColumns(t *testing.T, doctorID string) (enc []byte, hash *string) {
	t.Helper()
	var encArg any
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT phone_enc, phone_hash FROM doctors WHERE doctor_id = $1`, doctorID).
		Scan(&encArg, &hash))
	switch v := encArg.(type) {
	case nil:
		return nil, hash
	case []byte:
		return v, hash
	case string:
		return []byte(v), hash
	default:
		t.Fatalf("phone_enc  unexpected type %T", encArg)
		return nil, nil
	}
}

func TestITDoctorPhone_T361_EditWithoutPhoneKeepsCiphertext(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	in := t314Input("T361编辑保号")
	in.PhoneEnc = []byte{0x01, 0x02, 0x03, 0x04} // 只要「非空且不同于清空」即可，IT 不验证可解性
	in.PhoneHash = "t361-hash-keep"
	row, err := itStore.CreateDoctorAccount(ctx, in)
	require.NoError(t, err)
	tr.addRow(t, row)

	originalEnc, originalHash := t361PhoneColumns(t, row.DoctorID)
	require.Equal(t, in.PhoneEnc, originalEnc)
	require.NotNil(t, originalHash)

	// ① 只改名：入参里没有手机号 ⇒ 两列原样不动（前端「未改动」通道的库侧落点）
	_, err = itStore.UpdateDoctorAccount(ctx, row.DoctorID, DoctorAccountUpdate{
		Name: strptrT314("T361编辑后"),
	})
	require.NoError(t, err)
	encAfter, hashAfter := t361PhoneColumns(t, row.DoctorID)
	assert.Equal(t, originalEnc, encAfter, "🔴 未下发手机号的编辑不得动 phone_enc（洗号即真号丢失）")
	require.NotNil(t, hashAfter)
	assert.Equal(t, *originalHash, *hashAfter, "phone_hash 必须与密文同生同灭")

	// ② 明确清空（PhoneHash 指向空串）⇒ 两列一起 NULL，不留「有密文无哈希」的半清空态
	empty := ""
	_, err = itStore.UpdateDoctorAccount(ctx, row.DoctorID, DoctorAccountUpdate{PhoneHash: &empty})
	require.NoError(t, err)
	encCleared, hashCleared := t361PhoneColumns(t, row.DoctorID)
	assert.Nil(t, encCleared, "显式清空后密文列应为 NULL")
	assert.Nil(t, hashCleared, "显式清空后哈希列应为 NULL")

	// ③ 清空之后再「不带手机号」编辑，不得把 NULL 又当成改动写回别的东西
	_, err = itStore.UpdateDoctorAccount(ctx, row.DoctorID, DoctorAccountUpdate{Title: strptrT314("康复师")})
	require.NoError(t, err)
	encFinal, hashFinal := t361PhoneColumns(t, row.DoctorID)
	assert.Nil(t, encFinal)
	assert.Nil(t, hashFinal)
}

// TestITDoctorPhone_T361_SeedPlaceholderReadsBackVerbatim 证 seed 占位密文「读得回来、但只有 1 字节」：
// 解不开是密文本身的形态决定的，与当前部署用的 PHONE_ENC_KEY 无关 ⇒ 定性结论不依赖现网密钥。
func TestITDoctorPhone_T361_SeedPlaceholderReadsBackVerbatim(t *testing.T) {
	ctx := context.Background()
	var tr t314Tracker
	defer tr.cleanup(ctx, t)

	in := t314Input("T361种子占位")
	in.PhoneEnc = []byte{0x00} // 与 seed.sql 的 '\x00'::bytea 同形
	in.PhoneHash = "t361-hash-placeholder"
	row, err := itStore.CreateDoctorAccount(ctx, in)
	require.NoError(t, err)
	tr.addRow(t, row)

	got, err := itStore.GetDoctorAccount(ctx, row.DoctorID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []byte{0x00}, got.PhoneEnc, "读侧原样透传占位密文（非空 ⇒ handler 判 unreadable）")
	assert.Len(t, got.PhoneEnc, 1, "1 字节 < AES-GCM nonce(12B) ⇒ 任何密钥都解不开，与密钥漂移无关")
}
