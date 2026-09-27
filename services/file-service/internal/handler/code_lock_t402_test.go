// T402 甲-1／乙-1：文件域越权码与错误信封 data 格的「数字字面量」锁定。
//
// 为什么要钉字面量：本域既有断言写成 assert.Equal(t, float64(ErrorCodeForbidden), body["code"])，
// 比较的是符号 ⇒ 把 60403 改回 60003 或改成别的值，用例跟着常量走，逐格仍绿。
// 形状照 device-service 的 code_lock_t389_test.go 先例。
//
// 锁的依据是契约（架构 §3.5 域号分段），不是「现网按这个数字分流」：
//   - 网关 ReverseProxy 透传上游响应体，不自产 6xxxx；
//   - 三端按 HTTP 状态分流，全仓对 60003／60403 零消费方（T402 方案条普查）。
package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
	"github.com/bracesync/bracesync/services/file-service/internal/repo"
)

// t402FileCodeForbidden 刻意写成字面量，禁止替换成 ErrorCodeForbidden。
const (
	t402FileCodeForbidden = 60403
	t402FileHTTPForbidden = 403
)

// t402RawGet 打一条真实路由并返回**原始响应字节**：状态码 + 报文原文。
func t402RawGet(t *testing.T, store repo.Store, path, userID, role string) (int, string) {
	t.Helper()
	srv := setupTestServer(store)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("X-User-Id", userID)
	req.Header.Set("X-Role", role)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

// ── 1. 常量层 ─────────────────────────────────────────────────

func TestT402_FileForbiddenCodeIsLockedToNumericLiteral(t *testing.T) {
	assert.Equal(t, t402FileCodeForbidden, ErrorCodeForbidden,
		"文件域越权业务码契约值（域号 6 + 0 + HTTP 三位 403）。旧值 60003 已作废，改回去必红")
}

// ── 2. 线上层：拒绝响应原文逐字带 code:60403 与 data:null ─────────

// t402ForbiddenLegs 两条越权路径：
//   - /files/{id}：患者读他人文件（owner 校验，file_handler.go:294）
//   - /files/{id}/download：同一判定的下载分支（file_handler.go:330）
//
// 两条都走 errorJSON，故 data 格（乙-1）在同一处补齐。
func TestT402_FileForbiddenWireBodyCarriesLiteralCodeAndData(t *testing.T) {
	store := newMemStore()
	store.doctorTeam["DOC-1"] = "TEAM-T402"
	store.patientTeam["P-A"] = "TEAM-T402"
	seedFile(store, "F-T402", "patient", "P-A", model.FileStatusUploaded)

	for _, path := range []string{"/api/v1/files/F-T402", "/api/v1/files/F-T402/download"} {
		t.Run(path, func(t *testing.T) {
			// P-B 未登记团队 ⇒ 与 owner 不同团队，越权成立
			status, raw := t402RawGet(t, store, path, "P-B", "patient")

			assert.Equal(t, t402FileHTTPForbidden, status, "HTTP 状态不得随业务码一起改动，body=%s", raw)
			assert.Contains(t, raw, `"code":60403`,
				"响应报文原文须含字面 code:60403（只改常量或只改序列化路径都会在这里露出来）")
			// 乙-1：错误体补齐 data 格，前端统一取 res.data 才不至于拿到 undefined
			assert.Contains(t, raw, `"data":null`, "统一错误信封须含 data 字段（T402 乙-1）")

			var decoded map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &decoded), "body=%s", raw)
			code, ok := decoded["code"]
			require.True(t, ok, "统一响应体须有 code 字段，body=%s", raw)
			assert.Equal(t, float64(t402FileCodeForbidden), code, "code 解码后须为 %d", t402FileCodeForbidden)
			assert.Contains(t, decoded, "data", "解析后的信封要有 data 这一格（值可为 null）")
			assert.Nil(t, decoded["data"])
		})
	}
}
