// T389-N1：设备域越权码的「数字字面量」锁定。
//
// 缺陷原貌（卡面 N1）：全仓越权断言一律写成 assert.Equal(t, model.CodeForbidden, code)，
// 比较两侧都是符号 ⇒ 把常量 20403 改成别的值，用例跟着常量一起走，逐格仍绿。
// 而网关与前端是按 20403 这个数字分流错误提示的，于是码值漂移没有任何测试能拦住。
// 本文件把这条码钉成字面量：改常量必红。
//
// 只钉设备域这一条（PM 09-25 口径）：
//   - user-service 的越权码是 10403、file-service 是 60003，本卡不动它们；
//   - 同数字不同契约：data-service 的 CodeDeviceIDMismatch 也是 20403，但它的 HTTP 状态是
//     400、语义是「上报 deviceId 与路径不一致」（见 services/data-service/internal/model/model.go）。
//     故这里锁的是「device-service 的 403 越权响应体」，不是「20403 全仓唯一」这件事；
//     两域各自锁各自的码，谁将来想做跨服务统一码表，得先改掉 data-service 那一格。
package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// t389CodeForbidden / t389HTTPForbidden 刻意写成字面量。
// 禁止替换成 model.CodeForbidden —— 「用符号断言符号」正是本文件要堵掉的形状。
const (
	t389CodeForbidden = 20403
	t389HTTPForbidden = 403
)

// ── 1. 常量层：码值与 HTTP 状态各钉一次 ──────────────────────────────

func TestT389_ForbiddenCodeIsLockedToNumericLiteral(t *testing.T) {
	assert.Equal(t, t389CodeForbidden, model.CodeForbidden,
		"设备域越权业务码契约值（改这一格前先看卡内 N1 说明：网关与前端按此数字分流）")

	appErr := model.ErrForbidden("probe")
	require.NotNil(t, appErr)
	assert.Equal(t, t389CodeForbidden, appErr.Code, "ErrForbidden 必须携带被锁定的业务码")
	assert.Equal(t, t389HTTPForbidden, appErr.HTTPStatus, "越权错误的 HTTP 状态契约值")
}

// ── 2. 线上层：七条写端点的拒绝响应体逐字带 code:20403 ───────────────

// t389RawReq 与 t387Req 同一套请求构造，区别只在返回**原始响应字节**：
// 字面量要么出现在报文里，要么没出现，中间不允许有第二种解释。
func t389RawReq(t *testing.T, r *gin.Engine, method, path, role, uid string, body any) (int, []byte) {
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
		req.Header.Set(headerRole, role)
	}
	if uid != "" {
		req.Header.Set(headerUserID, uid)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

// t389DeniedRolesOnWire 卡面点名的两个非写角色（医护、客服）。
// 患者/身份缺失/未知角色那三格已由 T387 的用例覆盖，这里不重复。
func t389DeniedRolesOnWire() []string {
	return []string{roleDoctor, "ROLE_CS"}
}

func TestT389_WriteDenyWireBodyCarriesLiteralForbiddenCode(t *testing.T) {
	for _, ep := range t387Endpoints() {
		for _, role := range t389DeniedRolesOnWire() {
			t.Run(ep.name+"/"+role, func(t *testing.T) {
				r, st, _ := t387Env(t)
				path, body := ep.prepare(t, st, false)

				status, raw := t389RawReq(t, r, ep.method, path, role, t387DoctorUID, body)

				assert.Equal(t, t389HTTPForbidden, status, "body=%s", string(raw))
				assert.Contains(t, string(raw), `"code":20403`,
					"响应报文原文须含字面 code:20403（只改常量或只改序列化路径都会在这里露出来）")

				var decoded map[string]any
				require.NoError(t, json.Unmarshal(raw, &decoded), "body=%s", string(raw))
				got, ok := decoded["code"]
				require.True(t, ok, "统一响应体须有 code 字段，body=%s", string(raw))
				assert.Equal(t, float64(t389CodeForbidden), got, "code 字段解码后须为 %d", t389CodeForbidden)
			})
		}
	}
}
