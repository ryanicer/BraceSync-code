//go:build integration
// +build integration

// T679 迁移 000038 的真库验证：两个预置登录角色的 modules 各补第 17 键 todo
// （ROLE_ADMIN 16→17、ROLE_DOCTOR 7→8）。
//
// 依据 = Boss 2026-10-11 裁定 A 案「医护待办功能全栈单卡」，落地 T665 三屏定稿
// （commit 5994aa53，docs PR #1830）：新增页面 /todos「✅ 我的待办」，模块短键 todo，
// 与前端 apps/admin-web/src/router/permissions.ts 的 PAGE_MODULES 同序（侧栏第 2 位）。
//
// 本用例守四件事：
//  1. **前滚终值**：显式重放 000038 up 之后，两个角色的 modules 必须逐元素等于 PAGE_MODULES 序
//     （admin / doctor 各含 todo，且 todo 落在 dashboard 之后）；scope 不动（admin=all、doctor=team）；
//  2. **幂等**：再跑一次 up 落在同一终值（已建库重放、与 seed 并存的库都走这条）；
//  3. **回滚对称**：down 退回到 000029 的终态（16 / 7 项，逐值），再 up 回到 17 / 8；
//  4. **别动别人**：ROLE_CS（1 项 comm）与本迁移完全无关，两个角色的 scope 一字不动 ——
//     补的是「能不能进这一页」，医护「仅本团队」的数据范围由后端团队过滤（T350）负责，本迁移不预装。
//
// ⚠ t368 / t372 的历史用例跑完后库停在 000029 终态（doctor 7、admin 16，均无 todo），
// 故本用例**不读现库终值**，而是先显式重放自己的 up 再断言（与 t368 同款做法），
// 并在收尾重放 up 把库还原成 harness 的**真前滚终态**（17 / 8），不给后续用例留中间态。
//
// 只跑 SQL 文件、不复制其中语句 ⇒ 测的就是交付物本身（先例：t281 / t368 / t372_integration_test.go）。
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
	t679UpFile   = "000038_t679_todo_module_winner.up.sql"
	t679DownFile = "000038_t679_todo_module_winner.down.sql"
)

// 前滚终值（000038 之后）。顺序 = PAGE_MODULES 序 = 侧栏序，todo 落在 dashboard 之后（第 2 位）。
var (
	t679AdminAfter = []string{"dashboard", "todo", "realtime", "patients", "abnormal_report", "teams",
		"devices", "alerts", "comm", "orthosis", "install", "review", "review_tpl", "tech", "doctor_acct", "perm", "config"}
	t679DoctorAfter = []string{"dashboard", "todo", "realtime", "abnormal_report", "alerts", "orthosis", "review", "review_tpl"}
)

// down 目标 = 000029 的终态（退掉 todo 一键，其余逐字不变）。
var (
	t679AdminBefore = []string{"dashboard", "realtime", "patients", "abnormal_report", "teams", "devices",
		"alerts", "comm", "orthosis", "install", "review", "review_tpl", "tech", "doctor_acct", "perm", "config"}
	t679DoctorBefore = []string{"dashboard", "realtime", "abnormal_report", "alerts", "orthosis", "review", "review_tpl"}
)

// t679RolePerms 读库内某角色 permissions_json 的 scope/modules —— **不做投影**（与 t368 的过滤版不同，
// 本用例要断的就是「含 todo」的终值，故必须原样读出）。
func t679RolePerms(t *testing.T, roleID string) (scope string, modules []string) {
	t.Helper()
	ctx := context.Background()
	row, err := itStore.GetRole(ctx, roleID)
	require.NoError(t, err)
	require.NotNil(t, row, "预置角色 %s 应存在", roleID)
	var perms struct {
		Scope   string   `json:"scope"`
		Modules []string `json:"modules"`
	}
	require.NoError(t, json.Unmarshal([]byte(row.PermissionsJSON), &perms),
		"%s 的 permissions_json 解析失败：%s", roleID, row.PermissionsJSON)
	return perms.Scope, perms.Modules
}

