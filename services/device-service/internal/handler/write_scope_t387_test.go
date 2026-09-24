// T387：设备域写端点的身份门禁用例（越权必拒 + 拒绝路径零写 + 本角色照常成功的反证）。
//
// 缺陷原貌：bind / rebind / unbind / wifi / 安装记录创建 / 安装元数据回填 / 基线保存这七条
// 写端点在服务层零判定，而网关 staffOnlyPatterns 把医护与客服一并放行 ⇒ 医护令牌能把设备
// 绑到任意患者名下（含他团队患者）、改写他人安装备注与签名、覆盖校准基线。
//
// 本文件守四件事（卡面判据二、三）：
//  1. 医护 / 客服 / 患者 / 身份缺失 / 未知角色 → 403，且七条端点的写仓储与读探测计数增量均为 0
//     （判定排在 JSON 解析与任何仓储访问之前）；
//  2. 反证：技师与运营管理员对同一组端点照常 200，各自的目标写方法恰好落库一次；
//  3. 拒绝形状与「资源根本不存在」逐字同形 ⇒ deviceId / installId 存在性不作为探测面；
//  4. 技师与管理员不进入团队推导（PM 口径：走原路径不受团队隔离，技师面待 Boss 裁）——
//     放行路径上 fakeListStore 的两条团队探测计数为 0。
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
	"time"

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
	t387Device    = "DEV-T387-OWN"
	t387DeviceNo  = "DEV-T387-NOPE" // 未注册设备，用于存在性不可辨对照
	t387Patient   = "PAT-T387-A"
	t387PatientB  = "PAT-T387-B"
	t387Tech      = "TECH-T387"
	t387AdminUID  = "ADM-T387"
	t387DoctorUID = "ADM-D-T387"
)

// t387DefaultRole 给**既有用例**的共享请求夹具补默认身份：仅当这条请求打在本卡新收口的
// 七条写端点上、且调用方没显式给 X-Role 时，按技师代发。
//
// 为什么按路径判定而不是无条件补：handler_http_test / install_*_test / t299 / t356 里的
// 夹具测的是技师安装流程的业务行为（操作人一律 TECH-*），期望值不该因本卡逐条改写；
// 而 provision_ownership_t193_test.go 的「缺身份头必须 fail-closed」那一格走的是未被门禁的
// provision-key 路由，无条件代发会把它假绿掉。按路径判定 ⇒ 两类用例各守各的原样。
//
// 本卡门禁自身的用例走 t387Req（显式构造身份，含缺失那格），不依赖这里的默认值。
func t387DefaultRole(method, path string, headers map[string]string) map[string]string {
	if headers[headerRole] != "" || !t387GatedWritePath(method, path) {
		return headers
	}
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		out[k] = v
	}
	out[headerRole] = roleTech
	return out
}

// t387GatedWritePath 与 write_scope_t387.go 的七条挂载点一一对应（GET 安装记录/设备详情不在其列）
func t387GatedWritePath(method, path string) bool {
	switch method {
	case http.MethodPost:
		if path == "/api/v1/install-records" || path == "/api/v1/baselines" {
			return true
		}
		if !strings.HasPrefix(path, "/api/v1/devices/") {
			return false
		}
		for _, act := range []string{"/bind", "/rebind", "/unbind", "/wifi"} {
			if strings.HasSuffix(path, act) {
				return true
			}
		}
	case http.MethodPut:
		return strings.HasPrefix(path, "/api/v1/install-records/")
	}
	return false
}

// t387Store 计数壳：包住 testutil.FakeStore，记录七条写方法与三条只读探测的调用次数。
// 拒绝路径的硬判据就是这张计数表增量全 0 —— 返回 403 但已经落过库，等于没拦。
type t387Store struct {
	*testutil.FakeStore
	writes map[string]int
	reads  map[string]int
}

func newT387Store() *t387Store {
	return &t387Store{
		FakeStore: testutil.NewFakeStore(),
		writes:    map[string]int{},
		reads:     map[string]int{},
	}
}

// t387Counters 某一时刻的计数快照（增量判定用：prepare 阶段自身会写库）
type t387Counters struct{ writes, reads map[string]int }

func (s *t387Store) snapshot() t387Counters {
	w := make(map[string]int, len(s.writes))
	for k, v := range s.writes {
		w[k] = v
	}
	r := make(map[string]int, len(s.reads))
	for k, v := range s.reads {
		r[k] = v
	}
	return t387Counters{writes: w, reads: r}
}

func (c t387Counters) writeDelta(now t387Counters) map[string]int {
	out := map[string]int{}
	for k, v := range now.writes {
		if d := v - c.writes[k]; d != 0 {
			out[k] = d
		}
	}
	for k := range c.writes {
		if _, ok := out[k]; !ok {
			out[k] = 0
		}
	}
	return out
}

