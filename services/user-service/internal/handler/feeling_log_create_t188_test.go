// Package handler — T188 患者端佩戴感受录入（POST /api/v1/patients/:patientId/feeling-logs）
//
// 口径按 Boss 2026-09-23 18:55 裁决方案 A：两档 fitted|discomfort，独立「支具贴合度」
// 控件取消 ⇒ 请求体不含 fitLevel，也不做三档到两档的归并。
// 本文件只证 handler 层的校验与装配（含「三档值必须被拒」这条 A 的护栏）；
// 真库同日覆盖 + 不清医生回复位见 internal/repo/feeling_log_create_t188_it_test.go。
package handler

import (
	"encoding/json"
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

const t188Patient = "P20260001"

// t188SavedRow 假库回读行：logId 与 createdAt 由库生成，用来验证响应确实是落库后的整行
func t188SavedRow(areas ...string) repo.FeelingLogRow {
	level := "discomfort"
	notes := "胸椎处压得疼"
	return repo.FeelingLogRow{
		LogID:           4412,
		PatientID:       t188Patient,
		LogDate:         time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
		ComfortLevel:    &level,
		DiscomfortAreas: areas,
		Notes:           &notes,
		CreatedAt:       time.Date(2026, 9, 23, 10, 12, 0, 0, time.UTC),
	}
}

func t188Body(fields map[string]any, omit ...string) map[string]any {
	body := map[string]any{
		"logDate":         "2026-09-23",
		"feeling":         "discomfort",
		"discomfortAreas": []string{"胸椎", "右侧腰"},
		"notes":           "胸椎处压得疼",
	}
	for k, v := range fields {
		body[k] = v
	}
	for _, k := range omit {
		delete(body, k)
	}
	return body
}

func TestT188_CreateFeelingLog_OK(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feelingSaved = t188SavedRow("胸椎", "右侧腰")

	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil), selfHdr(t188Patient, "patient"))
	require.Equal(t, http.StatusOK, w.Code, "message=%s", resp.Message)

	in := e.store.feelingSaveIn
	assert.Equal(t, t188Patient, in.PatientID, "patientId 取路径，不被 body 覆盖")
	assert.Equal(t, "2026-09-23", in.LogDate)
	assert.Equal(t, "discomfort", in.ComfortLevel)
	assert.Equal(t, []string{"胸椎", "右侧腰"}, in.DiscomfortAreas)
	require.NotNil(t, in.Notes)
	assert.Equal(t, "胸椎处压得疼", *in.Notes)

	var dto model.FeelingLogDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "4412", dto.LogID, "响应回落库后的整行，前端保存后无需再查列表")
	require.NotNil(t, dto.Feeling)
	assert.Equal(t, "discomfort", *dto.Feeling)
	assert.Equal(t, []string{"胸椎", "右侧腰"}, dto.DiscomfortAreas)
	assert.Equal(t, "2026-09-23", dto.LogDate, "logDate 是业务日期（YYYY-MM-DD），不被提交时刻污染")
	assert.Equal(t, "2026-09-23T10:12:00Z", dto.CreatedAt)
}

// TestT188_CreateFeelingLog_BothTiersAccepted 两档都要能存（admin 筛选侧同两档词表）
func TestT188_CreateFeelingLog_BothTiersAccepted(t *testing.T) {
	for _, tier := range []string{"fitted", "discomfort"} {
		e := newEnv(t, true, true)
		e.store.feelingSaved = t188SavedRow()
		w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
			t188Body(map[string]any{"feeling": tier}), selfHdr(t188Patient, "patient"))
		require.Equal(t, http.StatusOK, w.Code, "档位 %q 应可存：message=%s", tier, resp.Message)
		assert.Equal(t, tier, e.store.feelingSaveIn.ComfortLevel)
	}
}

func TestT188_CreateFeelingLog_LogDateDefaultsToTodayCST(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feelingSaved = t188SavedRow()

	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil, "logDate"), selfHdr(t188Patient, "patient"))
	require.Equal(t, http.StatusOK, w.Code, "message=%s", resp.Message)
	// 切日口径与 feedbackStats 一致用 cstLoc（架构 §3.5），不用 UTC：
	// 北京 00:30 提交若按 UTC 会写成前一天。
	assert.Equal(t, time.Now().In(cstLoc).Format("2006-01-02"), e.store.feelingSaveIn.LogDate)
}

// TestT188_CreateFeelingLog_RejectsThreeTier 方案 A 的护栏：三档码值（PRD 旧稿 good/mild/pain）
// 一律 400，不得静默归并到两档——归并规则 Boss 明确不采（Q2 随 A 消失）。
func TestT188_CreateFeelingLog_RejectsThreeTier(t *testing.T) {
	for _, v := range []string{"good", "mild", "pain"} {
		e := newEnv(t, true, true)
		w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
			t188Body(map[string]any{"feeling": v}), selfHdr(t188Patient, "patient"))
		assert.Equal(t, http.StatusBadRequest, w.Code, "三档值 %q 应被拒", v)
		assert.Contains(t, resp.Message, "fitted|discomfort")
		assert.Zero(t, e.store.feelingSaveCalls, "校验失败不得触库")
	}
}

