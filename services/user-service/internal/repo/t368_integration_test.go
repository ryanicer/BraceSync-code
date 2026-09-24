//go:build integration
// +build integration

// T368 迁移 000028 的真库验证：ROLE_DOCTOR 的 modules 由 4 项补到 6 项（补 review / review_tpl）。
//
// 依据 = Boss 2026-09-24 09:3x 裁定 (a)「补库」（PM 卡内评论 09:35/09:36 转达，
// 派发单 docs/tasks/iris/T368-medical-page-perm-sync-prompt.md）。缺陷现网实测来自 Ella T345
// 验收报告唯一不通过项 D-A：医护侧栏 6 项、GET /api/v1/admin/me/permissions 只回 4 项。
//
// 本用例守三件事，缺一不可：
//  1. **前滚终值**：harness 顺序跑完全部 *.up.sql 之后，ROLE_DOCTOR.modules 必须逐元素等于
//     前端准入矩阵换算出的 6 键（与 apps/admin-web/test/permissions.spec.ts 的同源用例同一口径，
//     那里镜像源文件，这里读真库）；
//  2. **回滚对称**：跑 down 退回补库前的 4 项，再跑 up 回到 6 项 —— 只测 up 等于没测 down；
//  3. **别动别人**：ROLE_ADMIN（15 项，000026 的终态）与 ROLE_CS（1 项）在本迁移 up/down 两次
//     往返里逐字不变，医护自己的 scope 也必须是 team（补的是「能不能进这两页」，不是数据范围）。
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
)

// t368DoctorAfter 000028 前滚后的 ROLE_DOCTOR.modules —— 顺序即 apps/admin-web/src/router/
// permissions.ts 的 PAGE_MODULES 序，与前端 ROLE_PAGE_MATRIX.doctor 六页换算结果逐元素相等。
var t368DoctorAfter = []string{"dashboard", "realtime", "alerts", "orthosis", "review", "review_tpl"}

// t368DoctorBefore 补库前的 4 项（000017 播的那一份），down 的验收目标。
var t368DoctorBefore = []string{"dashboard", "realtime", "alerts", "orthosis"}

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
	// ① 前滚终值：harness 已按文件名序跑完全部 up.sql，直接读现库
	scope, modules := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t368DoctorAfter, modules,
		"000028 后 ROLE_DOCTOR.modules 应为 6 项且顺序与前端矩阵一致")
	assert.Equal(t, "team", scope, "补模块键不得顺带动数据范围（scope 仍是 team）")

	// ② 幂等：再跑一次 up 必须落在同一终值（已建库重放 / 与 seed 并存的库都走这条）
	runMigrationFile(t, t368UpFile)
	_, after := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t368DoctorAfter, after, "重复执行 000028 up 应幂等")

	// ③ admin / cs 不退化（派发单 §五 第三条）
	_, adminModules := t368RolePerms(t, "ROLE_ADMIN")
	assert.Len(t, adminModules, 15, "ROLE_ADMIN 仍是 000026 之后的 15 页全集")
	assert.Contains(t, adminModules, "review_tpl")
	_, csModules := t368RolePerms(t, "ROLE_CS")
	assert.Equal(t, []string{"comm"}, csModules, "ROLE_CS 一字未动")
}

func TestITT368DoctorReviewModulesRoundTrip(t *testing.T) {
	// 收尾无条件重放 up，不给后续用例留中间态（000028 up 是 UPDATE…WHERE role_id='ROLE_DOCTOR'，
	// 重放即幂等）。⚠ t.Cleanup 里禁用 require / runMigrationFile —— FailNow(runtime.Goexit)
	// 只允许发生在测试函数体内，在清理函数里调用会让整个包报 "subtest may have called
	// FailNow on a parent test"，把一次正常通过伪装成一次诡异的包级失败。
	t.Cleanup(func() {
		sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir(), t368UpFile))
		if err != nil {
			t.Errorf("还原时读 %s 失败: %v", t368UpFile, err)
			return
		}
		if _, execErr := itStore.pool.Exec(context.Background(), string(sqlBytes)); execErr != nil {
			t.Errorf("还原 ROLE_DOCTOR 到前滚态失败: %v", execErr)
		}
	})

	// ── 现场：当前是前滚后的 6 项 ──
	_, origModules := t368RolePerms(t, "ROLE_DOCTOR")
	require.Equal(t, t368DoctorAfter, origModules)

	// ── down：退回补库前的 4 项，scope 不动 ──
	runMigrationFile(t, t368DownFile)
	scope, modules := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t368DoctorBefore, modules, "000028 down 应退回裁定前的 4 项")
	assert.Equal(t, "team", scope, "down 也不得动 scope")

	// ── down 期间 admin / cs 同样不受影响（迁移只 WHERE role_id = 'ROLE_DOCTOR'）──
	_, adminModules := t368RolePerms(t, "ROLE_ADMIN")
	assert.Len(t, adminModules, 15, "down 期间 ROLE_ADMIN 不许被牵连")
	_, csModules := t368RolePerms(t, "ROLE_CS")
	assert.Equal(t, []string{"comm"}, csModules, "down 期间 ROLE_CS 不许被牵连")

	// ── 再 up：回到裁定终值 ──
	runMigrationFile(t, t368UpFile)
	_, after := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t368DoctorAfter, after, "down → up 往返后必须精确回到前滚终值")
}
