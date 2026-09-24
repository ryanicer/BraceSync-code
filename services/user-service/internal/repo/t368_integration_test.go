//go:build integration
// +build integration

// T368 迁移 000028 的真库验证：ROLE_DOCTOR 的 modules 由 4 项补到 6 项（补 review / review_tpl）。
//
// 依据 = Boss 2026-09-24 09:3x 裁定 (a)「补库」（PM 卡内评论 09:35/09:36 转达，
// 派发单 docs/tasks/iris/T368-medical-page-perm-sync-prompt.md）。缺陷现网实测来自 Ella T345
// 验收报告唯一不通过项 D-A：医护侧栏 6 项、GET /api/v1/admin/me/permissions 只回 4 项。
//
// 本用例守三件事，缺一不可：
//  1. **000028 写的集合**：显式重放 000028 up 之后，ROLE_DOCTOR.modules 必须逐元素等于该迁移写入的
//     6 键（与 apps/admin-web/test/permissions.spec.ts 的同源用例同一口径，那里镜像源文件，
//     这里读真库）。⚠ T372 之后「跑完全部 up.sql 的现库」是 7 键（000029 又补了 abnormal_report），
//     所以本条改为先重放本卡 up 再断言 —— 否则断的是别人的终值；
//  2. **回滚对称**：跑 down 退回补库前的 4 项，再跑 up 回到 6 项 —— 只测 up 等于没测 down；
//  3. **别动别人**：ROLE_ADMIN（16 项 = 000026 → 000029 的终态）与 ROLE_CS（1 项）在本迁移 up/down 两次
//     往返里逐字不变，医护自己的 scope 也必须是 team（补的是「能不能进这两页」，不是数据范围）。
//
// 🔴 收尾必须把 000029 一并重放：000028 up 只写 6 项，留在库里就是「doctor 少 abnormal_report」的
//
//	中间态，后面 t372 与任何按前滚终值断言的用例都会红。
//
// 只跑 SQL 文件、不复制其中语句 ⇒ 测的就是交付物本身（先例：t281_integration_test.go）。
package repo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t368UpFile   = "000028_t368_doctor_review_modules_iris.up.sql"
	t368DownFile = "000028_t368_doctor_review_modules_iris.down.sql"
	// t372UpFile 在同包 t372_integration_test.go 里声明，本文件复用它还原前滚终态（见文件头第 4 条）。
)

// t368DoctorAfter 000028 单独执行后写入的 ROLE_DOCTOR.modules —— 顺序即 apps/admin-web/src/router/
// permissions.ts 的 PAGE_MODULES 序（T368 当时的前端矩阵 doctor 六页换算结果）。
// ⚠ T372 起「前滚到库终态」是 7 键（多 abnormal_report），那是 000029 的验收目标，不写在这儿。
var t368DoctorAfter = []string{"dashboard", "realtime", "alerts", "orthosis", "review", "review_tpl"}

// t368DoctorBefore 补库前的 4 项（000017 播的那一份），down 的验收目标。
var t368DoctorBefore = []string{"dashboard", "realtime", "alerts", "orthosis"}

// t368DoctorT372After 000028 → 000029 全部前滚后的库终值（= 前端 doctor 可见 7 页换算结果）。
// 只在本文件用作「收尾还原是否成功」的判据，其自身的逐值验收在 t372_integration_test.go。
var t368DoctorT372After = []string{"dashboard", "realtime", "abnormal_report", "alerts", "orthosis", "review", "review_tpl"}

// t368RolePerms 读库内某角色的 permissions_json（只取本卡关心的两个键）。
func t368RolePerms(t *testing.T, roleID string) (scope string, modules []string) {
	t.Helper()
	ctx := context.Background()
	list, err := itStore.ListRoles(ctx)
	require.NoError(t, err)
	var row *RoleRow
	for i := range list {
		if list[i].RoleID == roleID {
			row = &list[i]
			break
		}
	}
	require.NotNil(t, row, "预置角色 %s 应存在", roleID)
	var perms struct {
		Scope   string   `json:"scope"`
		Modules []string `json:"modules"`
	}
	require.NoError(t, json.Unmarshal([]byte(row.PermissionsJSON), &perms),
		"%s 的 permissions_json 解析失败：%s", roleID, row.PermissionsJSON)
	return perms.Scope, perms.Modules
}

