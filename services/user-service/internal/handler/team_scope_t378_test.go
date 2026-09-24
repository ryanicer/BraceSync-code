// T378：患者沟通（feedbacks）域读写归属判据。
//
// 缺陷原貌：#205（T350）收口的是「患者列表 / 详情 / 告警 / 感受日志 / 数据概览 + 患者域三条读」，
// #208（T373）补的是「回复日志 / 存方案 / 方案历史」——feedbacks 这一族两头都不在枚举里，
// 于是任意医护令牌能：① 把别人团队那条反馈的状态推成 resolved（T374 起无反向接口，不可回滚）、
// ② 读全院反馈列表与全院统计条。
//
// 本文件守四件事（卡面三条判据 + 红线第 3 条）：
//  1. 越权必拒，且拒绝路径下库写次数为 0（只回 403 算半个判据）；
//  2. 反证：本团队内的处理动作与读数照常成功，防过度收紧把医护的正常面锁死；
//  3. 只收紧不放宽：运营 / 客服不探测、不过滤，「查无 → 404」逐字不变；
//  4. 状态机语义不动（T374 的两动作分流仍由 replyContent / markResolved 决定，此处不重复审）。
//
// 团队范围推导本身的用例在 team_scope_t350_test.go，本文件只审接线。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t378OwnPatient   = "P-T378-OWN"   // team_id = TEAM01（医护本人团队）
	t378OtherPatient = "P-T378-OTHER" // team_id = TEAM02
	t378NoTeamName   = "P-T378-NOTEAM"
	t378OwnFB        = 11 // 挂在 P-T378-OWN 名下
	t378OtherFB      = 22 // 挂在 P-T378-OTHER 名下
	t378NoTeamFB     = 33 // 挂在未分配团队的患者名下
	t378MissingFB    = 99
	t378DoctorAdmin  = "ADM-D001"
)

// t378Patient 造一名患者；teamID 传空串表示 patients.team_id 为 NULL（未分配团队）
func t378Patient(pid, teamID string) repo.PatientRow {
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

// t378FB 造一条反馈（三条分属本团队 / 他团队 / 无团队患者）
func t378FB(id int64, pid string) repo.FeedbackRow {
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return repo.FeedbackRow{
		FeedbackID: id, PatientID: pid, Content: "佩戴不适", Status: "pending", SubmitTime: at,
	}
}

// t378Env 三名患者 + 三条反馈，医护账号默认属 TEAM01
func t378Env(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, t350OwnTeam)
	e.store.patients = []repo.PatientRow{
		t378Patient(t378OwnPatient, t350OwnTeam),
		t378Patient(t378OtherPatient, t350OtherTeam),
		t378Patient(t378NoTeamName, ""),
	}
	e.store.feedbacks = []repo.FeedbackRow{
		t378FB(t378OwnFB, t378OwnPatient),
		t378FB(t378OtherFB, t378OtherPatient),
		t378FB(t378NoTeamFB, t378NoTeamName),
	}
	e.store.processOK = true
	return e
}

func t378Process(id int64) (string, map[string]any) {
	return "/api/v1/feedbacks/" + strconv.FormatInt(id, 10) + "/process",
		map[string]any{"replyContent": "已联系家长"}
}

// t378UntouchedScope 列表/统计替身的哨兵初值：用例结束后仍是本值即证明未触达 store。
var t378UntouchedScope = repo.FeedbackScope{TeamID: "__untouched__"}

// ── 1. 写侧：处理动作越权必拒，且拒绝路径零库写 ─────────────────────────

func TestT378_Process_CrossTeamDeniedWithZeroWrites(t *testing.T) {
	e := t378Env(t)
	path, body := t378Process(t378OtherFB)

	w, resp := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Equal(t, 0, e.store.processCalls, "跨团队处理必须零次库写，否则状态已被推成不可回滚的 resolved")
	assert.Equal(t, 1, e.store.feedbackTeamCalls, "判定只走只读探测")
}

