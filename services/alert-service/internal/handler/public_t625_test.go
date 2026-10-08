// Package handler — T625 盲区用例：患者身份调 GET /api/v1/alerts 的可见面
//
// 本文件是**新增**测试文件，不改动 public_impl_test.go（T028 实现侧）/
// t264_authz_test.go（T264 水平鉴权）/ handler_test.go（测试专家侧）；
// 复用的是同包既有 harness：fakePublicStore（public_impl_test.go:28）、
// newPublicHandler（:112）、headerRole/headerUserID/roleAdmin（public.go:41-43）。
//
// 现读的 handler 面（public.go:170 listAlerts）：
//   - :184-190 非 staff 身份 → 强制 filter.PatientID = X-User-Id，缺失则 :187 直接 403（fail-closed）
//   - 此后**再无类型/级别过滤**：store 返回什么行就序列化什么行（:233-236 逐行 toAlertItem）
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

// doAsPatient 以患者身份（X-Role: patient + X-User-Id）发 GET，并允许携带越权查询参数
func doAsPatient(h *Handler, target, userID string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set(headerRole, "patient")
	if userID != "" {
		req.Header.Set(headerUserID, userID)
	}
	h.Router().ServeHTTP(rec, req)
	return rec
}

// ─────────────────────────────────────────────────────────────
// 1) 患者调用者被钉在自己的 patientId 上：越权参数失效、看不到他人告警（现可绿）
// ─────────────────────────────────────────────────────────────

func TestT625_PatientListCallerIsPinnedToOwnPatientId(t *testing.T) {
	store := &fakePublicStore{}
	h := newPublicHandler(store)

	// 三重越权写法：直接 ?patientId=、伪造他人 ID、伪造 type 试图绕开自己的范围
	rec := doAsPatient(h, "/api/v1/alerts?patientId=P-VICTIM&page=2&pageSize=7", "P-SELF")
	require.Equal(t, http.StatusOK, rec.Code)

	// 落到存储的过滤条件里，患者维度只剩自己的 ID —— 越权参数被整条覆盖（不是拼接、不是取交集）
	assert.Equal(t, "P-SELF", store.filter.PatientID,
		"非 staff 身份的 patientId 必须由服务端强制覆盖为 X-User-Id（public.go:190）")
	assert.NotEqual(t, "P-VICTIM", store.filter.PatientID, "调用方自报的 patientId 不得生效")
	assert.Equal(t, 2, store.filter.Page, "分页参数不受身份覆盖影响")
	assert.Equal(t, 7, store.filter.PageSize)
	assert.Equal(t, 1, store.listHits)

	// 空 X-User-Id 必须 fail-closed：不能让「没带身份」退化成「不带 patientId 过滤 = 全表」
	noID := &fakePublicStore{}
	rec = doAsPatient(newPublicHandler(noID), "/api/v1/alerts", "")
	assert.Equal(t, http.StatusForbidden, rec.Code, "患者身份缺 X-User-Id 应 403 而不是放行全量")
	assert.Zero(t, noID.listHits, "fail-closed 时不得触达存储")

	// staff 那一侧仍按 ?patientId= 过滤（对照：证明上面那条覆盖只对非 staff 生效）
	staff := &fakePublicStore{}
	recStaff := doGet(newPublicHandler(staff), "/api/v1/alerts?patientId=P-QUERY")
	require.Equal(t, http.StatusOK, recStaff.Code)
	assert.Equal(t, "P-QUERY", staff.filter.PatientID)
}

// ─────────────────────────────────────────────────────────────
// 2) 类 7：患者面不得出现 sensor_drift 这类内部诊断告警 —— pending
// ─────────────────────────────────────────────────────────────

// 现状（现读，勿当成期望）：
//   - 服务端无类型过滤：public.go:170 listAlerts 只在 :190 钉 patientId，
//     既不排除类型也不排级别，repo 返回的行被逐条 toAlertItem（:233-236）原样上线；
//     类型白名单 validAlertTypes（:66-72）只校验**入参** ?type=，对出参无约束。
//   - 隐藏只发生在前端：packages/shared-utils/src/index.ts:111 HIDDEN_ALERT_TYPES（现值只有
//     'pressure_fluctuation'）+ isHiddenAlertType（:113），消费点 apps/patient-miniapp/src/utils/anomaly.ts:58。
//   - ⇒ sensor_drift（"传感器标定异常"，engine.go:74 产出、msg-service model.go:30 同字面值）
//     目前会原样出现在患者拉取的响应体里：技师/运营侧的内部设备诊断直接暴露给患者，
//     且前端集合一旦漏配就是零防线（服务端与前端两处口径，真源只在前端 = 类 7 缺陷）。
func TestT625_PatientWireMustNotCarryInternalDiagnosticTypes(t *testing.T) {
	reason := "T621 未合入：alert-service 需对非 staff 调用者剔除内部诊断类型（sensor_drift 一类，" +
		"服务端出参侧过滤，而不是只靠前端 HIDDEN_ALERT_TYPES 隐藏）；合入后去掉本行即转绿"
	t.Skip(reason)

	store := &fakePublicStore{
		total: 2,
		rows: []repo.AlertRow{
			{AlertID: 1, PatientID: "P-SELF", Type: "sensor_drift", SensorPoint: "P04",
				Detail: "空载采集点 P04 读数 -0.2N 异常", Ts: time.Unix(0, 0).UTC()},
			{AlertID: 2, PatientID: "P-SELF", Type: "pressure_high", SensorPoint: "P03",
				Ts: time.Unix(0, 0).UTC()},
		},
	}
	rec := doAsPatient(newPublicHandler(store), "/api/v1/alerts", "P-SELF")
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.NotContains(t, body, "sensor_drift", "患者响应体里不得出现内部诊断类型")

	var env pageEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Len(t, env.Data.List, 1, "内部诊断行应被服务端摘掉，只剩患者可自阅的那条")
	assert.Equal(t, "pressure_high", env.Data.List[0].Type)

	// 同一条判据也要覆盖「患者显式 ?type=sensor_drift 想要它」的写法：不得因为入参白名单
	// 里有这个类型就把它送回患者手上（现读 validAlertTypes 含 sensor_drift，public.go:70）。
	explicit := &fakePublicStore{total: 1, rows: []repo.AlertRow{
		{AlertID: 3, PatientID: "P-SELF", Type: "sensor_drift", Ts: time.Unix(0, 0).UTC()},
	}}
	rec = doAsPatient(newPublicHandler(explicit), "/api/v1/alerts?type=sensor_drift", "P-SELF")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "sensor_drift")
	assert.Zero(t, len(pageOf(t, rec).List), "显式索取内部类型时应回空集而不是报错")
}

// pageOf 解析成功响应里的分页 data（本用例自用的小工具，不改既有 pageEnvelope）
func pageOf(t *testing.T, rec *httptest.ResponseRecorder) alertPageData {
	t.Helper()
	var env pageEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return env.Data
}
