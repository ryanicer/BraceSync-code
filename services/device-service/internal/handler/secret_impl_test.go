// Package handler — internal 验签密钥端点实现侧测试（T032；不与 Ella handler_http_test.go 重叠）
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

func TestGetSecretHTTP_RegisteredDevice(t *testing.T) {
	env := newTestEnv(t)
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-SEC-001"}, nil)
	require.Equal(t, model.CodeOK, resp.Code)

	status, resp := env.do(t, http.MethodGet, "/internal/devices/DEV-SEC-001/secret", nil, nil)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.CodeOK, resp.Code)

	var data struct {
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Len(t, data.Secret, 64, "32 字节随机密钥 hex 编码后 64 字符")

	// 幂等：重复查询返回同一密钥（注册不得覆盖既有密钥）
	_, resp2 := env.do(t, http.MethodGet, "/internal/devices/DEV-SEC-001/secret", nil, nil)
	var data2 struct {
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(resp2.Data, &data2))
	assert.Equal(t, data.Secret, data2.Secret)
}

func TestGetSecretHTTP_UnknownDevice_20404(t *testing.T) {
	env := newTestEnv(t)
	status, resp := env.do(t, http.MethodGet, "/internal/devices/DEV-NOPE/secret", nil, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeNotFound, resp.Code)
}
