// Package handler — T311 反馈创建端点（POST /api/v1/feedbacks）
//
// 患者端配网失败页 submitWifiFailureFeedback 的落库通道：body 为
// { patientId, type, content, status? }，响应 { feedbackId }。
// fakeStore 只证明 handler 层的校验与装配；真库落库 + 列表回查见
// services/user-service/internal/repo/feedback_create_t311_it_test.go。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const t311Patient = "P20260001"

// t311Body 组装请求体；omit 列出的键不下发（用于验 status 缺省）
func t311Body(fields map[string]any, omit ...string) map[string]any {
	body := map[string]any{
		"patientId": t311Patient,
		"type":      "wifi_setup_failure",
		"content":   "WiFi 连接失败；设备 BSYNC-0A12；发生时间 2026-09-22 12:00",
		"status":    "pending",
	}
	for k, v := range fields {
		body[k] = v
	}
	for _, k := range omit {
		delete(body, k)
	}
	return body
}

func t311CreatedID(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var dto model.FeedbackCreatedDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	return dto.FeedbackID
}

func TestT311_CreateFeedback_OK(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feedbackID = 77

	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks", t311Body(nil), selfHdr(t311Patient, "patient"))
	require.Equal(t, http.StatusOK, w.Code, "body=%s", resp.Message)
	assert.Equal(t, model.CodeOK, resp.Code)
	assert.Equal(t, "77", t311CreatedID(t, resp.Data))

	in := e.store.feedbackIn
	assert.Equal(t, t311Patient, in.PatientID)
	assert.Equal(t, "wifi_setup_failure", in.Type)
	assert.Equal(t, "pending", in.Status)
	assert.True(t, strings.HasPrefix(in.Content, "WiFi 连接失败"), "content 原样落库：%s", in.Content)
}

// TestT311_CreateFeedback_StatusDefaultsPending status 不下发时按建表 DEFAULT 'pending' 补齐
// （前端 feedback.ts 目前恒发 pending，缺省分支是给其他调用方兜底的）
func TestT311_CreateFeedback_StatusDefaultsPending(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feedbackID = 78

	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks", t311Body(nil, "status"), selfHdr(t311Patient, "patient"))
	require.Equal(t, http.StatusOK, w.Code, "body=%s", resp.Message)
	assert.Equal(t, "pending", e.store.feedbackIn.Status)
	assert.Equal(t, "78", t311CreatedID(t, resp.Data))
}

// TestT311_CreateFeedback_PatientNotFound 外键不命中 → 404 明确错误码，不得 500
func TestT311_CreateFeedback_PatientNotFound(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feedbackErr = repo.ErrPatientNotFound

	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks", t311Body(nil), selfHdr(t311Patient, "patient"))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)
	assert.Contains(t, resp.Message, t311Patient)
}

// TestT311_CreateFeedback_ContentLengthBoundary 500 字放行、501 字 400。
// 取全中文正文：列宽按字符计，若校验按字节计，500 字中文（1500 字节）会被误拒。
func TestT311_CreateFeedback_ContentLengthBoundary(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feedbackID = 79

	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks",
		t311Body(map[string]any{"content": strings.Repeat("长", 500)}), selfHdr(t311Patient, "patient"))
	assert.Equal(t, http.StatusOK, w.Code, "500 字应放行：%s", resp.Message)

	w, resp = e.do(http.MethodPost, "/api/v1/feedbacks",
		t311Body(map[string]any{"content": strings.Repeat("长", 501)}), selfHdr(t311Patient, "patient"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Contains(t, resp.Message, "content")
}

// TestT311_CreateFeedback_TypeTooLong type 超 feedbacks.type VARCHAR(32) → 400
func TestT311_CreateFeedback_TypeTooLong(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks",
		t311Body(map[string]any{"type": strings.Repeat("t", 33)}), selfHdr(t311Patient, "patient"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Contains(t, resp.Message, "type")
}

// TestT311_CreateFeedback_BadStatus status 不在 CHECK 枚举内 → 400，不得落到库约束报错 500
func TestT311_CreateFeedback_BadStatus(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks",
		t311Body(map[string]any{"status": "closed"}), selfHdr(t311Patient, "patient"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
	assert.Empty(t, e.store.feedbackIn, "非法入参不得触库")
}

// TestT311_CreateFeedback_MissingRequired patientId / content 为空一律 400
func TestT311_CreateFeedback_MissingRequired(t *testing.T) {
	e := newEnv(t, true, true)
	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"patientId 缺失", t311Body(nil, "patientId")},
		{"patientId 空白", t311Body(map[string]any{"patientId": "   "})},
		{"content 缺失", t311Body(nil, "content")},
		{"content 空白", t311Body(map[string]any{"content": "  "})},
	} {
		w, resp := e.do(http.MethodPost, "/api/v1/feedbacks", tc.body, selfHdr(t311Patient, "patient"))
		assert.Equal(t, http.StatusBadRequest, w.Code, tc.name)
		assert.Equal(t, model.CodeInvalidParam, resp.Code, tc.name)
	}
	assert.Empty(t, e.store.feedbackIn, "非法入参不得触库")
}

// TestT311_CreateFeedback_CrossPatientForbidden 水平越权（T264 同族）：患者 token 只能为本人存档
func TestT311_CreateFeedback_CrossPatientForbidden(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feedbackID = 80

	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks", t311Body(nil), selfHdr("P20260002", "patient"))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Empty(t, e.store.feedbackIn, "越权请求不得触库")
}

// TestT311_CreateFeedback_StaffMayRecord staff（客服代录）不受 self-scope 约束
func TestT311_CreateFeedback_StaffMayRecord(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feedbackID = 81

	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks", t311Body(nil), selfHdr("CS0001", "ROLE_CS"))
	assert.Equal(t, http.StatusOK, w.Code, "body=%s", resp.Message)
	assert.Equal(t, t311Patient, e.store.feedbackIn.PatientID)
}

// TestT311_CreateFeedback_DBError 库异常仍按内部错误处理（不吞错、不伪装成功）
func TestT311_CreateFeedback_DBError(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.feedbackErr = errors.New("db down")

	w, resp := e.do(http.MethodPost, "/api/v1/feedbacks", t311Body(nil), selfHdr(t311Patient, "patient"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)
}
