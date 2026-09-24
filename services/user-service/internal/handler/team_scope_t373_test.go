// T373：患者域读写归属判定（写侧 D1 医护回复感受日志 / D2 保存矫形方案，读侧 A1 方案历史）。
//
// 缺陷原貌：#205（T350）只把「列表 + 详情」两条读链路接上了团队范围，这三处入口从不判归属 ——
// D1 是一条按 log_id 的无条件 UPDATE（连 reply_time 一并改写，医护原文不可恢复）、
// D2 按 patientId 无条件 INSERT、A1 走 assertAdminOrSelf 对全体 staff 放行 ⇒
// 任意医护令牌能改、能看任意患者的数据。
//
// 本文件守三件事：
//  1. 拒绝必须发生在库写之前：断言写替身调用次数为 0（只回 403 算半个判据）；
//  2. 反证：本团队内的同类操作照常成功，防过度收紧把正常医护写面一起锁死；
//  3. 只收紧不放宽：运营 / 客服路径的入参与状态码逐字不变（含「查无此人 404」）。
//
// 另守存在性不泄露：受限身份在 D1 / A1 上「查无此 id」与「跨团队」同码同形，永不出 404。
// 团队范围推导本身的用例在 team_scope_t350_test.go，本文件不重复审推导来源，只审接线。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t373OwnPatient   = "PAT-OWN"   // team_id = TEAM01（医护本人团队）
	t373OtherPatient = "PAT-OTHER" // team_id = TEAM02（他团队）
	t373NoTeamName   = "PAT-NOTEAM"
	t373OwnLog       = 5 // 挂在 PAT-OWN 名下
	t373OtherLog     = 9 // 挂在 PAT-OTHER 名下
	t373NoTeamLog    = 7 // 挂在未分配团队的患者名下
	t373MissingLog   = 999
	t373DoctorAdmin  = "ADM-D001"
)

// t373Patient 造一名患者；teamID 传空串表示 patients.team_id 为 NULL（未分配团队）
func t373Patient(pid, teamID string) repo.PatientRow {
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

// t373Env 三名患者 + 三条感受日志 + 一份方案历史，医护账号默认属 TEAM01
func t373Env(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, t350OwnTeam)
	e.store.patients = []repo.PatientRow{
		t373Patient(t373OwnPatient, t350OwnTeam),
		t373Patient(t373OtherPatient, t350OtherTeam),
		t373Patient(t373NoTeamName, ""),
	}
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	e.store.feelings = []repo.FeelingLogRow{
		{LogID: t373OwnLog, PatientID: t373OwnPatient, LogDate: at},
		{LogID: t373OtherLog, PatientID: t373OtherPatient, LogDate: at},
		{LogID: t373NoTeamLog, PatientID: t373NoTeamName, LogDate: at},
	}
	e.store.replyOK = true
	e.store.doctorFound = true
	e.store.doctorID = "D0001"
	e.store.createdPlan = &repo.OrthosisPlanRow{
		PlanID: 2, PatientID: t373OwnPatient, DoctorID: "D0001", Content: "新方案", CreatedAt: at,
	}
	return e
}

func t373Reply(logID int, content string) (string, any) {
	return "/api/v1/feeling-logs/" + strconv.Itoa(logID) + "/reply", map[string]string{"replyContent": content}
}

func t373SavePlan(patientID string) (string, any) {
	return "/api/v1/patients/" + patientID + "/orthosis-plans", map[string]string{"content": "新方案"}
}

// t373Shape 把调用方自己给出的资源 id 折成占位符，用于比对两种拒绝是否「同形」
func t373Shape(msg, id string) string {
	return strings.Replace(msg, id, "<id>", 1)
}

// ── 1. D1 医护回复感受日志：跨团队必须零次库写 ─────────────────────────

func TestT373_Reply_CrossTeamDeniedWithZeroWrites(t *testing.T) {
	e := t373Env(t)
	path, body := t373Reply(t373OtherLog, "越界回复")

	w, resp := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Equal(t, 0, e.store.replyCalls, "跨团队回复必须零次库写，否则医生原文已被覆盖")
	assert.Equal(t, "", e.store.lastReply, "写替身不该收到任何文本")
	assert.Equal(t, 1, e.store.feelingTeamCalls, "判定只走只读探测")
}

func TestT373_Reply_SameTeamSucceeds(t *testing.T) {
	e := t373Env(t)
	path, body := t373Reply(t373OwnLog, "贴合良好")

	w, resp := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, 1, e.store.replyCalls, "反证：本团队内医护写面不许被锁死")
	assert.Equal(t, "贴合良好", e.store.lastReply)
}