func TestT378_Process_MarkResolvedCrossTeamAlsoDenied(t *testing.T) {
	e := t378Env(t)

	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks/22/process",
		map[string]any{"markResolved": true}, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, 0, e.store.processCalls, "「标记已处理」是这条链路上最重的一写，同样不得触库")
}

func TestT378_Process_SameTeamSucceeds(t *testing.T) {
	e := t378Env(t)
	path, body := t378Process(t378OwnFB)

	w, resp := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, 1, e.store.processCalls, "反证：本团队内的医护处理动作不许被锁死")
	assert.Equal(t, "已联系家长", *e.store.lastProcessR)
}

func TestT378_Process_PatientWithoutTeamFailsClosed(t *testing.T) {
	e := t378Env(t)
	path, body := t378Process(t378NoTeamFB)

	w, _ := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.processCalls, "患者未分配团队不得当成「不过滤」放行")
}

func TestT378_Process_DoctorWithoutTeamFailsClosed(t *testing.T) {
	e := t378Env(t)
	t350DoctorInTeam(e, "") // doctors.team_id 为 NULL
	path, body := t378Process(t378OwnFB)

	w, _ := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.processCalls)
}

func TestT378_Process_MissingIdentityTouchesNothing(t *testing.T) {
	e := t378Env(t)
	path, body := t378Process(t378OwnFB)

	w, resp := e.do(http.MethodPost, path, body, map[string]string{"X-Role": t350DoctorHDR})
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, 0, e.store.feedbackTeamCalls, "身份缺失不得触库，连只读探测都不发")
	assert.Equal(t, 0, e.store.processCalls)
}

func TestT378_Process_ScopeLookupErrorFailsClosed(t *testing.T) {
	e := t378Env(t)
	e.store.feedbackTeamErr = errors.New("db down")
	path, body := t378Process(t378OwnFB)

	w, _ := e.do(http.MethodPost, path, body, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, 0, e.store.processCalls, "团队推导失败必须停在写之前")
}

