// Package handler T353：user-service 三条按 patientId 查询的列表端点补患者存在性判定，
// 「查无此人」回 404 加 10404（user-service CodeNotFound 本域即 10404），
// 与「有此人但无方案/无日志/无复查记录」的 200 空列表可区分。
// 水平鉴权必须先于存在性判定（403 优先，存在性不泄露给无权调用方）。
package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// t353UserPaths 三条列表入口（GET，患者 ID 为唯一变量）
func t353UserPaths(pid string) []struct{ name, path string } {
	return []struct{ name, path string }{
		{"orthosis-plans", "/api/v1/patients/" + pid + "/orthosis-plans"},
		{"feeling-logs", "/api/v1/patients/" + pid + "/feeling-logs"},
		{"review-records", "/api/v1/patients/" + pid + "/review-records"},
	}
}

// TestT353_User_PatientNotFound_Is404 fake store 未预置任何患者档案行 —— 三条入口一律 404 加 10404
func TestT353_User_PatientNotFound_Is404(t *testing.T) {
	e := newEnv(t, false, false)

	for _, p := range t353UserPaths("P99999999") {
		w, resp := e.do(http.MethodGet, p.path, nil, map[string]string{"X-Role": roleAdmin, "X-User-Id": "ADM001"})
		assert.Equal(t, http.StatusNotFound, w.Code, "%s 查无此人应 404", p.name)
		assert.Equal(t, model.CodeNotFound, resp.Code, "%s 业务码必须是 10404", p.name)
	}
}

// TestT353_User_ExistingPatient_EmptyList_Still200 患者存在但一条数据都没有：仍是 200 空数组
func TestT353_User_ExistingPatient_EmptyList_Still200(t *testing.T) {
	e := newEnv(t, false, false)
	e.store.patient = &repo.PatientRow{PatientID: "P0000001"}

	for _, p := range t353UserPaths("P0000001") {
		w, resp := e.do(http.MethodGet, p.path, nil, map[string]string{"X-Role": roleAdmin, "X-User-Id": "ADM001"})
		require.Equal(t, http.StatusOK, w.Code, "%s 存在者必须 200", p.name)
		assert.Equal(t, model.CodeOK, resp.Code)
		assert.Equal(t, "[]", string(resp.Data), "%s 无数据应回空数组而非 null", p.name)
	}
}

// TestT353_User_Authz_BeforeExistence 患者 token 枚举「不存在的他人」→ 403，不得因存在性判定改判 404
func TestT353_User_Authz_BeforeExistence(t *testing.T) {
	e := newEnv(t, false, false)
	e.store.patient = &repo.PatientRow{PatientID: "P0000001"} // 只本人有档案行

	for _, p := range t353UserPaths("P99999999") {
		w, resp := e.do(http.MethodGet, p.path, nil, map[string]string{"X-Role": "patient", "X-User-Id": "P0000001"})
		assert.Equal(t, http.StatusForbidden, w.Code, "%s 越权必须 403（先于存在性判定）", p.name)
		assert.Equal(t, model.CodeForbidden, resp.Code)
	}
}
