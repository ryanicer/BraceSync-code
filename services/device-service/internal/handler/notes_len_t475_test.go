// T475：PUT /api/v1/install-records/:id 上 notes 的 200 字符上限（T446 待裁第 1 项 Go 腿，PM 2026-09-29 甲案）。
//
// 缺陷原貌：上限只活在小程序输入框的 maxlength=200（apps/tech-miniapp/src/pages/install/index.vue:181），
// 后端 installMetaRequest 零长度判定、库列 install_records.notes 是无上限 TEXT（000001:127）
// ⇒ 绕开小程序直打 API 就能落任意长度备注，而产品现行为明明写着 200。
// 裁定不动 schema、不动前端 ⇒ 判定补在 handler 入口，写法沿用 alert-service 的 maxProcessNote。
//
// 四条行为线（同一现场，只让 notes 变动）：
//  1. 恰好 200 字符 → 200 且逐字落库；中文必须按字符计（200 汉字 = 600 字节，仍要放行），
//     否则就是把 len() 的字节数误当字符数，把产品允许的正常输入判红；
//  2. 201 字符 → 400（code 20400）且 UpdateInstallMeta 一次都不落 —— 校验排在写库之前；
//     同一条请求里带着的 wifiStatus 也要一起不落（整体拒绝，不是丢一列留一列）；
//  3. 键省略 / 空串 → 放行且该列不被改写（可选字段语义不变，不能被新判定顺手打断）；
//  4. 判定序：身份门禁排在长度校验之前 ⇒ 医护/客服带超长备注仍是 403，
//     本卡不新增「参数非法」这一层可辨面。
package handler

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// t475Notes 造一条 n 个字符的备注（中英混杂按 rune 数生成，别用字节数）
func t475Notes(runes int) string {
	var b strings.Builder
	pieces := []string{"装", "a", "具", "b"}
	for i := 0; i < runes; i++ {
		b.WriteString(pieces[i%len(pieces)])
	}
	return b.String()
}

// t475Path 建现场并返回该安装记录的 PUT 路径
func t475Path(t *testing.T, st *t387Store) (string, int64) {
	t.Helper()
	id := t387Install(t, st, t387Device)
	return "/api/v1/install-records/" + strconv.FormatInt(id, 10), id
}

// ── 1. 恰好到线：放行且逐字落库 ───────────────────────────────────────

func TestT475_UpdateInstallMeta_NotesAtLimit_AcceptedVerbatim(t *testing.T) {
	for _, tc := range []struct {
		name  string
		notes string
	}{
		{"ASCII 200 字符", strings.Repeat("a", installNotesMaxRunes)},
		// 200 个汉字是 600 字节：按 len() 判会误拒，这一格守的就是「按字符计」这个口径
		{"中文 200 字符（600 字节）", strings.Repeat("压", installNotesMaxRunes)},
		{"中英混杂 200 字符", t475Notes(installNotesMaxRunes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, installNotesMaxRunes, utf8.RuneCountInString(tc.notes),
				"用例本身要走字符口径，先自证样本 == %d 字符", installNotesMaxRunes)

			r, store, _ := t387Env(t)
			path, id := t475Path(t, store)
			before := store.snapshot()

			status, msg, code := t387Req(t, r, http.MethodPut, path, roleTech, t387Tech,
				map[string]string{"notes": tc.notes})

			require.Equal(t, http.StatusOK, status, "恰好 %d 字符被拦 = 上限判到了线内：%s", installNotesMaxRunes, msg)
			assert.Equal(t, model.CodeOK, code)
			assert.Equal(t, 1, before.writeDelta(store.snapshot())["UpdateInstallMeta"])
			stored := t447Stored(t, store, id)
			require.NotNil(t, stored.Notes, "放行路径必须真把备注落下去")
			assert.Equal(t, tc.notes, *stored.Notes, "落库值要与请求逐字相等")
		})
	}
}

// ── 2. 超一线：400 且零写 ─────────────────────────────────────────────

