// T350：alert-service 医护数据范围（GET /api/v1/alerts、GET /admin/abnormal-reports）。
//
// 缺陷原貌：listAlerts 的「强制覆盖 patientId」分支只对非 staff 生效（public.go:177-185），
// 医护作为 staff 走全量 ⇒ T341 现网实测医生告警首页 10 行患者一条不属他自己的团队。
package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func t350AlertReq(store *fakePublicStore, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	newPublicHandler(store).Router().ServeHTTP(rec, req)
	return rec
}

// TestT350_ListAlerts_DoctorScopedToOwnTeam 医护：filter 带自身团队，且 store 被查到身份
func TestT350_ListAlerts_DoctorScopedToOwnTeam(t *testing.T) {
	store := &fakePublicStore{doctorTeam: "TEAM01", doctorTeamOK: true}

	rec := t350AlertReq(store, "/api/v1/alerts", map[string]string{
		headerRole: roleDoctor, headerUserID: "ADM-D001",
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, store.doctorTeamHits, "团队须由身份推导")
	assert.True(t, store.filter.TeamScoped)
	assert.Equal(t, "TEAM01", store.filter.TeamID)
}

// TestT350_ListAlerts_DoctorWithoutTeamFailsClosed 无团队归属：TeamScoped 保留、团队为空（空集）
func TestT350_ListAlerts_DoctorWithoutTeamFailsClosed(t *testing.T) {
	store := &fakePublicStore{} // doctors 无行

	rec := t350AlertReq(store, "/api/v1/alerts", map[string]string{
		headerRole: roleDoctor, headerUserID: "ADM-D002",
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, store.filter.TeamScoped, "无团队不等于不受限")
	assert.Equal(t, "", store.filter.TeamID)
}

// TestT350_ListAlerts_DoctorMissingUserID_403 身份缺失 fail-closed，不触达 ListAlerts
func TestT350_ListAlerts_DoctorMissingUserID_403(t *testing.T) {
	store := &fakePublicStore{doctorTeam: "TEAM01", doctorTeamOK: true}

	rec := t350AlertReq(store, "/api/v1/alerts", map[string]string{headerRole: roleDoctor})

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 0, store.listHits, "鉴权未过不得查库")
}

// TestT350_ListAlerts_TeamLookupError_500 推导失败不得退化成全量
func TestT350_ListAlerts_TeamLookupError_500(t *testing.T) {
	store := &fakePublicStore{doctorTeamErr: errors.New("db down")}

	rec := t350AlertReq(store, "/api/v1/alerts", map[string]string{
		headerRole: roleDoctor, headerUserID: "ADM-D003",
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 0, store.listHits)
}

// TestT350_ListAlerts_StaffRolesUnchanged 只收紧不放宽：admin / cs / technician 路径逐字不变
func TestT350_ListAlerts_StaffRolesUnchanged(t *testing.T) {
	for _, role := range []string{roleAdmin, "ROLE_CS", "technician"} {
		t.Run(role, func(t *testing.T) {
			store := &fakePublicStore{doctorTeam: "TEAM01", doctorTeamOK: true}

			rec := t350AlertReq(store, "/api/v1/alerts?patientId=P-X", map[string]string{
				headerRole: role, headerUserID: "ADM-001",
			})

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, 0, store.doctorTeamHits, "%s 不该触发团队推导", role)
			assert.False(t, store.filter.TeamScoped)
			assert.Equal(t, "P-X", store.filter.PatientID, "%s 保留 ?patientId= 过滤", role)
		})
	}
}

// TestT350_ListAlerts_PatientBindingStillWins 患者域既有行为不回退：
// 非 staff 仍强制 patientId=X-User-Id，且不受团队推导影响（患者无 doctors 行）
func TestT350_ListAlerts_PatientBindingStillWins(t *testing.T) {
	store := &fakePublicStore{doctorTeam: "TEAM01", doctorTeamOK: true}

	rec := t350AlertReq(store, "/api/v1/alerts?patientId=OTHER", map[string]string{
		headerRole: "patient", headerUserID: "P-SELF",
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "P-SELF", store.filter.PatientID)
	assert.False(t, store.filter.TeamScoped)
	assert.Equal(t, 0, store.doctorTeamHits)
}

// TestT350_AbnormalReports_DoctorScopedToOwnTeam 异常报告聚合同口径收口
func TestT350_AbnormalReports_DoctorScopedToOwnTeam(t *testing.T) {
	store := &fakePublicStore{doctorTeam: "TEAM01", doctorTeamOK: true}

	rec := t350AlertReq(store,
		"/api/v1/admin/abnormal-reports?patientId=P9&type=pressure_high&start=2026-09-01&end=2026-09-07",
		map[string]string{headerRole: roleDoctor, headerUserID: "ADM-D001"})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, store.sumFilter.TeamScoped)
	assert.Equal(t, "TEAM01", store.sumFilter.TeamID)
	assert.Equal(t, "P9", store.sumFilter.PatientID, "原有 patientId 过滤不受影响")
}

// TestT350_AbnormalReports_AdminUnscoped 运营口径不变（全院）
func TestT350_AbnormalReports_AdminUnscoped(t *testing.T) {
	store := &fakePublicStore{doctorTeam: "TEAM01", doctorTeamOK: true}

	rec := t350AlertReq(store,
		"/api/v1/admin/abnormal-reports?patientId=P9&start=2026-09-01&end=2026-09-07",
		map[string]string{headerRole: roleAdmin, headerUserID: "ADM-001"})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, store.sumFilter.TeamScoped)
	assert.Equal(t, 0, store.doctorTeamHits)
}
