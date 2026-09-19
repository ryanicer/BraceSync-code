// Package handler — T248 admin 后端配套实现侧测试
//
// 覆盖两条 user-service 条目：
//   - 7.1 GET /api/v1/feedbacks/stats：统计栏三项 + 今日窗口必须按 Asia/Shanghai 切日
//   - 4.3 PUT /api/v1/admin/patients/:patientId：admin 档案编辑（姓名/性别/年龄/诊断/Cobb）
//
// 4.3 复用 T226 已装的 fakeStore.UpdatePatientProfile 桩与 spy 变量（同包，勿重复桩）。
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// ─────────────────────────────────────────────────────────────
// 7.1 统计栏
// ─────────────────────────────────────────────────────────────

var (
	t248StatsRow       repo.FeedbackStatsRow
	t248StatsStart     time.Time
	t248StatsEnd       time.Time
	t248StatsCallCount int
)

func (f *fakeStore) FeedbackStats(_ context.Context, todayStart, todayEnd time.Time) (repo.FeedbackStatsRow, error) {
	t248StatsStart, t248StatsEnd = todayStart, todayEnd
	t248StatsCallCount++
	return t248StatsRow, nil
}

func t248ResetStats() {
	t248StatsRow = repo.FeedbackStatsRow{}
	t248StatsStart = time.Time{}
	t248StatsEnd = time.Time{}
	t248StatsCallCount = 0
}

// TestT248_FeedbackStats_OK 三项字段透出 + 今日窗口按北京时间零点切日
func TestT248_FeedbackStats_OK(t *testing.T) {
	t248ResetStats()
	avg := 5400.0
	t248StatsRow = repo.FeedbackStatsRow{TodayCount: 12, PendingCount: 4, AvgReplySec: &avg}
	e := newEnv(t, true, true)

	w, resp := e.do(http.MethodGet, "/api/v1/feedbacks/stats", nil, selfHdr("ADMIN0001", roleAdmin))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	var dto model.FeedbackStatsDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, int64(12), dto.TodayCount)
	assert.Equal(t, int64(4), dto.PendingCount)
	require.NotNil(t, dto.AvgResponseSeconds)
	assert.Equal(t, 5400.0, *dto.AvgResponseSeconds)

	// 切日口径取证：窗口起点须为北京时间 00:00:00，止于次日零点（长度 1 天）
	require.Equal(t, 1, t248StatsCallCount)
	assert.Equal(t, "00:00:00", t248StatsStart.In(cstLoc).Format("15:04:05"))
	assert.True(t, t248StatsEnd.Equal(t248StatsStart.AddDate(0, 0, 1)),
		"今日窗口必须止于次日零点，实际 %v ~ %v", t248StatsStart, t248StatsEnd)
	assert.Equal(t, time.Now().In(cstLoc).Format("2006-01-02"), t248StatsStart.In(cstLoc).Format("2006-01-02"))
}

// TestT248_FeedbackStats_NoReplySample 无已回复样本 → avg 为 null（不以 0 冒充「秒回」）
func TestT248_FeedbackStats_NoReplySample(t *testing.T) {
	t248ResetStats()
	t248StatsRow = repo.FeedbackStatsRow{TodayCount: 3, PendingCount: 3}
	e := newEnv(t, true, true)

	w, resp := e.do(http.MethodGet, "/api/v1/feedbacks/stats", nil, selfHdr("ADMIN0001", roleAdmin))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, string(resp.Data), `"avgResponseSeconds":null`)
}

// ─────────────────────────────────────────────────────────────
// 4.3 admin 档案编辑
// ─────────────────────────────────────────────────────────────

func t248AdminHdr() map[string]string { return selfHdr("ADMIN0001", roleAdmin) }

func t248EditPayload() map[string]any {
	return map[string]any{
		"name":      "患者小明改",
		"gender":    "female",
		"age":       15,
		"diagnosis": "胸椎右侧凸 28°",
		"cobbAngle": 28.5,
	}
}