func (c t387Counters) readDelta(now t387Counters) map[string]int {
	out := map[string]int{}
	for k, v := range now.reads {
		if d := v - c.reads[k]; d != 0 {
			out[k] = d
		}
	}
	for k := range c.reads {
		if _, ok := out[k]; !ok {
			out[k] = 0
		}
	}
	return out
}

func (s *t387Store) Bind(ctx context.Context, p repo.BindParams) (*repo.BindOutcome, error) {
	s.writes["Bind"]++
	return s.FakeStore.Bind(ctx, p)
}

func (s *t387Store) Rebind(ctx context.Context, p repo.BindParams) (*repo.BindOutcome, error) {
	s.writes["Rebind"]++
	return s.FakeStore.Rebind(ctx, p)
}

func (s *t387Store) Unbind(ctx context.Context, deviceID, operatorID string) (bool, error) {
	s.writes["Unbind"]++
	return s.FakeStore.Unbind(ctx, deviceID, operatorID)
}

func (s *t387Store) SetWifiSSID(ctx context.Context, deviceID, ssid string) error {
	s.writes["SetWifiSSID"]++
	return s.FakeStore.SetWifiSSID(ctx, deviceID, ssid)
}

func (s *t387Store) CreateInstall(ctx context.Context, rec *model.InstallRecord) (int64, error) {
	s.writes["CreateInstall"]++
	return s.FakeStore.CreateInstall(ctx, rec)
}

func (s *t387Store) UpdateInstallMeta(ctx context.Context, installID int64, notes, signatureURL *string) error {
	s.writes["UpdateInstallMeta"]++
	return s.FakeStore.UpdateInstallMeta(ctx, installID, notes, signatureURL)
}

func (s *t387Store) SaveBaseline(ctx context.Context, installID int64, offsets []float32, calibratorID string) (int64, error) {
	s.writes["SaveBaseline"]++
	return s.FakeStore.SaveBaseline(ctx, installID, offsets, calibratorID)
}

func (s *t387Store) GetDevice(ctx context.Context, deviceID string) (*model.Device, error) {
	s.reads["GetDevice"]++
	return s.FakeStore.GetDevice(ctx, deviceID)
}

func (s *t387Store) PatientExists(ctx context.Context, patientID string) (bool, error) {
	s.reads["PatientExists"]++
	return s.FakeStore.PatientExists(ctx, patientID)
}

func (s *t387Store) TechExists(ctx context.Context, techID string) (bool, error) {
	s.reads["TechExists"]++
	return s.FakeStore.TechExists(ctx, techID)
}

// t387Env 装配带计数仓储 + fake ListStore（可观测团队探测次数）的路由
func t387Env(t *testing.T) (*gin.Engine, *t387Store, *fakeListStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	st := newT387Store()
	ls := &fakeListStore{doctorTeam: "TEAM-T387"}
	h := New(service.NewDeviceService(st, enc))
	h.SetListStore(ls)
	return h.Router(), st, ls
}

// t387Req 发起带身份头的请求。role 传空串 = 不发 X-Role（模拟身份缺失）。
// body 传 string 时按原样作请求体送（非法 JSON 用例），其余类型走 JSON 编码。
func t387Req(t *testing.T, r *gin.Engine, method, path, role, uid string, body any) (
	int, string, int,
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
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	}
	return w.Code, resp.Message, resp.Code
}

// t387Ep 一条写端点的可复用描述：prepare 造现场并返回实际路径与请求体
type t387Ep struct {
	name    string
	method  string
	write   string // 本端点应落的那一个写方法
	prepare func(t *testing.T, st *t387Store, absent bool) (path string, body any)
}

func t387Register(t *testing.T, st *t387Store, deviceID string) {
	t.Helper()
	_, err := st.RegisterDevice(context.Background(), &model.Device{
		DeviceID: deviceID, Model: model.DefaultModel, Status: model.StatusOnline,
	})
	require.NoError(t, err)
}

func t387Offsets() []float32 {
	v := make([]float32, model.PointCount)
	for i := range v {
		v[i] = 1
	}
	return v
}

// t387Install 造一条已绑定、已建安装记录（未校准）的现场，返回 installId
func t387Install(t *testing.T, st *t387Store, deviceID string) int64 {
	t.Helper()
	t387Register(t, st, deviceID)
	st.AddPatient(t387Patient)
	st.AddTech(t387Tech)
	_, err := st.Bind(context.Background(), repo.BindParams{
		DeviceID: deviceID, PatientID: t387Patient, OperatorID: t387Tech,
	})
	require.NoError(t, err)
	id, err := st.CreateInstall(context.Background(), &model.InstallRecord{
		DeviceID: deviceID, PatientID: t387Patient, TechID: t387Tech,
		CalibrateTime: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), WifiStatus: "unconfigured",
	})
	require.NoError(t, err)
	return id
}

