// T356 ②：device-service 侧的「详情键集合 ⊆ 列表键集合」表驱动断言。
//
// 与 user-service 那份（services/user-service/internal/handler/list_detail_superset_t356_test.go）
// 同构：本服务的成对 list/detail 资源是 devices 与 install-records。
// 两个 fake 分开用：详情端点走 service（repo.Store = testutil.FakeStore），
// 列表端点走 handler.SetListStore(repo.ListStore)。
//
// detailOnly 基线里那两条是实测结果，不是豁免口径：安装记录详情的 offsetValues / createdAt
// 只在单条读时由 baselines LEFT JOIN 与 install_records.created_at 带出，列表投影里没有，
// 页面也只在抽屉里用它们 —— 所以这里登记，而不是要求列表补列。
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
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

// listDetailCase 一条成对资源；seed 负责灌两侧数据并返回详情路径（安装记录 id 由 fake 分配）。
type listDetailCase struct {
	name       string
	listPath   string
	seed       func(t *testing.T, st *testutil.FakeStore, ls *fakeQueryStore) string
	detailOnly map[string]string
}

// fakeQueryStore repo.ListStore 的内存实现（设备 / 安装记录各一行）
type fakeQueryStore struct {
	devices  []repo.DeviceListItem
	installs []repo.InstallListItem
}

func (f *fakeQueryStore) ListDevices(_ context.Context, _ string, _, _ int) ([]repo.DeviceListItem, int64, error) {
	return f.devices, int64(len(f.devices)), nil
}

func (f *fakeQueryStore) ListInstallRecords(_ context.Context, _ string, _, _ int) ([]repo.InstallListItem, int64, error) {
	return f.installs, int64(len(f.installs)), nil
}

func t356Cases() []listDetailCase {
	bind := time.Date(2026, 9, 1, 2, 3, 4, 0, time.UTC)
	pName := "患者小明"
	tName := "技师小李"
	return []listDetailCase{
		{
			name:     "devices",
			listPath: "/api/v1/devices",
			seed: func(t *testing.T, st *testutil.FakeStore, ls *fakeQueryStore) string {
				t.Helper()
				pid := "P20260001"
				st.AddPatient(pid)
				_, err := st.RegisterDevice(context.Background(), &model.Device{
					DeviceID: "DEV-T356", Model: model.DefaultModel, FirmwareVersion: "v1.0.0",
					Status: model.StatusOnline, PatientID: &pid, BindTime: &bind,
				})
				require.NoError(t, err)
				ls.devices = []repo.DeviceListItem{{
					DeviceID: "DEV-T356", Model: model.DefaultModel, FirmwareVersion: "v1.0.0",
					PatientID: &pid, PatientName: &pName, Status: model.StatusOnline, BindTime: &bind,
				}}
				return "/api/v1/devices/DEV-T356"
			},
		},
		{
			name:     "install-records",
			listPath: "/api/v1/install-records",
			seed: func(t *testing.T, st *testutil.FakeStore, ls *fakeQueryStore) string {
				t.Helper()
				id, err := st.CreateInstall(context.Background(), &model.InstallRecord{
					DeviceID: "DEV-T356", PatientID: "P20260001", TechID: "T0001",
					CalibrateTime: bind, WifiStatus: "unconfigured",
				})
				require.NoError(t, err)
				ls.installs = []repo.InstallListItem{{
					InstallID: id, DeviceID: "DEV-T356", PatientID: "P20260001", PatientName: &pName,
					TechID: "T0001", TechName: &tName, CalibrateTime: bind, WifiStatus: "unconfigured",
				}}
				return "/api/v1/install-records/" + strconv.FormatInt(id, 10)
			},
			// 实测：这两列只在详情投影里出现（列表 SQL 不 JOIN baselines，也不取 created_at）
			detailOnly: map[string]string{
				"offsetValues": "基线 20 点偏移值，列表投影不 JOIN baselines（repo/query.go InstallListItem 无此列）",
				"createdAt":    "建档时间，契约注释即写明「列表 DTO 无该字段」（shared-types InstallRecordDetail）",
			},
		},
	}
}