// TestT248_AdminEditPatient_OK 五字段逐项落到 repo 入参（字段级取证）
func TestT248_AdminEditPatient_OK(t *testing.T) {
	t248ResetStats()
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001", t248EditPayload(), t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	assert.Equal(t, "P20260001", t226LastUpdatePatient)
	require.NotNil(t, t226LastUpdate.Name)
	assert.Equal(t, "患者小明改", *t226LastUpdate.Name)
	require.NotNil(t, t226LastUpdate.Gender)
	assert.Equal(t, "female", *t226LastUpdate.Gender)
	require.NotNil(t, t226LastUpdate.Age)
	assert.Equal(t, 15, *t226LastUpdate.Age)
	require.NotNil(t, t226LastUpdate.Diagnosis)
	assert.Equal(t, "胸椎右侧凸 28°", *t226LastUpdate.Diagnosis)
	require.NotNil(t, t226LastUpdate.CobbAngle)
	assert.Equal(t, 28.5, *t226LastUpdate.CobbAngle)
	// 身高/体重/紧急联系人属患者自助字段，本端点不得顺手写
	assert.Nil(t, t226LastUpdate.HeightCm)
	assert.Nil(t, t226LastUpdate.WeightKg)
	assert.Nil(t, t226LastUpdate.EmergencyContactPhone)

	var dto model.AdminPatientDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "P20260001", dto.PatientID)
}

// TestT248_AdminEditPatient_AdminOnly 非 admin / 缺角色头 → 403 且数据层零调用
func TestT248_AdminEditPatient_AdminOnly(t *testing.T) {
	t248ResetStats()
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	for _, hdr := range []map[string]string{
		selfHdr("D0001", "doctor"),
		selfHdr("CS001", "cs"),
		selfHdr("P20260001", "patient"),
		{}, // 缺头 fail-closed
	} {
		w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001", t248EditPayload(), hdr)
		assert.Equal(t, http.StatusForbidden, w.Code, "角色 %v 不得改他人档案", hdr)
		assert.Equal(t, model.CodeForbidden, resp.Code)
		assert.Empty(t, t226LastUpdatePatient, "拒绝时不得触达数据层")
	}
}

// TestT248_AdminEditPatient_NotFound 患者不存在 → 404
func TestT248_AdminEditPatient_NotFound(t *testing.T) {
	t248ResetStats()
	t226ResetStoreSpies()
	e := newEnv(t, true, true) // 不装载 patient → 桩返回 ErrPatientNotFound

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P-NOPE", t248EditPayload(), t248AdminHdr())
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, model.CodeNotFound, resp.Code)
}

// TestT248_AdminEditPatient_FieldGuards 归属/敏感字段一律 400；空请求体 400；越界值 400
func TestT248_AdminEditPatient_FieldGuards(t *testing.T) {
	t248ResetStats()
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	// phone 有专属端点（涉及 phone_hash 唯一键）；teamId/primaryDoctorId/status 属团队与指派通道
	for _, key := range []string{"phone", "teamId", "primaryDoctorId", "status", "heightCm", "deviceId"} {
		payload := t248EditPayload()
		delete(payload, "cobbAngle")
		payload[key] = "injected"
		w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001", payload, t248AdminHdr())
		assert.Equal(t, http.StatusBadRequest, w.Code, "非本端点字段 %q 必须 400", key)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
		assert.Empty(t, t226LastUpdatePatient)
	}

	// 空体：无字段可改
	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001", map[string]any{}, t248AdminHdr())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)

	// 值域
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"cobbAngle", 200},
		{"cobbAngle", -1},
		{"age", 200},
		{"gender", "other"},
		{"name", ""},
	} {
		t226ResetStoreSpies()
		w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001",
			map[string]any{tc.key: tc.value}, t248AdminHdr())
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s=%v 应拒绝", tc.key, tc.value)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
		assert.Empty(t, t226LastUpdatePatient)
	}
}