// t387Endpoints 卡面点名的七条写端点。absent=true 时把路径里的资源号换成不存在的号，
// 供「拒绝形状与查无同形」那条判据复用同一张表。
func t387Endpoints() []t387Ep {
	dev := func(absent bool) string {
		if absent {
			return t387DeviceNo
		}
		return t387Device
	}
	instPath := func(absent bool, id int64) string {
		if absent {
			return "/api/v1/install-records/999999"
		}
		return "/api/v1/install-records/" + strconv.FormatInt(id, 10)
	}
	return []t387Ep{
		{
			name: "bind", method: http.MethodPost, write: "Bind",
			prepare: func(t *testing.T, st *t387Store, absent bool) (string, any) {
				d := dev(absent)
				t387Register(t, st, d)
				st.AddPatient(t387Patient)
				return "/api/v1/devices/" + d + "/bind", map[string]string{"patientId": t387Patient}
			},
		},
		{
			name: "rebind", method: http.MethodPost, write: "Rebind",
			prepare: func(t *testing.T, st *t387Store, absent bool) (string, any) {
				d := dev(absent)
				t387Register(t, st, d)
				st.AddPatient(t387Patient)
				st.AddPatient(t387PatientB)
				_, err := st.Bind(context.Background(), repo.BindParams{
					DeviceID: d, PatientID: t387Patient, OperatorID: t387Tech,
				})
				require.NoError(t, err)
				return "/api/v1/devices/" + d + "/rebind", map[string]string{"patientId": t387PatientB}
			},
		},
		{
			name: "unbind", method: http.MethodPost, write: "Unbind",
			prepare: func(t *testing.T, st *t387Store, absent bool) (string, any) {
				d := dev(absent)
				t387Register(t, st, d)
				return "/api/v1/devices/" + d + "/unbind", nil
			},
		},
		{
			name: "wifi", method: http.MethodPost, write: "SetWifiSSID",
			prepare: func(t *testing.T, st *t387Store, absent bool) (string, any) {
				d := dev(absent)
				t387Register(t, st, d)
				return "/api/v1/devices/" + d + "/wifi", map[string]string{"ssid": "AP-T387"}
			},
		},
		{
			name: "create-install", method: http.MethodPost, write: "CreateInstall",
			prepare: func(t *testing.T, st *t387Store, absent bool) (string, any) {
				d := dev(absent)
				if !absent {
					t387Register(t, st, d)
					st.AddPatient(t387Patient)
					st.AddTech(t387Tech)
					_, err := st.Bind(context.Background(), repo.BindParams{
						DeviceID: d, PatientID: t387Patient, OperatorID: t387Tech,
					})
					require.NoError(t, err)
				}
				return "/api/v1/install-records", map[string]string{
					"deviceId": d, "patientId": t387Patient, "techId": t387Tech,
				}
			},
		},
		{
			name: "update-install-meta", method: http.MethodPut, write: "UpdateInstallMeta",
			prepare: func(t *testing.T, st *t387Store, absent bool) (string, any) {
				id := t387Install(t, st, t387Device)
				return instPath(absent, id), map[string]string{"notes": "T387 回填"}
			},
		},
		{
			name: "save-baseline", method: http.MethodPost, write: "SaveBaseline",
			prepare: func(t *testing.T, st *t387Store, absent bool) (string, any) {
				id := t387Install(t, st, t387Device)
				body := map[string]any{"installId": strconv.FormatInt(id, 10), "offsetValues": t387Offsets()}
				if absent {
					body["installId"] = "999999"
				}
				return "/api/v1/baselines", body
			},
		},
	}
}

// t387DeniedRoles 应被拒的身份：医护（卡面点名）、客服、患者、身份缺失、未知角色。
// 后三条是 fail-closed 的形状检查——网关正常时一定会注入 X-Role，直连与异常链路才会落进来。
func t387DeniedRoles() []string {
	return []string{roleDoctor, "ROLE_CS", rolePatient, "", "ROLE_GUEST"}
}

// ── 1. 越权必拒 + 拒绝路径零写 ────────────────────────────────────────

