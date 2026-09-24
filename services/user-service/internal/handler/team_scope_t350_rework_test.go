// T350 返工（D-2 患者维度感受日志 / D-3 患者维度复查记录）：两条 self-only 端点的医护身份腿。
//
// 缺陷原貌（PM 2026-09-25 派发单 二、D-1 与 D-2）：#205 与 #208 收口的都是「列表 + 详情 + 写侧」，
// 这两条按 patientId 取的读链路仍停在 T184/T130 的 T264 闸门（role != ADMIN ⇒ X-User-Id 必须等于
// patientId）⇒ 医生令牌连本团队患者也 403，工作台切患者时感受/复查两块内容一起打空。
// 现网三层用例都不覆盖「医护身份 × 这两条端点」，所以它能潜伏（派发单 b 项点名根因）。
//
// 本文件守四件事（两条端点逐条同形，清单驱动，防「修了一条漏一条」）：
//  1. 只收紧的方向正确：本团队可读回真数据（反证，空集样本证不了这条）；
//  2. 判定排在触库前：跨团队 / 无团队 / 查无此人 / 缺身份四格一律「列表读次数 0」；
//  3. 存在性不泄露：受限身份在患者号维度永不出 404，折掉调用者自报的号后同码同文案；
//  4. 只收紧不放宽：运营跨团队仍 200、查无此人仍 404；客服 / 技师仍吃原 self-only 403 文案；
//     患者本人 200 / 他人 403 一字不变。
//
// 团队范围推导本身（resolveTeamScope / GetPatientInTeam）的用例在 team_scope_t350_test.go，
// 本文件不重复审推导来源，只审这两处入口的接线。
package handler

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t350rOwnPatient     = "PAT-OWN"    // team_id = TEAM01（医护本人团队）
	t350rOtherPatient   = "PAT-OTHER"  // team_id = TEAM02（他团队）
	t350rNoTeamPatient  = "PAT-NOTEAM" // team_id 为 NULL
	t350rMissingPatient = "PAT-NOPE"
	t350rDoctorAdmin    = "ADM-D-T350R"
)

// t350rEndpoint 一条患者维度读路径 + 它的读替身计数器
type t350rEndpoint struct {
	name   string
	path   func(string) string
	legacy string // T264 self-only 闸门的原文案，只收紧不放宽 ⇒ 逐字守住
	calls  func(*fakeStore) int
	query  func(*fakeStore) string
}

var t350rEndpoints = []t350rEndpoint{
	{
		name:   "feeling-logs",
		path:   func(pid string) string { return "/api/v1/patients/" + pid + "/feeling-logs" },
		legacy: "may only query your own feeling logs",
		calls:  func(s *fakeStore) int { return s.feelingListCalls },
		query:  func(s *fakeStore) string { return s.lastFeelingQuery },
	},
	{
		name:   "review-records",
		path:   func(pid string) string { return "/api/v1/patients/" + pid + "/review-records" },
		legacy: "may only query your own review records",
		calls:  func(s *fakeStore) int { return s.reviewListCalls },
		query:  func(s *fakeStore) string { return s.lastReviewQuery },
	},
}

// t350rPatient 造一名患者；teamID 传空串表示 patients.team_id 为 NULL（未分配团队）
func t350rPatient(pid, teamID string) repo.PatientRow {
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

// t350rEnv 三名患者 + 各一条本团队数据（反证样本必须非空，否则分不清「放行」与「读回空集」）
func t350rEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, t350OwnTeam)
	e.store.patient = nil
	e.store.patients = []repo.PatientRow{
		t350rPatient(t350rOwnPatient, t350OwnTeam),
		t350rPatient(t350rOtherPatient, t350OtherTeam),
		t350rPatient(t350rNoTeamPatient, ""),
	}
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	e.store.feelings = []repo.FeelingLogRow{
		{LogID: 11, PatientID: t350rOwnPatient, LogDate: at, CreatedAt: at, ComfortScore: floatPtr(4)},
	}
	e.store.reviewRows = []repo.ReviewRecordRow{
		{ReviewID: "RV-T350R", PatientID: t350rOwnPatient, ReviewDate: at, CreatedAt: at, UpdatedAt: at},
	}
	return e
}

