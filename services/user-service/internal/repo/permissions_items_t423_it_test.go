//go:build integration
// +build integration

// Package repo 集成测试：T423「PUT 省略 items 键保库内原值」的库侧那一跳。
//
// handler 层的用例钉的是「写通道收到的那段 JSON」（fakeStore 记 payload），本用例钉的是
// 那段 JSON 经 $2::jsonb 落进 roles.permissions_json 再读回来的形状 —— T413 验收报告
// 第六节把「真实 jsonb 往返」列为未覆盖项（本机无 Docker 时只能靠 CI 间接背书），
// 而 T423 的收口结论正是「库里原值不动」，所以这一跳要自己站住：
// 三态（null / 空数组 / 显式清单）在 jsonb 里必须还是三个值，不能塌成两个。
//
// 🔴 形状断言只走解析、不走子串：jsonb 回读在冒号后带空格，且键按字典序重排，
// 按 Go 紧凑 JSON 的字面量去 Contains 会静默恒假（首版就是这么红的）。
//
// 只在集成层跑（testcontainers 起的临时 PG，不碰 staging seed；建的角色跑完删）。
package repo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t423ItemsShape 用指针收 items 键，把 null 与 [] 分开：
// 键值 null 时 ptr 为 nil；键值 [] 时 ptr 指着一个空切片。
// 键缺失与 null 同形，那一格不在这里判（请求侧的「省略」态由 handler 层用例覆盖）。
type t423ItemsShape struct {
	Items *[]string `json:"items"`
}

// t423Perms 读库内某角色 permissions_json：一次给出原文串 + items 的两种读法。
func t423Perms(t *testing.T, roleID string) (raw string, items []string, itemsPtr *[]string) {
	t.Helper()
	var perms struct {
		Items []string `json:"items"`
		Probe t423ItemsShape
	}
	ctx := context.Background()
	row, err := itStore.GetRole(ctx, roleID)
	require.NoError(t, err)
	require.NotNil(t, row, "临时角色 %s 应存在", roleID)
	require.NoError(t, json.Unmarshal([]byte(row.PermissionsJSON), &perms),
		"%s 的 permissions_json 解析失败：%s", roleID, row.PermissionsJSON)
	return row.PermissionsJSON, perms.Items, perms.Probe.Items
}

func TestITT423PermissionsItemsThreeStatesRoundTrip(t *testing.T) {
	ctx := context.Background()

	created, err := itStore.CreateRole(ctx, "T423集成临时角色", "T423 用例专用，跑完删",
		`{"scope":"team","modules":["alerts","comm"],"items":["alerts.view","comm.reply"]}`)
	require.NoError(t, err)
	require.NotNil(t, created)
	defer func() {
		if _, delErr := itStore.pool.Exec(ctx, `DELETE FROM roles WHERE role_id = $1`, created.RoleID); delErr != nil {
			t.Errorf("t423 cleanup role %s: %v", created.RoleID, delErr)
		}
	}()

	// ① 起点：库内是显式收窄清单（handler 的省略腿要保住的就是这一段）
	raw0, items, itemsPtr := t423Perms(t, created.RoleID)
	require.NotNil(t, itemsPtr, "显式清单不许解成 null 形状")
	assert.Equal(t, []string{"alerts.view", "comm.reply"}, *itemsPtr)
	assert.Equal(t, []string{"alerts.view", "comm.reply"}, items)

	// ② 省略腿的落库形状：写通道拿到「库里原值」整体写回，那段 JSON 应逐字不变
	ok, err := itStore.UpdateRolePermissions(ctx, created.RoleID,
		`{"scope":"team","modules":["alerts","comm"],"items":["alerts.view","comm.reply"]}`)
	require.NoError(t, err)
	require.True(t, ok)
	raw, itemsAfter, ptrAfter := t423Perms(t, created.RoleID)
	require.NotNil(t, ptrAfter, "显式清单不许在 jsonb 往返里变成 null")
	assert.Equal(t, []string{"alerts.view", "comm.reply"}, *ptrAfter)
	assert.Equal(t, []string{"alerts.view", "comm.reply"}, itemsAfter)
	assert.Equal(t, raw0, raw, "同值重写后库内那段 JSON 应逐字不变（读侧没有隐式物化）")

	// ③ 另两态各写一次：null = 未细化，[] = 显式全不勾，二者在库里必须还是两个值
	for _, tc := range []struct {
		name    string
		payload string
		wantNil bool
	}{
		{"未细化", `{"scope":"team","modules":["alerts","comm"],"items":null}`, true},
		{"显式全不勾", `{"scope":"team","modules":["alerts","comm"],"items":[]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := itStore.UpdateRolePermissions(ctx, created.RoleID, tc.payload)
			require.NoError(t, err)
			require.True(t, ok)
			_, items, ptr := t423Perms(t, created.RoleID)
			if tc.wantNil {
				assert.Nil(t, ptr, "库里未细化读回来不许是空数组指针")
				assert.Nil(t, items, "库里未细化读回来不许塌成空数组")
			} else {
				require.NotNil(t, ptr, "库里显式全不勾读回来不许塌成 nil")
				assert.Empty(t, *ptr, "[] 保住为空，不许被补成清单")
				assert.NotNil(t, items, "库里显式全不勾读回来不许塌成 nil")
				assert.Empty(t, items)
			}
		})
	}
}