func newT356Env(t *testing.T, ls *fakeQueryStore) (*gin.Engine, *testutil.FakeStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	store := testutil.NewFakeStore()
	h := New(service.NewDeviceService(store, enc))
	h.SetListStore(ls)
	return h.Router(), store
}

func TestT356ListDetailKeySuperset(t *testing.T) {
	for _, c := range t356Cases() {
		t.Run(c.name, func(t *testing.T) {
			ls := &fakeQueryStore{}
			router, store := newT356Env(t, ls)
			detailPath := c.seed(t, store, ls)

			listKeys := wireKeysT356(t, doJSONT356(t, router, c.listPath))
			detailKeys := wireKeysT356(t, doJSONT356(t, router, detailPath))
			assertSupersetT356(t, listKeys, detailKeys, c.detailOnly)
		})
	}
}

// TestT356CasesMatchRoutes 路由里每对成资源必须在表里，表里的路径必须仍是真路由。
func TestT356CasesMatchRoutes(t *testing.T) {
	router, _ := newT356Env(t, &fakeQueryStore{})
	pairs := derivedListDetailPairsT356(router.Routes())
	t.Logf("路由表推导出的成对资源：%d 个", len(pairs))

	registered := map[string]bool{}
	for _, c := range t356Cases() {
		registered[c.listPath] = true
	}
	for _, c := range t356Cases() {
		_, ok := pairs[c.listPath]
		assert.True(t, ok, "表里登记了 %s，但路由里已找不到与之成对的详情端点", c.listPath)
	}
	for listPath, p := range pairs {
		assert.True(t, registered[listPath], "成对路由 %s + %s 未登记进 t356Cases() —— 新资源要表态列表是否覆盖详情字段", p[0], p[1])
	}
}

func derivedListDetailPairsT356(routes gin.RoutesInfo) map[string][2]string {
	get := map[string]bool{}
	for _, r := range routes {
		if r.Method == http.MethodGet {
			get[r.Path] = true
		}
	}
	out := map[string][2]string{}
	for path := range get {
		i := strings.LastIndex(path, "/")
		if i <= 0 {
			continue
		}
		head, last := path[:i], path[i+1:]
		if len(last) > 1 && strings.HasPrefix(last, ":") && get[head] {
			out[head] = [2]string{head, path}
		}
	}
	return out
}

func doJSONT356(t *testing.T, router *gin.Engine, path string) json.RawMessage {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "GET %s 没回 200，键集合断言无从谈起", path)
	var resp struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	return resp.Data
}

func assertSupersetT356(t *testing.T, listKeys, detailKeys []string, detailOnly map[string]string) {
	t.Helper()
	inList := keySet(listKeys)
	inDetail := keySet(detailKeys)
	for _, k := range detailKeys {
		if inList[k] || detailOnly[k] != "" {
			continue
		}
		t.Errorf("详情返回了 %s 但列表没有 —— 列表接口漏回填字段（T337 同类）；确属详情独有就写进 detailOnly 并附理由", k)
	}
	for k, why := range detailOnly {
		if !inDetail[k] {
			t.Errorf("detailOnly 登记了 %s（理由：%s），但详情已不回该键 —— 基线要清", k, why)
			continue
		}
		if inList[k] {
			t.Errorf("detailOnly 登记了 %s（理由：%s），但列表现已返回该键 —— 基线要清", k, why)
		}
	}
}

func keySet(keys []string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

// wireKeysT356 取响应 data 的线上键集合：数组取首元素，pageData 信封取 list[0]。
// 空列表直接失败 —— 否则键集合断言空转。
func wireKeysT356(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	if obj, ok := v.(map[string]any); ok {
		inner, hasList := obj["list"]
		if !hasList {
			return sortedKeysT356(obj)
		}
		v = inner
	}
	arr, ok := v.([]any)
	require.True(t, ok, "响应 data 既不是对象也不是列表信封：%s", string(raw))
	require.NotEmpty(t, arr, "列表为空 ⇒ 键集合断言会空转，夹具必须给一行")
	row, ok := arr[0].(map[string]any)
	require.True(t, ok, "列表首行不是对象：%s", string(raw))
	return sortedKeysT356(row)
}

func sortedKeysT356(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
