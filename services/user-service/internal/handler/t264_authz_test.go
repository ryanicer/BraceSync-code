// Package handler T264：user-service listPlans 水平鉴权（GET /patients/:id/orthosis-plans）
package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// TestT264_ListPlans_Authz 矫形方案列表水平鉴权
func TestT264_ListPlans_Authz(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.patient = &repo.PatientRow{PatientID: "P1"} // T353：列表端点先判患者存在，鉴权用例需有档案行
	e.store.plans = []repo.OrthosisPlanRow{{PlanID: 1, PatientID: "P1"}}
	path := "/api/v1/patients/P1/orthosis-plans"

	// staff 放行
	w, _ := e.do(http.MethodGet, path, nil, map[string]string{"X-Role": roleAdmin, "X-User-Id": "A0001"})
	assert.Equal(t, http.StatusOK, w.Code, "admin 应放行")

	// 患者本人 → 200
	w, _ = e.do(http.MethodGet, path, nil, map[string]string{"X-Role": "patient", "X-User-Id": "P1"})
	assert.Equal(t, http.StatusOK, w.Code, "患者本人应放行")

	// 患者他人 → 403
	w, _ = e.do(http.MethodGet, path, nil, map[string]string{"X-Role": "patient", "X-User-Id": "P2"})
	assert.Equal(t, http.StatusForbidden, w.Code, "患者他人应 403")

	// 缺失身份头 → 403（fail-closed）
	w, _ = e.do(http.MethodGet, path, nil, nil)
	assert.Equal(t, http.StatusForbidden, w.Code, "缺失身份头应 403")
}