// t350rShape 折掉调用者自己填的患者号，用于比对两种拒绝是否「同形」
func t350rShape(msg, id string) string {
	return strings.ReplaceAll(msg, id, "<pid>")
}

// ── 1. 反证：本团队患者照常可读回真数据（派发单 b 项要守的就是这一格）────

func TestT350R_DoctorOwnTeamReads(t *testing.T) {
	for _, ep := range t350rEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			e := t350rEnv(t)

			w, resp := e.do(http.MethodGet, ep.path(t350rOwnPatient), nil,
				t350Hdr(t350DoctorHDR, t350rDoctorAdmin))
			require.Equal(t, http.StatusOK, w.Code, resp.Message)
			assert.Equal(t, model.CodeOK, resp.Code)
			assert.Equal(t, 1, ep.calls(e.store), "本团队医护必须真读到一次，否则等于把医生写面一起锁死")
			assert.Equal(t, t350rOwnPatient, ep.query(e.store))
			assert.Contains(t, string(resp.Data), t350rOwnPatient, "反证样本非空：读回的不是空集")
		})
	}
}

// ── 2. 四格合一 403，且判定排在触库前（列表读次数 0）─────────────────────

func TestT350R_DoctorDeniedCellsFoldIntoForbiddenWithoutRead(t *testing.T) {
	cases := []struct{ name, patientID string }{
		{"跨团队患者", t350rOtherPatient},
		{"患者未分配团队", t350rNoTeamPatient},
		{"查无此人", t350rMissingPatient},
	}
	for _, ep := range t350rEndpoints {
		for _, tc := range cases {
			t.Run(ep.name+" "+tc.name, func(t *testing.T) {
				e := t350rEnv(t)

				w, resp := e.do(http.MethodGet, ep.path(tc.patientID), nil,
					t350Hdr(t350DoctorHDR, t350rDoctorAdmin))
				require.Equal(t, http.StatusForbidden, w.Code, resp.Message)
				assert.Equal(t, model.CodeForbidden, resp.Code)
				assert.Equal(t, 0, ep.calls(e.store), "越权读不得触库取数据")
				assert.NotContains(t, string(resp.Data), t350rOwnPatient)
			})
		}
	}
}

// 无团队归属的医护：连本团队以外的正常患者也读不到（fail-closed，不退化成不过滤）
func TestT350R_DoctorWithoutTeamFailsClosed(t *testing.T) {
	for _, ep := range t350rEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			e := t350rEnv(t)
			t350DoctorInTeam(e, "")

			w, _ := e.do(http.MethodGet, ep.path(t350rOwnPatient), nil,
				t350Hdr(t350DoctorHDR, t350rDoctorAdmin))
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Equal(t, 0, ep.calls(e.store))
		})
	}
}

// 缺 X-User-Id：推导无法进行，必须在触库前 403
func TestT350R_MissingIdentityTouchesNothing(t *testing.T) {
	for _, ep := range t350rEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			e := t350rEnv(t)

			w, resp := e.do(http.MethodGet, ep.path(t350rOwnPatient), nil,
				map[string]string{"X-Role": t350DoctorHDR})
			assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
			assert.Equal(t, "", e.store.lastPatientQuery, "身份缺失不得触库")
			assert.Equal(t, 0, ep.calls(e.store))
		})
	}
}

// 推导报错：不得退化成「不判归属」
func TestT350R_TeamLookupErrorFailsClosed(t *testing.T) {
	for _, ep := range t350rEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			e := t350rEnv(t)
			e.store.doctorTeamErr = errors.New("db down")

			w, _ := e.do(http.MethodGet, ep.path(t350rOwnPatient), nil,
				t350Hdr(t350DoctorHDR, t350rDoctorAdmin))
			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Equal(t, 0, ep.calls(e.store))
		})
	}
}