func TestT475_UpdateInstallMeta_NotesOverLimit_400WithZeroWrites(t *testing.T) {
	for _, tc := range []struct {
		name  string
		notes string
	}{
		{"ASCII 201 字符", strings.Repeat("a", installNotesMaxRunes+1)},
		{"中文 201 字符", strings.Repeat("压", installNotesMaxRunes+1)},
		{"混杂 201 字符", t475Notes(installNotesMaxRunes + 1)},
		{"远超（2000 字符）", t475Notes(2000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Greater(t, utf8.RuneCountInString(tc.notes), installNotesMaxRunes,
				"样本必须真的过线，否则这一格是空跑")

			r, store, ls := t387Env(t)
			path, id := t475Path(t, store)
			before := store.snapshot()
			logs := t464CaptureLogs(t) // T464：字段名等技术细节走日志通道

			// 同一条请求带一个合法 wifiStatus：拒绝对要连它一起零写，否则「丢备注留状态」会把一次操作劈成半条
			status, msg, code := t387Req(t, r, http.MethodPut, path, roleTech, t387Tech,
				map[string]string{"notes": tc.notes, "wifiStatus": model.WifiStatusSkipped})

			require.Equal(t, http.StatusBadRequest, status, "notes 长度 %d 字符竟被放行", utf8.RuneCountInString(tc.notes))
			assert.Equal(t, model.CodeInvalidParam, code)
			// 双通道：响应体只剩中文短句，"哪个字段超限 + 超限多少" 落在同请求的日志行里
			assert.Equal(t, model.UserText(model.CodeInvalidParam), msg)
			assert.True(t, t464AnyLogContains(logs, "notes"),
				"字段名必须仍可从日志反查（技术文本被丢掉而不是改道，同样判红）")
			assert.NotContains(t, msg, "install_records", "文案不得泄露库表名")

			delta := before.writeDelta(store.snapshot())
			assert.Zero(t, delta["UpdateInstallMeta"], "长度校验必须排在写库之前：落一次就写进了无上限的 TEXT 列")
			for k, v := range delta {
				assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
			}
			rec := t447Stored(t, store, id)
			assert.Nil(t, rec.Notes, "被拒的备注不该落库")
			assert.Equal(t, model.WifiStatusUnconfigured, rec.WifiStatus,
				"同一条请求里的 wifiStatus 也要整体拒绝，不能被劈成半条写入")
			assert.Zero(t, ls.deviceTeamCalls, "长度不合格的请求不该再触达团队判定")
			assert.Zero(t, ls.installTeamCalls, "长度不合格的请求不该再触达团队判定")
		})
	}
}

// ── 3. 不送该列：可选字段语义不变 ─────────────────────────────────────

func TestT475_UpdateInstallMeta_OmittedOrEmptyNotes_StillAccepted(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      map[string]string
		wantNotes string
		unchanged bool // true = 本条请求不送 notes，该列必须保持原值（COALESCE 语义）
	}{
		{
			name: "省略 notes 键", body: map[string]string{"wifiStatus": model.WifiStatusSkipped},
			wantNotes: "T475 前置备注", unchanged: true,
		},
		{
			name: "notes 空串", body: map[string]string{"notes": "", "wifiStatus": model.WifiStatusSkipped},
			wantNotes: "T475 前置备注", unchanged: true,
		},
		{
			name: "短备注照常写", body: map[string]string{"notes": "T475 短备注"},
			wantNotes: "T475 短备注",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _ := t387Env(t)
			path, id := t475Path(t, store)

			// 先经同一通路写一次非默认备注，拿到「该列已有值」的现场
			status, msg, _ := t387Req(t, r, http.MethodPut, path, roleTech, t387Tech,
				map[string]string{"notes": "T475 前置备注"})
			require.Equal(t, http.StatusOK, status, "前置写备注失败：%s", msg)
			require.Equal(t, "T475 前置备注", *t447Stored(t, store, id).Notes)

			before := store.snapshot()
			status, msg, code := t387Req(t, r, http.MethodPut, path, roleTech, t387Tech, tc.body)

			require.Equal(t, http.StatusOK, status, "「%s」不该被新判定拦掉：%s", tc.name, msg)
			assert.Equal(t, model.CodeOK, code)
			assert.Equal(t, 1, before.writeDelta(store.snapshot())["UpdateInstallMeta"])

			rec := t447Stored(t, store, id)
			require.NotNil(t, rec.Notes)
			assert.Equal(t, tc.wantNotes, *rec.Notes,
				"不送该列时必须保持原值（COALESCE 语义），不能被新判定顺手洗成空")
			if tc.unchanged {
				assert.Equal(t, model.WifiStatusSkipped, rec.WifiStatus, "同一请求里的 wifiStatus 应照常写")
			}
		})
	}
}

// ── 4. 判定序：身份门禁先于长度校验 ──────────────────────────────────

func TestT475_UpdateInstallMeta_RoleGatePrecedesLengthCheck(t *testing.T) {
	long := t475Notes(installNotesMaxRunes + 1)
	for _, role := range t387DeniedRoles() {
		label := deniedLabel(role)
		t.Run(label+"/超长备注", func(t *testing.T) {
			r, store, _ := t387Env(t)
			path, _ := t475Path(t, store)
			before := store.snapshot()

			status, msg, code := t387Req(t, r, http.MethodPut, path, role, t387DoctorUID,
				map[string]string{"notes": long})

			assert.Equal(t, http.StatusForbidden, status,
				"越权身份带超长备注必须仍是 403，不能因新加的参数校验提前判 400：message=%s", msg)
			assert.Equal(t, model.CodeForbidden, code)
			for k, v := range before.writeDelta(store.snapshot()) {
				assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
			}
		})
	}
}
