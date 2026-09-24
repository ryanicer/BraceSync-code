// T350：医护数据范围「仅本团队患者」（PRD §7D.11 数据范围规则）。
//
// 缺陷原貌：patient 列表 / 患者详情 / 跨患者感受日志三条读链路的团队维度只从 query 取，
// 从不按调用者身份推导 ⇒ 医生令牌不传 teamId 就是全表，传别人的 teamId 就能切视图。
//
// 本文件守四件事：
//  1. 推导来源是身份（X-Role + X-User-Id → doctors.team_id），客户端 teamId 对医护无效；
//  2. fail-closed：医护无团队归属 → 空集标记（TeamScoped 且 TeamID 为空），绝不退化成不过滤；
//  3. 只收紧不放宽：运营 / 客服路径的入参语义逐字不变；
//  4. 存在性不泄露（判据③ 后半，第 8 轮打回项）：受限身份的详情读「跨团队」与「查无此人」
//     同码同形，患者详情端点对医护永不出 404。
package handler

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t350DoctorHDR = "ROLE_DOCTOR"
	t350OwnTeam   = "TEAM01" // samplePatient 所属团队
	t350OtherTeam = "TEAM02"
)

func t350Hdr(role, userID string) map[string]string {
	return map[string]string{"X-Role": role, "X-User-Id": userID}
}

// t350UntouchedFilter fakeStore.lastFilter 的哨兵初值：用例结束后仍是本值即证明未触达 store。
var t350UntouchedFilter = repo.PatientFilter{Keyword: "__untouched__"}

// t350DoctorInTeam 把医护身份绑到指定团队（doctors.team_id 的唯一事实源在 store 替身上）。
func t350DoctorInTeam(e *testEnv, teamID string) {
	e.store.doctorTeam = teamID
	e.store.doctorTeamFound = teamID != ""
}

// ── 1. 患者列表：医护按所属团队过滤，客户端 teamId 被忽略 ──────────────────

func TestT350_ListPatients_DoctorIgnoresClientTeamID(t *testing.T) {
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, t350OwnTeam)

	// 越界用例（卡面验收项）：医生令牌伪造 teamId=TEAM02
	w, resp := e.do(http.MethodGet,
		"/api/v1/admin/patients?teamId="+t350OtherTeam, nil, t350Hdr(t350DoctorHDR, "ADM-D001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	assert.Equal(t, t350OwnTeam, e.store.lastFilter.TeamID, "医护的过滤团队必须来自 doctors.team_id")
	assert.True(t, e.store.lastFilter.TeamScoped, "医护必须带受限标记，否则空串会被当成「不过滤」")
	assert.NotEqual(t, t350OtherTeam, e.store.lastFilter.TeamID, "客户端自报 teamId 不得生效")
}

func TestT350_ListPatients_DoctorWithoutTeamFailsClosed(t *testing.T) {
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, "") // doctors.team_id 为 NULL / 无 doctor 行

	w, _ := e.do(http.MethodGet, "/api/v1/admin/patients", nil, t350Hdr(t350DoctorHDR, "ADM-D002"))
	require.Equal(t, http.StatusOK, w.Code)

	assert.True(t, e.store.lastFilter.TeamScoped, "无团队仍是受限身份")
	assert.Equal(t, "", e.store.lastFilter.TeamID)
	// TeamScoped && TeamID=="" 在 repo 层落 "false" 谓词 ⇒ 空集，见 patientWhere
}

func TestT350_ListPatients_MissingUserIDForbidden(t *testing.T) {
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, t350OwnTeam)
	e.store.lastFilter = t350UntouchedFilter // 哨兵：被调用过就会被覆盖

	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients", nil,
		map[string]string{"X-Role": t350DoctorHDR})
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Equal(t, t350UntouchedFilter, e.store.lastFilter, "身份缺失不得触达 store.ListPatients")
}