// 存在性不泄露：跨团队与查无此人对医护同码同文案，这一端点永不出 404
func TestT350R_NoExistenceOracleForDoctor(t *testing.T) {
	for _, ep := range t350rEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			e := t350rEnv(t)
			wCross, cross := e.do(http.MethodGet, ep.path(t350rOtherPatient), nil,
				t350Hdr(t350DoctorHDR, t350rDoctorAdmin))
			wMiss, miss := e.do(http.MethodGet, ep.path(t350rMissingPatient), nil,
				t350Hdr(t350DoctorHDR, t350rDoctorAdmin))

			require.Equal(t, http.StatusForbidden, wCross.Code, cross.Message)
			assert.NotEqual(t, http.StatusNotFound, wMiss.Code, "受限身份永不出 404")
			assert.Equal(t, wCross.Code, wMiss.Code)
			assert.Equal(t, cross.Code, miss.Code)
			assert.Equal(t,
				t350rShape(cross.Message, t350rOtherPatient),
				t350rShape(miss.Message, t350rMissingPatient),
				"跨团队与查无此人必须同形，否则患者号存在性可枚举")
			assert.Equal(t, 0, ep.calls(e.store))
		})
	}
}

// ── 3. 只收紧不放宽：其余角色的入参与状态码逐字不变 ──────────────────────

func TestT350R_AdminKeepsCrossTeamReadAnd404(t *testing.T) {
	for _, ep := range t350rEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			e := t350rEnv(t)
			t350DoctorInTeam(e, t350OwnTeam) // 非医护身份即便能查到团队也不参与过滤

			w, resp := e.do(http.MethodGet, ep.path(t350rOtherPatient), nil,
				t350Hdr("ROLE_ADMIN", "ADM-001"))
			require.Equal(t, http.StatusOK, w.Code, resp.Message)
			assert.Equal(t, 1, ep.calls(e.store), "ROLE_ADMIN 保留跨团队读")

			w2, resp2 := e.do(http.MethodGet, ep.path(t350rMissingPatient), nil,
				t350Hdr("ROLE_ADMIN", "ADM-001"))
			assert.Equal(t, http.StatusNotFound, w2.Code, resp2.Message)
			assert.Equal(t, model.CodeNotFound, resp2.Code, "查无此人仍 404（T353 口径不变）")
		})
	}
}

// 客服 / 技师：仍吃 T264 的 self-only 403，本卡只给医护开门，不开别的口子
func TestT350R_SelfOnlyRolesKeepLegacyText(t *testing.T) {
	for _, ep := range t350rEndpoints {
		for _, role := range []string{"ROLE_CS", "technician"} {
			t.Run(ep.name+" "+role, func(t *testing.T) {
				e := t350rEnv(t)

				w, resp := e.do(http.MethodGet, ep.path(t350rOwnPatient), nil,
					t350Hdr(role, "ADM-001"))
				require.Equal(t, http.StatusForbidden, w.Code, resp.Message)
				assert.Contains(t, resp.Message, ep.legacy, "原文案逐字守住，不得被团队推导顶掉")
				assert.Equal(t, 0, ep.calls(e.store))
				assert.Equal(t, "", e.store.lastPatientQuery, "self-only 闸门在触库前")
			})
		}
	}
}

// 患者本人：200 / 他人 403，一字不变
func TestT350R_PatientSelfScopeUnchanged(t *testing.T) {
	for _, ep := range t350rEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			e := t350rEnv(t)

			w, resp := e.do(http.MethodGet, ep.path(t350rOwnPatient), nil,
				map[string]string{"X-Role": "patient", "X-User-Id": t350rOwnPatient})
			require.Equal(t, http.StatusOK, w.Code, resp.Message)
			assert.Equal(t, 1, ep.calls(e.store))

			w2, resp2 := e.do(http.MethodGet, ep.path(t350rOtherPatient), nil,
				map[string]string{"X-Role": "patient", "X-User-Id": t350rOwnPatient})
			assert.Equal(t, http.StatusForbidden, w2.Code, resp2.Message)
			assert.Contains(t, resp2.Message, ep.legacy)
		})
	}
}
