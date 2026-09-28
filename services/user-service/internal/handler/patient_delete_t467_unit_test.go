// T467：患者档案删除端点 DELETE /admin/patients/:patientId。
//
// 卡面是 T462 链 A「建档段 A2」的前置：建档能删不掉，测试数据就得手工进库清。
// 本文件守四件事：
//  1. 派发单点名的四格：删自建患者 200 / 删不存在 404 / 跨团队 403 / 重复删除按 404 口径；
//  2. 拒绝必须发生在库写之前：断言写替身调用次数为 0（只回 403/404 算半个判据）；
//  3. 反证：本团队医护照常删得掉、运营照常跨团队删得掉，防授权选型把正常写面一起锁死；
//     以及「放行客服/技师」这一格必须是 403（选型落在 doctorAdminOnly + handler allow-list 的证据）；
//  4. 只收紧不放宽 + 存在性不泄露：受限身份「查无此人」与「跨团队」同码同文，永不出 404。
//
// 另守 T464 双通道：409 的逐表计数只进技术日志，响应体 message 恒为该码的中文短句
// （fail() 走 model.UserText，任何「计数在 message 里」的写法在这格必判红）。
//
// 团队范围推导本身的用例在 team_scope_t350_test.go，本文件不重复审推导来源，只审接线。
package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t467OwnPatient    = "PAT-T467-OWN"    // team_id = TEAM01（医护本人团队）
	t467OtherPatient  = "PAT-T467-OTHER"  // team_id = TEAM02（他团队）
	t467NoTeamPatient = "PAT-T467-NOTEAM" // patients.team_id 为 NULL
	t467MissingLog    = "PAT-T467-NOPE"   // 夹具里没有的患者号
	t467AdminHDR      = "ROLE_ADMIN"
	t467AdminUser     = "ADM-T467-001"
)

// t467Patient 造一名患者；teamID 传空串表示 patients.team_id 为 NULL（未分配团队）
func t467Patient(pid, teamID string) repo.PatientRow {
	row := samplePatient()
	row.PatientID = pid
	if teamID == "" {
		row.TeamID = nil
	} else {
		t := teamID
		row.TeamID = &t
	}
	return row
}

// t467Env 三名患者（本团队 / 他团队 / 未分配团队），医护账号默认属 TEAM01
func t467Env(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, t350OwnTeam)
	e.store.patients = []repo.PatientRow{
		t467Patient(t467OwnPatient, t350OwnTeam),
		t467Patient(t467OtherPatient, t350OtherTeam),
		t467Patient(t467NoTeamPatient, ""),
	}
	return e
}

func t467Delete(pid string) string { return "/api/v1/admin/patients/" + pid }

// t467Admin 以运营身份发请求（不受团队范围约束的那一档）
func t467Admin() map[string]string { return t350Hdr(t467AdminHDR, t467AdminUser) }

// t467ProfileGone 复读档案是否真没了：走的是同一条 GET 详情链路（运营档），
// 断 404 而不是只断替身计数器 —— 「删成」必须以「读不到」为准。
func t467ProfileGone(t *testing.T, e *testEnv, pid string) {
	t.Helper()
	w, resp := e.do(http.MethodGet, t467Delete(pid), nil, t467Admin())
	assert.Equal(t, http.StatusNotFound, w.Code, "删成后详情应 404，实际：%s", resp.Message)
}

// ── 1. 删自建患者 200（链 A A2 前置）─────────────────────────────────────

func TestT467_AdminDeleteSelfBuiltPatientSucceeds(t *testing.T) {
	e := t467Env(t)

	w, resp := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t467Admin())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, model.CodeOK, resp.Code)
	assert.Equal(t, 1, e.store.deletePatientCalls)
	assert.Equal(t, t467OwnPatient, e.store.lastDeletePatient)
	t467ProfileGone(t, e, t467OwnPatient)
}

// 运营跨团队照删：只收紧不放宽（这一档的入参与状态码不许变）
func TestT467_AdminDeleteCrossTeamPatientStillAllowed(t *testing.T) {
	e := t467Env(t)

	w, resp := e.do(http.MethodDelete, t467Delete(t467OtherPatient), nil, t467Admin())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, 1, e.store.deletePatientCalls, "运营保留跨团队删，否则测试数据无从清理")
	assert.Equal(t, t467OtherPatient, e.store.lastPatientQuery, "运营走存在性判定（不带团队谓词）")
}

// ── 2. 删不存在 404：判定必须排在写之前 ────────────────────────────────

