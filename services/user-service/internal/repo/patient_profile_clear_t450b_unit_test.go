// T450-②b 乙案（PM 2026-09-28 17:41 卡内评论 2266 拍案）：admin 档案编辑的「显式置 NULL」入参形态。
//
// 本文件守写库 SQL 的形状（无库依赖，纯函数级）：
//  1. 置空列拼成字面 `col = NULL`，不占参数位 ⇒ 参数序号只由「给值」的列推进；
//  2. 混排时占位符编号仍连续（写死 $2 那一类失真在这格必判红，同 T461 的 M5）；
//  3. 表外列名报错返回，不静默丢；name 与注入串都落在这格；
//  4. 白名单只有一份来源（repo.PatientProfileClearColumns），且字面值锁死。
package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// strp / fint / f64 取地址的测试辅助（Go 测试里未调用的辅助函数会被 lint 判红，故每条都真用到）
func strp(s string) *string { return &s }

func fint(i int) *int { return &i }

func TestT450B_PatientProfileClearColumns_Literal(t *testing.T) {
	// 字面量锁：加/删一列、改列名映射，这条先红（handler 侧另有一条对偶锁）
	assert.Equal(t, map[string]string{
		"gender":    "gender",
		"age":       "age",
		"diagnosis": "diagnosis",
		"cobbAngle": "cobb_angle",
	}, PatientProfileClearColumns)
}

func TestT450B_BuildSQL_OnlyClears(t *testing.T) {
	sqlText, args, err := buildPatientProfileUpdateSQL("P1", PatientProfileUpdate{
		ClearColumns: []string{"diagnosis", "cobb_angle"},
	})
	require.NoError(t, err)
	assert.Equal(t,
		"UPDATE patients SET updated_at = NOW(), diagnosis = NULL, cobb_angle = NULL WHERE patient_id = $1",
		sqlText)
	// 置空列不消费参数 ⇒ 唯一实参是患者号
	assert.Equal(t, []any{"P1"}, args)
}

func TestT450B_BuildSQL_MixedKeepsPlaceholderNumbering(t *testing.T) {
	sqlText, args, err := buildPatientProfileUpdateSQL("P2", PatientProfileUpdate{
		Name:         strp("患者小明"),
		Age:          fint(0), // 0 是合法业务值：它进的是参数位，绝不能被读成「清空」
		Diagnosis:    strp("先天性侧弯"),
		ClearColumns: []string{"cobb_angle"},
	})
	require.NoError(t, err)
	assert.Equal(t,
		"UPDATE patients SET updated_at = NOW(), name = $1, age = $2, diagnosis = $3, "+
			"cobb_angle = NULL WHERE patient_id = $4",
		sqlText)
	assert.Equal(t, []any{"患者小明", 0, "先天性侧弯", "P2"}, args)
}

func TestT450B_BuildSQL_RejectsColumnsOutsideWhitelist(t *testing.T) {
	for _, col := range []string{"name", "phone_enc", "patient_id", "status", "diagnosis; DROP TABLE patients"} {
		sqlText, args, err := buildPatientProfileUpdateSQL("P3", PatientProfileUpdate{ClearColumns: []string{col}})
		assert.Error(t, err, "表外列名必须报错：%s", col)
		assert.Empty(t, sqlText)
		assert.Nil(t, args)
	}
}

func TestT450B_BuildSQL_NoFieldNoSQL(t *testing.T) {
	sqlText, args, err := buildPatientProfileUpdateSQL("P4", PatientProfileUpdate{})
	require.NoError(t, err)
	assert.Empty(t, sqlText, "无白名单字段也不该发一条只有 updated_at 的 UPDATE")
	assert.Nil(t, args)
}

func TestT450B_BuildSQL_SelfChannelShapeUnchanged(t *testing.T) {
	// 患者自助通道（T226）永远不给 ClearColumns：SQL 形状必须与本笔改动前逐字一致
	sqlText, args, err := buildPatientProfileUpdateSQL("P5", PatientProfileUpdate{
		HeightCm: f64p(162),
	})
	require.NoError(t, err)
	assert.Equal(t, "UPDATE patients SET updated_at = NOW(), height_cm = $1 WHERE patient_id = $2", sqlText)
	assert.Equal(t, []any{162.0, "P5"}, args)
}

func f64p(v float64) *float64 { return &v }
