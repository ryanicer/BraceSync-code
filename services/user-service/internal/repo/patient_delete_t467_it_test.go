//go:build integration
// +build integration

// Package repo 集成测试：T467 DELETE /admin/patients 真库判定
//
// 覆盖：删自建患者成功（链 A A2 的前置能力正证）；不存在 → ErrPatientNotFound；
// 删后再删仍是 404（不采「已经没了也算成功」的伪幂等）；
// 有外键的关联面（feeling_logs）非空 → 409 结构化错误、档案一行不少；
// 无外键的关联面（daily_wear_stats / device_bindings）同样拦下 ——
// 这三张表数据库不会拦（外键不存在，删除直接放行），只有逐表计数能拦，
// 所以「库里确实没有对应外键」这一事实要现场回读，不能靠迁移文件里读一眼。
// 最后把 patientRefTables 与库内实际集合对账：新增引用表却漏进名单 ⇒ 判红。
//
// 约定：只操作本用例自建的行，t.Cleanup 清场（共享种子库有既有用例断言全表行数）。
package repo

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t467NewPatient 自建一名无手机号患者（等价于小程序微信-only 建档），并登记清场。
func t467NewPatient(t *testing.T, tag string) string {
	t.Helper()
	p, err := itStore.CreatePatient(context.Background(), PatientInput{Name: "T467患者" + tag})
	require.NoError(t, err, tag)
	require.NotNil(t, p, tag)
	require.NotEmpty(t, p.PatientID, tag)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM feeling_logs WHERE patient_id = $1`, p.PatientID)
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM daily_wear_stats WHERE patient_id = $1`, p.PatientID)
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM device_bindings WHERE patient_id = $1`, p.PatientID)
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, p.PatientID)
	})
	return p.PatientID
}

// t467PatientRowCount patients 全表行数（判「被拒的写一行都没动」）
func t467PatientRowCount(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM patients`).Scan(&n))
	return n
}

// t467Exists 库侧直查该行是否还在（不经 GetPatient 的 join，排除读侧口径干扰）
func t467Exists(t *testing.T, patientID string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM patients WHERE patient_id = $1)`, patientID).Scan(&exists))
	return exists
}

// TestITDeletePatient_SelfBuiltSucceeds_T467 链 A 建档段（A2）依赖的能力：
// 自建、无任何关联面的患者可以真删掉。
func TestITDeletePatient_SelfBuiltSucceeds_T467(t *testing.T) {
	ctx := context.Background()
	pid := t467NewPatient(t, "干净")

	require.NoError(t, itStore.DeletePatient(ctx, pid))
	assert.False(t, t467Exists(t, pid), "删除后 patients 行应不存在")

	// 读侧同步收敛：GetPatient 走的是带 join 的那条 SELECT，删了要读到 nil 而不是报错
	row, err := itStore.GetPatient(ctx, pid)
	require.NoError(t, err)
	assert.Nil(t, row)
}

// TestITDeletePatient_MissingIsNotFound_T467 不存在 → ErrPatientNotFound，且零写入。
func TestITDeletePatient_MissingIsNotFound_T467(t *testing.T) {
	ctx := context.Background()
	before := t467PatientRowCount(t)

	err := itStore.DeletePatient(ctx, "P-T467-NOT-EXIST")
	assert.ErrorIs(t, err, ErrPatientNotFound)
	assert.Equal(t, before, t467PatientRowCount(t), "被拒的删除不得改动任何行")
}

// TestITDeletePatient_RepeatDeleteIsNotFound_T467 重复删除：第二次仍是 404。
// 反面对照（先成功一次）保证这条 404 不是因为「行从来就不存在」。
func TestITDeletePatient_RepeatDeleteIsNotFound_T467(t *testing.T) {
	ctx := context.Background()
	pid := t467NewPatient(t, "重复")

	require.NoError(t, itStore.DeletePatient(ctx, pid))
	assert.ErrorIs(t, itStore.DeletePatient(ctx, pid), ErrPatientNotFound,
		"第二次删除不得伪幂等返回 nil")
	assert.False(t, t467Exists(t, pid))
}

// TestITDeletePatient_FKReferencedIsInUse_T467 有外键的关联面非空 → *ErrPatientInUse，
// 档案与关联行都不得被删掉（判据：计数命中即拒，DELETE 语句根本不执行）。
func TestITDeletePatient_FKReferencedIsInUse_T467(t *testing.T) {
	ctx := context.Background()
	pid := t467NewPatient(t, "日志")
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO feeling_logs (patient_id, log_date, comfort_score) VALUES ($1, '2026-09-01', 3.5)`, pid)
	require.NoError(t, err)

	countBefore := t467PatientRowCount(t)
	err = itStore.DeletePatient(ctx, pid)
	require.Error(t, err)
	var inUse *ErrPatientInUse
	require.ErrorAs(t, err, &inUse, "外键关联面命中要返回结构化错误供 handler 映射 409")
	assert.Equal(t, map[string]int{"feeling_logs": 1}, inUse.Refs)
	assert.Contains(t, err.Error(), "feeling_logs=1", "技术文本带逐表计数（响应体只给中文短句）")
	assert.Equal(t, countBefore, t467PatientRowCount(t), "计数命中即拒：patients 一行都没少")
	assert.True(t, t467Exists(t, pid), "目标档案必须仍在")
}

