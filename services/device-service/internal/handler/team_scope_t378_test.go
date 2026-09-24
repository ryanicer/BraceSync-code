// T378：设备域读侧归属判据（devices / install_records）。
//
// 缺陷原貌：这一族既不在 #205（T350）也不在 #208（T373）的枚举里，于是任意医护令牌能
// 拉全院设备/安装记录列表，并按 deviceId、installId 逐条读他团队患者的详情
// （安装记录带技师备注与签名图链接）。
//
// 本文件守三件事（卡面判据）：
//  1. 列表：医护按 doctors.team_id 收窄；无团队归属落空集谓词，不得回落全院；
//  2. 单资源：跨团队一律 403，且与「资源不存在 / 未绑定患者」同形（id 存在性不可辨）；
//  3. 只收紧不放宽：运营 / 客服不带谓词、不探测，原有 404 形状逐字不变。
package handler

import (
	"context"
	"encoding/json"
	"errors"
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
	t378DoctorHDR = "ROLE_DOCTOR"
	t378OwnTeam   = "TEAM-T378-A"
	t378OtherTeam = "TEAM-T378-B"
	t378DoctorUID = "ADM-D-T378"
)

// t378Hdr 网关注入的身份头对（userID 传空串模拟头缺失）
func t378Hdr(role, userID string) map[string]string {
	h := map[string]string{"X-Role": role}
	if userID != "" {
		h["X-User-Id"] = userID
	}
	return h
}

// t378Env 装配带 svc（真设备/安装记录）+ fake ListStore（团队判定可编排）的路由。
func t378Env(t *testing.T, doctorTeam string) (*gin.Engine, *fakeListStore, *testutil.FakeStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	fs := testutil.NewFakeStore()
	ls := &fakeListStore{
		doctorTeam:        doctorTeam,
		deviceTeamResult:  map[string]bool{"DEV-T378-OWN": true, "DEV-T378-UNBOUND": false},
		installTeamResult: map[int64]bool{},
	}
	h := New(service.NewDeviceService(fs, enc))
	h.SetListStore(ls)
	return h.Router(), ls, fs
}

func t378Get(t *testing.T, r *gin.Engine, path string, hdr map[string]string) (
	*httptest.ResponseRecorder, struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	},
) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	}
	return w, resp
}

// t378Ctx 造数据用的 context（与请求 context 无关）
func t378Ctx() context.Context { return context.Background() }

// t378Seal 列表 scope 的哨兵初值：用例结束后仍是本值即证明未触库
var t378SealDevice = repo.ListScope{TeamID: "__untouched__"}

// ── 1. 列表读侧 ──────────────────────────────────────────────────────

func TestT378_ListDevices_DoctorScopedToOwnTeam(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)
	ls.lastDeviceScope = t378SealDevice

	w, resp := t378Get(t, r, "/api/v1/devices", t378Hdr(t378DoctorHDR, t378DoctorUID))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, repo.ListScope{TeamScoped: true, TeamID: t378OwnTeam}, ls.lastDeviceScope,
		"医护的过滤团队必须来自 doctors.team_id")
}

func TestT378_ListDevices_DoctorWithoutTeamFailsClosed(t *testing.T) {
	r, ls, _ := t378Env(t, "") // doctors 无行 / team_id 为 NULL
	ls.lastDeviceScope = t378SealDevice

	w, resp := t378Get(t, r, "/api/v1/devices", t378Hdr(t378DoctorHDR, t378DoctorUID))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, repo.ListScope{TeamScoped: true}, ls.lastDeviceScope,
		"无团队仍是受限身份（repo 侧落恒假谓词），不得退化成不过滤")
}

func TestT378_ListDevices_OpsUnscoped(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)
	ls.lastDeviceScope = t378SealDevice

	for _, role := range []string{"ROLE_ADMIN", "ROLE_CS"} {
		w, resp := t378Get(t, r, "/api/v1/devices", t378Hdr(role, "ADM-001"))
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", role, resp.Message)
		assert.Equal(t, repo.ListScope{}, ls.lastDeviceScope, "%s 不得带团队谓词", role)
	}
}

func TestT378_ListDevices_MissingIdentityTouchesNothing(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)
	ls.lastDeviceScope = t378SealDevice

	w, resp := t378Get(t, r, "/api/v1/devices", t378Hdr(t378DoctorHDR, ""))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Equal(t, t378SealDevice, ls.lastDeviceScope, "身份缺失不得下库查询")
}

func TestT378_ListInstallRecords_DoctorScopedAndOpsUnscoped(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)

	w, resp := t378Get(t, r, "/api/v1/install-records", t378Hdr(t378DoctorHDR, t378DoctorUID))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, repo.ListScope{TeamScoped: true, TeamID: t378OwnTeam}, ls.lastInstallScope)

	w, resp = t378Get(t, r, "/api/v1/install-records", t378Hdr("ROLE_ADMIN", "ADM-001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, repo.ListScope{}, ls.lastInstallScope, "运营保留全量")
}

// ── 2. 单资源读侧：跨团队必拒 + 存在性不可辨 ─────────────────────────

func TestT378_DeviceDetail_CrossTeamDenied(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)

	w, resp := t378Get(t, r, "/api/v1/devices/DEV-T378-OTHER", t378Hdr(t378DoctorHDR, t378DoctorUID))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, 1, ls.deviceTeamCalls, "判定只走只读探测")
}

