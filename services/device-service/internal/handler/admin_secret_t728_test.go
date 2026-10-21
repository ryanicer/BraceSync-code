// T728 设备验签密钥端点的用例（服务端专用端点 + 一次性回显 + 下载留痕 + admin-only 门禁）。
//
// 判据来自卡面「安全红线」六条与「自验」六条，本文件覆盖其中能在进程内判定的部分：
//
//	红线 1 取密钥只走服务端端点 ⇒ 明文只出现在响应体，密文/解密逻辑不出后端（A/B 两条）；
//	红线 2 一次性回显 ⇒ 幂等再注册不回发密钥，且明文不进任何留痕字符串（A/F 两条）；
//	红线 3 下载必审计 ⇒ 每次下载一行留痕、detail 只有设备 ID；留痕写不进就不发文件（D/E/G 三条）；
//	红线 4 仅 ROLE_ADMIN ⇒ doctor/cs/patient/技师/身份缺失/未知角色直调全部 403，且零读零写（C 条）；
//	红线 5 批量按勾选白名单 + 上限 ⇒ 超限与空名单回 400、不落留痕（F 条）；
//	红线 6 重生成 ⇒ 新值生效、旧值作废、secret_version 自增（H 条）。
//
// staging 现网那一层（真库 + 真网关 + 真浏览器）另按卡面自验六条贴现网读数，本文件不替代它。
package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
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
	t728DeviceA = "DEV-T728-AA"
	t728DeviceB = "DEV-T728-BB"
	t728DeviceC = "DEV-T728-CC"
	t728DeviceS = "DEV-T728-SEED" // 密文不成立那一台（复刻 T573 的 seed 形态）
	t728DeviceN = "DEV-T728-NONE" // 从未注册
	t728Admin   = "ADM-T728"
)

// hex64Re 一枚合规 device_secret 的字形（64 位 hex）：留痕「不含密钥」的判据就靠它整串匹配。
var hex64Re = regexp.MustCompile(`[0-9a-fA-F]{64}`)

// t728SeedSecretDevice 造一台「密钥可解」的设备：注册走的是 service 那条加密通路，
// 这里直接按同一把测试密钥（TestEncKey）把密文塞进仓储桩，好让 handler 侧真的解得开。
// t387Register 造出来的设备 device_secret_enc 是 nil，解不开 ⇒ 密钥通路必须用本夹具。
func t728SeedSecretDevice(t *testing.T, st *t387Store, deviceID string) string {
	t.Helper()
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	secret, err := crypto.RandomSecret(32)
	require.NoError(t, err)
	encBytes, err := enc.Encrypt([]byte(secret))
	require.NoError(t, err)
	_, err = st.RegisterDevice(context.Background(), &model.Device{
		DeviceID: deviceID, Model: model.DefaultModel, DeviceSecretEnc: encBytes,
		SecretVersion: 1, Status: model.StatusOnline,
	})
	require.NoError(t, err)
	return secret
}

// t728SeedBrokenSecretDevice 造一台密文不成立的设备（1 字节占位，短于 GCM 的 12 字节 nonce）：
// 现网 seed 里那批设备就是这个形状（T573），密钥通路必须按台降级而不是整批 500。
func t728SeedBrokenSecretDevice(t *testing.T, st *t387Store, deviceID string) {
	t.Helper()
	_, err := st.RegisterDevice(context.Background(), &model.Device{
		DeviceID: deviceID, Model: model.DefaultModel, DeviceSecretEnc: []byte{0x01},
		SecretVersion: 1, Status: model.StatusOnline,
	})
	require.NoError(t, err)
}

// t728Do 发一次带身份头的请求，返回整个 recorder（文件响应要读头，t387Req 只回信封字段）。
func t728Do(t *testing.T, r *gin.Engine, method, path, role, uid string, body any) *httptest.ResponseRecorder {
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
		req.Header.Set(headerRole, role)
	}
	if uid != "" {
		req.Header.Set(headerUserID, uid)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// t728Envelope 统一响应体（Data 留原始 JSON：密钥通路要按字段存在性判定，不是按结构体零值）
type t728Envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func t728Parse(t *testing.T, w *httptest.ResponseRecorder) t728Envelope {
	t.Helper()
	var env t728Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), "响应体不是统一信封：%s", w.Body.String())
	return env
}

