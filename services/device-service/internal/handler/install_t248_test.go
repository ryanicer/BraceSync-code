// Package handler T248 实现侧测试：安装记录 20 点偏移值回显（9.1）+ 校准状态三态（9.3）
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

// createInstallForCalib 建一台已绑定设备 + 一条安装记录，返回 installId
func createInstallForCalib(t *testing.T, env *testEnv, deviceID, patientID, techID string) string {
	t.Helper()
	env.store.AddPatient(patientID)
	env.store.AddTech(techID)

	_, _ = env.do(t, http.MethodPost, "/api/v1/devices",
		map[string]string{"deviceId": deviceID}, nil)
	_, _ = env.do(t, http.MethodPost, "/api/v1/devices/"+deviceID+"/bind",
		map[string]string{"patientId": patientID}, map[string]string{"X-User-Id": techID})

	status, resp := env.do(t, http.MethodPost, "/api/v1/install-records", map[string]string{
		"deviceId": deviceID, "patientId": patientID, "techId": techID,
	}, nil)
	require.Equal(t, http.StatusOK, status)
	var created struct {
		InstallID string `json:"installId"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &created))
	return created.InstallID
}

func saveBaselineFor(t *testing.T, env *testEnv, installID string, offsets []float32, techID string) {
	t.Helper()
	status, resp := env.do(t, http.MethodPost, "/api/v1/baselines", map[string]any{
		"installId": installID, "offsetValues": offsets,
	}, map[string]string{"X-User-Id": techID})
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, model.CodeOK, resp.Code)
}

func getInstallDetail(t *testing.T, env *testEnv, installID string) installDetailDTO {
	t.Helper()
	status, resp := env.do(t, http.MethodGet, "/api/v1/install-records/"+installID, nil, nil)
	require.Equal(t, http.StatusOK, status)
	var detail installDetailDTO
	require.NoError(t, json.Unmarshal(resp.Data, &detail))
	return detail
}

// offsetsWith 长度 20 的偏移值，index 处覆盖为指定值（其余 0.1）
func offsetsWith(patches map[int]float32) []float32 {
	offs := make([]float32, model.PointCount)
	for i := range offs {
		offs[i] = 0.1
	}
	for i, v := range patches {
		offs[i] = v
	}
	return offs
}

// TestInstallDetailUncalibrated_T248 未校准：offsetValues 序列化为 []（非 null）、calibStatus=uncalibrated
func TestInstallDetailUncalibrated_T248(t *testing.T) {
	env := newTestEnv(t)
	id := createInstallForCalib(t, env, "DEV-T248-A", "P-T248-A", "TECH-T248-A")

	detail := getInstallDetail(t, env, id)
	assert.Nil(t, detail.BaselineID)
	assert.NotNil(t, detail.OffsetValues)
	assert.Len(t, detail.OffsetValues, 0)
	assert.Equal(t, model.CalibStatusUncalibrated, detail.CalibStatus)

	// 原文断言：未校准必须是 []，不能是 null（前端 20 点网格直接遍历）
	_, raw := env.do(t, http.MethodGet, "/api/v1/install-records/"+id, nil, nil)
	assert.Contains(t, string(raw.Data), `"offsetValues":[]`)
}

// TestInstallDetailOffsetValuesEcho_T248 9.1 主用例：saveBaseline 后详情回显 20 点 + normal
func TestInstallDetailOffsetValuesEcho_T248(t *testing.T) {
	env := newTestEnv(t)
	id := createInstallForCalib(t, env, "DEV-T248-B", "P-T248-B", "TECH-T248-B")

	want := offsetsWith(map[int]float32{11: 0.9})
	saveBaselineFor(t, env, id, want, "TECH-T248-B")

	detail := getInstallDetail(t, env, id)
	require.NotNil(t, detail.BaselineID)
	assert.Len(t, detail.OffsetValues, model.PointCount)
	assert.Equal(t, want, detail.OffsetValues)
	assert.Equal(t, model.CalibStatusNormal, detail.CalibStatus)
}

// TestInstallDetailCalibAbnormal_T248 9.3：单点越界（正/负）→ abnormal
func TestInstallDetailCalibAbnormal_T248(t *testing.T) {
	cases := []struct {
		name    string
		device  string
		patient string
		tech    string
		offsets []float32
	}{
		{"正向越界", "DEV-T248-C1", "P-T248-C1", "TECH-T248-C1", offsetsWith(map[int]float32{11: 5.82})},
		{"负向越界取绝对值", "DEV-T248-C2", "P-T248-C2", "TECH-T248-C2", offsetsWith(map[int]float32{3: -3.5})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			id := createInstallForCalib(t, env, tc.device, tc.patient, tc.tech)
			saveBaselineFor(t, env, id, tc.offsets, tc.tech)
			assert.Equal(t, model.CalibStatusAbnormal, getInstallDetail(t, env, id).CalibStatus)
		})
	}
}

// TestInstallCalibThresholdConfigurable_T248 阈值走配置：调高后原异常点转 normal；0 关闭越界判定
func TestInstallCalibThresholdConfigurable_T248(t *testing.T) {
	env := newTestEnv(t)
	id := createInstallForCalib(t, env, "DEV-T248-D", "P-T248-D", "TECH-T248-D")
	abnormal := offsetsWith(map[int]float32{5: 5.82})
	saveBaselineFor(t, env, id, abnormal, "TECH-T248-D")

	assert.Equal(t, model.CalibStatusAbnormal, getInstallDetail(t, env, id).CalibStatus)

	t.Setenv("CALIB_OFFSET_ANOMALY_N", "10")
	assert.Equal(t, model.CalibStatusNormal, getInstallDetail(t, env, id).CalibStatus)

	t.Setenv("CALIB_OFFSET_ANOMALY_N", "0") // 不判定越界，只区分已/未校准
	assert.Equal(t, model.CalibStatusNormal, getInstallDetail(t, env, id).CalibStatus)

	t.Setenv("CALIB_OFFSET_ANOMALY_N", "not-a-number") // 非法值回落默认 2.8
	assert.Equal(t, model.CalibStatusAbnormal, getInstallDetail(t, env, id).CalibStatus)
}

// TestListInstallRecordsCalibStatus_T248 9.3：列表侧「校准」列三态派生
func TestListInstallRecordsCalibStatus_T248(t *testing.T) {
	bID := int64(7)
	store := &fakeListStore{
		installs: []repo.InstallListItem{
			{InstallID: 1, DeviceID: "D1", TechID: "T1", BaselineID: &bID, OffsetValues: offsetsWith(nil)},
			{InstallID: 2, DeviceID: "D2", TechID: "T1", BaselineID: &bID, OffsetValues: offsetsWith(map[int]float32{9: 3.1})},
			{InstallID: 3, DeviceID: "D3", TechID: "T1"},
		},
		installTotal: 3,
	}
	w := doGet(t, newQueryEnv(t, store), "/api/v1/install-records")
	require.Equal(t, http.StatusOK, w.Code)

	var page struct {
		List []installListDTO `json:"list"`
	}
	var resp struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	require.Len(t, page.List, 3)
	assert.Equal(t, model.CalibStatusNormal, page.List[0].CalibStatus)
	assert.Equal(t, model.CalibStatusAbnormal, page.List[1].CalibStatus)
	assert.Equal(t, model.CalibStatusUncalibrated, page.List[2].CalibStatus)
}
