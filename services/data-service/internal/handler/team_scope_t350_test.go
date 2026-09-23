// Package handler T350：data-service 侧「医护仅本团队患者」范围收口用例。
//
// 缺陷原貌（T340 现网复验 C4，staging 一手）：医生令牌 doctor_li 用
// GET /patients/P20260003/realtime 读到 TEAM02 患者的真帧数据 —— assertAdminOrSelf
// 把 staff 当一个整体放行，医生因此继承了运营的跨患者读能力。
//
// 本文件守两条方向：
//  1. 只收紧：ROLE_DOCTOR 跨团队 → 403；ROLE_ADMIN / ROLE_CS / 技师 / 患者本人的响应一字不变；
//  2. fail-closed：身份头缺失、推导报错、医生无团队，三种都不得退化成放行或全院聚合。
package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/service"
)

const (
	t350TeamA    = "TEAM-A"
	t350TeamB    = "TEAM-B"
	t350Admin    = "ADM-DOC-T350" // 有医生档案，属 TEAM-A
	t350NoTeam   = "ADM-DOC-NOTEAM"
	t350Operator = "ADM-OP-T350"
)

// t350PatientScope 三条患者域读路径（与 dashboard 的聚合路径分开装配）
func t350PatientScope(t *testing.T, lookup *stubPatientLookup) *testServer {
	t.Helper()
	return t340Server(lookup, true)
}

func t350Lookup() *stubPatientLookup {
	return &stubPatientLookup{
		teamOf: map[string]string{
			hPatient:     t350TeamA, // 本科室患者（hPatient 已绑定 hDevice）
			"P-OTHER":    t350TeamB, // 跨科室
			"P-NOTEAM-A": "",        // 患者未分配团队
		},
		doctorTeam: map[string]string{t350Admin: t350TeamA},
	}
}

// ─────────────────────────────────────────────────────────────
// 患者域单资源端点
// ─────────────────────────────────────────────────────────────

// TestT350_DoctorCrossTeam403 三条患者域读路径：跨团队一律 403，且不触发存在性查询。
func TestT350_DoctorCrossTeam403(t *testing.T) {
	cases := []struct{ name, path string }{
		{"realtime", "/api/v1/patients/P-OTHER/realtime"},
		{"records", "/api/v1/patients/P-OTHER/records?date=2026-08-08"},
		{"health-reports", "/api/v1/patients/P-OTHER/health-reports"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := t350Lookup()
			srv := t350PatientScope(t, lookup)

			w := doDataReq(t, srv.router, http.MethodGet, tc.path, roleDoctor, t350Admin)

			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			code, _ := decodeBody(t, w)
			assert.Equal(t, model.CodeForbidden, code)
			assert.Empty(t, lookup.lastSeen, "越权判定必须先于存在性查询（状态码差不得泄露档案是否存在，T340 口径）")
		})
	}
}

// TestT350_DoctorSameTeam200 同团队患者照常可读：本卡只加范围，不砍掉医生对本团队的数据访问。
func TestT350_DoctorSameTeam200(t *testing.T) {
	lookup := t350Lookup()
	srv := t350PatientScope(t, lookup)

	w := doDataReq(t, srv.router, http.MethodGet, "/api/v1/patients/"+hPatient+"/realtime", roleDoctor, t350Admin)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	code, data := decodeBody(t, w)
	assert.Equal(t, model.CodeOK, code)
	assert.Equal(t, hDevice, data["deviceId"], "本团队患者应拿到真设备快照")
	assert.Equal(t, []string{hPatient}, lookup.teamSeen)
}

// TestT350_DoctorTeamEdgeCases403 三类「不可见」都不放行：跨团队 / 患者无团队 / 患者档案不存在。
func TestT350_DoctorTeamEdgeCases403(t *testing.T) {
	cases := []struct {
		name      string
		patientID string
		lookup    *stubPatientLookup
	}{
		{"患者未分配团队", "P-NOTEAM-A", t350Lookup()},
		{"患者档案不存在", "P-NOPE", func() *stubPatientLookup {
			l := t350Lookup()
			l.known = []string{hPatient} // 只有本科室患者存在
			return l
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := t350PatientScope(t, tc.lookup)

			w := doDataReq(t, srv.router, http.MethodGet, "/api/v1/patients/"+tc.patientID+"/realtime", roleDoctor, t350Admin)

			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		})
	}
}

// TestT350_DoctorNoTeamIdentity_FailClosed 医生有身份但查不到团队 ⇒ 空集，绝不退化成全院。
func TestT350_DoctorNoTeamIdentity_FailClosed(t *testing.T) {
	srv := t350PatientScope(t, t350Lookup())

	w := doDataReq(t, srv.router, http.MethodGet, "/api/v1/patients/"+hPatient+"/realtime", roleDoctor, t350NoTeam)

	assert.Equal(t, http.StatusForbidden, w.Code, "无团队医生对任何患者都不该可读（fail-closed）")
}

