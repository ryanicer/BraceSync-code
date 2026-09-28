//go:build integration
// +build integration

// Package repo 集成测试：T450-②b 乙案（clearFields 显式置 NULL）真实 PG 落库取证
//
// 覆盖：四条可空列点名后置成真 NULL（不是空串）；同请求「给值 + 置空别的列」互不干扰；
// 未点名的列不被波及；表外列名在触库前就被拒（现值不变、注入串不进 SQL）；
// 不存在的 patient → ErrPatientNotFound。
// 单测侧只能看到 SQL 文本，「库里落的是 NULL 而非空串」这一格只有真库能判。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestITUpdatePatientProfile_ClearColumns_T450B(t *testing.T) {
	ctx := context.Background()

	const pid = "P-USR-IT-T450B"
	// 独立造行（不动种子行），冲突时复位成基线值 ⇒ 用例可重复跑
	_, err := itStore.pool.Exec(ctx, `
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, diagnosis, cobb_angle, status)
VALUES ($1, 'T450B患者', '\x00'::bytea, 'bb14' || repeat('0', 60), 'male', 14, '胸椎右侧凸', 28.00, 'active')
ON CONFLICT (patient_id) DO UPDATE SET
  name = 'T450B患者', gender = 'male', age = 14, diagnosis = '胸椎右侧凸', cobb_angle = 28.00`, pid)
	require.NoError(t, err)
	// 共享种子库：既有用例断言全表行数，测后必须清场
	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, pid)
	})

	// 基线：四列都有值（置空前若不成立，后面的 nil 判据就无从谈起）
	base, err := itStore.GetPatient(ctx, pid)
	require.NoError(t, err)
	require.NotNil(t, base)
	require.NotNil(t, base.Gender)
	require.NotNil(t, base.Age)
	require.NotNil(t, base.Diagnosis)
	require.NotNil(t, base.CobbAngle)

	// 判据 1：四列点名后置成真 NULL
	require.NoError(t, itStore.UpdatePatientProfile(ctx, pid, PatientProfileUpdate{
		ClearColumns: []string{"gender", "age", "diagnosis", "cobb_angle"},
	}))
	after, err := itStore.GetPatient(ctx, pid)
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Nil(t, after.Gender)
	assert.Nil(t, after.Age)
	assert.Nil(t, after.Diagnosis)
	assert.Nil(t, after.CobbAngle)
	assert.Equal(t, base.Name, after.Name, "name 不在置空名单，也不该被改动")

	// 库侧直接判「真 NULL 而非空串」：应用层指针为 nil 只说明读侧，这一发说明写侧
	var genderIsNull, ageIsNull, diagIsNull, cobbIsNull bool
	require.NoError(t, itStore.pool.QueryRow(ctx, `
SELECT gender IS NULL, age IS NULL, diagnosis IS NULL, cobb_angle IS NULL
FROM patients WHERE patient_id = $1`, pid,
	).Scan(&genderIsNull, &ageIsNull, &diagIsNull, &cobbIsNull))
	assert.True(t, genderIsNull, "gender 应为真 NULL")
	assert.True(t, ageIsNull, "age 应为真 NULL")
	assert.True(t, diagIsNull, "diagnosis 应为真 NULL（不是空串）")
	assert.True(t, cobbIsNull, "cobb_angle 应为真 NULL")

	// 回填：四列重新给值，供判据 2/3 观察「只动点名的列」
	diag := "腰背代偿"
	cobb := 12.50
	gender := "female"
	age := 0 // 0 是合法业务值（CHECK 0-150），与「置空」是两条通道
	require.NoError(t, itStore.UpdatePatientProfile(ctx, pid, PatientProfileUpdate{
		Gender:    &gender,
		Age:       &age,
		Diagnosis: &diag,
		CobbAngle: &cobb,
	}))
	refilled, err := itStore.GetPatient(ctx, pid)
	require.NoError(t, err)
	assert.Equal(t, "female", derefStr(refilled.Gender))
	assert.Equal(t, 0, derefInt(refilled.Age), "age=0 落 0，不落 NULL")
	assert.Equal(t, "腰背代偿", derefStr(refilled.Diagnosis))
	assert.Equal(t, 12.5, derefF(refilled.CobbAngle))

	// 判据 2 + 3：同一请求既给值又置空别的列，未点名的列保持原值
	newName := "T450B患者改"
	require.NoError(t, itStore.UpdatePatientProfile(ctx, pid, PatientProfileUpdate{
		Name:         &newName,
		ClearColumns: []string{"gender"},
	}))
	mixed, err := itStore.GetPatient(ctx, pid)
	require.NoError(t, err)
	assert.Equal(t, "T450B患者改", mixed.Name)
	assert.Nil(t, mixed.Gender, "点名的 gender 置空")
	assert.Equal(t, 0, derefInt(mixed.Age), "未点名的 age 保持 0（不得被洗成 NULL）")
	assert.Equal(t, "腰背代偿", derefStr(mixed.Diagnosis), "未点名的 diagnosis 保持")
	assert.Equal(t, 12.5, derefF(mixed.CobbAngle), "未点名的 cobb_angle 保持")

	// 判据 4：表外列名在触库前就被拒，现值不变（pg.go 再校验一遍列名是纵深防御）
	err = itStore.UpdatePatientProfile(ctx, pid, PatientProfileUpdate{
		ClearColumns: []string{"name"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not clearable")
	afterGuard, err := itStore.GetPatient(ctx, pid)
	require.NoError(t, err)
	assert.Equal(t, mixed.Name, afterGuard.Name, "被拒的写不得改动库内现值")
	assert.Nil(t, afterGuard.Gender)

	// 注入形状同样进不了 SQL：列名只能取白名单值集，带分号的串在 Exec 之前报错
	require.Error(t, itStore.UpdatePatientProfile(ctx, pid, PatientProfileUpdate{
		ClearColumns: []string{"diagnosis; DROP TABLE patients"},
	}))
	var stillThere int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM patients WHERE patient_id = $1`, pid).Scan(&stillThere))
	assert.Equal(t, 1, stillThere, "反证：patients 表与本行都还在")

	// 判据 5：不存在的 patient → ErrPatientNotFound（置空腿与给值腿同一条 UPDATE，判定序不变）
	err = itStore.UpdatePatientProfile(ctx, "P-NOT-EXIST-T450B", PatientProfileUpdate{
		ClearColumns: []string{"gender"},
	})
	assert.ErrorIs(t, err, ErrPatientNotFound)

	// 迁移侧事实回读：四列确实可空（若哪天给它们加了 NOT NULL，乙案的白名单就得跟着收口）
	var notNullCount int
	require.NoError(t, itStore.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
WHERE table_name = 'patients'
  AND column_name IN ('gender','age','diagnosis','cobb_angle')
  AND is_nullable = 'NO'`,
	).Scan(&notNullCount))
	assert.Equal(t, 0, notNullCount, "四列都应保持可空")
}