func TestT373_Reply_PatientWithoutTeamFailsClosed(t *testing.T) {
	e := t373Env(t)
	path, body := t373Reply(t373NoTeamLog, "x")

	w, resp := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, 0, e.store.replyCalls, "患者未分配团队不得当成「不过滤」放行")
}

func TestT373_Reply_DoctorWithoutTeamFailsClosed(t *testing.T) {
	e := t373Env(t)
	t350DoctorInTeam(e, "") // doctors.team_id 为 NULL
	path, body := t373Reply(t373OwnLog, "x")

	w, _ := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.replyCalls)
}

func TestT373_Reply_MissingIdentityTouchesNothing(t *testing.T) {
	e := t373Env(t)
	path, body := t373Reply(t373OwnLog, "x")

	w, resp := e.do(http.MethodPost, path, body, map[string]string{"X-Role": t350DoctorHDR})
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, 0, e.store.feelingTeamCalls, "身份缺失不得触库，连只读探测都不发")
	assert.Equal(t, 0, e.store.replyCalls)
}

func TestT373_Reply_ScopeLookupErrorFailsClosed(t *testing.T) {
	e := t373Env(t)
	e.store.feelingTeamErr = errors.New("db down")
	path, body := t373Reply(t373OwnLog, "x")

	w, _ := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	assert.Equal(t, http.StatusInternalServerError, w.Code, "探测失败不得退化成不判归属")
	assert.Equal(t, 0, e.store.replyCalls)
}

func TestT373_Reply_NoExistenceOracleForDoctor(t *testing.T) {
	e := t373Env(t)
	path, body := t373Reply(t373OtherLog, "x")
	wCross, cross := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t373DoctorAdmin))

	missingPath, missingBody := t373Reply(t373MissingLog, "x")
	wMiss, miss := e.do(http.MethodPost, missingPath, missingBody, t350Hdr(t350DoctorHDR, t373DoctorAdmin))

	assert.Equal(t, http.StatusForbidden, wCross.Code)
	assert.NotEqual(t, http.StatusNotFound, wMiss.Code, "受限身份在这一端点永不出 404")
	assert.Equal(t, wCross.Code, wMiss.Code)
	assert.Equal(t, cross.Code, miss.Code)
	assert.Equal(t,
		t373Shape(cross.Message, strconv.Itoa(t373OtherLog)),
		t373Shape(miss.Message, strconv.Itoa(t373MissingLog)),
		"跨团队与查无此 id 必须同形，否则 logId 存在性可辨")
	assert.Equal(t, 0, e.store.replyCalls)
}

func TestT373_Reply_StaffRolesKeepLegacyShape(t *testing.T) {
	for _, role := range []string{"ROLE_ADMIN", "ROLE_CS"} {
		t.Run(role, func(t *testing.T) {
			e := t373Env(t)
			path, body := t373Reply(t373OtherLog, "运营回复")

			w, resp := e.do(http.MethodPost, path, body, t350Hdr(role, "ADM-001"))
			require.Equal(t, http.StatusOK, w.Code, resp.Message)
			assert.Equal(t, 1, e.store.replyCalls)
			assert.Equal(t, 0, e.store.feelingTeamCalls, "不受限角色不做归属探测")

			e.store.replyOK = false
			missPath, missBody := t373Reply(t373MissingLog, "x")
			w2, resp2 := e.do(http.MethodPost, missPath, missBody, t350Hdr(role, "ADM-001"))
			assert.Equal(t, http.StatusNotFound, w2.Code, resp2.Message)
			assert.Equal(t, model.CodeNotFound, resp2.Code, "查无此日志仍 404，原语义不变")
		})
	}
}

// ── 2. D2 保存矫形方案：跨团队必须零次建方案 ───────────────────────────

func TestT373_SavePlan_CrossTeamDeniedWithZeroWrites(t *testing.T) {
	e := t373Env(t)
	path, body := t373SavePlan(t373OtherPatient)

	w, resp := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Equal(t, 0, e.store.createPlanCalls, "跨团队保存必须零次库写")
	assert.Equal(t, t373OtherPatient, e.store.lastPatientQuery, "患者行必须按带团队谓词的读法取")
}

func TestT373_SavePlan_SameTeamSucceeds(t *testing.T) {
	e := t373Env(t)
	e.store.latest, e.store.hasLatest = "v1.0", true
	path, body := t373SavePlan(t373OwnPatient)

	w, resp := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, 1, e.store.createPlanCalls, "反证：本团队内医护写面不许被锁死")
	var saved model.OrthosisPlanDTO
	require.NoError(t, json.Unmarshal(resp.Data, &saved))
	assert.Equal(t, "v1.1", saved.Version)
}

