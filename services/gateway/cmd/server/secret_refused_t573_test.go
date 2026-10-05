// Package main — T573：密钥查询「上游答了但拒绝」与「上游没答上」分叉成两支的实测。
//
// 现网读数（2026-10-05，本席自己复跑 3 次，形态稳定）：同一时刻、同一条路由上，
// 两台在册设备走到窗判定（401/20402），一台在册设备返 502/`"data":null`，
// 未注册设备返 401/20404 —— 502 是**按设备**的，而旧写法只有「device-service unavailable」
// 这一支，把单台设备的密钥材料问题报成了网关兜底的/upstream 不可用。
// 本文件钉的是分叉之后的两条腿：拒绝支 → 401/20401 + 判据；没答上支 → 仍是 502。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refusalEnvelope device-service 那一侧错误响应的线上形状：
// handler.fail 把 AppError.Data 放进统一响应体的 data 字段，网关按同一个键读回来。
const refusalEnvelope = `{"code":90001,"message":"服务暂时不可用","data":{"reason":"device_secret_unusable"}}`

// noReasonEnvelope 同为 90001，但没有 reason —— 例如设备服务自己的数据库故障，
// 那是全站状态，网关不许替它宣布「这是这台设备的问题」，仍走 502。
const noReasonEnvelope = `{"code":90001,"message":"服务暂时不可用","data":null}`

func TestSecretProvider_T573_AnsweredRefusal_CarriesReason(t *testing.T) {
	srv, _ := startSecretBackend(t, func(deviceID string) (int, string) {
		return http.StatusInternalServerError, refusalEnvelope
	})
	p := newDeviceServiceSecretProvider(srv.URL)

	_, err := p.GetDeviceSecret(context.Background(), "PRS-ML05-RC-20260701003")
	require.Error(t, err)

	var refused *SecretRefusedError
	require.True(t, errors.As(err, &refused), "上游答了的那一支必须是 SecretRefusedError，实际类型 %T", err)
	assert.Equal(t, 90001, refused.Code)
	assert.True(t, refused.SecretUnusable, "带 reason 的拒绝必须标成密钥不可用")
	assert.Contains(t, err.Error(), "code=90001", "错误文本形状不变（既有断言不许被分叉改掉）")
}

func TestSecretProvider_T573_AnsweredWithoutReason_IsNotClaimedUnusable(t *testing.T) {
	srv, _ := startSecretBackend(t, func(deviceID string) (int, string) {
		return http.StatusInternalServerError, noReasonEnvelope
	})
	p := newDeviceServiceSecretProvider(srv.URL)

	_, err := p.GetDeviceSecret(context.Background(), "PRS-ML05-RC-20260701003")
	require.Error(t, err)

	var refused *SecretRefusedError
	require.True(t, errors.As(err, &refused))
	assert.False(t, refused.SecretUnusable, "没给 reason 就不许升级成设备侧永久结论")
}

func TestSecretProvider_T573_EmptySecretStillRefused(t *testing.T) {
	srv, _ := startSecretBackend(t, func(deviceID string) (int, string) {
		return http.StatusOK, `{"code":0,"message":"success","data":{"secret":""}}`
	})
	p := newDeviceServiceSecretProvider(srv.URL)

	_, err := p.GetDeviceSecret(context.Background(), "DEV-A")
	require.Error(t, err, "空密钥仍是异常")
	assert.Contains(t, err.Error(), "code=0")
}

// TestSecretProvider_T573_BackendDown_NotRefused 负对照：不可达那一支不许被分叉吸进来，
// 否则真断流会被网关判成「这台设备的密钥有问题」，客户端从此不再重试。
func TestSecretProvider_T573_BackendDown_NotRefused(t *testing.T) {
	p := newDeviceServiceSecretProvider("http://127.0.0.1:1")

	_, err := p.GetDeviceSecret(context.Background(), "DEV-A")
	require.Error(t, err)
	var refused *SecretRefusedError
	assert.False(t, errors.As(err, &refused), "没答上不是拒绝：%v", err)
	assert.Contains(t, err.Error(), "query device-service secret")
}

func deviceAuthData(t *testing.T, body string) map[string]any {
	t.Helper()
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &envelope), "响应体必须是统一结构：%s", body)
	return envelope.Data
}

func TestDeviceSig_T573_SecretUnusable_Gives401Not502(t *testing.T) {
	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL,
		&fakeSecretsCtx{err: &SecretRefusedError{Code: 90001, Message: "服务暂时不可用", SecretUnusable: true}})

	body := `{}`
	hdrs := deviceHeaders("s", "POST", "/api/v1/device/records", body, time.Now())
	code, respBody := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body, hdrs)

	assert.Equal(t, http.StatusUnauthorized, code, "密钥不可用是设备身份不通过，不是网关不可用")
	data := deviceAuthData(t, respBody)
	assert.Equal(t, 20401, dataCode(respBody), "对外码回到既有 20401（客户端契约不动）")
	assert.Equal(t, "secret_unusable", data["device_auth"], "分叉判据必须留在响应面")
	assert.Equal(t, float64(90001), data["upstream_code"], "上游回执码要看得见，否则值班席仍只能靠日志分叉")
	assert.Empty(t, *received, "没验签的请求不得转发到 data-service")
}

func dataCode(body string) int {
	var envelope struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal([]byte(body), &envelope)
	return envelope.Code
}

// TestSecretProviderToDeviceSig_T573_EndToEnd 正对照：把真实 provider 接到真实路由上，
// 让 device-service 响应体的 data.reason 一路走到网关的分支判定。
// 上面那两支分别钉了 provider 与中间件，中间那段 JSON 键名对齐（跨服务边界的口头契约）
// 只有这一支会跑到——reason 键名写歪一位，这里就红。
func TestSecretProviderToDeviceSig_T573_EndToEnd(t *testing.T) {
	deviceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(refusalEnvelope))
	}))
	t.Cleanup(deviceSrv.Close)

	backend, received := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL, newDeviceServiceSecretProvider(deviceSrv.URL))

	body := `{}`
	hdrs := deviceHeaders("s", "POST", "/api/v1/device/records", body, time.Now())
	code, respBody := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body, hdrs)

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, 20401, dataCode(respBody))
	assert.Equal(t, "secret_unusable", deviceAuthData(t, respBody)["device_auth"])
	assert.Empty(t, *received)
}
