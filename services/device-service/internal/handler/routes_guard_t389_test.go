// T389-N3：设备域写端点的「路由推导式」守卫。
//
// 缺陷原貌（卡面 N3）：身份门禁是一条条挂在 handler 方法体里的，测试表是手写的。
// 新增一条写路由时，只要没人往表里补一行，它就带着「服务层零判定」的状态合进来，
// 而 CI 依旧全绿 —— 守卫守的是「已知的那七条」，不是「设备域所有写入口」。
//
// 本文件把方向反过来：以 gin 路由表为事实源（Router().Routes()），要求每一条写方法路由
// 必须落在下面两张表之一 ——
//   - 已收口表 t389GatedWrites：门禁挂在处理器里，且必须自带一条越权现场可供运行期反查；
//   - 豁免表 t389ExemptWrites：不收口，但理由必须写在表里（空理由 = 判红）。
//
// 两张表都没接住的新写路由 ⇒ 差集非空 ⇒ 判红。
//
// 这样「加路由忘加门禁」和「加了门禁忘加用例」都会红，而不需要有人记得来改这个文件；
// 唯一需要人来判断的是豁免表，而那一次判断会留下书面理由。
package handler

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t389GatedWrite 一条已收口的写路由。
// pattern 须与 gin 路由表里的形状逐字同形（含 :param 名）；
// epName 指向 t387Endpoints() 中的那条现场，用于真实发一次越权请求。
type t389GatedWrite struct {
	pattern string
	epName  string
	gate    string
}

// t389GatedWrites 卡面点名的七条（T387 收口面）。条数与本表逐字对应，少一条即「假覆盖」。
var t389GatedWrites = []t389GatedWrite{
	{"POST /api/v1/devices/:deviceId/bind", "bind", "T387 assertDeviceWriteRole"},
	{"POST /api/v1/devices/:deviceId/rebind", "rebind", "T387 assertDeviceWriteRole"},
	{"POST /api/v1/devices/:deviceId/unbind", "unbind", "T387 assertDeviceWriteRole"},
	{"POST /api/v1/devices/:deviceId/wifi", "wifi", "T387 assertDeviceWriteRole"},
	{"POST /api/v1/install-records", "create-install", "T387 assertDeviceWriteRole"},
	{"PUT /api/v1/install-records/:id", "update-install-meta", "T387 assertDeviceWriteRole"},
	{"POST /api/v1/baselines", "save-baseline", "T387 assertDeviceWriteRole"},
}

// t389ExemptWrites 有意不收口的写路由 ⇒ 理由必须写全。
// 新加一条进来就等于承认「这个写入口对医护/客服仍然敞开」，请先取得 PM 或 Boss 的裁定。
var t389ExemptWrites = map[string]string{
	"POST /api/v1/devices": "设备注册（T387 遗留第 2 项）：调用方是技师安装流程与 admin-web 设备管理页，" +
		"卡面未点名收紧；风险定性是脏数据（无归属设备入库）而非越权改绑。待裁，裁前不动。",
	"POST /api/v1/devices/:deviceId/provision-key": "T193 归属路径：正当调用方含患者本人（领自己绑定设备的配网密钥），" +
		"按技师/管理员 allow-list 收口会挡掉患者端；该端点已有 requireDeviceBoundToCaller 归属判定。",
	"POST /internal/devices/:deviceId/report": "服务间内部面：不经网关、调用方是 data-service，" +
		"X-Role 语义不是员工角色；收口成技师/管理员会断掉上报链路。",
}

// t389WriteMethods 写方法集合。读侧（GET）的团队隔离归 T378，不在本守卫射程内。
func t389WriteMethods() map[string]bool {
	return map[string]bool{
		http.MethodPost: true, http.MethodPut: true,
		http.MethodPatch: true, http.MethodDelete: true,
	}
}

