// T476：install_records.notes 的 200 字符上限补另两条写入通路（POST 创建 + baseline 回填）。
//
// 缺陷原貌：T475 把判定接在 PUT /api/v1/install-records/:id 的入口上，但同一个字符串还有两条
// 通路能到同两个库写点（repo.go:477 INSERT / repo.go:585 UPDATE COALESCE）——
//  1. POST /api/v1/install-records：installRequest.Notes 直接进 svc.CreateInstall；
//  2. POST /api/v1/baselines：baselineRequest.Notes 在 svc.SaveBaseline 之后直接进
//     svc.UpdateInstallMeta，不经 PUT handler。
//
// 两条都是请求体绑定来的用户输入（不是服务端固定文案），所以「PUT 有上限」在现网等于没有上限。
//
// 判据沿用 T475 四条线，各通路逐条对齐：
//  1. 边界内（恰好 200 字符，含中文按字符计）→ 放行且逐字落库；
//  2. 超界（201 起）→ 400 code 20400，且拒绝路径零写；
//     baseline 通路要额外证「基线行也没落」——只拒备注会留下基线已写、备注被丢的半条结果；
//  3. 键省略 / 空串 → 照常放行，可选字段语义不被新判定打断；
//  4. 判定序：身份门禁先于长度校验 ⇒ 医护/客服/患者带超长备注仍是 403。
//
// 双通道（T464）：响应体只剩中文短句，"哪个字段超限 + 超限多少" 走日志行。
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

const (
	t476DevCreateOK   = "DEV-T476-CREATE-OK"
	t476DevCreateLong = "DEV-T476-CREATE-LONG"
	t476DevCreateOpt  = "DEV-T476-CREATE-OPT"
	t476DevCreateDeny = "DEV-T476-CREATE-DENY"
)

// t476Bind 为一个新设备建好「已注册 + 已绑定 + 患者/技师存在」的现场。
// POST 创建通路每次成功一次就往库里加一条安装记录，所以每条用例用各自的设备号，
// 免得回读时分不清读到的是哪一条。
func t476Bind(t *testing.T, st *t387Store, deviceID string) {
	t.Helper()
	t387Register(t, st, deviceID)
	st.AddPatient(t387Patient)
	st.AddTech(t387Tech)
	_, err := st.Bind(context.Background(), repo.BindParams{
		DeviceID: deviceID, PatientID: t387Patient, OperatorID: t387Tech,
	})
	require.NoError(t, err)
}

// t476Req 同 t387Req，但把响应 data 一起带回来 —— 创建通路要从响应里取 installId 才能回读落库值，
// 而 t387Req 只给 status + code + message。
func t476Req(t *testing.T, r http.Handler, method, path, role, uid string, body any) (
	int, string, int, map[string]any,
) {
	t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if role != "" {
		req.Header.Set("X-Role", role)
	}
	if uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	}
	return w.Code, resp.Message, resp.Code, resp.Data
}

// t476InstallID 从 POST /install-records 的响应里取新记录号
func t476InstallID(t *testing.T, data map[string]any) int64 {
	t.Helper()
	raw, _ := data["installId"].(string)
	require.NotEmpty(t, raw, "创建通路没回 installId，无法回读落库值：%+v", data)
	id, err := strconv.ParseInt(raw, 10, 64)
	require.NoError(t, err)
	return id
}

// ── 1. POST /api/v1/install-records：边界内 ───────────────────────────

func TestT476_CreateInstall_NotesAtLimit_AcceptedVerbatim(t *testing.T) {
	for _, tc := range []struct {
		name  string
		notes string
	}{
		{"ASCII 200 字符", strings.Repeat("a", installNotesMaxRunes)},
		{"中文 200 字符（600 字节）", strings.Repeat("压", installNotesMaxRunes)},
		{"中英混杂 200 字符", t475Notes(installNotesMaxRunes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, installNotesMaxRunes, utf8.RuneCountInString(tc.notes),
				"样本必须走字符口径，先自证 == %d 字符", installNotesMaxRunes)

			r, store, _ := t387Env(t)
			t476Bind(t, store, t476DevCreateOK)
			before := store.snapshot()

			status, msg, code, data := t476Req(t, r, http.MethodPost, "/api/v1/install-records",
				roleTech, t387Tech, map[string]string{
					"deviceId": t476DevCreateOK, "patientId": t387Patient, "techId": t387Tech, "notes": tc.notes,
				})

			require.Equal(t, http.StatusOK, status, "恰好 %d 字符被拦 = 上限判到了线内：%s", installNotesMaxRunes, msg)
			assert.Equal(t, model.CodeOK, code)
			assert.Equal(t, 1, before.writeDelta(store.snapshot())["CreateInstall"])

			stored := t447Stored(t, store, t476InstallID(t, data))
			require.NotNil(t, stored.Notes, "放行路径必须真把备注落下去")
			assert.Equal(t, tc.notes, *stored.Notes, "落库值要与请求逐字相等")
		})
	}
}