func TestT188_CreateFeelingLog_FeelingRequired(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil, "feeling"), selfHdr(t188Patient, "patient"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp.Message, "feeling is required")
	assert.Zero(t, e.store.feelingSaveCalls)
}

// TestT188_CreateFeelingLog_RejectsUnknownArea 词表按 Q4 裁定锁设计稿 8 区中文原词；
// 后台既有 mock/seed 用的英文码（thoracic 等）不在写侧白名单内，避免两套词混写。
func TestT188_CreateFeelingLog_RejectsUnknownArea(t *testing.T) {
	for _, area := range []string{"thoracic", "颈部", ""} {
		e := newEnv(t, true, true)
		w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
			t188Body(map[string]any{"discomfortAreas": []string{area}}), selfHdr(t188Patient, "patient"))
		assert.Equal(t, http.StatusBadRequest, w.Code, "部位 %q 应被拒", area)
		assert.Contains(t, resp.Message, "discomfortArea")
		assert.Zero(t, e.store.feelingSaveCalls)
	}
}

func TestT188_CreateFeelingLog_NotesLengthGuard(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(map[string]any{"notes": strings.Repeat("疼", 201)}), selfHdr(t188Patient, "patient"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp.Message, "notes exceeds")

	// 200 字（列宽上限）按字符计，不按字节：中文 3 字节/字，按字节会误拒
	e2 := newEnv(t, true, true)
	e2.store.feelingSaved = t188SavedRow()
	w2, resp2 := e2.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(map[string]any{"notes": strings.Repeat("疼", 200)}), selfHdr(t188Patient, "patient"))
	require.Equal(t, http.StatusOK, w2.Code, "message=%s", resp2.Message)
}

func TestT188_CreateFeelingLog_LogDateFormat(t *testing.T) {
	for _, bad := range []string{"2026-9-23", "2026/09/23", "20260923", "yesterday"} {
		e := newEnv(t, true, true)
		w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
			t188Body(map[string]any{"logDate": bad}), selfHdr(t188Patient, "patient"))
		assert.Equal(t, http.StatusBadRequest, w.Code, "logDate %q 应被拒", bad)
		assert.Contains(t, resp.Message, "logDate")
		assert.Zero(t, e.store.feelingSaveCalls)
	}
}

// TestT188_CreateFeelingLog_EmptyAreasAndNotes 只勾档位也要能存：部位可空、备注可空
func TestT188_CreateFeelingLog_EmptyAreasAndNotes(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feelingSaved = t188SavedRow()
	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(map[string]any{"discomfortAreas": []string{}, "notes": "   "}),
		selfHdr(t188Patient, "patient"))
	require.Equal(t, http.StatusOK, w.Code, "message=%s", resp.Message)
	assert.Empty(t, e.store.feelingSaveIn.DiscomfortAreas)
	assert.Nil(t, e.store.feelingSaveIn.Notes, "空白备注存 NULL，不存空串")

	var dto model.FeelingLogDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.NotNil(t, dto.DiscomfortAreas, "响应数组字段出 [] 不出 null（前端 .map 直接可用）")
}

// TestT188_CreateFeelingLog_SelfScope 水平鉴权同读端点：非本人患者 403，staff 可代录
func TestT188_CreateFeelingLog_SelfScope(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil), selfHdr("P20260002", "patient"))
	assert.Equal(t, http.StatusForbidden, w.Code, "message=%s", resp.Message)
	assert.Zero(t, e.store.feelingSaveCalls, "越权请求不得触库")

	// 无身份头：fail-closed
	e2 := newEnv(t, true, true)
	w2, _ := e2.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs", t188Body(nil), nil)
	assert.Equal(t, http.StatusForbidden, w2.Code)

	e3 := newEnv(t, true, true)
	e3.store.feelingSaved = t188SavedRow()
	w3, resp3 := e3.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil), map[string]string{"X-Role": "ROLE_DOCTOR", "X-User-Id": "D0001"})
	require.Equal(t, http.StatusOK, w3.Code, "staff 代录应放行：message=%s", resp3.Message)
	assert.Equal(t, t188Patient, e3.store.feelingSaveIn.PatientID)
}

func TestT188_CreateFeelingLog_PatientNotFound(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feelingSaveErr = repo.ErrPatientNotFound
	w, _ := e.do(http.MethodPost, "/api/v1/patients/NOPE/feeling-logs",
		t188Body(nil), map[string]string{"X-Role": "ROLE_ADMIN", "X-User-Id": "ADM001"})
	assert.Equal(t, http.StatusNotFound, w.Code, "外键不命中要 404，不能裸抛 500")
}

func TestT188_CreateFeelingLog_StoreError(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feelingSaveErr = errors.New("db down")
	w, _ := e.do(http.MethodPost, "/api/v1/patients/"+t188Patient+"/feeling-logs",
		t188Body(nil), selfHdr(t188Patient, "patient"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