// t389DerivedWrites 从真实路由表反推「设备域所有写入口」，返回 "METHOD /path" 形状的有序集合
func t389DerivedWrites(t *testing.T, r *gin.Engine) []string {
	t.Helper()
	wm := t389WriteMethods()
	seen := map[string]bool{}
	var out []string
	for _, rt := range r.Routes() {
		if !wm[rt.Method] {
			continue
		}
		// 探针端点不是业务写入口
		if rt.Path == "/healthz" || rt.Path == "/metrics" {
			continue
		}
		key := rt.Method + " " + rt.Path
		require.False(t, seen[key], "路由表里出现重复写路由 %s", key)
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	require.NotEmpty(t, out, "路由表里推不出任何写路由 ⇒ 本守卫已失去意义，先查 Router() 装配")
	return out
}

// ── 1. 覆盖性：每条写路由要么收口、要么带理由豁免 ────────────────────

func TestT389_EveryDerivedWriteRouteIsGatedOrExempt(t *testing.T) {
	r, _, _ := t387Env(t)
	derived := t389DerivedWrites(t, r)

	gated := map[string]t389GatedWrite{}
	for _, g := range t389GatedWrites {
		require.NotEmpty(t, g.epName, "已收口路由 %s 必须指明对应的越权现场（epName）", g.pattern)
		require.False(t, gated[g.pattern] != (t389GatedWrite{}), "已收口表里 %s 重复登记", g.pattern)
		gated[g.pattern] = g
	}
	require.Len(t, t389GatedWrites, 7, "卡面点名的收口面是七条，本表条数须与之相等")

	for _, key := range derived {
		_, isGated := gated[key]
		reason, isExempt := t389ExemptWrites[key]
		if !isGated {
			assert.True(t, isExempt,
				"新增写路由 %s 既未收口也未登记豁免 ⇒ 设备域出现了一个无门禁的写入口。"+
					"要么挂 assertDeviceWriteRole 并进已收口表（含越权现场），"+
					"要么进豁免表并写明理由。", key)
		}
		if isExempt {
			assert.NotEmpty(t, strings.TrimSpace(reason), "豁免表里的 %s 必须自带理由", key)
			assert.False(t, isGated, "%s 同时出现在已收口表与豁免表 ⇒ 两张表口径互斥", key)
		}
	}

	// 反向：表里不许留已经不存在的路由（否则守卫守的是空气）
	inDerived := map[string]bool{}
	for _, key := range derived {
		inDerived[key] = true
	}
	for _, g := range t389GatedWrites {
		assert.True(t, inDerived[g.pattern],
			"已收口表里的 %s 在路由表里找不到 ⇒ 该门禁已随路由改名/下线路由，表要跟着改", g.pattern)
	}
	for pattern := range t389ExemptWrites {
		assert.True(t, inDerived[pattern], "豁免表里的 %s 在路由表里已不存在", pattern)
	}
}

// ── 2. 真实性：已收口的每条路由，发一次越权请求确实被挡 ──────────────

// TestT389_GatedRoutesFromRouterActuallyDenyNonWriterRoles 以路由表为循环起点，
// 逐条用真实路径（:param 换成现场资源号）向已收口端点发医护 / 客服身份的写请求。
// 判据是 T387 的门禁真的挂在了这条路由的处理器上：403 + 业务码字面量 20403（N1 的锁）。
func TestT389_GatedRoutesFromRouterActuallyDenyNonWriterRoles(t *testing.T) {
	r, st, _ := t387Env(t)
	derived := t389DerivedWrites(t, r)

	eps := map[string]t387Ep{}
	for _, ep := range t387Endpoints() {
		eps[ep.name] = ep
	}
	gated := map[string]t389GatedWrite{}
	for _, g := range t389GatedWrites {
		gated[g.pattern] = g
	}

	for _, key := range derived {
		g, isGated := gated[key]
		if !isGated {
			continue
		}
		ep, ok := eps[g.epName]
		if !ok {
			t.Errorf("已收口路由 %s 指向的越权现场 %q 不存在 ⇒ 这条路由没有可跑的拒绝用例", key, g.epName)
			continue
		}
		for _, role := range t389DeniedRolesOnWire() {
			t.Run(key+" "+role, func(t *testing.T) {
				path, body := ep.prepare(t, st, false)
				status, raw := t389RawReq(t, r, ep.method, path, role, t387DoctorUID, body)

				assert.Equal(t, t389HTTPForbidden, status, "%s → body=%s", key, string(raw))
				assert.Contains(t, string(raw), `"code":20403`,
					"%s 的拒绝报文须逐字带 code:20403", key)
			})
		}
	}
}
