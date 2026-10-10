// T467 患者档案删除（repo 侧纯函数判据，无库依赖）。
//
// 本文件守三件事，都是「真库跑不到、但一改就崩」的形状：
//  1. 关联表白名单的字面锁 —— 16 张表一张不许少，尤其是三张「有 patient_id 但没外键」的
//     （pressure_records / daily_wear_stats / device_bindings）。数据库拦不住它们，
//     漏一张就是「删患者顺手留下一堆无主体的佩戴明细/日聚合/绑定历史」；
//  2. 计数 SQL 的形状：一表一条 COUNT、患者号只走 $1 参数位，绝不拼串；
//  3. 409 文案可比对：按声明序渲染，并发兜底的约束名排在末尾（map 迭代序不定，故只锁一名未知键）。
package repo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestT467_PatientRefTables_LiteralLock 白名单逐字锁：增删一张表、改动 hasFK 标记，这条先红。
// hasFK 的真实依据见 scripts/db/migrations（000001:97/123/189/219/229/243/258/272/284、
// 000003:13/48、000009:10、000035:26 有 REFERENCES patients；后三张只有列没有外键）。
func TestT467_PatientRefTables_LiteralLock(t *testing.T) {
	assert.Equal(t, []patientRefTable{
		{name: "devices", hasFK: true},
		{name: "install_records", hasFK: true},
		{name: "alerts", hasFK: true},
		{name: "orthosis_plans", hasFK: true},
		{name: "feeling_logs", hasFK: true},
		{name: "feedbacks", hasFK: true},
		{name: "health_reports", hasFK: true},
		{name: "patient_preferences", hasFK: true},
		{name: "consents", hasFK: true},
		{name: "notification_records", hasFK: true},
		{name: "quota_grants", hasFK: true},
		{name: "review_records", hasFK: true},
		{name: "advice_logs", hasFK: true},
		{name: "pressure_records", hasFK: false},
		{name: "daily_wear_stats", hasFK: false},
		{name: "device_bindings", hasFK: false},
	}, patientRefTables)

	// 基数单独再锁一次：上面那张字面表被人整段替换时，这条给的是「少了哪一类」的读数
	assert.Len(t, patientRefTables, 16)
	var noFK []string
	for _, tbl := range patientRefTables {
		if !tbl.hasFK {
			noFK = append(noFK, tbl.name)
		}
	}
	assert.Equal(t, []string{"pressure_records", "daily_wear_stats", "device_bindings"}, noFK,
		"数据库不拦删除的三张表必须一起数，这是派发单「禁止级联误删业务数据」的落点")
}

// TestT467_PatientRefCountSQL_Shape 计数 SQL 形状：每表一条 COUNT(*)、患者号只在参数位。
func TestT467_PatientRefCountSQL_Shape(t *testing.T) {
	sqlText := patientRefCountSQL()

	assert.Equal(t, len(patientRefTables)-1, strings.Count(sqlText, " UNION ALL "),
		"16 张表汇成一条语句 = 15 个 UNION ALL，缺一张就少一个")
	assert.Equal(t, len(patientRefTables), strings.Count(sqlText, "patient_id = $1"),
		"患者号必须逐表走 $1 参数位（不拼串）")
	assert.NotContains(t, sqlText, ";", "不得有多语句拼接")

	// 每张表名出现两次：一次是 409 文案里的机读键（SELECT '表名'），一次是 FROM 的表
	for _, tbl := range patientRefTables {
		assert.Equal(t, 2, strings.Count(sqlText, tbl.name),
			"%s 应在 SQL 中出现两次（机读键 + FROM），实际：%d", tbl.name, strings.Count(sqlText, tbl.name))
	}

	// 反向锁：三张无外键表真在语句里（漏数它们时数据库不会报错，只有这条能判红）
	for _, name := range []string{"pressure_records", "daily_wear_stats", "device_bindings"} {
		require.Contains(t, sqlText, "FROM "+name+" WHERE patient_id = $1",
			"无外键表 %s 必须进计数语句：数据库不拦它，漏了就是孤儿行", name)
	}
}

// TestT467_ErrPatientInUse_MessageIsDeterministic 409 文案按声明序渲染，可被用例逐字比对。
func TestT467_ErrPatientInUse_MessageIsDeterministic(t *testing.T) {
	// 声明序：devices 排在 alerts 之前，与 map 的哈希序无关
	err := &ErrPatientInUse{Refs: map[string]int{"alerts": 2, "devices": 1}}
	assert.Equal(t, "patient in use: devices=1, alerts=2", err.Error())

	// 并发兜底路径的约束名不在白名单里，排在已知项之后
	mixed := &ErrPatientInUse{Refs: map[string]int{"devices_patient_id_fkey": 1, "feedbacks": 3}}
	assert.Equal(t, "patient in use: feedbacks=3, devices_patient_id_fkey=1", mixed.Error())
}

// TestT467_ErrPatientInUse_SatisfiesErrorInterface 兜底：handler 侧用 errors.As 取结构化错误，
// 类型断言一旦失效（比如哪天改成值接收者），409 会静默退化成 500。
func TestT467_ErrPatientInUse_SatisfiesErrorInterface(t *testing.T) {
	var err error = &ErrPatientInUse{Refs: map[string]int{"consents": 1}}
	assert.Contains(t, err.Error(), "consents=1")
}
