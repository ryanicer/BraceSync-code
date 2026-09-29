// T485：安装记录写通路的审计留痕（作用对象类型 install_record）。
//
// 现场（派发单 §一，来源 Ella T479 验收实测）：审计表当日的作用对象类型只有七类，
// 「安装记录」不在其中，而安装记录的两条 HTTP 主写（POST /install-records、
// PUT /install-records/:id）此前在 device-service 零审计调用 —— 两头叠在一起，
// 「安装记录写无审计」这条合规读数连判定力都没有（对象类型压根没有，否定式读数必空）。
//
// 七条行为线：
//  1. 创建成功 → 恰好一行留痕，target_id 是新建那条的号，操作人取网关注入头；
//  2. 回填成功 → 一行留痕，detail 的 changed 只含本次真提交的列；
//  3. 只回填一列 → changed 就只有一个值（不是三列全冒出来）；
//  4. 空 body 的 PUT → 仍留一行、changed 为空（成功即留痕，与 user-service 表驱动埋点同口径）；
//  5. 身份门禁拒掉的写 → 零留痕（被拒的请求不许在审计面上编一行）；
//  6. 查无此记录 → 非 2xx 且零留痕；
//  7. 留痕写失败 → 主写仍是 200，只多一条 WARN（审计失败不反转已经落库的写）。
package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
	"github.com/bracesync/bracesync/services/device-service/internal/service"
	"github.com/bracesync/bracesync/services/device-service/internal/testutil"
)

const (
	t485InstallPath = "/api/v1/install-records"
	t485Notes       = "安装备注：腰托位"
	t485SigURL      = "https://cdn.example.invalid/sig/t485.png"
)

func t485InstallURL(id int64) string {
	return fmt.Sprintf("%s/%d", t485InstallPath, id)
}

// t485Prepare 造「已注册、已绑定、技师与患者都在」的现场（不建安装记录，本卡的创建腿要新建）。
func t485Prepare(t *testing.T, st *t387Store) {
	t.Helper()
	t387Register(t, st, t387Device)
	st.AddPatient(t387Patient)
	st.AddTech(t387Tech)
	_, err := st.Bind(context.Background(), repo.BindParams{
		DeviceID: t387Device, PatientID: t387Patient, OperatorID: t387Tech,
	})
	require.NoError(t, err)
}

// ── 1. 创建留痕 ──────────────────────────────────────────────────────

func TestT485_CreateInstall_WritesOneAuditRow(t *testing.T) {
	r, st, ls := t387Env(t)
	t485Prepare(t, st)
	before := st.snapshot()

	status, msg, code := t387Req(t, r, http.MethodPost, t485InstallPath, roleTech, t387Tech,
		map[string]string{"deviceId": t387Device, "patientId": t387Patient, "techId": t387Tech, "notes": t485Notes})

	require.Equal(t, http.StatusOK, status, "创建被拒 = 本卡通路没打通：%s", msg)
	assert.Equal(t, model.CodeOK, code)

	delta := before.writeDelta(st.snapshot())
	assert.Equal(t, 1, delta["CreateInstall"], "主写落一次")
	assert.Equal(t, 1, delta["WriteInstallRecordAudit"], "应恰好落一行安装记录留痕")

	rows := st.FakeStore.InstallRecordAudits()
	require.Len(t, rows, 1)
	got := rows[0]

	// 留痕的号必须真指着库里那条新建的安装记录（不钉死桩的自增起点，改夹具也不会假红）
	id, err := strconv.ParseInt(got.InstallID, 10, 64)
	require.NoError(t, err, "target_id 得是数字串，否则 admin 审计页按号反查对不上")
	rec, err := st.FakeStore.GetInstall(context.Background(), id)
	require.NoError(t, err, "留痕指向的安装记录不在库里 = 编了个号")
	assert.Equal(t, t387Device, rec.DeviceID)

	assert.Equal(t, t387Tech, got.OperatorID, "operator_id：取 gateway 注入的 X-User-Id")
	assert.Equal(t, roleTech, got.OperatorRole, "operator_role")
	assert.NotEmpty(t, got.IP, "ip：留痕要能反查发起侧")
	assert.Equal(t, fmt.Sprintf(installAuditDescCreate, got.InstallID), got.Description)
	assert.Nil(t, got.Changed, "创建路径没有「本次改了哪几列」这个量，别填三列名糊弄过去")

	assert.Zero(t, delta["SetWifiSSID"], "创建不该顺带走配网回写")
	assert.Zero(t, delta["WriteWifiClearAudit"], "创建不该落清除留痕（另一类动作、另一个对象类型）")
	assert.Zero(t, ls.installTeamCalls, "放行路径不得引入团队探测")
}

