// Package handler 实现侧测试（T030）：管理端列表端点 HTTP 层用例（fake ListStore）
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

func init() { gin.SetMode(gin.TestMode) }

// fakeListStore repo.ListStore 内存实现
type fakeListStore struct {
	devices      []repo.DeviceListItem
	deviceTotal  int64
	devicesErr   error
	installs     []repo.InstallListItem
	installTotal int64
	installsErr  error
	lastKeyword  string
	lastPage     int
	lastPageSize int
}

func (f *fakeListStore) ListDevices(_ context.Context, keyword string, page, pageSize int) ([]repo.DeviceListItem, int64, error) {
	f.lastKeyword, f.lastPage, f.lastPageSize = keyword, page, pageSize
	return f.devices, f.deviceTotal, f.devicesErr
}

func (f *fakeListStore) ListInstallRecords(_ context.Context, keyword string, page, pageSize int) ([]repo.InstallListItem, int64, error) {
	f.lastKeyword, f.lastPage, f.lastPageSize = keyword, page, pageSize
	return f.installs, f.installTotal, f.installsErr
}

// newQueryEnv 装配带 ListStore 的路由（svc 允许 nil：列表端点不依赖 svc）
func newQueryEnv(t *testing.T, store repo.ListStore) *Handler {
	t.Helper()
	h := New(nil)
	h.SetListStore(store)
	return h
}

func doGet(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	return w
}

func TestListDevicesEndpoint(t *testing.T) {
	bindTime := time.Date(2026, 7, 16, 2, 0, 0, 0, time.UTC)
	pName := "患者小明"
	store := &fakeListStore{
		devices: []repo.DeviceListItem{{
			DeviceID: "PRS-001", Model: "PRS-ML05-RC", FirmwareVersion: "v1.2.0",
			PatientID: strPtr("P20260001"), PatientName: &pName,
			WifiSSID: strPtr("ClinicWiFi"), BindTime: &bindTime, Status: "online",
		}},
		deviceTotal: 1,
	}
	h := newQueryEnv(t, store)

	w := doGet(t, h, "/api/v1/devices?keyword=PRS&page=1&pageSize=5")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "PRS", store.lastKeyword)
	assert.Equal(t, 1, store.lastPage)
	assert.Equal(t, 5, store.lastPageSize)

	var resp struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	var page struct {
		List []struct {
			DeviceID    string  `json:"deviceId"`
			PatientName *string `json:"patientName"`
			BindTime    *string `json:"bindTime"`
		} `json:"list"`
		Total int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	require.Len(t, page.List, 1)
	assert.Equal(t, "PRS-001", page.List[0].DeviceID)
	require.NotNil(t, page.List[0].PatientName)
	assert.Equal(t, "患者小明", *page.List[0].PatientName)
	assert.Equal(t, "2026-07-16T02:00:00Z", *page.List[0].BindTime)

	// 分页校验 → 400
	assert.Equal(t, http.StatusBadRequest, doGet(t, h, "/api/v1/devices?page=0").Code)
	assert.Equal(t, http.StatusBadRequest, doGet(t, h, "/api/v1/devices?pageSize=999").Code)

	// store 错误 → 500
	store.devicesErr = errors.New("db")
	assert.Equal(t, http.StatusInternalServerError, doGet(t, h, "/api/v1/devices").Code)
}

func TestListDevicesWithoutStore(t *testing.T) {
	h := New(nil) // 未注入 ListStore
	assert.Equal(t, http.StatusInternalServerError, doGet(t, h, "/api/v1/devices").Code)
	assert.Equal(t, http.StatusInternalServerError, doGet(t, h, "/api/v1/install-records").Code)
}

func TestListInstallRecordsEndpoint(t *testing.T) {
	pName, tName := "患者小明", "技师老陈"
	baselineID := int64(7)
	store := &fakeListStore{
		installs: []repo.InstallListItem{{
			InstallID: 1, DeviceID: "PRS-001", PatientID: "P20260001", PatientName: &pName,
			TechID: "T0001", TechName: &tName,
			CalibrateTime: time.Date(2026, 7, 16, 2, 0, 0, 0, time.UTC),
			BaselineID:    &baselineID, Notes: strPtr("顺利"), WifiStatus: "connected",
		}},
		installTotal: 1,
	}
	h := newQueryEnv(t, store)

	w := doGet(t, h, "/api/v1/install-records?keyword=老陈")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "老陈", store.lastKeyword)

	var resp struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	var page struct {
		List []struct {
			InstallID   string  `json:"installId"`
			PatientName *string `json:"patientName"`
			TechName    *string `json:"techName"`
			BaselineID  *string `json:"baselineId"`
			Notes       string  `json:"notes"`
		} `json:"list"`
		Total    int64 `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	require.Len(t, page.List, 1)
	assert.Equal(t, "1", page.List[0].InstallID)
	assert.Equal(t, "技师老陈", *page.List[0].TechName)
	assert.Equal(t, "7", *page.List[0].BaselineID)
	assert.Equal(t, "顺利", page.List[0].Notes)
	assert.Equal(t, 20, page.PageSize) // 默认分页

	assert.Equal(t, http.StatusBadRequest, doGet(t, h, "/api/v1/install-records?page=x").Code)
	store.installsErr = errors.New("db")
	assert.Equal(t, http.StatusInternalServerError, doGet(t, h, "/api/v1/install-records").Code)
}

func strPtr(s string) *string { return &s }
