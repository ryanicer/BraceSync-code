// T448：清除设备 WiFi 的审计留痕通路（Boss 2026-09-28 裁定③甲「只加审计，不扩枚举」）。
//
// 现场口径（T446 §二 已定性）：清除是纯 BLE 动作（B513 写 0x02、等 B512 notify=0），
// 「没有 HTTP 端点」是正确形态、不是漏做；真缺口是它零留痕。卡面三条约束决定了落点形状：
// 不新增写端点 ⇒ 复用技师端已经在打的 POST /devices/:deviceId/wifi；
// 不扩枚举 ⇒ cleared 支只写 audit_logs，devices.wifi_ssid 与 install_records.wifi_status 一字不动
// （「清除后云端状态陈旧」是同一缺口里的另一半，卡面明列范围外）；
// 不动 BLE 链路 ⇒ 服务端只做被动接收，判定序仍是身份门禁在最前（T387 口径）。
//
// 六条行为线：
//  1. cleared=true → 200，恰好一次留痕，且两个状态列零改写；
//  2. 只带 ssid → 原配网回写照旧（本卡不许把它改坏），且不产生留痕；
//  3. ssid 与 cleared 同现 → 400 且零写（两种语义不许混在一次请求里）；
//  4. 设备不存在 → 404 且零留痕（不给脏号编造审计行的机会）；
//  5. 身份门禁形状不变：医护 / 患者仍是 403 同一句话，且拒绝路径一格不落；
//  6. 管理员放行（与技师同侧，T387 的 allow-list 不因新支变化）。
package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

const t448Ssid = "BraceHome-5G"

func t448WifiPath(deviceID string) string { return "/api/v1/devices/" + deviceID + "/wifi" }

// t448ClearedBody 清除留痕的上报体：只有 cleared 一个键，ssid 缺席。
func t448ClearedBody() map[string]any { return map[string]any{"cleared": true} }

// t448PreProvisioned 造「配网已成功、即将清除」的现场：设备 + 安装记录 + 一次 ssid 回写。
// 返回 installId，并确认那两列已是 connected（清除支必须一个都不改）。
func t448PreProvisioned(t *testing.T, r *gin.Engine, st *t387Store) int64 {
	t.Helper()
	id := t387Install(t, st, t387Device)
	status, msg, _ := t387Req(t, r, http.MethodPost, t448WifiPath(t387Device), roleTech, t387Tech,
		map[string]string{"ssid": t448Ssid})
	require.Equal(t, http.StatusOK, status, "前置的配网回写必须成功，否则现场不成立：%s", msg)
	dev, err := st.FakeStore.GetDevice(context.Background(), t387Device)
	require.NoError(t, err)
	require.NotNil(t, dev.WifiSSID)
	require.Equal(t, t448Ssid, *dev.WifiSSID)
	return id
}

// ── 1. cleared=true：只留痕，两个状态列一字不动 ───────────────────────

func TestT448_WifiClearReport_AuditsOnlyAndLeavesStateColumns(t *testing.T) {
	r, st, ls := t387Env(t)
	installID := t448PreProvisioned(t, r, st)
	before := st.snapshot()

	status, msg, code := t387Req(t, r, http.MethodPost, t448WifiPath(t387Device), roleTech, t387Tech, t448ClearedBody())

	require.Equal(t, http.StatusOK, status, "清除留痕被拒 = 本卡通路没打通：%s", msg)
	assert.Equal(t, model.CodeOK, code)

	delta := before.writeDelta(st.snapshot())
	assert.Equal(t, 1, delta["WriteWifiClearAudit"], "应恰好落一次留痕")
	assert.Zero(t, delta["SetWifiSSID"], "清除支不得改写 devices.wifi_ssid")
	assert.Zero(t, delta["UpdateInstallMeta"], "清除支不得改写安装记录")

	rows := st.FakeStore.WifiClearAudits()
	require.Len(t, rows, 1)
	assert.Equal(t, t387Device, rows[0].DeviceID, "留痕必须带设备标识")
	assert.Equal(t, t387Tech, rows[0].OperatorID, "留痕必须带操作人")
	assert.Equal(t, roleTech, rows[0].OperatorRole)

	dev, err := st.FakeStore.GetDevice(context.Background(), t387Device)
	require.NoError(t, err)
	require.NotNil(t, dev.WifiSSID, "清除后 ssid 仍是配网时那个值（状态同步是卡面范围外项，不许顺手改）")
	assert.Equal(t, t448Ssid, *dev.WifiSSID)
	rec, err := st.FakeStore.GetInstall(context.Background(), installID)
	require.NoError(t, err)
	assert.Equal(t, model.WifiStatusConnected, rec.WifiStatus, "wifi_status 仍是 connected，本卡不引入状态回写")

	assert.Zero(t, ls.deviceTeamCalls, "放行路径不得引入团队探测")
	assert.Zero(t, ls.installTeamCalls, "放行路径不得引入团队探测")
}

// ── 2. 只带 ssid：既有配网回写照旧，且不产生留痕 ─────────────────────

