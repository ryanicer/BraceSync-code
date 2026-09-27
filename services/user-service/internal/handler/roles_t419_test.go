// T419 格一（Boss 裁定，PM 2026-09-27 12:4x 转达）：医护「基础权限模板」的默认预设不得含患者沟通。
//
// 另立文件而不改 roles_t252_test.go：那份是 T252 11.2 的行为契约用例集（模板 3 条 / 增删改 / 审计），
// 本卡只加一条预设层的锁定。
package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestT419_DoctorTemplateHasNoCommModule(t *testing.T) {
	tpl, found := findRoleTemplate("doctor")
	require.True(t, found, "医护模板必须在（3 条 = 设计稿 权限控制.html 下拉）")

	// 判据来自 PRD §7D.11 矩阵第 8 行「💬 患者沟通 医护 —」，与 seed.sql 的 ROLE_DOCTOR.modules 同口径。
	assert.NotContains(t, tpl.Permissions.Modules, "comm",
		"医护预设模板不得含 comm：库内预置行与权限矩阵都不给医护患者沟通页")
	// 其余各项一字不动 —— Boss 本次只裁了 comm 一格，patients 是同性但未裁项，不顺手改
	assert.Equal(t, []string{"dashboard", "realtime", "patients", "alerts", "orthosis"},
		tpl.Permissions.Modules)

	// 描述也是用户可见文案（新增角色弹窗、角色列表都渲染它）：授权里没有沟通，文案不许再写「+沟通」
	assert.Equal(t, "患者数据+告警处理", tpl.Description)
	assert.NotContains(t, tpl.Description, "沟通")

	// 反向：客服那一格的 comm（矩阵第 8 行 cs ✅）不许被本格收口顺手削掉
	cs, found := findRoleTemplate("cs")
	require.True(t, found)
	assert.Equal(t, []string{"comm"}, cs.Permissions.Modules)
}
