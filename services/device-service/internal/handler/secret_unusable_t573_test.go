// Package handler — T573：internal 密钥端点在「密钥材料不可用」那一支的**线上响应体形状**。
//
// 网关按 secretEnvelope.Data{secret,reason} 这两个键名读回来（services/gateway/cmd/server/secret_provider.go），
// 那是一份没有编译期约束的口头契约：这里用同名 struct 反解一次，键名歪一位就红。
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// secretDataShape 与网关侧 secretEnvelope.Data 同名同形的反解夹具
type secretDataShape struct {
	Secret string `json:"secret"`
	Reason string `json:"reason"`
}

func TestGetSecretHTTP_T573_PlaceholderSecret_DataCarriesReason(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.store.RegisterDevice(context.Background(), &model.Device{
		DeviceID:        "PRS-ML05-RC-20260701003",
		DeviceSecretEnc: []byte{0x00}, // seed 占位形状，短于 GCM nonce
	})
	require.NoError(t, err)

	status, resp := env.do(t, http.MethodGet, "/internal/devices/PRS-ML05-RC-20260701003/secret", nil, nil)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, model.CodeInternal, resp.Code)

	var data secretDataShape
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Empty(t, data.Secret, "拒绝支不得漏出任何密钥形状")
	assert.Equal(t, model.ReasonDeviceSecretUnusable, data.Reason)

	// 对照：未注册那一支仍是 20404，没被这次改动带跑
	status2, resp2 := env.do(t, http.MethodGet, "/internal/devices/DEV-T573-NOPE/secret", nil, nil)
	assert.Equal(t, http.StatusNotFound, status2)
	assert.Equal(t, model.CodeNotFound, resp2.Code)

	var nope secretDataShape
	require.NoError(t, json.Unmarshal(resp2.Data, &nope))
	assert.Empty(t, nope.Reason, "20404 一支不附 reason，网关那侧必须继续走 20404")
}