// ── 2. POST /api/v1/install-records：超界必拒且零写 ───────────────────

func TestT476_CreateInstall_NotesOverLimit_400WithZeroWrites(t *testing.T) {
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
			t476Bind(t, store, t476DevCreateLong)
			before := store.snapshot()
			logs := t464CaptureLogs(t)

			// 同一条请求带一个合法 signatureUrl：拒绝对要连它一起零写，
			// 否则「丢备注留签名」会把一次安装劈成半条记录。
			status, msg, code, _ := t476Req(t, r, http.MethodPost, "/api/v1/install-records",
				roleTech, t387Tech, map[string]string{
					"deviceId": t476DevCreateLong, "patientId": t387Patient, "techId": t387Tech,
					"notes": tc.notes, "signatureUrl": "cos://sig/t476.png",
				})

			require.Equal(t, http.StatusBadRequest, status, "notes 长度 %d 字符竟被放行", utf8.RuneCountInString(tc.notes))
			assert.Equal(t, model.CodeInvalidParam, code)
			assert.Equal(t, model.UserText(model.CodeInvalidParam), msg)
			assert.True(t, t464AnyLogContains(logs, "notes"),
				"字段名必须仍可从日志反查（技术文本被丢掉而不是改道，同样判红）")
			assert.NotContains(t, msg, "install_records", "文案不得泄露库表名")

			delta := before.writeDelta(store.snapshot())
			assert.Zero(t, delta["CreateInstall"], "长度校验必须排在写库之前：落一次就写进了无上限的 TEXT 列")
			for k, v := range delta {
				assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
			}
			assert.Zero(t, ls.deviceTeamCalls, "长度不合格的请求不该再触达团队判定")
			assert.Zero(t, ls.installTeamCalls, "长度不合格的请求不该再触达团队判定")
		})
	}
}

// ── 3. POST /api/v1/install-records：可选字段语义不变 ─────────────────

func TestT476_CreateInstall_OmittedOrEmptyNotes_StillAccepted(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bodyExtra map[string]string
		wantNil   bool // true = 本条请求不送 notes，库里该列必须仍是 NULL
	}{
		{name: "省略 notes 键", bodyExtra: nil, wantNil: true},
		{name: "notes 空串", bodyExtra: map[string]string{"notes": ""}, wantNil: true},
		{name: "短备注照常写", bodyExtra: map[string]string{"notes": "T476 短备注"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _ := t387Env(t)
			t476Bind(t, store, t476DevCreateOpt)
			before := store.snapshot()

			body := map[string]string{
				"deviceId": t476DevCreateOpt, "patientId": t387Patient, "techId": t387Tech,
			}
			for k, v := range tc.bodyExtra {
				body[k] = v
			}
			status, msg, code, data := t476Req(t, r, http.MethodPost, "/api/v1/install-records",
				roleTech, t387Tech, body)

			require.Equal(t, http.StatusOK, status, "「%s」不该被新判定拦掉：%s", tc.name, msg)
			assert.Equal(t, model.CodeOK, code)
			assert.Equal(t, 1, before.writeDelta(store.snapshot())["CreateInstall"])

			stored := t447Stored(t, store, t476InstallID(t, data))
			if tc.wantNil {
				assert.Nil(t, stored.Notes, "不送该列时库里应保持 NULL，而不是被顺手写成空串")
			} else {
				require.NotNil(t, stored.Notes)
				assert.Equal(t, "T476 短备注", *stored.Notes)
			}
		})
	}
}

// ── 4. POST /api/v1/baselines：边界内（基线与备注都落）────────────────

