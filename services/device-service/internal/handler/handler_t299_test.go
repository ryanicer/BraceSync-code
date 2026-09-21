// Package handler T299「一患者一设备」HTTP 层：被拒时对外 409/20409 加可读文案
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

func TestBindHTTP_PatientHasOtherDevice_Conflict(t *testing.T) {
	env := newTestEnv(t)
	env.store.AddPatient("P-T299-H")
	for _, devID := range []string{"DEV-T299-H1", "DEV-T299-H2"} {
		_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": devID}, nil)
		require.Equal(t, model.CodeOK, resp.Code)
	}

	status, resp := env.do(t, http.MethodPost, "/api/v1/devices/DEV-T299-H1/bind",
		map[string]string{"patientId": "P-T299-H"}, map[string]string{"X-User-Id": "TECH-H"})
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, model.CodeOK, resp.Code)

	status, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-T299-H2/bind",
		map[string]string{"patientId": "P-T299-H"}, map[string]string{"X-User-Id": "TECH-H"})
	assert.Equal(t, http.StatusConflict, status, "患者已有生效设备时再绑须 409")
	assert.Equal(t, model.CodeConflict, resp.Code)
	assert.Contains(t, resp.Message, "DEV-T299-H1", "message 须给出占位设备，供前端提示先解绑")

	// 被拒后原绑定完好：H1 仍在该患者名下，H2 未绑定
	_, got := env.do(t, http.MethodGet, "/api/v1/devices/DEV-T299-H1", nil, nil)
	var dev1 model.DeviceDTO
	require.NoError(t, json.Unmarshal(got.Data, &dev1))
	require.NotNil(t, dev1.PatientID)
	assert.Equal(t, "P-T299-H", *dev1.PatientID)

	_, got = env.do(t, http.MethodGet, "/api/v1/devices/DEV-T299-H2", nil, nil)
	var dev2 model.DeviceDTO
	require.NoError(t, json.Unmarshal(got.Data, &dev2))
	assert.Nil(t, dev2.PatientID)
}
