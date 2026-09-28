// T447：PUT /api/v1/install-records/:id 上 wifi_status 四值的通路用例。
//
// 缺陷原貌：installMetaRequest 里根本没有 wifiStatus 这个键，技师端 PUT 每次都带着它，
// 被 ShouldBindJSON 静默丢弃 ⇒ 「已跳过配网 / 连接失败」两态在现网从未落进 install_records，
// 而库里 CHECK 也只放行两值（Boss 2026-09-28 裁定②甲把值集扩到四值）。
//
// 四条行为线（同一现场，只让 wifiStatus 变动）：
//  1. 枚举内四值 → 200 且逐字落库（大小写敏感，落库值 == 请求值）；
//  2. 集合外取值 → 400（code 20400）且 UpdateInstallMeta 一次都不落 —— 校验排在写库之前，
//     否则脏值穿到 SQL 由 install_records_wifi_status_check 报 23514、被 handler 兜成 500；
//  3. 键省略 / 空串 → 该列不被改写（COALESCE 语义），同一条请求里的 notes 照常写入；
//  4. 判定序：身份门禁排在枚举之前 ⇒ 医护带非法取值仍是 403（本卡不新增「参数非法」可辨面）。
package handler

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// t447WifiBody 只变 wifiStatus 的请求体（notes 恒带：拒绝对要连 notes 一起零写才够硬）
func t447WifiBody(status string) map[string]string {
	return map[string]string{"notes": "T447 回填", "wifiStatus": status}
}

// t447Setup 造一条安装记录现场（FakeStore 建记录时把 wifi_status 一律置成 unconfigured），
// 返回路径与该记录的 installId。
func t447Setup(t *testing.T, st *t387Store) (string, int64) {
	t.Helper()
	id := t387Install(t, st, t387Device)
	return "/api/v1/install-records/" + strconv.FormatInt(id, 10), id
}

func t447Stored(t *testing.T, st *t387Store, id int64) *model.InstallRecord {
	t.Helper()
	rec, err := st.FakeStore.GetInstall(context.Background(), id)
	require.NoError(t, err)
	return rec
}

// ── 1. 枚举内四值：逐个放行且逐字落库 ─────────────────────────────────

func TestT447_UpdateInstallMeta_WifiStatusEnumValues_StoredVerbatim(t *testing.T) {
	for _, st := range []string{
		model.WifiStatusConnected,
		model.WifiStatusUnconfigured,
		model.WifiStatusFailed,
		model.WifiStatusSkipped,
	} {
		t.Run(st, func(t *testing.T) {
			r, store, ls := t387Env(t)
			path, id := t447Setup(t, store)
			before := store.snapshot()

			status, msg, code := t387Req(t, r, http.MethodPut, path, roleTech, t387Tech, t447WifiBody(st))

			require.Equal(t, http.StatusOK, status, "枚举内取值 %q 被拦 = 四态通路没打通：%s", st, msg)
			assert.Equal(t, model.CodeOK, code)
			assert.Equal(t, 1, before.writeDelta(store.snapshot())["UpdateInstallMeta"],
				"应恰好落一次 UpdateInstallMeta")
			assert.Equal(t, st, t447Stored(t, store, id).WifiStatus, "落库值必须与请求逐字相等（大小写敏感）")
			// 新校验不该把 T387 的团队探测面带进来（放行路径仍是原路径）
			assert.Zero(t, ls.deviceTeamCalls, "放行路径不得引入团队探测")
			assert.Zero(t, ls.installTeamCalls, "放行路径不得引入团队探测")
		})
	}
}

// ── 2. 集合外取值：400 且零写 ─────────────────────────────────────────

// CONNECTED / Skipped 是 T446 词形表里实测存在的大小写脏值形状；skip / configuring 是
// 「看着像但不是」的近邻样本（configuring 只存在于患者端 stores/device.ts 自己的四值集，
// 不是库里能存的东西）；带空格的两侧样本守住「不做 TrimSpace 后放行」。
func TestT447_UpdateInstallMeta_WifiStatusOutsideEnum_400WithZeroWrites(t *testing.T) {
	for _, bad := range []string{"CONNECTED", "Connected", "skip", "skipped ", " skipped", "configuring", "已连接", "unknown"} {
		t.Run(bad, func(t *testing.T) {
			r, store, ls := t387Env(t)
			path, id := t447Setup(t, store)
			before := store.snapshot()

			status, msg, code := t387Req(t, r, http.MethodPut, path, roleTech, t387Tech, t447WifiBody(bad))

			require.Equal(t, http.StatusBadRequest, status, "wifiStatus=%q 竟被放行，将穿到 SQL CHECK", bad)
			assert.Equal(t, model.CodeInvalidParam, code)
			assert.Contains(t, msg, "wifiStatus", "文案要点明是哪个字段非法，便于前端定位")
			assert.NotContains(t, msg, "install_records_wifi_status_check", "文案不得泄露库内约束名")

			delta := before.writeDelta(store.snapshot())
			assert.Zero(t, delta["UpdateInstallMeta"], "枚举校验必须排在写库之前：落一次就把脏值写进了库里")
			for k, v := range delta {
				assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
			}
			assert.Equal(t, model.WifiStatusUnconfigured, t447Stored(t, store, id).WifiStatus,
				"库内 wifi_status 不该被脏值请求改动")
			assert.Nil(t, t447Stored(t, store, id).Notes, "同一条请求里的 notes 也不该落库（整体拒绝）")
			assert.Zero(t, ls.deviceTeamCalls, "枚举不合格的请求不该再触达团队判定")
			assert.Zero(t, ls.installTeamCalls, "枚举不合格的请求不该再触达团队判定")
		})
	}
}

