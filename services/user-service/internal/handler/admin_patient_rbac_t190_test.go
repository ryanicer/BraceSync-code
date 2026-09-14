// Package handler — T190：后台患者管理 5 个写端点的 handler 层角色判定（纵深防御）。
//
// gateway RBAC 矩阵（services/gateway/cmd/server/rbac.go）已在入口收口，本文件验证
// 第二层：即便请求绕过网关直连 user-service（内网扁平、容器互通），非 admin 角色
// 也必须 403，且不得触达 store（不产生任何写库/审计副作用）。
package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// adminHdr 模拟 gateway 为 ROLE_ADMIN JWT 注入的身份头
var adminHdr = map[string]string{"X-Role": roleAdmin}

// t190WriteCases 5 个写端点的请求方法/路径/请求体（body 为合法值，用于验证 admin 放行）
type t190WriteCase struct {
	name   string
	method string
	path   string
	body   any
}

func t190WriteCases() []t190WriteCase {
	return []t190WriteCase{
		{"createPatient", http.MethodPost, "/api/v1/admin/patients",
			model.CreatePatientRequestDTO{Name: "患者小明", Phone: "13800138000"}},
		{"batchBind", http.MethodPost, "/api/v1/admin/patients/batch-bind",
			model.BatchBindRequestDTO{PatientIDs: []string{"P20260001"}, TeamID: "TEAM01"}},
		{"assignTeam", http.MethodPut, "/api/v1/admin/patients/P20260001/team",
			model.AssignTeamRequestDTO{TeamID: "TEAM02"}},
		{"unbindWechat", http.MethodPost, "/api/v1/admin/patients/P20260001/unbind-wechat", nil},
		{"updatePhone", http.MethodPut, "/api/v1/admin/patients/P20260001/phone",
			map[string]string{"phone": "13900139000", "reason": "换号"}},
	}
}

// TestT190_AdminPatientWrites_NonAdminForbidden 非 admin 角色（含缺失 X-Role）→ 403 + store 零触达
func TestT190_AdminPatientWrites_NonAdminForbidden(t *testing.T) {
	lowRoles := []struct {
		name    string
		headers map[string]string
	}{
		{"patient", map[string]string{"X-Role": "patient", "X-User-Id": "P20260002"}},
		{"doctor", map[string]string{"X-Role": "ROLE_DOCTOR", "X-User-Id": "D0001"}},
		{"cs", map[string]string{"X-Role": "ROLE_CS", "X-User-Id": "CS001"}},
		{"technician", map[string]string{"X-Role": "technician", "X-User-Id": "TECH001"}},
		{"missing-role-header", map[string]string{"X-User-Id": "P20260002"}},
		{"forged-self-id", map[string]string{"X-Role": "patient", "X-User-Id": "P20260001"}},
	}

	for _, lr := range lowRoles {
		for _, c := range t190WriteCases() {
			t.Run(c.name+"/"+lr.name, func(t *testing.T) {
				e := newEnv(t, true, true)
				// 放通业务前置数据：若鉴权失效，这些 fixture 会让请求一路写到底
				p := samplePatient()
				e.store.patient = &p
				e.store.createdPatient = &p
				e.store.assignedPatient = &p
				e.store.batchBindResult = &repo.BatchBindResult{}

				w, resp := e.do(c.method, c.path, c.body, lr.headers)

				assert.Equal(t, http.StatusForbidden, w.Code, "%s 角色 %s 应 403", lr.name, c.name)
				assert.Equal(t, model.CodeForbidden, resp.Code)

				assert.Empty(t, e.store.lastCreateInput.Name, "不得触达 store.CreatePatient")
				assert.Empty(t, e.store.lastAssignPatient, "不得触达 store.AssignPatientTeam")
				assert.Empty(t, e.store.lastBatchIDs, "不得触达 store.BatchBindPatients")
				assert.Empty(t, e.store.lastPatientQuery, "不得触达 store.GetPatient")
			})
		}
	}
}

// TestT190_AdminPatientWrites_AdminAllowed ROLE_ADMIN → 业务语义不变（各端点仍 200）
func TestT190_AdminPatientWrites_AdminAllowed(t *testing.T) {
	for _, c := range t190WriteCases() {
		t.Run(c.name+"/admin", func(t *testing.T) {
			e := newEnv(t, true, true)
			p := samplePatient()
			e.store.patient = &p
			e.store.createdPatient = &p
			e.store.assignedPatient = &p
			e.store.batchBindResult = &repo.BatchBindResult{}

			w, resp := e.do(c.method, c.path, c.body, adminHdr)

			assert.Equal(t, http.StatusOK, w.Code, "ROLE_ADMIN %s 应 200，body=%s", c.name, w.Body.String())
			assert.Equal(t, model.CodeOK, resp.Code)
		})
	}
}

// TestT190_AdminPatientReads_NoHandlerLayerCheck 读端点在 handler 层不设角色判定
// （由 gateway staff-only 矩阵负责）——固定现状，避免后续误以为读也有兜底。
func TestT190_AdminPatientReads_NoHandlerLayerCheck(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients", nil, map[string]string{"X-Role": "patient"})
	assert.Equal(t, http.StatusOK, w.Code, "读端点 handler 层无角色判定（现状记录，网关层已 403）")
	assert.Equal(t, model.CodeOK, resp.Code)
}