func TestITT368DoctorReviewModulesUp(t *testing.T) {
	// ① 000028 写的集合：先显式重放本卡 up（库现值含 T372 的 abnormal_report，直接读会断到别人的终值）
	runMigrationFile(t, t368UpFile)
	scope, modules := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t368DoctorAfter, modules,
		"000028 的 modules 应为 6 项且顺序与当时的前端矩阵一致")
	assert.Equal(t, "team", scope, "补模块键不得顺带动数据范围（scope 仍是 team）")

	// ② 幂等：再跑一次 up 必须落在同一终值（已建库重放 / 与 seed 并存的库都走这条）
	runMigrationFile(t, t368UpFile)
	_, after := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t368DoctorAfter, after, "重复执行 000028 up 应幂等")

	// ③ admin / cs 不退化（派发单 §五 第三条）
	_, adminModules := t368RolePerms(t, "ROLE_ADMIN")
	assert.Len(t, adminModules, 16, "ROLE_ADMIN 仍是 000026 → 000029 之后的 16 页全集")
	assert.Contains(t, adminModules, "review_tpl")
	_, csModules := t368RolePerms(t, "ROLE_CS")
	assert.Equal(t, []string{"comm"}, csModules, "ROLE_CS 一字未动")

	// ④ 收口：把 000029 前滚回来，不给后续用例留「doctor 少 abnormal_report」的中间态
	runMigrationFile(t, t372UpFile)
	_, restored := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t368DoctorT372After, restored, "收尾后库应回到全部迁移前滚的终态")
}

func TestITT368DoctorReviewModulesRoundTrip(t *testing.T) {
	// 收尾无条件重放 up，不给后续用例留中间态（000028 up 是 UPDATE…WHERE role_id='ROLE_DOCTOR'，
	// 重放即幂等；000029 同理）。⚠ t.Cleanup 里禁用 require / runMigrationFile —— FailNow(runtime.Goexit)
	// 只允许发生在测试函数体内，在清理函数里调用会让整个包报 "subtest may have called
	// FailNow on a parent test"，把一次正常通过伪装成一次诡异的包级失败。
	t.Cleanup(func() {
		for _, f := range []string{t368UpFile, t372UpFile} {
			sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir(), f))
			if err != nil {
				t.Errorf("还原时读 %s 失败: %v", f, err)
				continue
			}
			if _, execErr := itStore.pool.Exec(context.Background(), string(sqlBytes)); execErr != nil {
				t.Errorf("还原 ROLE_DOCTOR 到前滚态失败（%s）: %v", f, execErr)
			}
		}
	})

	// ── 现场：本卡 up 单独执行后的 6 项（库终值另含 T372 一键，见 ① 的说明）──
	runMigrationFile(t, t368UpFile)
	_, origModules := t368RolePerms(t, "ROLE_DOCTOR")
	require.Equal(t, t368DoctorAfter, origModules)

	// ── down：退回补库前的 4 项，scope 不动 ──
	runMigrationFile(t, t368DownFile)
	scope, modules := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t368DoctorBefore, modules, "000028 down 应退回裁定前的 4 项")
	assert.Equal(t, "team", scope, "down 也不得动 scope")

	// ── down 期间 admin / cs 同样不受影响（迁移只 WHERE role_id = 'ROLE_DOCTOR'）──
	_, adminModules := t368RolePerms(t, "ROLE_ADMIN")
	assert.Len(t, adminModules, 16, "down 期间 ROLE_ADMIN 不许被牵连")
	_, csModules := t368RolePerms(t, "ROLE_CS")
	assert.Equal(t, []string{"comm"}, csModules, "down 期间 ROLE_CS 不许被牵连")

	// ── 再 up：回到本卡终值 ──
	runMigrationFile(t, t368UpFile)
	_, after := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t368DoctorAfter, after, "down → up 往返后必须精确回到本卡 up 的终值")
}