// ── 3. 不送该列：wifi_status 保持原值，notes 照常写 ────────────────────

func TestT447_UpdateInstallMeta_OmittedOrEmptyKeyLeavesColumn(t *testing.T) {
	for _, tc := range []struct {
		name string
		body map[string]string
	}{
		{"省略键", map[string]string{"notes": "T447 只改备注"}},
		{"空串", map[string]string{"notes": "T447 只改备注", "wifiStatus": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _ := t387Env(t)
			id := t387Install(t, store, t387Device)
			path := "/api/v1/install-records/" + strconv.FormatInt(id, 10)

			// 先经同一通路写一次 failed，拿到「该列已有非默认值」的现场
			status, msg, _ := t387Req(t, r, http.MethodPut, path, roleTech, t387Tech,
				t447WifiBody(model.WifiStatusFailed))
			require.Equal(t, http.StatusOK, status, "前置写 failed 失败：%s", msg)
			require.Equal(t, model.WifiStatusFailed, t447Stored(t, store, id).WifiStatus)

			before := store.snapshot()
			status, msg, code := t387Req(t, r, http.MethodPut, path, roleTech, t387Tech, tc.body)

			require.Equal(t, http.StatusOK, status, "「%s」不该被当成非法取值：%s", tc.name, msg)
			assert.Equal(t, model.CodeOK, code)
			assert.Equal(t, 1, before.writeDelta(store.snapshot())["UpdateInstallMeta"])

			rec := t447Stored(t, store, id)
			assert.Equal(t, model.WifiStatusFailed, rec.WifiStatus,
				"不送该列时必须保持原值（COALESCE 语义），不能被洗成默认值")
			require.NotNil(t, rec.Notes, "同一条请求里的 notes 应照常写入")
			assert.Equal(t, "T447 只改备注", *rec.Notes)
		})
	}
}

// ── 4. 判定序：身份门禁先于枚举校验 ──────────────────────────────────

func TestT447_UpdateInstallMeta_RoleGatePrecedesEnumCheck(t *testing.T) {
	for _, role := range t387DeniedRoles() {
		label := deniedLabel(role)
		t.Run(label+"/枚举内", func(t *testing.T) {
			r, store, _ := t387Env(t)
			path, _ := t447Setup(t, store)
			before := store.snapshot()

			status, msg, code := t387Req(t, r, http.MethodPut, path, role, t387DoctorUID,
				t447WifiBody(model.WifiStatusSkipped))

			assert.Equal(t, http.StatusForbidden, status, "message=%s", msg)
			assert.Equal(t, model.CodeForbidden, code)
			for k, v := range before.writeDelta(store.snapshot()) {
				assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
			}
		})
		// 同一身份带非法取值：必须仍是 403，不能因为新增的参数校验提前判 400，
		// 让「这个号不存在」与「这个号不是我能改的」之外的第三种形状泄露出来。
		t.Run(label+"/集合外", func(t *testing.T) {
			r, store, _ := t387Env(t)
			path, _ := t447Setup(t, store)
			before := store.snapshot()

			status, msg, code := t387Req(t, r, http.MethodPut, path, role, t387DoctorUID,
				t447WifiBody("CONNECTED"))

			assert.Equal(t, http.StatusForbidden, status, "越权身份带脏值不该收到参数级 400：message=%s", msg)
			assert.Equal(t, model.CodeForbidden, code)
			for k, v := range before.writeDelta(store.snapshot()) {
				assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
			}
		})
	}
}

// ── 5. 反证：本角色照常成功（同族 T387 判据二，只带 notes 不带 wifiStatus）──

func TestT447_UpdateInstallMeta_NotesOnlyStillWorksForWriterRoles(t *testing.T) {
	ids := map[string]string{roleTech: t387Tech, roleAdmin: t387AdminUID}
	for _, role := range []string{roleTech, roleAdmin} {
		t.Run(role, func(t *testing.T) {
			r, store, _ := t387Env(t)
			path, _ := t447Setup(t, store)
			before := store.snapshot()

			status, msg, code := t387Req(t, r, http.MethodPut, path, role, ids[role],
				map[string]string{"notes": "T447 反证：只回填备注"})

			require.Equal(t, http.StatusOK, status, "反证失败：%s 以 %s 应照常成功，message=%s", "update-install-meta", role, msg)
			assert.Equal(t, model.CodeOK, code)
			assert.Equal(t, 1, before.writeDelta(store.snapshot())["UpdateInstallMeta"])
		})
	}
}