func TestT467_AdminDeleteMissingPatientIs404WithZeroWrites(t *testing.T) {
	e := t467Env(t)

	w, resp := e.do(http.MethodDelete, t467Delete(t467MissingLog), nil, t467Admin())
	require.Equal(t, http.StatusNotFound, w.Code, resp.Message)
	assert.Equal(t, model.CodeNotFound, resp.Code)
	assert.Equal(t, 0, e.store.deletePatientCalls, "查无此人不得发删除语句")
	t464UserMessage(t, w, model.CodeNotFound)
	// 患者号只进日志通道（响应体里已换成中文短句）
	assert.NotContains(t, w.Body.String(), t467MissingLog)
	t464TechLogContains(t, w, t467MissingLog)
}

// 重复删除按 404 口径落契约，不采「已经没了也算成功」的伪幂等。
// 判据要点在第二次的库写次数仍是 1：第二次被存在性判定拦在写之前。
func TestT467_RepeatDeleteIs404NotFakeIdempotent(t *testing.T) {
	e := t467Env(t)

	w1, resp1 := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t467Admin())
	require.Equal(t, http.StatusOK, w1.Code, resp1.Message)

	w2, resp2 := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t467Admin())
	assert.Equal(t, http.StatusNotFound, w2.Code, resp2.Message)
	assert.Equal(t, model.CodeNotFound, resp2.Code, "重复删除的口径要落进契约，不能时好时坏")
	assert.Equal(t, 1, e.store.deletePatientCalls, "第二次不得再发删除语句")
}

// ── 3. 跨团队 403：医护只碰得到本团队患者 ──────────────────────────────

func TestT467_DoctorDelete_CrossTeamDeniedWithZeroWrites(t *testing.T) {
	e := t467Env(t)

	w, resp := e.do(http.MethodDelete, t467Delete(t467OtherPatient), nil, t350Hdr(t350DoctorHDR, t467AdminUser))
	require.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Equal(t, 0, e.store.deletePatientCalls, "跨团队删除必须零次库写，否则他团队档案直接消失")
	assert.Equal(t, "", e.store.lastDeletePatient)
	assert.Equal(t, t467OtherPatient, e.store.lastPatientQuery, "判定必须走带团队谓词的只读探测")
}

func TestT467_DoctorDelete_SameTeamSucceeds(t *testing.T) {
	e := t467Env(t)

	w, resp := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t350Hdr(t350DoctorHDR, t467AdminUser))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, 1, e.store.deletePatientCalls, "反证：本团队内医护写面不许被锁死")
	t467ProfileGone(t, e, t467OwnPatient)
}

func TestT467_DoctorDelete_PatientWithoutTeamFailsClosed(t *testing.T) {
	e := t467Env(t)

	w, _ := e.do(http.MethodDelete, t467Delete(t467NoTeamPatient), nil, t350Hdr(t350DoctorHDR, t467AdminUser))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.deletePatientCalls, "患者未分配团队不得当成「不过滤」放行")
}

func TestT467_DoctorDelete_DoctorWithoutTeamFailsClosed(t *testing.T) {
	e := t467Env(t)
	t350DoctorInTeam(e, "") // doctors.team_id 为 NULL
	w, _ := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t350Hdr(t350DoctorHDR, t467AdminUser))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.deletePatientCalls)
}

func TestT467_DoctorDelete_MissingUserIDTouchesNothing(t *testing.T) {
	e := t467Env(t)

	w, _ := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, map[string]string{"X-Role": t350DoctorHDR})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "", e.store.lastPatientQuery, "身份缺失不得触库，连只读探测都不发")
	assert.Equal(t, 0, e.store.deletePatientCalls)
}

func TestT467_DoctorDelete_ScopeLookupErrorFailsClosed(t *testing.T) {
	e := t467Env(t)
	e.store.patientErr = errors.New("db down")

	w, _ := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t350Hdr(t350DoctorHDR, t467AdminUser))
	assert.Equal(t, http.StatusInternalServerError, w.Code, "探测失败不得退化成不判归属")
	assert.Equal(t, 0, e.store.deletePatientCalls)
}

// 存在性不泄露：受限身份在删除端点上「跨团队」与「查无此人」同码同文，永不出 404
func TestT467_DoctorDelete_NoExistenceOracle(t *testing.T) {
	e := t467Env(t)

	wCross, cross := e.do(http.MethodDelete, t467Delete(t467OtherPatient), nil, t350Hdr(t350DoctorHDR, t467AdminUser))
	wMiss, miss := e.do(http.MethodDelete, t467Delete(t467MissingLog), nil, t350Hdr(t350DoctorHDR, t467AdminUser))

	assert.Equal(t, http.StatusForbidden, wCross.Code)
	assert.NotEqual(t, http.StatusNotFound, wMiss.Code, "受限身份在这一端点永不出 404")
	assert.Equal(t, wCross.Code, wMiss.Code)
	assert.Equal(t, cross.Code, miss.Code)
	assert.Equal(t, cross.Message, miss.Message, "两格的响应文案必须逐字一致")
	assert.Equal(t, 0, e.store.deletePatientCalls)
}

