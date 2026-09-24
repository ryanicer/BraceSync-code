// Package main — T379 三源权限门禁的「网关真值」腿
//
// 为什么需要这一条：前端门禁（apps/admin-web/test/perm-gateway-cross-source.spec.ts）要判「某个角色
// 打某个端点会不会被网关 403」，只能解析本文件的 rbac.go 源码文本 + 复刻 roleAuthz 的判定顺序。
// 那份复刻本身就是第四套说法——它错了，前端门禁就会绿着放行真实的越权或红着拦掉正常调用。
// 所以这里不比对源码文本，直接把 testdata/perm-gateway-table.json 的每一行发给**真实中间件**
// （setupRouter 全量路由 + httptest 模拟后端），量实际状态码：
//
//	deny=true  → 必须 403 且响应体是统一信封（网关真的拦住了，请求不得触达后端）
//	deny=false → 必须非 403；若回 404 说明网关没注册该路由（gin 的组中间件对未匹配路径不执行
//	             ⇒ RBAC 根本不参与，属绕过面，单列判红，见下方放行分支的注释）
//
// 表由前端门禁生成（UPDATE_GATEWAY_TABLE=1 npx vitest run test/perm-gateway-cross-source.spec.ts）。
// 表落在 services/ 下是刻意的：改网关矩阵或重生成表都会触发 CI-Go，两条腿在各自 job 里互相钉住。
package main

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// permGateRow 一行「(角色, 方法, 路径) → 网关是否拒」判定；path 已用表里的 sample 值填成可请求的具体路径
type permGateRow struct {
	Role   string `json:"role"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Deny   bool   `json:"deny"`
	Why    string `json:"why"`
}

type permGateTable struct {
	Sample string        `json:"sample"`
	Rows   []permGateRow `json:"rows"`
}

func readPermGateTable(t *testing.T) permGateTable {
	t.Helper()
	raw, err := os.ReadFile("testdata/perm-gateway-table.json")
	require.NoError(t, err, "缺 golden 表：在前端门禁里跑 UPDATE_GATEWAY_TABLE=1 npx vitest run test/perm-gateway-cross-source.spec.ts 生成")
	var tbl permGateTable
	require.NoError(t, json.Unmarshal(raw, &tbl), "golden 表不是合法 JSON")
	require.NotEmpty(t, tbl.Rows, "golden 表为空等于没钉")
	require.NotEmpty(t, tbl.Sample, "golden 表没写样本路径段值")

	// 防空转：三角色都要在场，且至少一行 deny（全 pass 的表证不了任何拦截）
	seen := map[string]bool{}
	denies := 0
	for _, r := range tbl.Rows {
		seen[r.Role] = true
		if r.Deny {
			denies++
		}
	}
	for _, role := range []string{roleAdmin, roleDoctor, roleCS} {
		assert.True(t, seen[role], "golden 表里没有 %s 的行，该角色的网关列等于没对拍", role)
	}
	assert.Greater(t, denies, 0, "golden 表一行 deny 都没有：门禁的网关列失去反证力")
	return tbl
}

// TestRBAC_T379_PermGateTableMatchesRealMiddleware golden 逐行打真实中间件，钉住前端那份复刻
func TestRBAC_T379_PermGateTableMatchesRealMiddleware(t *testing.T) {
	tbl := readPermGateTable(t)
	backend, received := captureBackend(t)
	gw := startFullGateway(t, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, testJWTSecretMain)

	tokens := map[string]map[string]string{
		roleAdmin:  rbacToken(t, roleAdmin),
		roleDoctor: rbacToken(t, roleDoctor),
		roleCS:     rbacToken(t, roleCS),
	}
	// 同角色复用 token：129 行 × 每行签一次 JWT 没意义，只拖慢
	for _, row := range tbl.Rows {
		t.Run(row.Role+" "+row.Method+" "+row.Path, func(t *testing.T) {
			hdr, ok := tokens[row.Role]
			require.True(t, ok, "golden 里出现了网关不认的角色 %s", row.Role)
			before := len(*received)
			code, body := httpDoFull(t, row.Method, gw.URL+row.Path, `{}`, hdr)
			reachedBackend := len(*received) > before

			if row.Deny {
				assert.Equal(t, http.StatusForbidden, code,
					"golden 判 %s %s %s 为 403（%s），实得 %d —— 前端门禁的判定复刻与网关真实行为分叉",
					row.Role, row.Method, row.Path, row.Why, code)
				assert.Contains(t, body, `"code":403`)
				assert.False(t, reachedBackend, "被拒请求不得触达后端：%s %s", row.Method, row.Path)
				return
			}
			// 放行行：不得 403。404 也一并判红——它意味着该 method+path 在网关没注册路由，
			// gin 的组中间件（jwtAuth/roleAuthz）对未匹配路径根本不会执行，等于绕过 RBAC。
			assert.NotEqual(t, http.StatusForbidden, code,
				"golden 判 %s %s %s 放行，实得 403 —— 网关矩阵比判定更严，前端门禁会漏报真实拦截",
				row.Role, row.Method, row.Path)
			if code == http.StatusNotFound {
				// DELETE /api/v1/admin/technicians/:techId 是已知的「注册了但契约无此端点」防绕过路由
				// （T039-H2，proxy_services.go:63），ROLE_ADMIN 透传后端 404 属预期；
				// 其余 404 都按「网关未注册 = RBAC 不参与」处理。
				assert.Contains(t, row.Path, "/technicians/",
					"放行行回 404 且不是已知的防绕过路由：该路径没注册进网关，roleAuthz 不会执行（绕过面），须补登记")
			}
		})
	}
}

// TestRBAC_T379_PermGateTableCoversVisiblePages 表必须覆盖到每个角色可见页至少一条端点，
// 否则「页面 × 网关」那一格在 golden 里是空的，前端门禁的派生失效也能瞒过 Go 这条腿。
func TestRBAC_T379_PermGateTableCoversVisiblePages(t *testing.T) {
	tbl := readPermGateTable(t)
	perRole := map[string]map[string]bool{}
	for _, row := range tbl.Rows {
		if perRole[row.Role] == nil {
			perRole[row.Role] = map[string]bool{}
		}
		perRole[row.Role][row.Method+" "+row.Path] = true
	}
	// 下限口径（不是页数真值 —— 页数真值只有前端 ROLE_PAGE_MATRIX 一份，这里再抄一份反而会腐烂）。
	// 它只拦一种失效：表里某角色的行几乎被清空（派生失灵或 golden 被手工裁过），那时端点数会掉到页数以下。
	// 数字取自 T379 当轮的可见页数 admin 16 / 医护 7 / 客服 1；加页会让它变严不变松，故不随每次改页回改。
	want := map[string]int{roleAdmin: 16, roleDoctor: 7, roleCS: 1}
	for role, pages := range want {
		require.Contains(t, perRole, role)
		assert.GreaterOrEqual(t, len(perRole[role]), pages,
			"golden 里 %s 的端点数少于其可见页数（%d < %d）：多半是页面→端点的派生瞎了", role, len(perRole[role]), pages)
	}
}