// TestT350_MissingIdentityOrLookupError 身份头缺失 403、推导报错 500，两者都不得放行。
func TestT350_MissingIdentityOrLookupError(t *testing.T) {
	t.Run("缺 X-User-Id 403 且不查库", func(t *testing.T) {
		lookup := t350Lookup()
		srv := t350PatientScope(t, lookup)

		w := doDataReq(t, srv.router, http.MethodGet, "/api/v1/patients/"+hPatient+"/realtime", roleDoctor, "")

		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		assert.Empty(t, lookup.teamSeen)
		assert.Empty(t, lookup.lastSeen)
	})

	t.Run("推导报错 500", func(t *testing.T) {
		lookup := t350Lookup()
		lookup.teamErr = errors.New("db down")
		srv := t350PatientScope(t, lookup)

		w := doDataReq(t, srv.router, http.MethodGet, "/api/v1/patients/"+hPatient+"/realtime", roleDoctor, t350Admin)

		require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
		code, _ := decodeBody(t, w)
		assert.Equal(t, model.CodeInternal, code)
	})
}

// TestT350_NonDoctorRolesUnchanged 只收紧不放宽：其余角色与患者本人的响应一字不变。
func TestT350_NonDoctorRolesUnchanged(t *testing.T) {
	paths := []string{
		"/api/v1/patients/P-OTHER/realtime",
		"/api/v1/patients/P-OTHER/records?date=2026-08-08",
		"/api/v1/patients/P-OTHER/health-reports",
	}
	for _, role := range []string{roleAdmin, "ROLE_CS", "technician"} {
		t.Run(role+" 跨团队仍 200", func(t *testing.T) {
			for _, path := range paths {
				lookup := t350Lookup()
				srv := t350PatientScope(t, lookup)

				w := doDataReq(t, srv.router, http.MethodGet, path, role, t350Operator)

				assert.Equal(t, http.StatusOK, w.Code, role+" 读 "+path+"："+w.Body.String())
				assert.Empty(t, lookup.teamSeen, "非医生不应触发团队推导")
			}
		})
	}

	t.Run("患者本人 200 / 他人 403（T264 语义不变）", func(t *testing.T) {
		lookup := t350Lookup()
		srv := t350PatientScope(t, lookup)

		w := doDataReq(t, srv.router, http.MethodGet, "/api/v1/patients/"+hPatient+"/realtime", "ROLE_PATIENT", hPatient)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		w = doDataReq(t, srv.router, http.MethodGet, "/api/v1/patients/P-OTHER/realtime", "ROLE_PATIENT", hPatient)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

		assert.Empty(t, lookup.teamSeen, "患者路径不参与团队推导")
	})
}

// ─────────────────────────────────────────────────────────────
// 数据概览 6 端点：scope 推导与透传
// ─────────────────────────────────────────────────────────────

// t350DashRouter 装配带身份推导的 dashboard 路由
func t350DashRouter(t *testing.T, q DashboardQuerier, lookup PatientLookup) http.Handler {
	t.Helper()
	return newRouterWithScopeDeps(q, lookup)
}

func t350Querier() *mockQuerier {
	return &mockQuerier{
		kpi:        &service.DashboardKPIDTO{TotalPatients: 1},
		wearTrend:  []service.WearTrendPoint{{Date: "09-20", AvgHours: 6}},
		alertTrend: []service.AlertTrendPoint{{Date: "09-20", Count: 1}},
		teamRank:   []service.TeamRankingDTO{{Rank: 1, TeamName: t350TeamA}},
		docRank:    []service.DoctorRankingDTO{{Rank: 1, DoctorName: "王医生", TeamName: t350TeamA}},
		dist:       []service.WearDistributionBucket{{Range: "6-8小时", Count: 2}},
	}
}

// dashboardEndpoints 6 条读路径 + querier 方法名（scopes map 的键）
var dashboardEndpoints = []struct{ path, op string }{
	{"/api/v1/admin/dashboard/kpi?period=week", "GetKPI"},
	{"/api/v1/admin/dashboard/wear-trend?days=7", "GetWearTrend"},
	{"/api/v1/admin/dashboard/alert-trend?days=7", "GetAlertTrend"},
	{"/api/v1/admin/dashboard/team-ranking", "GetTeamRanking"},
	{"/api/v1/admin/dashboard/doctor-ranking", "GetDoctorRanking"},
	{"/api/v1/admin/dashboard/wear-distribution", "GetWearDistribution"},
}