func TestT448_WifiSsidWriteback_KeepsOldShapeAndDoesNotAudit(t *testing.T) {
	r, st, _ := t387Env(t)
	installID := t387Install(t, st, t387Device)
	before := st.snapshot()

	status, msg, code := t387Req(t, r, http.MethodPost, t448WifiPath(t387Device), roleTech, t387Tech,
		map[string]string{"ssid": t448Ssid})

	require.Equal(t, http.StatusOK, status, "配网回写被本卡改坏：%s", msg)
	assert.Equal(t, model.CodeOK, code)
	delta := before.writeDelta(st.snapshot())
	assert.Equal(t, 1, delta["SetWifiSSID"])
	assert.Zero(t, delta["WriteWifiClearAudit"], "配网回写不是清除，不许落留痕")
	assert.Empty(t, st.FakeStore.WifiClearAudits())

	rec, err := st.FakeStore.GetInstall(context.Background(), installID)
	require.NoError(t, err)
	assert.Equal(t, model.WifiStatusConnected, rec.WifiStatus, "connected 回写照旧（T447 那条唯一通路）")
}

// ── 3. ssid 与 cleared 同现：400 且零写 ──────────────────────────────

func TestT448_WifiClearReport_RejectsBodyMixingSsidAndCleared(t *testing.T) {
	r, st, _ := t387Env(t)
	_ = t387Install(t, st, t387Device)
	before := st.snapshot()

	status, msg, code := t387Req(t, r, http.MethodPost, t448WifiPath(t387Device), roleTech, t387Tech,
		map[string]any{"ssid": t448Ssid, "cleared": true})

	assert.Equal(t, http.StatusBadRequest, status, "两种语义混在一次请求里必须拒：%s", msg)
	assert.Equal(t, model.CodeInvalidParam, code)
	delta := before.writeDelta(st.snapshot())
	assert.Zero(t, delta["WriteWifiClearAudit"])
	assert.Zero(t, delta["SetWifiSSID"])
}

// ── 4. 设备不存在：404 且零留痕 ──────────────────────────────────────

func TestT448_WifiClearReport_UnknownDevice_404WithNoAudit(t *testing.T) {
	r, st, _ := t387Env(t)
	before := st.snapshot()

	status, msg, code := t387Req(t, r, http.MethodPost, t448WifiPath(t387DeviceNo), roleTech, t387Tech, t448ClearedBody())

	assert.Equal(t, http.StatusNotFound, status, "查无此设备仍按 404（不新增「是否存在」可辨面）：%s", msg)
	assert.Equal(t, model.CodeNotFound, code)
	assert.Zero(t, before.writeDelta(st.snapshot())["WriteWifiClearAudit"], "脏设备号不得编造审计行")
	assert.Empty(t, st.FakeStore.WifiClearAudits())
}

// ── 5. 身份门禁形状不变：拒绝路径一格不落 ────────────────────────────

func TestT448_WifiClearReport_RoleGateMessageAndZeroWritesUnchanged(t *testing.T) {
	for _, tc := range []struct{ name, role string }{
		{"医护", roleDoctor},
		{"患者", rolePatient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, st, _ := t387Env(t)
			_ = t387Install(t, st, t387Device)
			before := st.snapshot()
			logs := t464CaptureLogs(t)

			status, msg, code := t387Req(t, r, http.MethodPost, t448WifiPath(t387Device), tc.role, t387Tech, t448ClearedBody())

			assert.Equal(t, http.StatusForbidden, status)
			assert.Equal(t, model.CodeForbidden, code)
			// T399 钉的是整句相等：cleared 支不得改动作名，也不得多出一句可辨文案。
			// T464 后这句英文在日志行里比对，响应体给中文（新支与老支同句 ⇒ 用户面不可辨）。
			assert.Equal(t, `role "`+tc.role+`" may not configure device wifi`, t464TechnicalLog(t, logs))
			assert.Equal(t, model.UserText(model.CodeForbidden), msg)
			delta := before.writeDelta(st.snapshot())
			assert.Zero(t, delta["WriteWifiClearAudit"])
			assert.Zero(t, delta["SetWifiSSID"])
			assert.Empty(t, st.FakeStore.WifiClearAudits())
		})
	}
}

// ── 6. 管理员与技师同侧放行（T387 的 allow-list 不因新支变化）────────

func TestT448_WifiClearReport_AdminRoleAlsoPasses(t *testing.T) {
	r, st, _ := t387Env(t)
	_ = t387Install(t, st, t387Device)
	before := st.snapshot()

	status, msg, _ := t387Req(t, r, http.MethodPost, t448WifiPath(t387Device), roleAdmin, "ADMIN-T448", t448ClearedBody())

	require.Equal(t, http.StatusOK, status, "管理员在本域写侧本就放行：%s", msg)
	assert.Equal(t, 1, before.writeDelta(st.snapshot())["WriteWifiClearAudit"])
	require.Len(t, st.FakeStore.WifiClearAudits(), 1)
	assert.Equal(t, roleAdmin, st.FakeStore.WifiClearAudits()[0].OperatorRole)
	assert.Equal(t, "ADMIN-T448", st.FakeStore.WifiClearAudits()[0].OperatorID)
}