func TestT350_ListPatients_TeamLookupErrorFailsClosed(t *testing.T) {
	e := newEnv(t, false, false)
	e.store.doctorTeamErr = errors.New("db down")
	e.store.lastFilter = t350UntouchedFilter

	w, _ := e.do(http.MethodGet, "/api/v1/admin/patients", nil, t350Hdr(t350DoctorHDR, "ADM-D003"))
	assert.Equal(t, http.StatusInternalServerError, w.Code, "推导失败不得退化成全量")
	assert.Equal(t, t350UntouchedFilter, e.store.lastFilter, "推导失败不得触达 store.ListPatients")
}

// ── 2. 只收紧不放宽：运营 / 客服入参语义逐字不变 ─────────────────────────

func TestT350_ListPatients_AdminKeepsClientTeamFilter(t *testing.T) {
	for _, role := range []string{"ROLE_ADMIN", "ROLE_CS"} {
		t.Run(role, func(t *testing.T) {
			e := newEnv(t, false, false)
			t350DoctorInTeam(e, t350OwnTeam) // 即便身份能查到团队也不该参与非医护角色

			w, resp := e.do(http.MethodGet,
				"/api/v1/admin/patients?teamId="+t350OtherTeam, nil, t350Hdr(role, "ADM-001"))
			require.Equal(t, http.StatusOK, w.Code, resp.Message)

			assert.Equal(t, t350OtherTeam, e.store.lastFilter.TeamID, "%s 保留客户端团队筛选", role)
			assert.False(t, e.store.lastFilter.TeamScoped, "%s 不受团队隔离", role)
		})
	}
}

func TestT350_ListPatients_NoRoleHeaderKeepsLegacyBehavior(t *testing.T) {
	// 缺失 X-Role（既有测试与内部直连调用的常态）：不按医护收紧，也不放宽任何现有权限
	e := newEnv(t, false, false)
	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients?teamId="+t350OtherTeam, nil, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, t350OtherTeam, e.store.lastFilter.TeamID)
	assert.False(t, e.store.lastFilter.TeamScoped)
}

// ── 3. 患者详情：跨团队 403 ─────────────────────────────────────────────

func TestT350_GetPatient_DoctorCrossTeamForbidden(t *testing.T) {
	e := newEnv(t, false, false)
	p := samplePatient() // TEAM01
	e.store.patient = &p
	t350DoctorInTeam(e, t350OtherTeam)

	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients/P20260001", nil,
		t350Hdr(t350DoctorHDR, "ADM-D001"))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.NotContains(t, string(resp.Data), "患者小明", "越权响应不得带出患者档案")
}

func TestT350_GetPatient_DoctorOwnTeamOK(t *testing.T) {
	e := newEnv(t, false, false)
	p := samplePatient()
	e.store.patient = &p
	t350DoctorInTeam(e, t350OwnTeam)

	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients/P20260001", nil,
		t350Hdr(t350DoctorHDR, "ADM-D001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Contains(t, string(resp.Data), `"patientId":"P20260001"`)
}

func TestT350_GetPatient_UnassignedPatientDeniedForDoctor(t *testing.T) {
	e := newEnv(t, false, false)
	p := samplePatient()
	p.TeamID = nil // 未分配团队
	e.store.patient = &p
	t350DoctorInTeam(e, t350OwnTeam)

	w, _ := e.do(http.MethodGet, "/api/v1/admin/patients/P20260001", nil,
		t350Hdr(t350DoctorHDR, "ADM-D001"))
	assert.Equal(t, http.StatusForbidden, w.Code, "待分配患者不属于任何团队，医护不可见")
}

func TestT350_GetPatient_AdminSeesAnyTeam(t *testing.T) {
	e := newEnv(t, false, false)
	p := samplePatient()
	e.store.patient = &p

	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients/P20260001", nil,
		t350Hdr("ROLE_ADMIN", "ADM-001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
}

// 判据③ 后半（PM 第 8 轮打回项）：受限身份的患者详情不得成为「患者号存在性 oracle」。
// 打回前的现网原文：跨团队 P20260001 回 403、不存在的 P99999999 回 404，
// 一名已登录医护据此可枚举哪些患者号真实存在。本用例守「同码 + 同文案」。
func TestT350_GetPatient_DoctorCannotProbeExistence(t *testing.T) {
	p := samplePatient() // TEAM01，对 doctor_li 是跨团队

	// 甲：读「存在但跨团队」
	ea := newEnv(t, false, false)
	existing := p
	ea.store.patient = &existing
	t350DoctorInTeam(ea, t350OtherTeam)
	wa, respA := ea.do(http.MethodGet, "/api/v1/admin/patients/P20260001", nil,
		t350Hdr(t350DoctorHDR, "ADM-D001"))

	// 乙：读「根本不存在」的号
	eb := newEnv(t, false, false)
	t350DoctorInTeam(eb, t350OtherTeam)
	wb, respB := eb.do(http.MethodGet, "/api/v1/admin/patients/P20269999", nil,
		t350Hdr(t350DoctorHDR, "ADM-D001"))

	require.Equal(t, http.StatusForbidden, wa.Code, respA.Message)
	require.Equal(t, http.StatusForbidden, wb.Code, "不存在不得回 404（与跨团队同码）")
	assert.Equal(t, model.CodeForbidden, respB.Code)
	assert.Equal(t, respA.Code, respB.Code, "业务码必须一致，否则码值本身就是 oracle")
	// 文案同理：抹掉调用者自己填的患者号后必须逐字相同（Alice 举证的判读面含文案差异）
	assert.Equal(t,
		strings.ReplaceAll(respA.Message, "P20260001", "<pid>"),
		strings.ReplaceAll(respB.Message, "P20269999", "<pid>"),
		"两句文案去掉患者号后必须同形")
	assert.NotContains(t, respB.Message, "not found", "不得出现「不存在」字样")
}

// 无团队归属的医护：读不存在的号同样 403（fail-closed 与存在性闭合走同一条路径）
func TestT350_GetPatient_DoctorWithoutTeamNotFoundForbidden(t *testing.T) {
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, "")

	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients/P20269999", nil,
		t350Hdr(t350DoctorHDR, "ADM-D002"))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
}