func TestT387_WriteDeniedForNonWriterRolesWithZeroWrite(t *testing.T) {
	for _, ep := range t387Endpoints() {
		for _, role := range t387DeniedRoles() {
			t.Run(ep.name+"/"+deniedLabel(role), func(t *testing.T) {
				r, st, ls := t387Env(t)
				path, body := ep.prepare(t, st, false)
				before := st.snapshot()

				status, msg, code := t387Req(t, r, ep.method, path, role, t387DoctorUID, body)

				assert.Equal(t, http.StatusForbidden, status, "message=%s", msg)
				assert.Equal(t, model.CodeForbidden, code)
				assert.Contains(t, msg, "may not", "拒绝文案要点明是身份不允许，不是资源问题")
				assert.NotContains(t, msg, ep.write, "文案不得泄露将命中的仓储方法名")
				for k, v := range before.writeDelta(st.snapshot()) {
					assert.Zero(t, v, "拒绝路径不得触碰写方法 %s", k)
				}
				for k, v := range before.readDelta(st.snapshot()) {
					assert.Zero(t, v, "拒绝路径不得先读后拒（%s 探测即泄露存在性）", k)
				}
				assert.Zero(t, ls.deviceTeamCalls, "写侧门禁不该走设备团队探测")
				assert.Zero(t, ls.installTeamCalls, "写侧门禁不该走安装记录团队探测")
			})
		}
	}
}

func deniedLabel(role string) string {
	if role == "" {
		return "missing-role"
	}
	return role
}

// ── 2. 反证：本角色照常成功 ───────────────────────────────────────────

func TestT387_WriteAllowedForTechAndAdmin(t *testing.T) {
	ids := map[string]string{roleTech: t387Tech, roleAdmin: t387AdminUID}
	for _, ep := range t387Endpoints() {
		for _, role := range []string{roleTech, roleAdmin} {
			t.Run(ep.name+"/"+role, func(t *testing.T) {
				r, st, ls := t387Env(t)
				path, body := ep.prepare(t, st, false)
				before := st.snapshot()

				status, msg, code := t387Req(t, r, ep.method, path, role, ids[role], body)

				require.Equal(t, http.StatusOK, status, "反证失败：%s 以 %s 应照常成功，message=%s", ep.name, role, msg)
				assert.Equal(t, model.CodeOK, code)
				delta := before.writeDelta(st.snapshot())
				assert.Equal(t, 1, delta[ep.write], "应恰好落一次 %s，实际增量=%v", ep.write, delta)
				// PM 口径：技师与管理员走原路径，不受团队隔离（技师面待 Boss 裁，裁前不动）
				assert.Zero(t, ls.deviceTeamCalls, "放行路径不得引入团队探测")
				assert.Zero(t, ls.installTeamCalls, "放行路径不得引入团队探测")
			})
		}
	}
}

// ── 3. 不泄露存在性：跨身份拒绝与「查无此号」同形 ─────────────────────

func TestT387_DenyShapeIndistinguishableFromMissingResource(t *testing.T) {
	for _, ep := range t387Endpoints() {
		t.Run(ep.name, func(t *testing.T) {
			r, st, _ := t387Env(t)
			existPath, existBody := ep.prepare(t, st, false)
			absentPath, absentBody := ep.prepare(t, newT387Store(), true)

			_, msgExist, codeExist := t387Req(t, r, ep.method, existPath, roleDoctor, t387DoctorUID, existBody)
			_, msgAbsent, codeAbsent := t387Req(t, r, ep.method, absentPath, roleDoctor, t387DoctorUID, absentBody)

			require.Equal(t, model.CodeForbidden, codeExist)
			require.Equal(t, model.CodeForbidden, codeAbsent)
			assert.Equal(t, foldT387IDs(msgExist), foldT387IDs(msgAbsent),
				"存在与不存在必须同码同文案，否则资源号仍是探测面")
		})
	}
}

// foldT387IDs 把文案里可能出现的资源号折成占位符（当前文案不带号，这里防的是将来加号）
func foldT387IDs(msg string) string {
	for _, s := range []string{t387Device, t387DeviceNo, t387Patient, t387PatientB, t387Tech, "999999"} {
		msg = strings.ReplaceAll(msg, s, "<id>")
	}
	return msg
}

// ── 4. 判定排在解析之前 ──────────────────────────────────────────────

// TestT387_DenyPrecedesBodyParsing 非法 body 在受限身份下不得先吐 20400：
// 那等于让医护用「400 / 403 谁先来」探端点差异。
func TestT387_DenyPrecedesBodyParsing(t *testing.T) {
	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/devices/" + t387Device + "/bind"},
		{http.MethodPost, "/api/v1/devices/" + t387Device + "/wifi"},
		{http.MethodPost, "/api/v1/install-records"},
		{http.MethodPut, "/api/v1/install-records/abc"},
		{http.MethodPost, "/api/v1/baselines"},
	}
	for _, tc := range cases {
		r, st, _ := t387Env(t)
		t387Register(t, st, t387Device)

		status, msg, _ := t387Req(t, r, tc.method, tc.path, roleDoctor, t387DoctorUID, "{not-json")
		assert.Equal(t, http.StatusForbidden, status, "%s %s → %s", tc.method, tc.path, msg)
		assert.Zero(t, st.snapshot().writes["Bind"]+st.snapshot().writes["CreateInstall"])
	}
}
