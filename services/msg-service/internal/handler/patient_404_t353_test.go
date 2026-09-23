// Package handler T353：msg-service 三条按 patientId 查询的 GET 端点，
// 「查无此人」要与「有此人但无偏好行/无通知记录」在 HTTP 面上可区分（404 加 10404，
// 口径承接 data-service T340）。患者域水平鉴权必须在存在性判定之前（403 优先，
// 存在性不泄露给无权调用方）。
package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
)

const (
	t353ExistPID = "P20260001"
	t353GonePID  = "P99999999" // FakeStore 未预置档案行 —— 等价于 patients 表无此患者
)

// t353Endpoints 三条只读入口（路径模板共用，患者 ID 为唯一变量）
func t353Endpoints(pid string) []struct {
	name, path string
} {
	return []struct{ name, path string }{
		{"subscription-quota", "/api/v1/patients/" + pid + "/subscription-quota"},
		{"wear-reminder", "/api/v1/patients/" + pid + "/wear-reminder"},
		{"notifications", "/api/v1/patients/" + pid + "/notifications"},
	}
}

// TestT353_Msg_PatientNotFound_Is404 查无此人：三条入口一律 404 加 10404
func TestT353_Msg_PatientNotFound_Is404(t *testing.T) {
	f := newHTTPFixture(t)
	f.store.SeedPatientGone(t353GonePID)

	for _, e := range t353Endpoints(t353GonePID) {
		w, resp := f.do(t, http.MethodGet, e.path, "", map[string]string{
			"X-Role": roleAdmin, "X-User-Id": "ADM001",
		})
		assert.Equal(t, http.StatusNotFound, w.Code, "%s 查无此人应 404", e.name)
		assert.Equal(t, model.CodePatientNotFound, resp.Code, "%s 业务码必须是 10404", e.name)
	}
}

// TestT353_Msg_ExistingPatient_Still200 患者存在：三条入口仍按原语义回 200
// （无偏好行时给默认额度/默认提醒、无记录时给空页 —— 本轮不改这层语义）
func TestT353_Msg_ExistingPatient_Still200(t *testing.T) {
	f := newHTTPFixture(t)

	for _, e := range t353Endpoints(t353ExistPID) {
		w, resp := f.do(t, http.MethodGet, e.path, "", map[string]string{
			"X-Role": roleAdmin, "X-User-Id": "ADM001",
		})
		require.Equal(t, http.StatusOK, w.Code, "%s 存在者必须 200", e.name)
		assert.Equal(t, model.CodeOK, resp.Code)
		assert.NotEmpty(t, resp.Data, "%s 200 应带载荷", e.name)
	}
}

// TestT353_Msg_Authz_BeforeExistence 水平越权优先：患者拿自己 token 枚举「不存在的他人」，
// 必须仍是 403 而不是 404 —— 否则等于把「这个 ID 存不存在」泄露给无权调用方。
func TestT353_Msg_Authz_BeforeExistence(t *testing.T) {
	f := newHTTPFixture(t)
	f.store.SeedPatientGone(t353GonePID)

	for _, e := range t353Endpoints(t353GonePID) {
		w, resp := f.do(t, http.MethodGet, e.path, "", map[string]string{
			"X-Role": "patient", "X-User-Id": t353ExistPID,
		})
		assert.Equal(t, http.StatusForbidden, w.Code, "%s 越权必须 403（先于存在性判定）", e.name)
		assert.Equal(t, model.CodeForbidden, resp.Code)
	}
}