// 只收紧不放宽：不受限角色（运营 / 客服）的 404 语义逐字不变
func TestT350_GetPatient_NotFoundStill404ForAdmin(t *testing.T) {
	for _, role := range []string{"ROLE_ADMIN", "ROLE_CS"} {
		t.Run(role, func(t *testing.T) {
			e := newEnv(t, false, false)
			t350DoctorInTeam(e, t350OwnTeam) // 非医护身份即便能查到团队也不参与过滤

			w, resp := e.do(http.MethodGet, "/api/v1/admin/patients/P20269999", nil,
				t350Hdr(role, "ADM-001"))
			assert.Equal(t, http.StatusNotFound, w.Code, resp.Message)
			assert.Equal(t, model.CodeNotFound, resp.Code)
		})
	}
}

// ── 4. 跨患者感受日志：同一套推导 ───────────────────────────────────────

func TestT350_ListFeelingLogsAdmin_DoctorScopedToTeam(t *testing.T) {
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, t350OwnTeam)

	w, resp := e.do(http.MethodGet, "/api/v1/admin/feeling-logs", nil,
		t350Hdr(t350DoctorHDR, "ADM-D001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, t350OwnTeam, e.store.lastFeelingAdminFilter.TeamID)
	assert.True(t, e.store.lastFeelingAdminFilter.TeamScoped)
}

func TestT350_ListFeelingLogsAdmin_DoctorWithoutTeamFailsClosed(t *testing.T) {
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, "")

	w, _ := e.do(http.MethodGet, "/api/v1/admin/feeling-logs", nil,
		t350Hdr(t350DoctorHDR, "ADM-D001"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", e.store.lastFeelingAdminFilter.TeamID)
	assert.True(t, e.store.lastFeelingAdminFilter.TeamScoped, "空团队必须是空集语义")
}

func TestT350_ListFeelingLogsAdmin_AdminUnscoped(t *testing.T) {
	e := newEnv(t, false, false)

	w, resp := e.do(http.MethodGet, "/api/v1/admin/feeling-logs", nil,
		t350Hdr("ROLE_ADMIN", "ADM-001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.False(t, e.store.lastFeelingAdminFilter.TeamScoped)
	assert.Equal(t, "", e.store.lastFeelingAdminFilter.TeamID)
}

// ── 5. repo 层谓词见 repo/team_scope_t350_unit_test.go（patientWhere 为包内函数）