func TestT476_SaveBaseline_NotesAtLimit_AcceptedVerbatim(t *testing.T) {
	for _, tc := range []struct {
		name  string
		notes string
	}{
		{"ASCII 200 字符", strings.Repeat("a", installNotesMaxRunes)},
		{"中文 200 字符（600 字节）", strings.Repeat("压", installNotesMaxRunes)},
		{"中英混杂 200 字符", t475Notes(installNotesMaxRunes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, installNotesMaxRunes, utf8.RuneCountInString(tc.notes),
				"样本必须走字符口径，先自证 == %d 字符", installNotesMaxRunes)

			r, store, _ := t387Env(t)
			id := t387Install(t, store, t387Device)
			before := store.snapshot()

			status, msg, code, _ := t476Req(t, r, http.MethodPost, "/api/v1/baselines",
				roleTech, t387Tech, map[string]any{
					"installId": strconv.FormatInt(id, 10), "offsetValues": t387Offsets(), "notes": tc.notes,
				})

			require.Equal(t, http.StatusOK, status, "恰好 %d 字符被拦 = 上限判到了线内：%s", installNotesMaxRunes, msg)
			assert.Equal(t, model.CodeOK, code)
			delta := before.writeDelta(store.snapshot())
			assert.Equal(t, 1, delta["SaveBaseline"], "放行路径基线要真落")
			assert.Equal(t, 1, delta["UpdateInstallMeta"], "放行路径备注要真回填")
			assert.Equal(t, tc.notes, *t447Stored(t, store, id).Notes, "落库值要与请求逐字相等")
		})
	}
}

// ── 5. POST /api/v1/baselines：超界必拒，且基线行也不能留 ─────────────

func TestT476_SaveBaseline_NotesOverLimit_400WithZeroWrites(t *testing.T) {
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
			id := t387Install(t, store, t387Device)
			before := store.snapshot()
			logs := t464CaptureLogs(t)

			status, msg, code, _ := t476Req(t, r, http.MethodPost, "/api/v1/baselines",
				roleTech, t387Tech, map[string]any{
					"installId": strconv.FormatInt(id, 10), "offsetValues": t387Offsets(),
					"notes": tc.notes, "signatureUrl": "cos://sig/t476.png",
				})

			require.Equal(t, http.StatusBadRequest, status, "notes 长度 %d 字符竟被放行", utf8.RuneCountInString(tc.notes))
			assert.Equal(t, model.CodeInvalidParam, code)
			assert.Equal(t, model.UserText(model.CodeInvalidParam), msg)
			assert.True(t, t464AnyLogContains(logs, "notes"),
				"字段名必须仍可从日志反查（技术文本被丢掉而不是改道，同样判红）")
			assert.NotContains(t, msg, "install_records", "文案不得泄露库表名")

			delta := before.writeDelta(store.snapshot())
			// 这一格是本通路的要害：基线写在备注回填之前，判定排在后面就会留下
			// 「基线已落、备注被丢」的半条结果，而客户端收到的是 400。
			assert.Zero(t, delta["SaveBaseline"], "判定必须排在 SaveBaseline 之前：半条结果没法回滚")
			assert.Zero(t, delta["UpdateInstallMeta"], "长度校验必须排在写库之前")
			for k, v := range delta {
				assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
			}
			rec := t447Stored(t, store, id)
			assert.Nil(t, rec.Notes, "被拒的备注不该落库")
			assert.Nil(t, rec.BaselineID, "被拒的请求也不该留下基线行")
			assert.Zero(t, ls.deviceTeamCalls, "长度不合格的请求不该再触达团队判定")
			assert.Zero(t, ls.installTeamCalls, "长度不合格的请求不该再触达团队判定")
		})
	}
}

// ── 6. POST /api/v1/baselines：不送 notes 时校准主写不受影响 ──────────

func TestT476_SaveBaseline_OmittedOrEmptyNotes_StillAccepted(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bodyExtra map[string]string
		wantNil   bool
	}{
		{name: "省略 notes 键", bodyExtra: nil, wantNil: true},
		{name: "notes 空串", bodyExtra: map[string]string{"notes": ""}, wantNil: true},
		{name: "短备注照常回填", bodyExtra: map[string]string{"notes": "T476 校准备注"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _ := t387Env(t)
			id := t387Install(t, store, t387Device)
			before := store.snapshot()

			body := map[string]any{
				"installId": strconv.FormatInt(id, 10), "offsetValues": t387Offsets(),
			}
			for k, v := range tc.bodyExtra {
				body[k] = v
			}
			status, msg, code, _ := t476Req(t, r, http.MethodPost, "/api/v1/baselines",
				roleTech, t387Tech, body)

			require.Equal(t, http.StatusOK, status, "「%s」不该被新判定拦掉：%s", tc.name, msg)
			assert.Equal(t, model.CodeOK, code)
			delta := before.writeDelta(store.snapshot())
			assert.Equal(t, 1, delta["SaveBaseline"], "校准主写不该因备注键缺失被挡")

			rec := t447Stored(t, store, id)
			if tc.wantNil {
				assert.Nil(t, rec.Notes, "不送该列时库里应保持原值（COALESCE 语义），基线通路同样不例外")
				assert.Zero(t, delta["UpdateInstallMeta"], "notes 与 signatureUrl 都没送时不该触发回填")
			} else {
				require.NotNil(t, rec.Notes)
				assert.Equal(t, "T476 校准备注", *rec.Notes)
				assert.Equal(t, 1, delta["UpdateInstallMeta"], "非空备注应照常回填")
			}
		})
	}
}

