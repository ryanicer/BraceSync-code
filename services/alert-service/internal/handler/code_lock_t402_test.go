// T402 甲-1／丁-2：告警域两个收口码的「数字字面量」锁定。
//
// 为什么要钉字面量：本域既有断言全部写成 assert.Equal(t, codeNotFound, env.Code)，
// 比较两侧都是符号 ⇒ 把常量 40404 改成别的数字，用例跟着常量一起走，逐格仍绿。
// 形状照 device-service 的 code_lock_t389_test.go 先例（常量层 + 线上报文层各锁一次）。
//
// 本域未收口的两格（Boss 22:07 勾的是丁-2，只收 403 与 404）：
//   - codeInvalidParam = 400、codeInternalError = 500、codeConflict = 409 仍是裸 HTTP 数字，
//     属有意的中间态，不是漏项；收口留给告警域自己的卡。
//   - envelope 的 Data 字段带 omitempty，错误体里根本没有 data 这一格（其余五服务与网关有）。
//     这条与本卡的 乙-1（文件域补 data）同题，但改的是告警域全部拒绝响应的形状，
//     丁-2 未授权，故此处只登记不修。
package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t402AlertCodeForbidden / t402AlertCodeNotFound 刻意写成字面量。
// 禁止替换成 codeForbidden / codeNotFound —— 「用符号断言符号」正是本文件要堵掉的形状。
const (
	t402AlertCodeForbidden = 40403
	t402AlertCodeNotFound  = 40404
	t402AlertHTTPForbidden = 403
	t402AlertHTTPNotFound  = 404
)

// ── 1. 常量层 ─────────────────────────────────────────────────

func TestT402_AlertForbiddenAndNotFoundCodesAreLocked(t *testing.T) {
	assert.Equal(t, t402AlertCodeForbidden, codeForbidden,
		"告警域越权业务码契约值（域号 4 + 0 + HTTP 三位 403）")
	assert.Equal(t, t402AlertCodeNotFound, codeNotFound,
		"告警域资源不存在业务码契约值（域号 4 + 0 + HTTP 三位 404）")
}

// ── 2. 线上层：拒绝响应报文原文逐字带 code ────────────────────────

// t402RawDenied 打一条 staff-only 端点，返回原始报文（不预解析）。
// 字面量要么在报文里，要么不在，中间不允许有第二种解释。
func t402RawDenied(t *testing.T, role string) (int, string) {
	t.Helper()
	store := &fakePublicStore{}
	rec := doReport(newPublicHandler(store), t300SummaryPath+t300Query, role)
	assert.Zero(t, store.sumHits, "鉴权未过不得查库")
	return rec.Code, rec.Body.String()
}

func TestT402_AlertForbiddenWireBodyCarriesLiteralCode(t *testing.T) {
	for _, role := range []string{"ROLE_PATIENT", "ROLE_CS", ""} {
		status, raw := t402RawDenied(t, role)

		assert.Equal(t, t402AlertHTTPForbidden, status, "HTTP 状态不得随业务码一起改动，body=%s", raw)
		assert.Contains(t, raw, `"code":40403`,
			"响应报文原文须含字面 code:40403（只改常量或只改序列化路径都会在这里露出来）")

		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &decoded), "body=%s", raw)
		assert.Equal(t, float64(t402AlertCodeForbidden), decoded["code"], "code 解码后须为 %d", t402AlertCodeForbidden)
	}
}

func TestT402_AlertNotFoundWireBodyCarriesLiteralCode(t *testing.T) {
	store := &fakePublicStore{exists: false}
	rec := doProcess(newPublicHandler(store), "999")
	raw := rec.Body.String()

	assert.Equal(t, t402AlertHTTPNotFound, rec.Code, "HTTP 状态不得随业务码一起改动，body=%s", raw)
	assert.Contains(t, raw, `"code":40404`, "响应报文原文须含字面 code:40404")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded), "body=%s", raw)
	assert.Equal(t, float64(t402AlertCodeNotFound), decoded["code"], "code 解码后须为 %d", t402AlertCodeNotFound)
}

// ── 3. 反证：未收口的两格仍是裸数字（丁-2 的中间态要有锚点）────────
//
// 这三条不是「期望它们永远不变」，而是：若哪天把告警域其余裸码一起收口（丁-1），
// 这三条会红，逼改动者回到本卡与丁-1 的裁定处对一遍口径。

func TestT402_AlertUnalignedCodesStillBareHTTPNumbers(t *testing.T) {
	assert.Equal(t, 400, codeInvalidParam, "丁-2 只收 403/404；本格未收，值变必红")
	assert.Equal(t, 500, codeInternalError, "丁-2 只收 403/404；本格未收，值变必红")
	assert.Equal(t, 409, codeConflict, "丁-2 只收 403/404；本格未收，值变必红")

	store := &fakePublicStore{}
	rec := doReport(newPublicHandler(store), t300SummaryPath+"?start=2026-09-01&end=2026-09-03", roleAdmin)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":400`, "未收口格仍按裸 HTTP 数字出报文")
}