// t728InternalSecret 走 T032 那条 internal 端点取库里现值（「回显的就是库里那一版」的对照口径）。
func t728InternalSecret(t *testing.T, r *gin.Engine, deviceID string) string {
	t.Helper()
	w := t728Do(t, r, http.MethodGet, "/internal/devices/"+deviceID+"/secret", "", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var env struct {
		Code int `json:"code"`
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	require.Equal(t, model.CodeOK, env.Code, w.Body.String())
	return env.Data.Secret
}

// ── A. 注册一次性回显：本次新建才有明文，且明文就是库里那一版 ──────────

func TestT728_RegisterEchoesSecretOnlyOnCreation(t *testing.T) {
	r, _, _ := t387Env(t)
	const deviceID = "DEV-T728-NEW"

	first := t728Parse(t, t728Do(t, r, http.MethodPost, "/api/v1/devices", roleAdmin, t728Admin,
		map[string]string{"deviceId": deviceID}))
	require.Equal(t, model.CodeOK, first.Code, string(first.Data))
	var created struct {
		DeviceID     string `json:"deviceId"`
		DeviceSecret string `json:"deviceSecret"`
	}
	require.NoError(t, json.Unmarshal(first.Data, &created))
	assert.Equal(t, deviceID, created.DeviceID)
	assert.Len(t, created.DeviceSecret, 64, "回显的必须是 64 位 hex 密钥")
	assert.True(t, model.ValidDeviceSecret(created.DeviceSecret), "回显值必须过固件同款校验")
	assert.Equal(t, created.DeviceSecret, t728InternalSecret(t, r, deviceID),
		"回显的那一发必须与库里落的一致（卡面自验 1：下载/回显内容=DB）")

	// 幂等再注册：本次生成的随机数没落库 ⇒ 一个密钥字段都不许出现（否则回出去的是错值）
	again := t728Do(t, r, http.MethodPost, "/api/v1/devices", roleAdmin, t728Admin,
		map[string]string{"deviceId": deviceID})
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	assert.NotContains(t, again.Body.String(), "deviceSecret",
		"命中既有设备不得回发密钥明文（卡面需求 1「仅一次」的另一半）")
}

// ── B. 单台 .env 下载：两行格式 + 附件头 + 不缓存 ─────────────────────

func TestT728_DownloadSingleSecretEnv(t *testing.T) {
	r, st, _ := t387Env(t)
	secret := t728SeedSecretDevice(t, st, t728DeviceA)

	w := t728Do(t, r, http.MethodGet, "/api/v1/admin/devices/"+t728DeviceA+"/secret-file", roleAdmin, t728Admin, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	body := w.Body.String()
	assert.Equal(t, "DEVICE_ID="+t728DeviceA+"\nDEVICE_SECRET="+secret+"\n", body,
		"文件体必须与密道文件同口径的两行（卡面需求 2）")
	assert.Equal(t, secret, t728InternalSecret(t, r, t728DeviceA), "两行里的密钥=库里现值")
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
	assert.Equal(t, `attachment; filename="`+t728DeviceA+`.env"`, w.Header().Get("Content-Disposition"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "密钥文件不得进浏览器/代理缓存")

	audits := st.DeviceSecretReadAudits()
	require.Len(t, audits, 1, "一次下载一行留痕")
	assert.Equal(t, []string{t728DeviceA}, audits[0].DeviceIDs)
	assert.Empty(t, audits[0].Skipped)
	assert.Equal(t, t728Admin, audits[0].OperatorID)
	assert.Equal(t, roleAdmin, audits[0].OperatorRole)
	assert.NotEmpty(t, audits[0].IP)
	assertSecretFreeOfAuditRead(t, audits[0], secret)
}

// assertSecretFreeOfAuditRead 留痕里不许出现密钥（红线 3：detail 只记设备 ID）。
// 做法是把入参里所有可见字符串拼起来，既查「明文整串」也查「任意 64 位 hex 字形」——
// 后者防的是换一种编码把密钥塞进描述（比如带分隔符的截断值改成长 hex 指纹）。
func assertSecretFreeOfAuditRead(t *testing.T, in repo.DeviceSecretReadAuditInput, secret string) {
	t.Helper()
	joined := strings.Join([]string{
		in.Description, in.OperatorID, in.OperatorRole, in.IP,
	}, "|") + "|" + strings.Join(in.DeviceIDs, ",") + "|" + strings.Join(in.Skipped, ",")
	assert.NotContains(t, joined, secret, "留痕不得出现密钥明文")
	assert.NotRegexp(t, hex64Re, joined, "留痕不得出现任何 64 位 hex 形态的密钥材料")
}

// ── C. 仅 ROLE_ADMIN：其余身份三条端点全 403，且判定排在读写之前 ───────

func TestT728_SecretEndpointsDeniedForNonAdminRoles(t *testing.T) {
	eps := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"secret-file", http.MethodGet, "/api/v1/admin/devices/" + t728DeviceA + "/secret-file", nil},
		{"secret-export", http.MethodPost, "/api/v1/admin/device-secrets/export",
			map[string]any{"deviceIds": []string{t728DeviceA}}},
		{"secret-regenerate", http.MethodPost, "/api/v1/admin/devices/" + t728DeviceA + "/secret-regenerate", nil},
	}
	// 技师也在被拒之列：卡面红线 4 点名 doctor/tech，而 T387 那一族写端点放的是 admin+tech。
	denied := []string{roleDoctor, "ROLE_CS", rolePatient, roleTech, "", "ROLE_GUEST"}
	for _, ep := range eps {
		for _, role := range denied {
			t.Run(ep.name+"/"+labelT728Role(role), func(t *testing.T) {
				r, st, _ := t387Env(t)
				t728SeedSecretDevice(t, st, t728DeviceA)
				before := st.snapshot()

				w := t728Do(t, r, ep.method, ep.path, role, t728Admin, ep.body)

				require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
				env := t728Parse(t, w)
				assert.Equal(t, model.CodeForbidden, env.Code)
				assert.Equal(t, model.UserText(model.CodeForbidden), env.Message, "给用户的是中文短句，不是技术文本")
				for k, v := range before.writeDelta(st.snapshot()) {
					assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
				}
				for k, v := range before.readDelta(st.snapshot()) {
					assert.Zero(t, v, "拒绝路径不得先读后拒（%s 探测即泄露存在性）", k)
				}
				assert.Empty(t, st.DeviceSecretReadAudits(), "被拒的请求不该留下任何下载留痕")
			})
		}
	}
}

func labelT728Role(role string) string {
	if role == "" {
		return "missing-role"
	}
	return role
}

// ── D/E. 批量 zip：全绿恰好只有勾选那几台；拿不出的逐台降级并留名单 ────

func TestT728_BatchExportZipHasOneEnvPerSelectedDevice(t *testing.T) {
	r, st, _ := t387Env(t)
	secrets := map[string]string{
		t728DeviceA: t728SeedSecretDevice(t, st, t728DeviceA),
		t728DeviceB: t728SeedSecretDevice(t, st, t728DeviceB),
		t728DeviceC: t728SeedSecretDevice(t, st, t728DeviceC),
	}

	w := t728Do(t, r, http.MethodPost, "/api/v1/admin/device-secrets/export", roleAdmin, t728Admin,
		map[string]any{"deviceIds": []string{t728DeviceA, t728DeviceB, t728DeviceC}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))
	require.Regexp(t, `attachment; filename="device-secrets-\d{8}T\d{6}Z\.zip"`, w.Header().Get("Content-Disposition"))

	entries := t728ZipEntries(t, w.Body.Bytes())
	// 验收 3「内含 3 个 .env」：全绿时不许夹带任何额外条目
	require.Len(t, entries, 3, "zip 条目必须恰好是勾选的三台")
	for deviceID, secret := range secrets {
		content, ok := entries[deviceID+".env"]
		require.True(t, ok, "条目名必须是「设备号.env」（卡面需求 3）")
		assert.Equal(t, "DEVICE_ID="+deviceID+"\nDEVICE_SECRET="+secret+"\n", string(content),
			"每台一个 .env、文件体两行且与库里现值一致")
	}

	audits := st.DeviceSecretReadAudits()
	require.Len(t, audits, 1, "一次批量一行留痕")
	assert.ElementsMatch(t, []string{t728DeviceA, t728DeviceB, t728DeviceC}, audits[0].DeviceIDs)
	assert.Empty(t, audits[0].Skipped)
	for _, secret := range secrets {
		assertSecretFreeOfAuditRead(t, audits[0], secret)
	}
}

func TestT728_BatchExportDegradesPerDevice(t *testing.T) {
	r, st, _ := t387Env(t)
	good := t728SeedSecretDevice(t, st, t728DeviceA)
	t728SeedBrokenSecretDevice(t, st, t728DeviceS)

	w := t728Do(t, r, http.MethodPost, "/api/v1/admin/device-secrets/export", roleAdmin, t728Admin,
		map[string]any{"deviceIds": []string{t728DeviceA, t728DeviceS, t728DeviceN}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	entries := t728ZipEntries(t, w.Body.Bytes())
	require.Contains(t, entries, t728DeviceA+".env", "能导的那台必须进包")
	assert.Equal(t, "DEVICE_ID="+t728DeviceA+"\nDEVICE_SECRET="+good+"\n", string(entries[t728DeviceA+".env"]))
	require.Contains(t, entries, secretSkippedEntryName, "有跳过才多一个名单文件（全绿时不含）")
	manifest := string(entries[secretSkippedEntryName])
	assert.Contains(t, manifest, t728DeviceS)
	assert.Contains(t, manifest, t728DeviceN)
	assert.NotContains(t, manifest, good, "跳过名单里也不许出现任何密钥")
	assert.NotRegexp(t, hex64Re, manifest, "跳过名单里不许出现任何 64 位 hex 形态的密钥材料")

	audits := st.DeviceSecretReadAudits()
	require.Len(t, audits, 1)
	assert.Equal(t, []string{t728DeviceA}, audits[0].DeviceIDs)
	assert.ElementsMatch(t, []string{t728DeviceS, t728DeviceN}, audits[0].Skipped)
}

// ── F. 批量白名单与上限：超限/空名单先拒，不落留痕也不出包 ─────────────

func TestT728_BatchExportRejectsEmptyAndOverCap(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"empty", map[string]any{"deviceIds": []string{}}},
		{"over-cap", map[string]any{"deviceIds": t728DeviceIDs(service.MaxSecretExportBatch + 1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, st, _ := t387Env(t)
			t728SeedSecretDevice(t, st, t728DeviceA)

			w := t728Do(t, r, http.MethodPost, "/api/v1/admin/device-secrets/export", roleAdmin, t728Admin, tc.body)

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Equal(t, model.CodeInvalidParam, t728Parse(t, w).Code)
			assert.Empty(t, st.DeviceSecretReadAudits(), "被拒的请求不该留下任何下载留痕")
		})
	}
}

func TestT728_BatchExportOnlyTouchesRequestedIDs(t *testing.T) {
	r, st, _ := t387Env(t)
	t728SeedSecretDevice(t, st, t728DeviceA)
	other := t728SeedSecretDevice(t, st, t728DeviceB)

	w := t728Do(t, r, http.MethodPost, "/api/v1/admin/device-secrets/export", roleAdmin, t728Admin,
		map[string]any{"deviceIds": []string{t728DeviceA, t728DeviceA, "  "}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	entries := t728ZipEntries(t, w.Body.Bytes())
	assert.Len(t, entries, 1, "重复勾选只出一台，未勾选的那台绝不进包（红线 5）")
	assert.NotContains(t, w.Body.String(), other, "未勾选设备的密钥不得出现在包里")
}

// ── G. 留痕是披露的前置条件：审计写不进就不发文件 ────────────────────

func TestT728_AuditFailureBlocksDisclosure(t *testing.T) {
	_, base, _ := t387Env(t)
	secret := t728SeedSecretDevice(t, base, t728DeviceA)
	r := t728RouterWithStore(t, &t728BrokenAuditStore{t387Store: base})

	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"single", http.MethodGet, "/api/v1/admin/devices/" + t728DeviceA + "/secret-file", nil},
		{"batch", http.MethodPost, "/api/v1/admin/device-secrets/export",
			map[string]any{"deviceIds": []string{t728DeviceA}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := t728Do(t, r, tc.method, tc.path, roleAdmin, t728Admin, tc.body)

			require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Equal(t, model.CodeInternal, t728Parse(t, w).Code)
			assert.NotContains(t, w.Body.String(), secret, "留痕失败时一个密钥字符都不许漏出")
			assert.NotRegexp(t, hex64Re, w.Body.String(), "留痕失败时响应体不得含任何 64 位 hex")
		})
	}
}

// t728RouterWithStore 只换仓储桩、其余夹具与 t387Env 逐字同形（留痕写不进去那条要这么起）。
func t728RouterWithStore(t *testing.T, st repo.Store) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	h := New(service.NewDeviceService(st, enc))
	h.SetListStore(&fakeListStore{doctorTeam: "TEAM-T387"})
	return h.Router()
}

// t728BrokenAuditStore 只把下载留痕这一格换成失败，其余全走 t387Store。
type t728BrokenAuditStore struct {
	*t387Store
	attempts int
}

func (s *t728BrokenAuditStore) WriteDeviceSecretReadAudit(context.Context, repo.DeviceSecretReadAuditInput) error {
	s.attempts++
	return io.ErrClosedPipe
}

// ── H. 重新生成：新值落库、旧值作废、版本号自增、留痕带两版号 ──────────

func TestT728_RegenerateReplacesSecretAndBumpsVersion(t *testing.T) {
	r, st, _ := t387Env(t)
	old := t728SeedSecretDevice(t, st, t728DeviceA)

	w := t728Do(t, r, http.MethodPost, "/api/v1/admin/devices/"+t728DeviceA+"/secret-regenerate", roleAdmin, t728Admin, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var data struct {
		DeviceID      string `json:"deviceId"`
		Secret        string `json:"secret"`
		SecretVersion int    `json:"secretVersion"`
	}
	require.NoError(t, json.Unmarshal(t728Parse(t, w).Data, &data))
	assert.Equal(t, t728DeviceA, data.DeviceID)
	assert.True(t, model.ValidDeviceSecret(data.Secret), "重生成回显的必须仍是 64 位 hex")
	assert.NotEqual(t, old, data.Secret, "新值必须与旧值不同")
	assert.Equal(t, 2, data.SecretVersion, "secret_version 自增（红线 6）")

	assert.Equal(t, data.Secret, t728InternalSecret(t, r, t728DeviceA), "库里现值=新回显值")
	download := t728Do(t, r, http.MethodGet, "/api/v1/admin/devices/"+t728DeviceA+"/secret-file", roleAdmin, t728Admin, nil)
	require.Equal(t, http.StatusOK, download.Code)
	assert.NotContains(t, download.Body.String(), old, "旧值不再是任何端点能交出的东西（作废）")

	regens := st.DeviceSecretRegenerateAudits()
	require.Len(t, regens, 1, "重生成一行留痕")
	assert.Equal(t, t728DeviceA, regens[0].DeviceID)
	assert.Equal(t, 1, regens[0].OldVersion)
	assert.Equal(t, 2, regens[0].NewVersion)
	joined := regens[0].Description + "|" + regens[0].DeviceID
	assert.NotContains(t, joined, old)
	assert.NotContains(t, joined, data.Secret)
	assert.NotRegexp(t, hex64Re, joined, "重生成留痕里不得出现任何密钥材料")
}

// ── I. 不可解设备：单台下载按 T573 那一支回错，不假装成功 ──────────────

func TestT728_UnusableSecretDeviceFallsBackToT573Reason(t *testing.T) {
	r, st, _ := t387Env(t)
	t728SeedBrokenSecretDevice(t, st, t728DeviceS)

	w := t728Do(t, r, http.MethodGet, "/api/v1/admin/devices/"+t728DeviceS+"/secret-file", roleAdmin, t728Admin, nil)

	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	env := t728Parse(t, w)
	assert.Equal(t, model.CodeInternal, env.Code)
	assert.Contains(t, string(env.Data), model.ReasonDeviceSecretUnusable,
		"密文不成立按设备归位（T573 那一支），前端据此引导走重新生成")
	assert.Empty(t, st.DeviceSecretReadAudits(), "什么都没披露，也就不留「下载了」的痕")
}

// ── J. 文件名收敛：响应头注入与 zip 条目穿越都留不下来 ─────────────────

func TestT728_FilenameTokenSanitizer(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"DEV-A_1", "DEV-A_1"},
		{"DEV/../A", "DEVA"},     // 路径穿越：斜杠与点段都落不进白名单（点号被丢是这条的全部意义）
		{"DEV\r\nX: 1", "DEVX1"}, // 响应头注入：CR/LF 与冒号被丢
		{"DEV\"A\"", "DEVA"},     // 引号逃逸
		{"中文DEV", "DEV"},         // 非 ASCII
		{"", "device"},           // 全空不许交出空文件名
		{"DEV-" + strings.Repeat("A", 60), "DEV-" + strings.Repeat("A", 44)}, // 限长=48 个字符整
	} {
		assert.Equal(t, tc.want, safeSecretFilenameToken(tc.in, 48), "输入 %q", tc.in)
	}
}

// ── 夹具辅助 ────────────────────────────────────────────────────────

// t728ZipEntries 解开响应里的 zip，返回 条目名 → 内容。
func t728ZipEntries(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err, "响应体必须是合法 zip")
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(rc)
		require.NoError(t, rc.Close())
		require.NoError(t, err)
		require.NotContains(t, f.Name, "/", "zip 条目不得带路径分隔（防穿越）")
		require.NotContains(t, f.Name, "..", "zip 条目不得带点段（防穿越）")
		out[f.Name] = content
	}
	return out
}

func t728DeviceIDs(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, "DEV-T728-X"+strconv.Itoa(i))
	}
	return out
}