// TestT378_Process_CrossTeamAndMissingAreIndistinguishable 存在性不泄露：
// 同一把医护令牌打「他团队那条」与「根本不存在那条」必须同码同形。
func TestT378_Process_CrossTeamAndMissingAreIndistinguishable(t *testing.T) {
	e := t378Env(t)
	pathCross, body := t378Process(t378OtherFB)
	pathMissing, _ := t378Process(t378MissingFB)

	w1, r1 := e.do(http.MethodPost, pathCross, body, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	w2, r2 := e.do(http.MethodPost, pathMissing, body, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	require.Equal(t, http.StatusForbidden, w1.Code, r1.Message)
	require.Equal(t, http.StatusForbidden, w2.Code, r2.Message)
	assert.Equal(t, model.CodeForbidden, r2.Code)
	assert.NotEmpty(t, r1.Message)
	// 两条文案只差调用方自己给出的那个 id（t373Shape 折成占位符）；折后必须逐字相同。
	assert.Equal(t,
		t373Shape(r1.Message, strconv.FormatInt(t378OtherFB, 10)),
		t373Shape(r2.Message, strconv.FormatInt(t378MissingFB, 10)),
		"跨团队与查无此 id 必须同形，否则 feedbackId 存在性可辨")
	assert.Equal(t, 0, e.store.processCalls)
}

// TestT378_Process_OpsRoleKeepsLegacyStatusCodes 只收紧不放宽：
// 运营 / 客服不触发归属探测，查无此条仍是 404（不是 403）。
func TestT378_Process_OpsRoleKeepsLegacyStatusCodes(t *testing.T) {
	for _, role := range []string{"ROLE_ADMIN", "ROLE_CS"} {
		e := t378Env(t)
		e.store.processOK = false // 反馈不存在
		path, body := t378Process(t378MissingFB)

		w, resp := e.do(http.MethodPost, path, body, t350Hdr(role, "ADM-001"))
		assert.Equal(t, http.StatusNotFound, w.Code, "%s: %s", role, resp.Message)
		assert.Equal(t, model.CodeNotFound, resp.Code)
		assert.Equal(t, 0, e.store.feedbackTeamCalls, "%s 不得进入团队探测路径", role)
	}
}

// ── 2. 读侧：列表与统计条按团队收窄，且两者同一条谓词 ───────────────────

func TestT378_ListFeedbacks_DoctorScopedToOwnTeam(t *testing.T) {
	e := t378Env(t)

	w, resp := e.do(http.MethodGet, "/api/v1/feedbacks", nil, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.True(t, e.store.lastFeedbackListScope.TeamScoped)
	assert.Equal(t, t350OwnTeam, e.store.lastFeedbackListScope.TeamID)

	var list []model.FeedbackDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1, "医护只该看到本团队那条反馈")
	assert.Equal(t, t378OwnPatient, list[0].PatientID)
}

func TestT378_ListFeedbacks_DoctorWithoutTeamFailsClosed(t *testing.T) {
	e := t378Env(t)
	t350DoctorInTeam(e, "")

	w, resp := e.do(http.MethodGet, "/api/v1/feedbacks", nil, t350Hdr(t350DoctorHDR, t378DoctorAdmin))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.True(t, e.store.lastFeedbackListScope.TeamScoped, "无团队不等于不受限")
	assert.Equal(t, "", e.store.lastFeedbackListScope.TeamID)

	var list []model.FeedbackDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	assert.Len(t, list, 0, "无团队归属必须落空集，不得回落到全院")
}

func TestT378_ListFeedbacks_OpsUnscoped(t *testing.T) {
	e := t378Env(t)
	e.store.lastFeedbackListScope = t378UntouchedScope

	w, resp := e.do(http.MethodGet, "/api/v1/feedbacks", nil, t350Hdr("ROLE_ADMIN", "ADM-001"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.False(t, e.store.lastFeedbackListScope.TeamScoped, "运营侧不得带团队谓词")
	assert.NotEqual(t, t378UntouchedScope, e.store.lastFeedbackListScope, "确认真的把 scope 传到了 store")

	var list []model.FeedbackDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	assert.Len(t, list, 3, "反证：运营仍见全量三条")
}

func TestT378_ListFeedbacks_MissingIdentityTouchesNothing(t *testing.T) {
	e := t378Env(t)
	e.store.lastFeedbackListScope = t378UntouchedScope

	w, _ := e.do(http.MethodGet, "/api/v1/feedbacks", nil, map[string]string{"X-Role": t350DoctorHDR})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, t378UntouchedScope, e.store.lastFeedbackListScope, "身份缺失不得触库")
}

func TestT378_FeedbackStats_SameScopeAsList(t *testing.T) {
	e := t378Env(t)
	hdr := t350Hdr(t350DoctorHDR, t378DoctorAdmin)

	w, resp := e.do(http.MethodGet, "/api/v1/feedbacks", nil, hdr)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	listScope := e.store.lastFeedbackListScope
	require.Equal(t, repo.FeedbackScope{TeamScoped: true, TeamID: t350OwnTeam}, listScope)

	w, resp = e.do(http.MethodGet, "/api/v1/feedbacks/stats", nil, hdr)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	// 三项计数与列表页共用同一条团队谓词：两处 scope 必须逐字相等，
	// 否则就是「列表 1 条、统计条 12 条」那类同页两口径（T371 族）。
	assert.Equal(t, listScope, e.store.lastFeedbackStatsScope)
}

func TestT378_FeedbackStats_OpsUnscoped(t *testing.T) {
	e := t378Env(t)

	w, resp := e.do(http.MethodGet, "/api/v1/feedbacks/stats", nil, t350Hdr("ROLE_CS", "ADM-002"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.False(t, e.store.lastFeedbackStatsScope.TeamScoped, "客服按矩阵取全量，不受团队隔离")
}