// ── 2 / 3 / 4. 回填留痕：changed 只含本次真提交的列 ──────────────────

func TestT485_UpdateInstallMeta_AuditCarriesOnlySubmittedColumns(t *testing.T) {
	cases := []struct {
		name string
		body map[string]string
		want []string
	}{
		{"三列全提交", map[string]string{"notes": t485Notes, "signatureUrl": t485SigURL, "wifiStatus": "skipped"},
			[]string{"notes", "signature_url", "wifi_status"}},
		{"只提交备注", map[string]string{"notes": t485Notes}, []string{"notes"}},
		{"只提交 WiFi 状态", map[string]string{"wifiStatus": "failed"}, []string{"wifi_status"}},
		// 空 body：repo 侧 COALESCE 全保持原值，但请求确实成功 ⇒ 按「成功即留痕」留一行、changed 为空。
		{"空 body", map[string]string{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, st, _ := t387Env(t)
			id := t387Install(t, st, t387Device)
			before := st.snapshot()

			status, msg, code := t387Req(t, r, http.MethodPut, t485InstallURL(id), roleTech, t387Tech, tc.body)

			require.Equal(t, http.StatusOK, status, "回填被拒 = 本卡通路没打通：%s", msg)
			assert.Equal(t, model.CodeOK, code)

			delta := before.writeDelta(st.snapshot())
			assert.Equal(t, 1, delta["UpdateInstallMeta"], "主写落一次")
			assert.Equal(t, 1, delta["WriteInstallRecordAudit"], "应恰好落一行留痕")

			rows := st.FakeStore.InstallRecordAudits()
			require.Len(t, rows, 1)
			idStr := strconv.FormatInt(id, 10)
			assert.Equal(t, idStr, rows[0].InstallID, "target_id 是被回填那条的号")
			assert.Equal(t, t387Tech, rows[0].OperatorID)
			assert.Equal(t, fmt.Sprintf(installAuditDescUpdate, idStr), rows[0].Description)
			assert.Equal(t, tc.want, rows[0].Changed, "changed 要恰好等于本次提交的列（nil = 一列都没提交）")
		})
	}
}

// ── 5. 被门禁拒掉的写不许编造留痕 ────────────────────────────────────

func TestT485_DeniedRolesLeaveNoAuditRow(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		method     string
		path       func() string
		body       any
	}{
		{"创建/医护", roleDoctor, http.MethodPost, func() string { return t485InstallPath },
			map[string]string{"deviceId": t387Device, "patientId": t387Patient, "techId": t387Tech}},
		{"创建/患者", rolePatient, http.MethodPost, func() string { return t485InstallPath },
			map[string]string{"deviceId": t387Device, "patientId": t387Patient, "techId": t387Tech}},
		{"回填/医护", roleDoctor, http.MethodPut, func() string { return t485InstallURL(1) },
			map[string]string{"notes": t485Notes}},
		{"回填/患者", rolePatient, http.MethodPut, func() string { return t485InstallURL(1) },
			map[string]string{"notes": t485Notes}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, st, _ := t387Env(t)
			_ = t387Install(t, st, t387Device) // 回填腿要有真记录，否则「零留痕」可能只是撞在 404 上
			before := st.snapshot()

			status, msg, code := t387Req(t, r, tc.method, tc.path(), tc.role, t387DoctorUID, tc.body)

			assert.Equal(t, http.StatusForbidden, status, "身份门禁形状变了：%s", msg)
			assert.Equal(t, model.CodeForbidden, code)
			delta := before.writeDelta(st.snapshot())
			assert.Zero(t, delta["CreateInstall"]+delta["UpdateInstallMeta"], "被拒的写不许落库")
			assert.Zero(t, delta["WriteInstallRecordAudit"], "被拒的写不许在审计面上编一行")
			assert.Empty(t, st.FakeStore.InstallRecordAudits())
		})
	}
}

// ── 6. 查无此记录：非 2xx 且零留痕 ───────────────────────────────────

func TestT485_UpdateMissingInstall_NoAuditRow(t *testing.T) {
	r, st, _ := t387Env(t)
	_ = t387Install(t, st, t387Device)
	before := st.snapshot()

	status, msg, code := t387Req(t, r, http.MethodPut, t485InstallURL(999999), roleTech, t387Tech,
		map[string]string{"notes": t485Notes})

	// 仓储那一步照样要试（查无正是从它回来的），判据是「留痕一格不落」，不是「写方法没被调用」。
	assert.Equal(t, http.StatusNotFound, status, "查无此号仍按 404：%s", msg)
	assert.Equal(t, model.CodeNotFound, code)
	delta := before.writeDelta(st.snapshot())
	assert.Zero(t, delta["WriteInstallRecordAudit"], "脏号不得编造审计行")
	assert.Empty(t, st.FakeStore.InstallRecordAudits())
}

