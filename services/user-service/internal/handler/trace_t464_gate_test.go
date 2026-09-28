// T464 双通道门禁（用户域）：错误响应带 trace、技术文本只进日志、成功响应形状不变。
package handler

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
)

// t464GeneratedRe 服务侧自造的关联号形状（网关没透传进来时）。
var t464GeneratedRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// TestT464_UserForbiddenCarriesTraceCorrelatedWithLog 越权 403：trace 两键齐、
// message 是中文短句，「为什么拒」的英文技术文本只能按关联号从日志反查。
func TestT464_UserForbiddenCarriesTraceCorrelatedWithLog(t *testing.T) {
	e := newEnv(t, true, true)
	w, _ := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil), selfHdr("P20260002", "patient"))

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	t464UserMessage(t, w, model.CodeForbidden)
	assert.Zero(t, e.store.feelingSaveCalls, "越权请求不得触库")
	t464TechLogContains(t, w, "may only access your own data")
}

// TestT464_TechnicalTextNeverReachesBody 判定①：响应体里不得再出现英文技术文本。
func TestT464_TechnicalTextNeverReachesBody(t *testing.T) {
	e := newEnv(t, true, true)
	w, _ := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil), selfHdr("P20260002", "patient"))

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "may only access your own data")
	assert.NotContains(t, w.Body.String(), "feeling")
	assert.Contains(t, w.Body.String(), model.UserText(model.CodeForbidden))
}

// TestT464_InboundRequestIDReused 网关透传的关联号原样回传（跨跳对齐的前提）。
func TestT464_InboundRequestIDReused(t *testing.T) {
	e := newEnv(t, true, true)
	hdr := selfHdr("P20260002", "patient")
	hdr[HeaderRequestID] = "usr-t464-0001"
	w, _ := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs", t188Body(nil), hdr)

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Equal(t, "usr-t464-0001", t464RequestID(t, w))
	t464TechLogContains(t, w, "may only access your own data")
}

// TestT464_GeneratedRequestIDShape 无入站关联号时服务自造 16 位十六进制。
func TestT464_GeneratedRequestIDShape(t *testing.T) {
	e := newEnv(t, true, true)
	w, _ := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil), selfHdr("P20260002", "patient"))

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Regexp(t, t464GeneratedRe, t464RequestID(t, w))
}

// TestT464_SuccessBodyKeepsShape 成功响应不得出现 trace 键（omitempty 后与改前逐字节同形）。
func TestT464_SuccessBodyKeepsShape(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feelingSaved = t188SavedRow("胸椎")
	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil), selfHdr(t188Patient, "patient"))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), `"trace"`, "成功响应不该带 trace：%s", w.Body.String())
	assert.Equal(t, "success", resp.Message)
}
