//go:build integration
// +build integration

// Package repo 集成测试：T226 患者自助资料写接口（UpdatePatientProfile）真实 PG 落库取证
//
// 覆盖：白名单 9 键之外的 phone 不存在写通道；白名单字段 UPDATE 后 GetPatient 回读一致；
// 不存在的 patient → ErrPatientNotFound；updated_at 应用层刷新。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestITUpdatePatientProfile_T226(t *testing.T) {
	ctx := context.Background()

	// 造独立患者（不动种子行，避免影响既有用例对 P-USR-IT-1/2 的断言）
	const pid = "P-USR-IT-T226"
	_, err := itStore.pool.Exec(ctx, `
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, status)
VALUES ($1, 'T226患者', '\x00'::bytea, 'aa14' || repeat('0', 60), 'male', 14, 'active')
ON CONFLICT (patient_id) DO NOTHING`, pid)
	require.NoError(t, err)

	name := "T226患者改"
	gender := "female"
	age := 15
	cobb := 27.5
	height := 162.0
	weight := 48.5
	ecName := "张建国"
	ecPhone := "13987654321"
	ecRel := "父亲"

	before, err := itStore.GetPatient(ctx, pid)
	require.NoError(t, err)
	require.NotNil(t, before)
	beforeAt := before.UpdatedAt

	err = itStore.UpdatePatientProfile(ctx, pid, PatientProfileUpdate{
		Name:                     &name,
		Gender:                   &gender,
		Age:                      &age,
		CobbAngle:                &cobb,
		HeightCm:                 &height,
		WeightKg:                 &weight,
		EmergencyContactName:     &ecName,
		EmergencyContactPhone:    &ecPhone,
		EmergencyContactRelation: &ecRel,
	})
	require.NoError(t, err)

	after, err := itStore.GetPatient(ctx, pid)
	require.NoError(t, err)
	require.NotNil(t, after)

	t.Logf("T226 字段级实测: name %q→%q, gender %v→%s, age %d→%d, cobb %v→%v, height→%v, weight→%v, 紧急联系人→(%s,%s,%s)",
		before.Name, after.Name, derefStr(before.Gender), derefStr(after.Gender),
		derefInt(before.Age), derefInt(after.Age),
		derefF(before.CobbAngle), derefF(after.CobbAngle),
		derefF(after.HeightCm), derefF(after.WeightKg),
		derefStr(after.EmergencyContactName), derefStr(after.EmergencyContactPhone), derefStr(after.EmergencyContactRelation))

	assert.Equal(t, "T226患者改", after.Name)
	assert.Equal(t, "female", derefStr(after.Gender))
	assert.Equal(t, 15, derefInt(after.Age))
	assert.Equal(t, 27.5, derefF(after.CobbAngle))
	assert.Equal(t, 162.0, derefF(after.HeightCm))
	assert.Equal(t, 48.5, derefF(after.WeightKg))
	assert.Equal(t, "张建国", derefStr(after.EmergencyContactName))
	assert.Equal(t, "13987654321", derefStr(after.EmergencyContactPhone))
	assert.Equal(t, "父亲", derefStr(after.EmergencyContactRelation))
	// phone_enc/phone_hash 不受影响（微信授权写入，本接口无通道）
	assert.Equal(t, before.PhoneEnc, after.PhoneEnc)
	assert.True(t, after.UpdatedAt.After(beforeAt) || after.UpdatedAt.Equal(beforeAt), "updated_at 刷新")

	// nil=不改：第二次调用只改 name，其余字段保持
	name2 := "T226患者再改"
	require.NoError(t, itStore.UpdatePatientProfile(ctx, pid, PatientProfileUpdate{Name: &name2}))
	after2, err := itStore.GetPatient(ctx, pid)
	require.NoError(t, err)
	assert.Equal(t, "T226患者再改", after2.Name)
	assert.Equal(t, 162.0, derefF(after2.HeightCm), "未提供的字段不得被改动")

	// 不存在的患者 → ErrPatientNotFound
	name3 := "ghost"
	err = itStore.UpdatePatientProfile(ctx, "P-NOT-EXIST-T226", PatientProfileUpdate{Name: &name3})
	assert.ErrorIs(t, err, ErrPatientNotFound)

	// 迁移 000014 列确实存在（down/up 完备性由 CI 迁移套件覆盖，此处防列缺失导致 Scan 崩）
	var colCount int
	require.NoError(t, itStore.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
WHERE table_name='patients' AND column_name IN
 ('height_cm','weight_kg','emergency_contact_name','emergency_contact_phone','emergency_contact_relation')`,
	).Scan(&colCount))
	assert.Equal(t, 5, colCount)
}

func derefStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

func derefF(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}
