// T399：设备域写端点「动作名」维度锁定（源自 T387 复验证据包 E2 反证）。
//
// 缺陷原貌（E2_swap_action_name.log）：把 handler.go 里 createInstall 调用点传给
// assertDeviceWriteRole 的 action 换成 "bind a device"，rc=0、整包 68+88 全绿。
// 原因是本维度此前只有一句 assert.Contains(t, msg, "may not")（write_scope_t387_test.go:407），
// 七条端点共用同一子串 ⇒ 动作名逐格怎么写、写没写串，测试看不见。
//
// 锁的依据是「错配会把 A 端点的拒绝说成 B 端点的动作」，不是「现网按文案分流」（实测）：
//   - 三端与 packages/shared-types 搜 "may not" / "install record" / "bind a device" 零命中，
//     前端按 HTTP 状态码分流（口径同 T389 N1 的订正，别再写成「有消费方按此文本分支」）；
//   - 网关是 ReverseProxy 透传响应体，不解析 message；
//   - 真正会被这格改动影响的是书面契约：docs/api/api-contracts.ts 已把 403 文案形状写成
//     `role "..." may not <动作>`，本卡把七条端点各自的 <动作> 补成逐条契约并钉进测试。
//
// 为什么只锁文案、不动生产代码：
//   - 「路由 → 处理器 → 仓储方法」这条链其实已经有牙：反证腿逐格断言 delta[ep.write]==1
//     （write_scope_t387_test.go:445），把 A 路由挂到 B 处理器会当场红；本卡补的是这条链之外
//     唯一无锁的一维 —— 传给门禁的自由文本；
//   - 若改成「生产侧一张 action 常量表 + 测试 import 它」，就退化成「用符号断言符号」，
//     正是 T389 N1 要堵掉的形状。所以生产保持调用点字面量，测试自带另一套字面量。
package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// t399Action 一条写路由的越权文案。三个字段全是字面量：
// epName 对齐 t387Endpoints()，method 对齐 t387Store 的仓储计数键，action 对齐 403 文案。
type t399Action struct {
	epName string
	method string
	action string
}

// t399ExpectedActions 七条收口写端点的动作名。
// 增删一条 = 设备域收口面变了，必须同时改 t389GatedWrites 与 api-contracts.ts。
func t399ExpectedActions() []t399Action {
	return []t399Action{
		{"bind", "Bind", "bind a device"},
		{"rebind", "Rebind", "rebind a device"},
		{"unbind", "Unbind", "unbind a device"},
		{"wifi", "SetWifiSSID", "configure device wifi"},
		{"create-install", "CreateInstall", "create an install record"},
		{"update-install-meta", "UpdateInstallMeta", "update an install record"},
		{"save-baseline", "SaveBaseline", "save a calibration baseline"},
	}
}

func t399EndpointByName(t *testing.T, epName string) t387Ep {
	t.Helper()
	for _, ep := range t387Endpoints() {
		if ep.name == epName {
			return ep
		}
	}
	t.Fatalf("t387Endpoints() 里没有动作名表引用的端点 %q ⇒ 两张表已漂移", epName)
	return t387Ep{}
}

// ── 1. 逐端点：403 文案里的动作名必须是这条端点自己的动作 ──────────────

// TestT399_DenyMessageNamesTheEndpointItWasHitOn 用 ROLE_DOCTOR 逐条打七条写端点，
// 按整串相等判定（不是 Contains）：换成别端点的动作名、改措辞、加后缀，都判红。
func TestT399_DenyMessageNamesTheEndpointItWasHitOn(t *testing.T) {
	want := fmt.Sprintf("role %q may not %%s", roleDoctor)
	for _, act := range t399ExpectedActions() {
		t.Run(act.epName, func(t *testing.T) {
			r, st, _ := t387Env(t)
			ep := t399EndpointByName(t, act.epName)
			path, body := ep.prepare(t, st, false)

			status, msg, code := t387Req(t, r, ep.method, path, roleDoctor, t387DoctorUID, body)

			require.Equal(t, http.StatusForbidden, status, "message=%s", msg)
			assert.Equal(t, model.CodeForbidden, code)
			assert.Equal(t, fmt.Sprintf(want, act.action), msg,
				"越权文案的动作名契约值（E2 反证：把调用点动作名换成别端点的，此前全绿）")
			assert.NotContains(t, msg, act.method, "文案不得泄露将命中的仓储方法名")
		})
	}
}

// ── 2. 动作名两两不同：E2 那种「借别端点的名字」在表层面就先立不住 ──────

func TestT399_ActionNamesArePairwiseDistinct(t *testing.T) {
	acts := t399ExpectedActions()
	seen := map[string]string{}
	for _, a := range acts {
		require.NotEmpty(t, a.action, "端点 %s 的动作名不许留空（留空等于本卡无判据）", a.epName)
		require.NotEmpty(t, a.epName)
		require.NotEmpty(t, a.method)
		if owner, dup := seen[a.action]; dup {
			t.Errorf("动作名 %q 同时挂在端点 %s 与 %s 上 ⇒ 越权文案会把一次拒绝归到别的动作", a.action, owner, a.epName)
		}
		seen[a.action] = a.epName
	}
	assert.Len(t, acts, 7, "设备域收口面是七条写端点，本表条数须与之相等")
}

// ── 3. 三张表互相对齐：动作名表 / 越权现场表 / 路由守卫表不许各说各话 ──

func TestT399_ActionTableCoversExactlyTheGatedRoutes(t *testing.T) {
	gated := map[string]bool{}
	for _, g := range t389GatedWrites {
		gated[g.epName] = true
	}
	require.Len(t, t389GatedWrites, 7, "已收口路由表条数漂移，先回看 T389 N3")

	acts := t399ExpectedActions()
	require.Len(t, acts, len(t389GatedWrites), "动作名表与已收口路由表条数不一致")

	for _, a := range acts {
		assert.True(t, gated[a.epName],
			"动作名表里的 %s 不在 t389GatedWrites ⇒ 要么路由未收口，要么本表多了一条", a.epName)
	}
	actionSet := map[string]bool{}
	for _, a := range acts {
		actionSet[a.epName] = true
	}
	for _, g := range t389GatedWrites {
		assert.True(t, actionSet[g.epName],
			"已收口路由 %s 没有动作名判据 ⇒ 该端点的越权文案处于无锁定状态", g.pattern)
	}

	// 仓储方法名也要与越权现场表一致：反证腿靠的就是这个键。
	for _, a := range acts {
		ep := t399EndpointByName(t, a.epName)
		assert.Equal(t, a.method, ep.write,
			"端点 %s 的仓储方法名与动作名表不一致 ⇒ 反证腿与文案腿守的不是同一个处理器", a.epName)
	}
}