func TestT373_SavePlan_DoctorWithoutTeamFailsClosed(t *testing.T) {
	e := t373Env(t)
	t350DoctorInTeam(e, "")
	path, body := t373SavePlan(t373OwnPatient)

	w, _ := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.createPlanCalls)
}

func TestT373_SavePlan_MissingIdentityTouchesNothing(t *testing.T) {
	e := t373Env(t)
	path, body := t373SavePlan(t373OwnPatient)

	w, _ := e.do(http.MethodPost, path, body, map[string]string{"X-Role": t350DoctorHDR})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "", e.store.lastPatientQuery, "身份缺失不得触库")
	assert.Equal(t, 0, e.store.createPlanCalls)
}

func TestT373_SavePlan_StaffRolesKeepLegacyShape(t *testing.T) {
	for _, role := range []string{"ROLE_ADMIN", "ROLE_CS"} {
		t.Run(role, func(t *testing.T) {
			e := t373Env(t)
			path, body := t373SavePlan(t373OtherPatient)

			w, resp := e.do(http.MethodPost, path, body, t350Hdr(role, "ADM-001"))
			require.Equal(t, http.StatusOK, w.Code, resp.Message)
			assert.Equal(t, 1, e.store.createPlanCalls, "%s 保留跨团队写", role)

			missPath, missBody := t373SavePlan("PAT-NOPE")
			w2, resp2 := e.do(http.MethodPost, missPath, missBody, t350Hdr(role, "ADM-001"))
			assert.Equal(t, http.StatusNotFound, w2.Code, resp2.Message)
			assert.Equal(t, model.CodeNotFound, resp2.Code, "查无此人仍 404，原语义不变")
		})
	}
}

// ── 3. A1 方案历史读取：跨团队 403 且不触库 ────────────────────────────

func TestT373_ListPlans_CrossTeamDeniedWithoutRead(t *testing.T) {
	e := t373Env(t)
	w, resp := e.do(http.MethodGet, "/api/v1/patients/"+t373OtherPatient+"/orthosis-plans",
		nil, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Equal(t, 0, e.store.planListCalls, "越权读不得触库取方案")
}

func TestT373_ListPlans_SameTeamSucceeds(t *testing.T) {
	e := t373Env(t)
	e.store.plans = []repo.OrthosisPlanRow{{
		PlanID: 1, PatientID: t373OwnPatient, DoctorID: "D0001", Content: "方案A",
		Version: "v1.0", CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}}
	w, resp := e.do(http.MethodGet, "/api/v1/patients/"+t373OwnPatient+"/orthosis-plans",
		nil, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var list []model.OrthosisPlanDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	assert.Len(t, list, 1)
	assert.Equal(t, 1, e.store.planListCalls)
}

func TestT373_ListPlans_NoExistenceOracleForDoctor(t *testing.T) {
	e := t373Env(t)
	wCross, cross := e.do(http.MethodGet, "/api/v1/patients/"+t373OtherPatient+"/orthosis-plans",
		nil, t350Hdr(t350DoctorHDR, t373DoctorAdmin))
	wMiss, miss := e.do(http.MethodGet, "/api/v1/patients/PAT-NOPE/orthosis-plans",
		nil, t350Hdr(t350DoctorHDR, t373DoctorAdmin))

	assert.Equal(t, http.StatusForbidden, wCross.Code)
	assert.NotEqual(t, http.StatusNotFound, wMiss.Code, "受限身份在方案历史上永不出 404")
	assert.Equal(t, cross.Code, miss.Code)
	assert.Equal(t,
		t373Shape(cross.Message, t373OtherPatient),
		t373Shape(miss.Message, "PAT-NOPE"))
}

func TestT373_ListPlans_StaffRolesKeepLegacyShape(t *testing.T) {
	for _, role := range []string{"ROLE_ADMIN", "ROLE_CS"} {
		t.Run(role, func(t *testing.T) {
			e := t373Env(t)
			w, resp := e.do(http.MethodGet, "/api/v1/patients/"+t373OtherPatient+"/orthosis-plans",
				nil, t350Hdr(role, "ADM-001"))
			require.Equal(t, http.StatusOK, w.Code, resp.Message)
			assert.Equal(t, 1, e.store.planListCalls, "%s 保留跨任意患者读", role)

			w2, resp2 := e.do(http.MethodGet, "/api/v1/patients/PAT-NOPE/orthosis-plans",
				nil, t350Hdr(role, "ADM-001"))
			assert.Equal(t, http.StatusNotFound, w2.Code, resp2.Message)
			assert.Equal(t, model.CodeNotFound, resp2.Code, "查无此人仍 404，原语义不变")
		})
	}
}
