// T356 ②：把 T337 那条「详情有的键列表必须有」断言泛化成表驱动，覆盖本服务全部成对
// list/detail 只读资源；并用 gin 路由表反查「新增成对路由没进表」⇒ 显式判红。
//
// 为什么这条断言值得泛化：GET /api/v1/teams 少回填 description/status（T337）、
// 少 leader/createdAt（T333）都是同一形状 —— 详情 DTO 与列表 DTO 是两个结构体，
// 给详情加列时列表那边不会跟着改。表驱动 + 路由反查把「忘了」变成「CI 红」。
package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// listDetailCase 一条成对资源。detailOnly 是「已核对过、确实只在详情出现」的键基线；
// 往详情 DTO 加新列时必须在这里表态（写进基线并附理由，或同步补列表列），否则判红。
type listDetailCase struct {
	name       string
	listPath   string
	detailPath string
	headers    map[string]string
	setup      func(e *testEnv)
	detailOnly map[string]string
}

func t356PatientRow() repo.PatientRow {
	teamName := "一组"
	at := time.Date(2026, 8, 3, 1, 2, 3, 0, time.UTC)
	return repo.PatientRow{
		PatientID: "P20260001", Name: "张小明", Status: "active",
		TeamID: strPtr("TEAM01"), TeamName: &teamName,
		CreatedAt: at, UpdatedAt: at,
	}
}

func t356Cases() []listDetailCase {
	return []listDetailCase{
		{
			name: "patients", listPath: "/api/v1/admin/patients", detailPath: "/api/v1/admin/patients/P20260001",
			setup: func(e *testEnv) {
				row := t356PatientRow()
				e.store.patients = []repo.PatientRow{row}
				e.store.patientTotal = 1
				e.store.patient = &row
			},
		},
		{
			name: "teams", listPath: "/api/v1/teams", detailPath: "/api/v1/teams/TEAM01",
			setup: func(e *testEnv) {
				e.store.teams = t337TeamRows()
				e.store.gotTeam = t337TeamDetailRow()
			},
		},
		{
			name: "flow-templates", listPath: "/api/v1/admin/flow/templates", detailPath: "/api/v1/admin/flow/templates/FLOW_T0A1B2C3D4",
			headers: hdr(flowRoleAdmin, "A0001"),
			setup: func(e *testEnv) {
				e.store.flow.listTemplates = []repo.FlowTemplateRow{*fixFlowTemplateRow()}
				e.store.flow.listTemplateTotal = 1
				e.store.flow.tpl = fixFlowTemplateRow()
			},
		},
	}
}

// TestListDetailKeySuperset 逐资源比对两侧响应的线上键集合。
func TestListDetailKeySuperset(t *testing.T) {
	for _, c := range t356Cases() {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			c.setup(e)

			wList, listResp := e.do(http.MethodGet, c.listPath, nil, c.headers)
			require.Equal(t, http.StatusOK, wList.Code, "列表接口没回 200，键集合断言无从谈起")
			wDetail, detailResp := e.do(http.MethodGet, c.detailPath, nil, c.headers)
			require.Equal(t, http.StatusOK, wDetail.Code, "详情接口没回 200，键集合断言无从谈起")

			assertSupersetT356(t, wireKeysT356(t, listResp.Data), wireKeysT356(t, detailResp.Data), c.detailOnly)
		})
	}
}

// TestListDetailCasesMatchRoutes 表与路由表必须一一对应：
// ① 路由里新出现的成对资源没进表 ⇒ 判红（卡片要的「忘配显式失败」）；
// ② 表里的路径在路由里找不到 ⇒ 判红（接口改名/下线，基线不能烂在原地）。
func TestListDetailCasesMatchRoutes(t *testing.T) {
	e := newEnv(t, true, true)
	pairs := derivedListDetailPairs(methodPathsT356(e.h.Router().Routes()))
	t.Logf("路由表推导出的成对资源：%d 个", len(pairs))

	registered := map[string]bool{}
	for _, c := range t356Cases() {
		registered[c.listPath] = true
	}
	for _, c := range t356Cases() {
		_, ok := pairs[c.listPath]
		assert.True(t, ok, "表里登记了 %s（详情 %s），但路由里已找不到这对端点", c.listPath, c.detailPath)
	}
	for _, p := range pairs {
		assert.True(t, registered[p[0]], "成对路由 %s + %s 未登记进 t356Cases() —— 新资源要表态列表是否覆盖详情字段", p[0], p[1])
	}
}

// derivedListDetailPairs 从 gin 路由表推导成对只读资源：
// GET <prefix>/<:param> 且 GET <prefix> 也存在 ⇒ 一对。
// 于是 /teams 与 /teams/:teamId 成对，而 /teams/:teamId/members（尾段非参数）、
// /admin/roles/:roleId/permissions 都不算 —— 它们本来就是不同资源。
func derivedListDetailPairs(methodPaths []string) map[string][2]string {
	get := map[string]bool{}
	for _, mp := range methodPaths {
		if method, path, ok := strings.Cut(mp, " "); ok && method == http.MethodGet {
			get[path] = true
		}
	}
	out := map[string][2]string{}
	for path := range get {
		i := strings.LastIndex(path, "/")
		if i <= 0 {
			continue
		}
		head, last := path[:i], path[i+1:]
		if len(last) > 1 && strings.HasPrefix(last, ":") && get[head] {
			out[head] = [2]string{head, path}
		}
	}
	return out
}

func methodPathsT356(routes gin.RoutesInfo) []string {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.Method+" "+r.Path)
	}
	sort.Strings(out)
	return out
}

func assertSupersetT356(t *testing.T, listKeys, detailKeys []string, detailOnly map[string]string) {
	t.Helper()
	inList := map[string]bool{}
	for _, k := range listKeys {
		inList[k] = true
	}
	inDetail := map[string]bool{}
	for _, k := range detailKeys {
		inDetail[k] = true
	}
	for _, k := range detailKeys {
		if inList[k] || detailOnly[k] != "" {
			continue
		}
		t.Errorf("详情返回了 %s 但列表没有 —— 列表接口漏回填字段（T337 同类）；确属详情独有就写进 detailOnly 并附理由", k)
	}
	// 基线腐烂也判红：登记的独有键若已被列表补上、或详情不再回它，都要清理
	for k, why := range detailOnly {
		if !inDetail[k] {
			t.Errorf("detailOnly 登记了 %s（理由：%s），但详情已不回该键 —— 基线要清", k, why)
			continue
		}
		if inList[k] {
			t.Errorf("detailOnly 登记了 %s（理由：%s），但列表现已返回该键 —— 基线要清", k, why)
		}
	}
}

// wireKeysT356 取响应 data 的线上键集合：数组取首元素，PageData 信封取 list[0]。
// 空列表直接失败 —— 否则键集合断言空转。
func wireKeysT356(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	if obj, ok := v.(map[string]any); ok {
		inner, hasList := obj["list"]
		if !hasList {
			return sortedKeysT356(obj)
		}
		v = inner
	}
	arr, ok := v.([]any)
	require.True(t, ok, "响应 data 既不是对象也不是列表信封：%s", string(raw))
	require.NotEmpty(t, arr, "列表为空 ⇒ 键集合断言会空转，夹具必须给一行")
	row, ok := arr[0].(map[string]any)
	require.True(t, ok, "列表首行不是对象：%s", string(raw))
	return sortedKeysT356(row)
}

func sortedKeysT356(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
