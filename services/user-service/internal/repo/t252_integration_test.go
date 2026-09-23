//go:build integration
// +build integration

// T252 真库集成测试：migration 000016 落库 + 2.2 逐点阈值 / 11.2 角色增删改 / 12.3 审计读写
//
// 本地无 Docker 时由 CI 的 go-integration job 执行（harness 顺序 apply scripts/db/migrations/*.up.sql，
// 因此本文件同时是 000016 SQL 的落地验证）。
// T262：预置角色口径按 Boss 2026-09-20 裁定收敛为 3 个，000017 的落地验证见
// TestITT262PresetRolesCollapsedToThree。
// 只使用本文件私有的 ID/键值域，避免与 seedITData 及既有用例互污染。
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── 2.2 逐采集点阈值 ──

func sysConfigValue(t *testing.T, key string) (string, bool) {
	t.Helper()
	var v string
	err := itStore.pool.QueryRow(context.Background(),
		`SELECT config_value FROM sys_configs WHERE config_key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	require.NoError(t, err)
	return v, true
}

func TestITT252AlertPointRulesRoundTrip(t *testing.T) {
	ctx := context.Background()
	upper, lower := 20.5, 5.25

	// 迁移播种的四条全局键存在且值正确
	// （下限值 1 是 000021/T281 的结果：000016 播的 10 属 ÷10 前旧量纲，见 TestITT281）
	for key, want := range map[string]string{
		"threshold_pressure_low":    "1",
		"device_offline_minutes":    "30",
		"continuous_wear_max_hours": "23",
		"report_timeout_minutes":    "5",
	} {
		got, ok := sysConfigValue(t, key)
		require.True(t, ok, "000016 应播 %s", key)
		assert.Equal(t, want, got)
	}

	// 首次保存：统一上限 + 两点（一点独立阈值、一点只改勾选）
	require.NoError(t, itStore.SaveAlertRules(ctx,
		[]ConfigKV{{Key: "threshold_pressure_high", Value: "50"}},
		[]AlertPointRuleRow{
			{PointID: "P03", Monitored: true, UpperN: &upper, LowerN: &lower},
			{PointID: "P07", Monitored: false},
		}, "ADM-USR-IT"))

	rows, err := itStore.ListAlertPointRules(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2, "稀疏存放：只有显式保存过的点有行")
	assert.Equal(t, "P03", rows[0].PointID, "按 point_id 升序")
	require.NotNil(t, rows[0].UpperN)
	assert.InDelta(t, 20.5, *rows[0].UpperN, 1e-9, "NUMERIC(6,2) 精度保真")
	require.NotNil(t, rows[0].LowerN)
	assert.InDelta(t, 5.25, *rows[0].LowerN, 1e-9)
	assert.False(t, rows[1].Monitored)
	assert.Nil(t, rows[1].UpperN, "NULL = 跟随统一上限")
	assert.Nil(t, rows[1].LowerN)

	v, ok := sysConfigValue(t, "threshold_pressure_high")
	require.True(t, ok)
	assert.Equal(t, "50", v, "统一上限与逐点同一事务落 sys_configs")

	// 二次保存：UPSERT 覆盖而非追加；upperN 传 nil ⇒ 列回落 NULL
	require.NoError(t, itStore.SaveAlertRules(ctx, nil,
		[]AlertPointRuleRow{{PointID: "P03", Monitored: true}, {PointID: "P07", Monitored: true, UpperN: &upper}}, "ADM-USR-IT"))
	rows, err = itStore.ListAlertPointRules(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2, "UPSERT 不产生新行")
	assert.Nil(t, rows[0].UpperN)
	assert.True(t, rows[0].Monitored)
	require.NotNil(t, rows[1].UpperN)
	assert.InDelta(t, 20.5, *rows[1].UpperN, 1e-9)
	updatedBy, err := queryString(ctx, `SELECT updated_by FROM alert_point_rules WHERE point_id = 'P03'`)
	require.NoError(t, err)
	assert.Equal(t, "ADM-USR-IT", updatedBy)

	// 恢复默认：清表 + 统一上下限回默认，且不动其它全局键
	require.NoError(t, itStore.ResetAlertRules(ctx,
		[]ConfigKV{{Key: "threshold_pressure_high", Value: "45"}, {Key: "threshold_pressure_low", Value: "10"}}, "ADM-USR-IT"))
	rows, err = itStore.ListAlertPointRules(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows, "恢复默认 = 清空逐点表")
	v, _ = sysConfigValue(t, "threshold_pressure_high")
	assert.Equal(t, "45", v)
	v, _ = sysConfigValue(t, "report_timeout_minutes")
	assert.Equal(t, "5", v, "恢复默认不越界改全局规则")
}

func TestITT252AlertPointRulesRejectsUnknownPointID(t *testing.T) {
	ctx := context.Background()
	// 编号 CHECK 与前端 pointId 口径同源：P21 / P0 都不该写得进
	for _, bad := range []string{"P21", "P0", "p01", "PX1"} {
		err := itStore.SaveAlertRules(ctx, nil,
			[]AlertPointRuleRow{{PointID: bad, Monitored: true}}, "ADM-USR-IT")
		assert.Error(t, err, "非法点位编号 %q 应被 DB CHECK 拒绝", bad)
	}
	rows, err := itStore.ListAlertPointRules(ctx)
	require.NoError(t, err)
	for _, r := range rows {
		assert.NotContains(t, []string{"P21", "P0", "p01", "PX1"}, r.PointID, "失败的写入不留残行")
	}
}

// ── 11.2 角色增删改 ──

// customRoleIDRe 自定义角色 ID 的形状（`ROLE_C` + 10 位大写 hex，见 roles_write.go:31-39）。
var customRoleIDRe = regexp.MustCompile(`^ROLE_C[0-9A-F]{10}$`)

func TestITT262PresetRolesCollapsedToThree(t *testing.T) {
	ctx := context.Background()
	list, err := itStore.ListRoles(ctx)
	require.NoError(t, err)
	byID := map[string]RoleRow{}
	for _, r := range list {
		byID[r.RoleID] = r
	}

	// ① 000016 误播的 5 个必须已被 000017 删除
	//    （Boss 2026-09-20 裁定：主任医师/主治医师/康复师/护士是**职称**，走 doctors.title；
	//      超级管理员与 ROLE_ADMIN 功能重复）
	for _, gone := range []string{"ROLE_SUPER_ADMIN", "ROLE_CHIEF_DOCTOR", "ROLE_ATTENDING_DOCTOR",
		"ROLE_REHAB_THERAPIST", "ROLE_NURSE"} {
		_, ok := byID[gone]
		assert.False(t, ok, "000017 应删除误播角色 %s", gone)
	}

	// ② 预置登录角色 = 3 个，name/scope 与 scripts/db/seed/seed.sql 同源
	//    （000017 幂等补播这 3 条，故 harness 只跑 migrations、不跑 seed.sql 也成立；
	//     name 取 000025 之后的现值 —— ROLE_DOCTOR 显示名已由「医生」改「医护」，
	//     Boss 2026-09-22 14:21 裁定只改称谓，role_id 仍是 ROLE_DOCTOR ⇒ 本条同时守住「键没被顺手改」）
	for _, want := range []struct{ id, name, scope string }{
		{"ROLE_ADMIN", "运营管理员", "all"},
		{"ROLE_DOCTOR", "医护", "team"},
		{"ROLE_CS", "客服", "all_patients"},
	} {
		row, ok := byID[want.id]
		require.True(t, ok, "预置登录角色 %s 应存在", want.id)
		assert.Equal(t, want.name, row.Name)
		assert.Equal(t, "enabled", row.Status)
		var perms struct {
			Scope   string   `json:"scope"`
			Modules []string `json:"modules"`
		}
		require.NoError(t, json.Unmarshal([]byte(row.PermissionsJSON), &perms))
		assert.Equal(t, want.scope, perms.Scope)
		assert.NotEmpty(t, perms.Modules)
	}

	// ③ 收口证明：剔除测试专用行（harness 播的 ROLE_IT + 各用例新建的自定义角色）后，
	//    roles 表**恰好** 3 条，且就是上面那 3 个 —— 不许多、不许少、不许有第三个来源的预置字面量。
	// 🔴 自定义角色 ID 必须**整串**匹配 `ROLE_C` + 10 位大写 hex（repo/roles_write.go:31-39）：
	//    用 HasPrefix("ROLE_C") 会把预置的 **ROLE_CS** 一并吞掉（本用例首版即因此在 CI 漏判 ROLE_CS）。
	preset := []string{}
	for id := range byID {
		if id == "ROLE_IT" || customRoleIDRe.MatchString(id) {
			continue
		}
		preset = append(preset, id)
	}
	sort.Strings(preset)
	assert.Equal(t, []string{"ROLE_ADMIN", "ROLE_CS", "ROLE_DOCTOR"}, preset,
		"预置角色集应恰好为 3 个登录角色")
}

func TestITT252RoleCRUD(t *testing.T) {
	ctx := context.Background()
	name := "集成自定义角色" + t.Name()

	taken, err := itStore.RoleNameTaken(ctx, name, "")
	require.NoError(t, err)
	assert.False(t, taken)

	created, err := itStore.CreateRole(ctx, name, "集成用例角色", `{"scope":"team","modules":["patients"]}`)
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Contains(t, created.RoleID, "ROLE_C", "服务端生成 ID 前缀")
	assert.LessOrEqual(t, len(created.RoleID), 32, "role_id VARCHAR(32) 不越列宽")
	assert.Equal(t, "enabled", created.Status, "新角色默认启用")
	assert.Equal(t, 0, created.MemberCount)
	assert.Equal(t, "集成用例角色", *created.Description)

	// 查重语义（两条写入口都用它）：exclude 空 → 撞自己的名字算占用；
	// exclude 传自身 roleId → 排除本行（改名改回原名不算冲突）。
	// 注：roles.name 无唯一索引，查重是单条 SELECT EXISTS，并发窗口内仍可能落进两个同名，故列为待裁项。
	taken, err = itStore.RoleNameTaken(ctx, name, "")
	require.NoError(t, err)
	assert.True(t, taken, "exclude 为空时同名应判占用（拦 409）")

	taken, err = itStore.RoleNameTaken(ctx, name, created.RoleID)
	require.NoError(t, err)
	assert.False(t, taken, "排除自身 roleId 后同名不再判占用")

	// 编辑：只改描述，其余保持
	updated, err := itStore.UpdateRole(ctx, created.RoleID, nil, strPtrIT("改后描述"), nil)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, name, updated.Name, "未提交的列不改")
	assert.Equal(t, "改后描述", *updated.Description)

	// 启停 + 改名（非预置允许）
	status := "disabled"
	newName := name + "-改"
	updated, err = itStore.UpdateRole(ctx, created.RoleID, &newName, nil, &status)
	require.NoError(t, err)
	assert.Equal(t, newName, updated.Name)
	assert.Equal(t, "disabled", updated.Status)
	assert.Equal(t, "改后描述", *updated.Description, "nil 描述不清空既有值")

	_, err = itStore.UpdateRole(ctx, "ROLE_C_NOT_EXIST", &newName, nil, nil)
	assert.ErrorIs(t, err, ErrRoleNotFound)

	// 被账号引用的角色删除被拒（seedITData：ADM-USR-IT 挂在 ROLE_IT 上）
	inUse := itStore.DeleteRole(ctx, "ROLE_IT")
	var conflict *ErrRoleInUse
	require.ErrorAs(t, inUse, &conflict)
	assert.Equal(t, 1, conflict.MemberCount)
	role, err := itStore.GetRole(ctx, "ROLE_IT")
	require.NoError(t, err)
	require.NotNil(t, role, "拒绝删除后角色仍在")

	require.NoError(t, itStore.DeleteRole(ctx, created.RoleID))
	role, err = itStore.GetRole(ctx, created.RoleID)
	require.NoError(t, err)
	assert.Nil(t, role, "删除后读不到")
	assert.ErrorIs(t, itStore.DeleteRole(ctx, created.RoleID), ErrRoleNotFound)
}

// ── 12.3 审计读写 ──

func TestITT252AuditLogWriteAndQuery(t *testing.T) {
	ctx := context.Background()
	const (
		action   = "it_t252_config_change"
		targetID = "P-USR-IT-1"
		opID     = itAdmin
		opName   = "集成账号" // seedITData 里 ADM-USR-IT 的 name
	)
	require.NoError(t, itStore.WriteAuditLog(ctx, AuditInput{
		OperatorID: opID, OperatorRole: "ROLE_IT", Action: action,
		TargetType: "alert_rule", TargetID: targetID,
		Description: "保存告警规则：统一上限 50N",
		Detail:      map[string]any{"pointIds": []string{"P01", "P07"}},
		IP:          "10.0.0.9",
	}))
	// 内部动作可无操作人（NULL 而非空串）
	require.NoError(t, itStore.WriteAuditLog(ctx, AuditInput{
		Action: action, TargetType: "alert_rule", TargetID: "P-USR-IT-2", Description: "系统自动恢复默认",
	}))

	to := time.Now().Add(time.Minute)
	from := to.Add(-time.Hour)
	rows, total, err := itStore.QueryAuditLogs(ctx, AuditFilter{
		From: &from, To: &to, Action: action, Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	assert.True(t, rows[0].Ts.After(rows[1].Ts) || rows[0].LogID > rows[1].LogID, "按 ts 倒序")

	var first *AuditLogRow
	for i := range rows {
		if rows[i].OperatorName != nil {
			first = &rows[i]
		}
	}
	require.NotNil(t, first, "操作人显示名应跨表反查出来")
	assert.Equal(t, opName, *first.OperatorName)
	assert.Equal(t, "ROLE_IT", *first.OperatorRole)
	assert.Equal(t, "10.0.0.9", *first.IP)
	assert.Equal(t, "alert_rule", *first.TargetType)
	var detail struct {
		Description string   `json:"description"`
		PointIDs    []string `json:"pointIds"`
	}
	require.NoError(t, json.Unmarshal([]byte(*first.Detail), &detail))
	assert.Equal(t, "保存告警规则：统一上限 50N", detail.Description, "description 合并进 detail JSONB")
	assert.Equal(t, []string{"P01", "P07"}, detail.PointIDs)

	// 操作人筛选按 ID 或显示名 ILIKE；目标筛选精确匹配
	rows, total, err = itStore.QueryAuditLogs(ctx, AuditFilter{Operator: "集成", Page: 1, PageSize: 10, Action: action})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "按显示名模糊匹配只命中有操作人的那条")
	require.Len(t, rows, 1)
	assert.Equal(t, targetID, *rows[0].TargetID)

	// 无操作人的行：operator 相关列为 NULL，不编造
	var anonymous *AuditLogRow
	rows, _, err = itStore.QueryAuditLogs(ctx, AuditFilter{TargetID: "P-USR-IT-2", Action: action, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	anonymous = &rows[0]
	assert.Nil(t, anonymous.OperatorID)
	assert.Nil(t, anonymous.OperatorName)
	assert.Nil(t, anonymous.IP)

	// 时间窗外查不到（半开区间生效）
	early := from.Add(-2 * time.Hour)
	earlyTo := from.Add(-time.Hour)
	_, total, err = itStore.QueryAuditLogs(ctx, AuditFilter{From: &early, To: &earlyTo, Action: action, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
}

func queryString(ctx context.Context, sql string, args ...any) (string, error) {
	var v string
	err := itStore.pool.QueryRow(ctx, sql, args...).Scan(&v)
	return v, err
}

func strPtrIT(s string) *string { return &s }