// TestT350_DashboardDoctorScoped 医生令牌：6 端点全部带上本团队范围，且团队只推导一次/请求。
func TestT350_DashboardDoctorScoped(t *testing.T) {
	for _, ep := range dashboardEndpoints {
		t.Run(ep.path, func(t *testing.T) {
			lookup, q := t350Lookup(), t350Querier()
			router := t350DashRouter(t, q, lookup)

			w := doDataReq(t, router, http.MethodGet, ep.path, roleDoctor, t350Admin)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, q.scopes, ep.op)
			assert.Equal(t, model.ScopeTeam(t350TeamA), q.scopes[ep.op])
			assert.Equal(t, 1, lookup.teamLookup, "团队只从 doctors 表推导一次，不接受任何入参")
		})
	}
}

// TestT350_DashboardDoctorNoTeam 无团队医生 ⇒ Limited 且空团队（repo 侧恒假谓词 → 全零聚合）。
func TestT350_DashboardDoctorNoTeam(t *testing.T) {
	lookup, q := t350Lookup(), t350Querier()
	router := t350DashRouter(t, q, lookup)

	w := doDataReq(t, router, http.MethodGet, "/api/v1/admin/dashboard/kpi?period=today", roleDoctor, t350NoTeam)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := q.scopes["GetKPI"]
	assert.True(t, got.Scoped(), "无团队也必须收口，不能退回全院")
	assert.Equal(t, "", got.TeamID)
}

// TestT350_DashboardOpsUnscoped 运营 / 客服 / 技师：全院口径 + 完全不查 doctors。
func TestT350_DashboardOpsUnscoped(t *testing.T) {
	for _, role := range []string{roleAdmin, "ROLE_CS", "technician"} {
		t.Run(role, func(t *testing.T) {
			lookup, q := t350Lookup(), t350Querier()
			router := t350DashRouter(t, q, lookup)

			for _, ep := range dashboardEndpoints {
				w := doDataReq(t, router, http.MethodGet, ep.path, role, t350Operator)
				require.Equal(t, http.StatusOK, w.Code, ep.path+": "+w.Body.String())
				assert.Equal(t, model.ScopeAll(), q.scopes[ep.op], "非医生保持全院口径（只收紧不放宽）")
			}
			assert.Zero(t, lookup.teamLookup, "非医生不做团队推导")
		})
	}
}

// TestT350_DashboardFailClosed 医生缺身份头 403、推导失败 500：都不打到聚合层。
func TestT350_DashboardFailClosed(t *testing.T) {
	t.Run("缺 X-User-Id 403", func(t *testing.T) {
		lookup, q := t350Lookup(), t350Querier()
		router := t350DashRouter(t, q, lookup)

		w := doDataReq(t, router, http.MethodGet, "/api/v1/admin/dashboard/team-ranking", roleDoctor, "")

		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		assert.Empty(t, q.scopes, "鉴权未过不应执行任何聚合查询")
	})

	t.Run("推导失败 500", func(t *testing.T) {
		lookup, q := t350Lookup(), t350Querier()
		lookup.teamErr = errors.New("db down")
		router := t350DashRouter(t, q, lookup)

		w := doDataReq(t, router, http.MethodGet, "/api/v1/admin/dashboard/kpi", roleDoctor, t350Admin)

		require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
		assert.Empty(t, q.scopes, "推导失败不得继续跑聚合查询")
		assert.Equal(t, 1, lookup.teamLookup, "推导确实发起过一次（失败点在推导本身，不是漏装）")
	})

	t.Run("未注入 PatientLookup 500", func(t *testing.T) {
		router := newRouterWithScopeDeps(t350Querier(), nil)

		w := doDataReq(t, router, http.MethodGet, "/api/v1/admin/dashboard/wear-distribution", roleDoctor, t350Admin)

		assert.Equal(t, http.StatusInternalServerError, w.Code, "漏装配不得退化成全院聚合："+w.Body.String())
	})
}

// TestT350_DashboardClientCannotOverrideScope 客户端自报 teamId 一律无效（权限来源只有服务端推导）。
func TestT350_DashboardClientCannotOverrideScope(t *testing.T) {
	lookup, q := t350Lookup(), t350Querier()
	router := t350DashRouter(t, q, lookup)

	w := doDataReq(t, router, http.MethodGet,
		"/api/v1/admin/dashboard/kpi?period=month&teamId="+t350TeamB, roleDoctor, t350Admin)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, model.ScopeTeam(t350TeamA), q.scopes["GetKPI"], "伪造 teamId 不得改范围")
	assert.NotEqual(t, t350TeamB, q.scopes["GetKPI"].TeamID)
}

// 编译期确认替身满足 handler 侧契约（生产实现分别是 repo.PatientRepo 与 service.DashboardService）
var (
	_ PatientLookup    = (*stubPatientLookup)(nil)
	_ DashboardQuerier = (*mockQuerier)(nil)
)