// TestITDeletePatient_NoFKReferencedStillBlocked_T467 无外键的三张关联表（佩戴日聚合、
// 绑定历史）非空时同样拦下，并且现场证明数据库本身不会拦 ——
// 少数一张 = 删患者顺手留下一堆无主体的业务数据，派发单点名的正是这一面。
func TestITDeletePatient_NoFKReferencedStillBlocked_T467(t *testing.T) {
	ctx := context.Background()
	pid := t467NewPatient(t, "无外键")

	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO daily_wear_stats (patient_id, stat_date, wear_minutes) VALUES ($1, '2026-09-02', 480)`, pid)
	require.NoError(t, err)
	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO device_bindings (device_id, patient_id, reason) VALUES ('T467-DEV-1', $1, 'install')`, pid)
	require.NoError(t, err)

	// 前提事实回读：这两张表在库内没有指向 patients 的外键 ⇒ 单靠数据库拦不住删除
	for _, tbl := range []string{"daily_wear_stats", "device_bindings"} {
		var fkCount int
		require.NoError(t, itStore.pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM pg_constraint con
JOIN pg_class rel ON rel.oid = con.conrelid
JOIN pg_namespace n ON n.oid = rel.relnamespace
WHERE con.contype = 'f' AND n.nspname = 'public'
  AND rel.relname = $1 AND con.confrelid = 'patients'::regclass`, tbl,
		).Scan(&fkCount))
		assert.Equal(t, 0, fkCount, "%s 应无指向 patients 的外键（本用例判据的前提）", tbl)
	}

	err = itStore.DeletePatient(ctx, pid)
	require.Error(t, err)
	var inUse *ErrPatientInUse
	require.ErrorAs(t, err, &inUse)
	assert.Equal(t, map[string]int{"daily_wear_stats": 1, "device_bindings": 1}, inUse.Refs)
	// 顺序按 patientRefTables 声明序渲染：日聚合在绑定历史之前
	assert.Equal(t, "patient in use: daily_wear_stats=1, device_bindings=1", err.Error())
	assert.True(t, t467Exists(t, pid), "无外键也不能让删除放行")
}

// TestITDeletePatient_ClearRefsThenDeletes_T467 关联面清空后即可删（409 是可解状态，
// 不是永久锁死）。pressure_records 是分区分表且插入依赖预建分区，本用例不经它走这条路。
func TestITDeletePatient_ClearRefsThenDeletes_T467(t *testing.T) {
	ctx := context.Background()
	pid := t467NewPatient(t, "清空后可删")
	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO daily_wear_stats (patient_id, stat_date, wear_minutes) VALUES ($1, '2026-09-03', 60)`, pid)
	require.NoError(t, err)

	require.Error(t, itStore.DeletePatient(ctx, pid))
	_, err = itStore.pool.Exec(ctx, `DELETE FROM daily_wear_stats WHERE patient_id = $1`, pid)
	require.NoError(t, err)
	require.NoError(t, itStore.DeletePatient(ctx, pid))
	assert.False(t, t467Exists(t, pid))
}

// TestITDeletePatient_RefTableListMatchesDatabase_T467 名单对账（门禁）：
// 库内「带 patient_id 列的顶层表」全集必须恰好等于 patientRefTables；
// 其中指向 patients 的外键子集必须恰好等于 hasFK=true 的那批。
// 将来新增引用表却忘了加进计数名单 ⇒ 这里判红，而不是静默漏删成孤儿数据。
func TestITDeletePatient_RefTableListMatchesDatabase_T467(t *testing.T) {
	ctx := context.Background()

	// 顶层表：relkind r/p 且不是别人的分区（排除 pressure_records_YYYYMM 三张子表）
	rows, err := itStore.pool.Query(ctx, `
SELECT c.relname
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'patient_id'
                   AND a.attnum > 0 AND NOT a.attisdropped
WHERE n.nspname = 'public' AND c.relkind IN ('r','p') AND c.relispartition = false
  AND c.relname <> 'patients'
ORDER BY 1`)
	require.NoError(t, err)
	var dbTables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		dbTables = append(dbTables, name)
	}
	rows.Close()
	require.NoError(t, rows.Err())

	var want []string
	for _, tbl := range patientRefTables {
		want = append(want, tbl.name)
	}
	sort.Strings(want)
	assert.Equal(t, want, dbTables, "patientRefTables 与库内实际引用表不一致")

	fkRows, err := itStore.pool.Query(ctx, `
SELECT DISTINCT rel.relname
FROM pg_constraint con
JOIN pg_class rel ON rel.oid = con.conrelid
JOIN pg_namespace n ON n.oid = rel.relnamespace
WHERE con.contype = 'f' AND n.nspname = 'public' AND con.confrelid = 'patients'::regclass
ORDER BY 1`)
	require.NoError(t, err)
	var dbFK []string
	for fkRows.Next() {
		var name string
		require.NoError(t, fkRows.Scan(&name))
		dbFK = append(dbFK, name)
	}
	fkRows.Close()
	require.NoError(t, fkRows.Err())

	var wantFK []string
	for _, tbl := range patientRefTables {
		if tbl.hasFK {
			wantFK = append(wantFK, tbl.name)
		}
	}
	sort.Strings(wantFK)
	assert.Equal(t, wantFK, dbFK, "hasFK 标注与库内真实外键不一致")

	// 反证：名单里的表名在库里都真实存在（拼错一个名字，计数 SQL 会整条报错而不是漏数）
	refs, err := itStore.countPatientRefs(ctx, "P-T467-NOT-EXIST")
	require.NoError(t, err, "计数 SQL 可执行 = 15 张表名与列名都还在")
	assert.Empty(t, refs, "不存在的患者不应命中任何关联行")
}