// ── 7. 留痕失败不反转主写 ────────────────────────────────────────────

// t485BrokenAuditStore 只把留痕这一格换成失败，其余方法与计数全走 t387Store
// ——「审计表写不进」是本卡唯一要注入的故障，别顺手改掉别的通路行为。
type t485BrokenAuditStore struct {
	*t387Store
	attempts int
}

func (s *t485BrokenAuditStore) WriteInstallRecordAudit(context.Context, repo.InstallAuditInput) error {
	s.attempts++
	return errors.New("audit_logs unavailable（T485 故障注入桩）")
}

func t485EnvWithStore(t *testing.T, st repo.Store) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	return New(service.NewDeviceService(st, enc)).Router()
}

func TestT485_AuditFailure_DoesNotFlipMainWrite(t *testing.T) {
	base := newT387Store()
	st := &t485BrokenAuditStore{t387Store: base}
	r := t485EnvWithStore(t, st)
	t485Prepare(t, base)
	logs := t464CaptureLogs(t)

	status, msg, code := t387Req(t, r, http.MethodPost, t485InstallPath, roleTech, t387Tech,
		map[string]string{"deviceId": t387Device, "patientId": t387Patient, "techId": t387Tech})

	require.Equal(t, http.StatusOK, status, "留痕失败被反转成 5xx = 与本卡口径相反（主写已落库）：%s", msg)
	assert.Equal(t, model.CodeOK, code)
	assert.Equal(t, 1, base.snapshot().writes["CreateInstall"], "安装记录本身必须已经落库")
	assert.Equal(t, 1, st.attempts, "留痕只尝试一次，不重试刷屏")

	var warned bool
	for _, entry := range logs.Entries() {
		text, _ := entry["message"].(string)
		tt, _ := entry["target_type"].(string)
		if text == "install record audit write failed" && tt == "install_record" {
			warned = true
			tid, _ := entry["target_id"].(string)
			_, err := strconv.ParseInt(tid, 10, 64)
			assert.NoError(t, err, "WARN 行要带得回是哪条安装记录漏了留痕，实际 target_id=%q", tid)
		}
	}
	assert.True(t, warned, "留痕失败必须有一条带 target_type 的 WARN 行，否则运维查不到：%+v", logs.Entries())
}

// ── 词形对平：changed 的列名要与 repo 的 SET 子句同源 ────────────────

// updateInstallMetaSQL 取出 repo.UpdateInstallMeta 那条 UPDATE 的原文。
// 为什么钉这一格：留痕里的 changed 是给「这次改了什么」反查用的，列名一旦与 SQL 分了叉，
// 读留痕的人按 notes_ 去筛就永远筛不到，而库里没有约束会提醒任何人。
var updateInstallMetaSQL = regexp.MustCompile(`(?s)func \(r \*PGStore\) UpdateInstallMeta.*?\n}`)

func TestT485_ChangedColumnNamesMatchUpdateSQL(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "repo", "repo.go"))
	require.NoError(t, err, "repo/repo.go 被移动或改名时本用例必须响：列名失去同源看守")

	body := updateInstallMetaSQL.Find(raw)
	require.NotNil(t, body, "没能从 repo.go 里取出 UpdateInstallMeta，本探测要跟实现一起改")
	sqlText := string(body)

	got := changedInstallMetaColumns(strPtr("n"), strPtr("s"), strPtr("connected"))
	require.Equal(t, []string{"notes", "signature_url", "wifi_status"}, got, "changed 词形变了要同步改本用例与留痕消费方")
	for _, col := range got {
		assert.Contains(t, sqlText, col+" =", "留痕列名 %q 不在 UPDATE 的 SET 子句里：两处词形已分叉", col)
	}

	// 正对照：探测真的读到了那条 SQL 的主体，而不是空文件巧合。
	assert.Contains(t, sqlText, "COALESCE", "SET 子句里没有 COALESCE = 读错了函数体，本探测失效")
	// 反证：本卡没提交过的列名不该被当成提交列（证明上面的循环不是恒真）。
	assert.Empty(t, changedInstallMetaColumns(nil, nil, nil), "一列都没提交时 changed 必须是 nil")
	for _, bogus := range []string{"wifi_ssid", "install_id", "baseline_id"} {
		assert.NotContains(t, changedInstallMetaColumns(strPtr("n"), strPtr("s"), strPtr("connected")), bogus,
			"changed 里混进了非本通路可改的列 %q", bogus)
	}
}
