// Package handler 实现侧测试（T122）：安装记录单条查询/更新 HTTP 用例
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// TestInstallRecordGetById_GET_OK 新建后 GET /:id 返回详情
func TestInstallRecordGetById_GET_OK(t *testing.T) {
	env := newTestEnv(t)
	env.store.AddPatient("P-300")
	env.store.AddTech("TECH-3")

	_, _ = env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-G-001"}, nil)
	_, _ = env.do(t, http.MethodPost, "/api/v1/devices/DEV-G-001/bind",
		map[string]string{"patientId": "P-300"}, map[string]string{"X-User-Id": "TECH-3"})

	status, resp := env.do(t, http.MethodPost, "/api/v1/install-records", map[string]string{
		"deviceId": "DEV-G-001", "patientId": "P-300", "techId": "TECH-3",
	}, nil)
	require.Equal(t, http.StatusOK, status)
	var created struct {
		InstallID string `json:"installId"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &created))
	require.NotEmpty(t, created.InstallID)

	// GET /:id → 200 + 详情
	status, resp = env.do(t, http.MethodGet, "/api/v1/install-records/"+created.InstallID, nil, nil)
	require.Equal(t, http.StatusOK, status)
	var detail installDetailDTO
	require.NoError(t, json.Unmarshal(resp.Data, &detail))
	assert.Equal(t, created.InstallID, detail.InstallID)
	assert.Equal(t, "DEV-G-001", detail.DeviceID)
	assert.Equal(t, "P-300", detail.PatientID)
	assert.Equal(t, "TECH-3", detail.TechID)
	assert.Equal(t, "unconfigured", detail.WifiStatus)
}

// TestInstallRecordGetById_NotFound 不存在 id → 20404
func TestInstallRecordGetById_NotFound(t *testing.T) {
	env := newTestEnv(t)
	status, resp := env.do(t, http.MethodGet, "/api/v1/install-records/999999", nil, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeNotFound, resp.Code)
}

// TestInstallRecordGetById_InvalidId 非数字 id → 20400
func TestInstallRecordGetById_InvalidId(t *testing.T) {
	env := newTestEnv(t)
	status, resp := env.do(t, http.MethodGet, "/api/v1/install-records/abc", nil, nil)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

// TestInstallRecordUpdateMeta_PUT_OK 更新 notes/signatureUrl 后 GET 验证回填
func TestInstallRecordUpdateMeta_PUT_OK(t *testing.T) {
	env := newTestEnv(t)
	env.store.AddPatient("P-301")
	env.store.AddTech("TECH-4")

	_, _ = env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-G-002"}, nil)
	_, _ = env.do(t, http.MethodPost, "/api/v1/devices/DEV-G-002/bind",
		map[string]string{"patientId": "P-301"}, map[string]string{"X-User-Id": "TECH-4"})

	_, resp := env.do(t, http.MethodPost, "/api/v1/install-records", map[string]string{
		"deviceId": "DEV-G-002", "patientId": "P-301", "techId": "TECH-4",
	}, nil)
	var created struct {
		InstallID string `json:"installId"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &created))

	// PUT 回填 notes + signatureUrl
	status, resp := env.do(t, http.MethodPut, "/api/v1/install-records/"+created.InstallID,
		map[string]string{"notes": "matrix 校准完成", "signatureUrl": "cos://sig/2.png"}, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.CodeOK, resp.Code)

	// GET 验证回填生效
	_, resp = env.do(t, http.MethodGet, "/api/v1/install-records/"+created.InstallID, nil, nil)
	var detail installDetailDTO
	require.NoError(t, json.Unmarshal(resp.Data, &detail))
	assert.Equal(t, "matrix 校准完成", detail.Notes)
	assert.Equal(t, "cos://sig/2.png", detail.SignatureURL)
}

// TestInstallRecordUpdateMeta_NotFound 不存在 id → 20404
func TestInstallRecordUpdateMeta_NotFound(t *testing.T) {
	env := newTestEnv(t)
	status, resp := env.do(t, http.MethodPut, "/api/v1/install-records/999999",
		map[string]string{"notes": "x"}, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeNotFound, resp.Code)
}

// TestInstallRecordUpdateMeta_InvalidId 非数字 id → 20400
func TestInstallRecordUpdateMeta_InvalidId(t *testing.T) {
	env := newTestEnv(t)
	status, resp := env.do(t, http.MethodPut, "/api/v1/install-records/abc",
		map[string]string{"notes": "x"}, nil)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}