func TestITT679TodoModuleUp(t *testing.T) {
	// ① 显式重放本卡 up，再断两个角色的终值（库现值含 t368/t372 收尾留下的中间态，直接读会断错）
	runMigrationFile(t, t679UpFile)

	scope, admin := t679RolePerms(t, "ROLE_ADMIN")
	assert.Equal(t, t679AdminAfter, admin, "000038 后 ROLE_ADMIN.modules 应为 17 项且顺序与 PAGE_MODULES 一致")
	assert.Equal(t, "all", scope, "补模块键不得顺带动数据范围（scope 仍是 all）")

	dscope, doctor := t679RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t679DoctorAfter, doctor, "000038 后 ROLE_DOCTOR.modules 应为 8 项（含 todo）")
	assert.Equal(t, "team", dscope, "补模块键不得顺带动数据范围（scope 仍是 team）")

	// todo 必须**只有一个拼法**：词表一改，前后端与库三处镜像同时红
	assert.Contains(t, admin, "todo")
	assert.Contains(t, doctor, "todo")

	// ② 幂等：再跑一次 up 必须落在同一终值
	runMigrationFile(t, t679UpFile)
	_, adminAgain := t679RolePerms(t, "ROLE_ADMIN")
	assert.Equal(t, t679AdminAfter, adminAgain, "重复执行 000038 up 应幂等")

	// ③ ROLE_CS 一字未动
	_, cs := t679RolePerms(t, "ROLE_CS")
	assert.Equal(t, []string{"comm"}, cs, "客服不参与待办页授权")
}

func TestITT679TodoModuleRoundTrip(t *testing.T) {
	// 收尾无条件重放 up，把库还原成 harness 真前滚终态（17 / 8），不给后续用例留中间态。
	// ⚠ t.Cleanup 里禁用 require / runMigrationFile（FailNow 不许出现在清理函数内，见 t368 同款注释）。
	t.Cleanup(func() {
		sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir(), t679UpFile))
		if err != nil {
			t.Errorf("还原时读 %s 失败: %v", t679UpFile, err)
			return
		}
		if _, execErr := itStore.pool.Exec(context.Background(), string(sqlBytes)); execErr != nil {
			t.Errorf("还原 000038 前滚态失败: %v", execErr)
		}
	})

	// ── 现场：先重放本卡 up 到 17 / 8 ──
	runMigrationFile(t, t679UpFile)
	_, origAdmin := t679RolePerms(t, "ROLE_ADMIN")
	require.Equal(t, t679AdminAfter, origAdmin)
	_, origDoctor := t679RolePerms(t, "ROLE_DOCTOR")
	require.Equal(t, t679DoctorAfter, origDoctor)

	// ── down：退回 000029 的终态（16 / 7，只退 todo 一键）──
	runMigrationFile(t, t679DownFile)
	scope, admin := t679RolePerms(t, "ROLE_ADMIN")
	assert.Equal(t, t679AdminBefore, admin, "000038 down 应退回 16 项 = 000029 的终态")
	assert.Equal(t, "all", scope, "down 也不得动 scope")
	dscope, doctor := t679RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t679DoctorBefore, doctor, "000038 down 应退回 7 项 = 000029 的终态")
	assert.Equal(t, "team", dscope, "down 也不得动 scope")

	// down 期间也不许牵连客服
	_, cs := t679RolePerms(t, "ROLE_CS")
	assert.Equal(t, []string{"comm"}, cs, "down 期间 ROLE_CS 不许被牵连")

	// ── 再 up：回到 17 / 8 ──
	runMigrationFile(t, t679UpFile)
	_, afterAdmin := t679RolePerms(t, "ROLE_ADMIN")
	assert.Equal(t, t679AdminAfter, afterAdmin, "down → up 往返后 admin 必须精确回到本卡终值")
	_, afterDoctor := t679RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t679DoctorAfter, afterDoctor, "down → up 往返后 doctor 必须精确回到本卡终值")
}
