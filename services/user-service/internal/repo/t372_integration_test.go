//go:build integration
// +build integration

// T372 迁移 000029 的真库验证：两个预置登录角色的 modules 各补第 16 键 abnormal_report
// （ROLE_ADMIN 15→16、ROLE_DOCTOR 6→7）。
//
// 依据 = Boss 2026-09-24 裁定 (a)「异常报告按设计稿拆独立页」
// （派发单 docs/tasks/iris/T372-abnormal-report-page-prompt.md；PRD §7D.11 矩阵第 4 行
// admin ✅ / doctor ✅（仅本团队患者）/ cs —，原「PAGE_MODULES 缺第 16 项」的登记就此收口）。
//
// 本用例守四件事：
//  1. **前滚终值**：harness 按文件名序跑完全部 *.up.sql 之后，两个角色的 modules 必须逐元素等于
//     apps/admin-web/src/router/permissions.ts 的 PAGE_MODULES 序（那里 vitest 镜像源文件比对，
//     这里读真库 ⇒ 两端的「同源」各有一道闸）；
//  2. **幂等**：再跑一次 up 落在同一终值（已建库重放、与 seed 并存的库都走这条）；
//  3. **回滚对称**：down 退回到 000026 / 000028 的终态（15 / 6 项，逐值不是只退一键），再 up 回到 16 / 7；
//  4. **别动别人**：ROLE_CS（1 项 comm）与本迁移完全无关，两个角色的 scope 一字不动 ——
//     补的是「能不能进这一页」，医护「仅本团队」的数据范围由后端的团队过滤负责（本迁移不预装）。
//
// 只跑 SQL 文件、不复制其中语句 ⇒ 测的就是交付物本身（先例：t281 / t368_integration_test.go）。
package repo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t372UpFile   = "000029_t372_abnormal_report_module_iris.up.sql"
	t372DownFile = "000029_t372_abnormal_report_module_iris.down.sql"
)

// 前滚终值（000029 之后）。顺序 = PAGE_MODULES 序 = 设计稿侧栏序，
// abnormal_report 落在 patients 之后（admin）/ realtime 之后（doctor）。
var (
	t372AdminAfter = []string{"dashboard", "realtime", "patients", "abnormal_report", "teams", "devices",
		"alerts", "comm", "orthosis", "install", "review", "review_tpl", "tech", "doctor_acct", "perm", "config"}
	t372DoctorAfter = []string{"dashboard", "realtime", "abnormal_report", "alerts", "orthosis", "review", "review_tpl"}
)

// down 目标 = 000026 / 000028 的终态（退掉 abnormal_report 一键，其余逐字不变）。
var (
	t372AdminBefore = []string{"dashboard", "realtime", "patients", "teams", "devices",
		"alerts", "comm", "orthosis", "install", "review", "review_tpl", "tech", "doctor_acct", "perm", "config"}
	t372DoctorBefore = []string{"dashboard", "realtime", "alerts", "orthosis", "review", "review_tpl"}
)

func TestITT372AbnormalReportModuleUp(t *testing.T) {
	// ① 前滚终值：harness 已按文件名序跑完全部 up.sql，直接读现库
	scope, admin := t368RolePerms(t, "ROLE_ADMIN")
	assert.Equal(t, t372AdminAfter, admin, "000029 后 ROLE_ADMIN.modules 应为 16 项且顺序与 PAGE_MODULES 一致")
	assert.Equal(t, "all", scope, "补模块键不得顺带动数据范围（scope 仍是 all）")

	_, doctor := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t372DoctorAfter, doctor, "000029 后 ROLE_DOCTOR.modules 应为 7 项（含 abnormal_report）")

	// abnormal_report 必须**只有一个拼法**：词表一改，前后端与库三处镜像同时红
	assert.Contains(t, admin, "abnormal_report")
	assert.Contains(t, doctor, "abnormal_report")

	// ② 幂等
	runMigrationFile(t, t372UpFile)
	_, adminAgain := t368RolePerms(t, "ROLE_ADMIN")
	assert.Equal(t, t372AdminAfter, adminAgain, "重复执行 000029 up 应幂等")

	// ③ ROLE_CS 一字未动
	_, cs := t368RolePerms(t, "ROLE_CS")
	assert.Equal(t, []string{"comm"}, cs, "客服不许拿到异常报告页")
}

func TestITT372AbnormalReportModuleRoundTrip(t *testing.T) {
	// ⚠ t.Cleanup 里禁用 require / runMigrationFile（FailNow 不许出现在清理函数内，见 t368 同款注释）。
	t.Cleanup(func() {
		sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir(), t372UpFile))
		if err != nil {
			t.Errorf("还原时读 %s 失败: %v", t372UpFile, err)
			return
		}
		if _, execErr := itStore.pool.Exec(context.Background(), string(sqlBytes)); execErr != nil {
			t.Errorf("还原 000029 前滚态失败: %v", execErr)
		}
	})

	// ── 现场：前滚后的 16 / 7 ──
	_, origAdmin := t368RolePerms(t, "ROLE_ADMIN")
	require.Equal(t, t372AdminAfter, origAdmin)
	_, origDoctor := t368RolePerms(t, "ROLE_DOCTOR")
	require.Equal(t, t372DoctorAfter, origDoctor)

	// ── down：退到 000026 / 000028 的终态 ──
	runMigrationFile(t, t372DownFile)
	scope, admin := t368RolePerms(t, "ROLE_ADMIN")
	assert.Equal(t, t372AdminBefore, admin, "000029 down 应退回 15 项（只退 abnormal_report 一键）")
	assert.Equal(t, "all", scope)
	_, doctor := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t372DoctorBefore, doctor, "000029 down 应退回 6 项 = 000028 的终态")

	// down 期间也不许牵连客服
	_, cs := t368RolePerms(t, "ROLE_CS")
	assert.Equal(t, []string{"comm"}, cs, "down 期间 ROLE_CS 不许被牵连")

	// ── 再 up：回到 16 / 7 ──
	runMigrationFile(t, t372UpFile)
	_, afterAdmin := t368RolePerms(t, "ROLE_ADMIN")
	assert.Equal(t, t372AdminAfter, afterAdmin, "down → up 往返后 admin 必须精确回到前滚终值")
	_, afterDoctor := t368RolePerms(t, "ROLE_DOCTOR")
	assert.Equal(t, t372DoctorAfter, afterDoctor, "down → up 往返后 doctor 必须精确回到前滚终值")
}