func TestT378_DeviceDetail_SameTeamSucceeds(t *testing.T) {
	r, _, fs := t378Env(t, t378OwnTeam)
	_, err := fs.RegisterDevice(t378Ctx(), &model.Device{
		DeviceID: "DEV-T378-OWN", Model: model.DefaultModel, Status: model.StatusOnline,
	})
	require.NoError(t, err)

	w, resp := t378Get(t, r, "/api/v1/devices/DEV-T378-OWN", t378Hdr(t378DoctorHDR, t378DoctorUID))
	require.Equal(t, http.StatusOK, w.Code, resp.Message, "反证：本团队设备照常可读")
}

// TestT378_DeviceDetail_CrossTeamAndMissingAreIndistinguishable 设备号存在性不得可辨：
// 「他团队」「未绑定患者」「根本不存在」三格同码同形。
func TestT378_DeviceDetail_CrossTeamAndMissingAreIndistinguishable(t *testing.T) {
	r, _, fs := t378Env(t, t378OwnTeam)
	_, err := fs.RegisterDevice(t378Ctx(), &model.Device{
		DeviceID: "DEV-T378-UNBOUND", Model: model.DefaultModel, Status: model.StatusOffline,
	})
	require.NoError(t, err)

	hdr := t378Hdr(t378DoctorHDR, t378DoctorUID)
	w1, r1 := t378Get(t, r, "/api/v1/devices/DEV-T378-OTHER", hdr)
	w2, r2 := t378Get(t, r, "/api/v1/devices/DEV-T378-UNBOUND", hdr)
	w3, r3 := t378Get(t, r, "/api/v1/devices/DEV-T378-NOPE", hdr)
	require.Equal(t, http.StatusForbidden, w1.Code, r1.Message)
	require.Equal(t, http.StatusForbidden, w2.Code, r2.Message)
	require.Equal(t, http.StatusForbidden, w3.Code, r3.Message)
	assert.Equal(t, t378Shape(r1.Message, "DEV-T378-OTHER"), t378Shape(r2.Message, "DEV-T378-UNBOUND"))
	assert.Equal(t, t378Shape(r1.Message, "DEV-T378-OTHER"), t378Shape(r3.Message, "DEV-T378-NOPE"))
}

func t378Shape(msg, id string) string {
	return strings.Replace(msg, id, "<id>", 1)
}

func TestT378_Bindings_CrossTeamDeniedSameGate(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)

	w, resp := t378Get(t, r, "/api/v1/devices/DEV-T378-OTHER/bindings", t378Hdr(t378DoctorHDR, t378DoctorUID))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, 1, ls.deviceTeamCalls, "绑定历史与详情共用同一道门禁")
}

func TestT378_DeviceDetail_OpsRoleKeepsLegacy404(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)

	w, resp := t378Get(t, r, "/api/v1/devices/DEV-T378-NOPE", t378Hdr("ROLE_ADMIN", "ADM-001"))
	assert.Equal(t, http.StatusNotFound, w.Code, resp.Message)
	assert.Equal(t, model.CodeNotFound, resp.Code, "运营侧查无仍是原 404，不得被改成 403")
	assert.Equal(t, 0, ls.deviceTeamCalls, "不受限角色不进入探测路径")
}

func TestT378_InstallDetail_CrossTeamDenied(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)
	w, resp := t378Get(t, r, "/api/v1/install-records/77", t378Hdr(t378DoctorHDR, t378DoctorUID))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, 1, ls.installTeamCalls)
	assert.Equal(t, 0, ls.deviceTeamCalls, "安装记录门禁不得误走设备探测")
}

func TestT378_InstallDetail_SameTeamSucceeds(t *testing.T) {
	r, ls, fs := t378Env(t, t378OwnTeam)
	fs.AddPatient("P-T378")
	id, err := fs.CreateInstall(t378Ctx(), &model.InstallRecord{
		DeviceID: "DEV-T378-OWN", PatientID: "P-T378", TechID: "T0001",
		CalibrateTime: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), WifiStatus: "unconfigured",
	})
	require.NoError(t, err)
	ls.installTeamResult[id] = true

	w, resp := t378Get(t, r, "/api/v1/install-records/"+strconv.FormatInt(id, 10),
		t378Hdr(t378DoctorHDR, t378DoctorUID))
	require.Equal(t, http.StatusOK, w.Code, resp.Message, "反证：本团队安装记录照常可读")
}

// ── 3. 推导失败 / 探测失败一律 fail-closed ───────────────────────────

func TestT378_ScopeLookupErrorFailsClosed(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)
	ls.doctorTeamErr = errors.New("db down")
	ls.lastDeviceScope = t378SealDevice

	w, resp := t378Get(t, r, "/api/v1/devices", t378Hdr(t378DoctorHDR, t378DoctorUID))
	assert.Equal(t, http.StatusInternalServerError, w.Code, resp.Message)
	assert.Equal(t, t378SealDevice, ls.lastDeviceScope, "团队推导失败时不得再发列表查询")
}

func TestT378_DeviceProbeErrorFailsClosed(t *testing.T) {
	r, ls, _ := t378Env(t, t378OwnTeam)
	ls.deviceTeamErr = errors.New("db down")

	w, _ := t378Get(t, r, "/api/v1/devices/DEV-T378-OWN", t378Hdr(t378DoctorHDR, t378DoctorUID))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