// ── 4. 角色 allow-list：客服/技师/患者/缺角色一律 403 且不触库 ──────────
//
// 这一组是授权选型的证据：网关把这条登记在 doctorAdminOnlyPatterns（admin+doctor），
// handler 再兜一层「绕过网关直连服务」。若误登记进 staffOnlyPatterns，客服删任意患者会一路放行。

func TestT467_NonAllowedRolesDeniedWithoutTouchingStore(t *testing.T) {
	for _, role := range []string{"ROLE_CS", "technician", "patient"} {
		t.Run(role, func(t *testing.T) {
			e := t467Env(t)

			w, resp := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t350Hdr(role, "OP-"+role))
			assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
			assert.Equal(t, model.CodeForbidden, resp.Code)
			assert.Equal(t, "", e.store.lastPatientQuery, "%s 不得触库，连存在性判定都不发", role)
			assert.Equal(t, 0, e.store.deletePatientCalls)
		})
	}
}

func TestT467_MissingRoleHeaderDeniedWithoutTouchingStore(t *testing.T) {
	e := t467Env(t)

	w, _ := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.deletePatientCalls, "X-Role 缺失按无权限处理（fail-closed）")
}

// ── 5. 409 关联面非空：拦持有业务数据，且档案没被删 ────────────────────

func TestT467_InUseIs409AndProfileSurvives(t *testing.T) {
	e := t467Env(t)
	e.store.deletePatientErr = &repo.ErrPatientInUse{Refs: map[string]int{"devices": 1, "feeling_logs": 2}}

	w, resp := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t467Admin())
	require.Equal(t, http.StatusConflict, w.Code, resp.Message)
	assert.Equal(t, model.CodeConflict, resp.Code)
	t464UserMessage(t, w, model.CodeConflict)

	// 逐表计数只进日志通道：响应体既无英文技术文本，也不许漏掉可反查的机读键
	assert.NotContains(t, w.Body.String(), "devices")
	t464TechLogContains(t, w, "devices=1")
	t464TechLogContains(t, w, "feeling_logs=2")

	w2, resp2 := e.do(http.MethodGet, t467Delete(t467OwnPatient), nil, t467Admin())
	assert.Equal(t, http.StatusOK, w2.Code, resp2.Message, "被拒的删除不得动到档案")
}

func TestT467_GenericStoreErrorIs500(t *testing.T) {
	e := t467Env(t)
	e.store.deletePatientErr = errors.New("connection reset")

	w, resp := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t467Admin())
	assert.Equal(t, http.StatusInternalServerError, w.Code, resp.Message)
	assert.Equal(t, model.CodeInternal, resp.Code, "库错不得被误映射成 404/409")
	assert.Equal(t, 1, e.store.deletePatientCalls)
}

// ── 6. 审计与路由接线（新增一条就必须两张表都在）──────────────────────

// TestT467_RouteIsRegistered 未注册该路由时 gin 回 404 + 空 body，与「查无此人 404」同码不同体，
// 故除状态码外还要断响应信封：断言走的是本服务的统一响应面。
func TestT467_RouteIsRegistered(t *testing.T) {
	e := t467Env(t)

	w, resp := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t467Admin())
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code, "命中 handler 才会进统一响应面")
	assert.Contains(t, w.Body.String(), `"code":0`)
}

// TestT467_Audit_SuccessWritesExactlyOneRow 删档必须留痕：同族五条患者写端点全在 auditRoutes 表内，
// 收口档案这条不能漏（等保 §9.2a 的「谁删了哪份档案」只有这一条通道可反查）。
func TestT467_Audit_SuccessWritesExactlyOneRow(t *testing.T) {
	e := t467Env(t)

	w, resp := e.do(http.MethodDelete, t467Delete(t467OwnPatient), nil, t467Admin())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Equal(t, auditActionDataModify, row.Action)
	assert.Equal(t, "patient", row.TargetType)
	assert.Equal(t, t467OwnPatient, row.TargetID)
	assert.Contains(t, row.Description, "删除患者档案")
	assert.Equal(t, t467AdminUser, row.OperatorID, "操作人取网关注入的身份头")
}

// TestT467_Audit_DeniedLeavesNoRow 被拒的删除不得留痕（auditTrail 只在 HTTP < 400 写行）：
// 否则攻击面探测会把审计表刷满，真实删除动作被淹掉。
func TestT467_Audit_DeniedLeavesNoRow(t *testing.T) {
	e := t467Env(t)

	w, _ := e.do(http.MethodDelete, t467Delete(t467OtherPatient), nil, t350Hdr(t350DoctorHDR, t467AdminUser))
	require.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, e.store.auditRows)
	assert.Equal(t, 0, e.store.deletePatientCalls)
}