// ── 7. 判定序：身份门禁先于长度校验（两条通路各验一遍）────────────────

func TestT476_BothPaths_RoleGatePrecedesLengthCheck(t *testing.T) {
	long := t475Notes(installNotesMaxRunes + 1)
	for _, role := range t387DeniedRoles() {
		label := deniedLabel(role)

		t.Run("create-install/"+label+"/超长备注", func(t *testing.T) {
			r, store, _ := t387Env(t)
			t476Bind(t, store, t476DevCreateDeny)
			before := store.snapshot()

			status, msg, code, _ := t476Req(t, r, http.MethodPost, "/api/v1/install-records",
				role, t387DoctorUID, map[string]string{
					"deviceId": t476DevCreateDeny, "patientId": t387Patient, "techId": t387Tech, "notes": long,
				})

			assert.Equal(t, http.StatusForbidden, status,
				"越权身份带超长备注必须仍是 403，不能因新加的参数校验提前判 400：message=%s", msg)
			assert.Equal(t, model.CodeForbidden, code)
			for k, v := range before.writeDelta(store.snapshot()) {
				assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
			}
		})

		t.Run("save-baseline/"+label+"/超长备注", func(t *testing.T) {
			r, store, _ := t387Env(t)
			id := t387Install(t, store, t387Device)
			before := store.snapshot()

			status, msg, code, _ := t476Req(t, r, http.MethodPost, "/api/v1/baselines",
				role, t387DoctorUID, map[string]any{
					"installId": strconv.FormatInt(id, 10), "offsetValues": t387Offsets(), "notes": long,
				})

			assert.Equal(t, http.StatusForbidden, status,
				"越权身份带超长备注必须仍是 403，不能因新加的参数校验提前判 400：message=%s", msg)
			assert.Equal(t, model.CodeForbidden, code)
			for k, v := range before.writeDelta(store.snapshot()) {
				assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
			}
		})
	}
}

// ── 8. 判定源本身：三条通路共用一个函数，锁它的边界与放行条件 ─────────

func TestT476_CheckInstallNotes_IsTheSingleSharedBoundary(t *testing.T) {
	// T476 的缺陷正是「同一个判定在另一个入口没接上」。这里直接锁判定函数本体：
	// 三格口径（线内/线上/线外）+ 空串放行，任何一条通路换了写法都会在这一格判红。
	for _, tc := range []struct {
		name      string
		notes     string
		wantPass  bool
		wantRunes int
	}{
		{"空串 = 不送该列", "", true, 0},
		{"1 字符", "装", true, 1},
		{"199 字符（边界内）", t475Notes(installNotesMaxRunes - 1), true, installNotesMaxRunes - 1},
		{"200 字符（边界上）", t475Notes(installNotesMaxRunes), true, installNotesMaxRunes},
		{"201 字符（边界外）", t475Notes(installNotesMaxRunes + 1), false, installNotesMaxRunes + 1},
		{"200 汉字 = 600 字节仍算 200", strings.Repeat("压", installNotesMaxRunes), true, installNotesMaxRunes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantRunes, utf8.RuneCountInString(tc.notes), "样本口径本身先自证")
			appErr := checkInstallNotes(tc.notes)
			if tc.wantPass {
				assert.Nil(t, appErr, "%d 字符不该被拒", tc.wantRunes)
				return
			}
			require.NotNil(t, appErr, "%d 字符必须被拒", tc.wantRunes)
			assert.Equal(t, model.CodeInvalidParam, appErr.Code)
			assert.Contains(t, appErr.Message, "notes", "技术文本要能反查字段名")
			assert.Contains(t, appErr.Message, strconv.Itoa(installNotesMaxRunes))
			assert.Contains(t, appErr.Message, strconv.Itoa(tc.wantRunes))
		})
	}
}
