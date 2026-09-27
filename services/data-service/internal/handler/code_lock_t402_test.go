// T402 甲-1：数据域越权码的「数字字面量」锁定。
//
// 为什么要钉字面量：本域既有断言写成 assert.Equal(t, model.CodeForbidden, code)，
// 两侧都是符号 ⇒ 把 30403 改回裸 HTTP 的 403、或改成别的值，用例跟着常量走，逐格仍绿。
// 形状照 device-service 的 code_lock_t389_test.go 先例（常量层 + 线上报文层各锁一次）。
//
// 本域另有一格未收口（属 T402 丙-1，缓动）：CodeDeviceIDMismatch = 20403 与设备域撞值。
// 它的 HTTP 状态是 400、语义是「上报 deviceId 与路径不一致」，Boss 22:07 裁定要先由
// Andy 或硬件方只读确认设备直连通道有无按 20403 数字含义分支，确认前不动它。
// 本文件不锁那一格，只锁 CodeForbidden。
package handler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// t402DataCodeForbidden 刻意写成字面量，禁止替换成 model.CodeForbidden。
const (
	t402DataCodeForbidden = 30403
	t402DataHTTPForbidden = 403
)

// ── 1. 常量层：码值与 HTTP 状态各钉一次 ─────────────────────────

func TestT402_DataForbiddenCodeIsLockedToNumericLiteral(t *testing.T) {
	assert.Equal(t, t402DataCodeForbidden, model.CodeForbidden,
		"数据域越权业务码契约值（域号 3 + 0 + HTTP 三位 403）")

	appErr := model.ErrForbidden("probe")
	require.NotNil(t, appErr)
	assert.Equal(t, t402DataCodeForbidden, appErr.Code, "ErrForbidden 必须携带被锁定的业务码")
	assert.Equal(t, t402DataHTTPForbidden, appErr.HTTPStatus, "越权错误的 HTTP 状态契约值（甲-1 不动状态码）")
}

// ── 2. 线上层：拒绝响应报文原文逐字带 code:30403 ─────────────────

func TestT402_DataForbiddenWireBodyCarriesLiteralCode(t *testing.T) {
	lookup := t350Lookup()
	srv := t340Server(lookup, true)

	w := doDataReq(t, srv.router, "GET", "/api/v1/patients/P-OTHER/realtime", roleDoctor, t350Admin)
	raw := w.Body.String()

	assert.Equal(t, t402DataHTTPForbidden, w.Code, "HTTP 状态不得随业务码一起改动，body=%s", raw)
	assert.Contains(t, raw, `"code":30403`,
		"响应报文原文须含字面 code:30403（只改常量或只改序列化路径都会在这里露出来）")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded), "body=%s", raw)
	got, ok := decoded["code"]
	require.True(t, ok, "统一响应体须有 code 字段，body=%s", raw)
	assert.Equal(t, float64(t402DataCodeForbidden), got, "code 解码后须为 %d", t402DataCodeForbidden)
}
