// Package handler — Handler HTTP 层实现侧测试（Winner 实现，T017 转绿阶段）
//
// 与 Ella 预置契约测试（handler_test.go）互补：本文件经真实 Gin 路由验证
// internal/handler/handler.go 的实现（内存 FakeStore，无 DB 依赖），覆盖：
//   - 内部接口 X-Internal-Service 头鉴权（架构 §5.2）
//   - 告警通知受理（正常 / 未知 type / 额度耗尽降级）
//   - 授予额度 Idempotency-Key 幂等 + 缺头 400
//   - 佩戴提醒读写 + 非法时间 400
//   - 规则管理（未知 type 400）+ 通知记录分页/过滤校验
//   - T185 患者域水平鉴权（本人 200 / 越权 403 / grant 仅 admin）
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/msg-service/internal/service"
	"github.com/bracesync/bracesync/services/msg-service/internal/testutil"
)

// T185 患者域测试身份头（网关 §5.2 注入契约）：
// hdrSelf = 患者本人 token（X-User-Id 与 path 中 patientId 一致）；
// hdrAdmin = ROLE_ADMIN（订阅额度授予这类权益写操作仅此角色可调用）。
var (
	hdrSelf  = map[string]string{"X-User-Id": testQP, "X-Role": "patient"}
	hdrAdmin = map[string]string{"X-User-Id": "ADM001", "X-Role": roleAdmin}
)

// testQP 测试用患者 ID（与两个契约测试文件的既有路径保持一致）
const testQP = "P20260001"

// withHdr 合并请求头为新的 map（不改写入参，避免共享夹具被用例互相污染）
func withHdr(bases ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range bases {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// httpFixture 路由级测试夹具（真实 Handler + FakeStore + mock 发送器）
type httpFixture struct {
	router http.Handler
	store  *testutil.FakeStore
	svc    *service.NotifyService
}

func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := testutil.NewFakeStore()
	wx := service.NewMockWechatSender(zerolog.Nop())
	sms := service.NewMockSMSSender(zerolog.Nop())
	svc := service.NewNotifyService(store, wx, sms, zerolog.Nop())
	svc.SetNow(func() time.Time { return time.Date(2026, 8, 10, 20, 5, 0, 0, time.FixedZone("CST", 8*3600)) })
	store.SeedRule(model.NotifyRule{
		Type: model.AlertTypePressureHigh, Channels: []string{model.ChannelWechat, model.ChannelSMS},
		NotifyTargets: []string{model.TargetPatient, model.TargetDoctor},
	})
	return &httpFixture{router: New(svc).Router(), store: store, svc: svc}
}

type httpResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (f *httpFixture) do(t *testing.T, method, path, body string, headers map[string]string) (*httptest.ResponseRecorder, httpResp) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	var resp httpResp
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w, resp
}

// ─────────────────────────────────────────────────────────────
// 内部接口鉴权（架构 §5.2）
// ─────────────────────────────────────────────────────────────

func TestHTTPSendAlert_MissingInternalHeader_401(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPost, "/internal/msg/send",
		`{"alertId":"A-1","type":"pressure_high","patientId":"P20260001","detail":"x"}`, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code, "内部接口必须携带 X-Internal-Service 头")
	assert.Equal(t, model.CodeInternalDisabled, resp.Code)
}

func TestHTTPSendAlert_Accepted(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPost, "/internal/msg/send",
		`{"alertId":"A-20260810-001","type":"pressure_high","patientId":"P20260001","deviceId":"PRS-ML05-RC-20260701001","detail":"压力偏高"}`,
		map[string]string{"X-Internal-Service": "alert-service"})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	var result model.SendResult
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.True(t, result.Accepted)
	assert.False(t, result.Degraded)
	assert.NotEmpty(t, result.RecordID, "受理返回通知记录 ID")
}

func TestHTTPSendAlert_UnknownType_NotAccepted(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPost, "/internal/msg/send",
		`{"type":"bogus_type","patientId":"P20260001","detail":"x"}`,
		map[string]string{"X-Internal-Service": "alert-service"})
	require.Equal(t, http.StatusOK, w.Code)
	var result model.SendResult
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	assert.False(t, result.Accepted, "未知告警类型不发送")
}

func TestHTTPSendAlert_BadBody_400(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPost, "/internal/msg/send", `{invalid`,
		map[string]string{"X-Internal-Service": "alert-service"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

// ─────────────────────────────────────────────────────────────
// 订阅额度
// ─────────────────────────────────────────────────────────────

func TestHTTPGrantQuota_MissingIdempotencyKey_400(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPost, "/api/v1/patients/P20260001/subscription-quota/grant", `{}`, withHdr(hdrAdmin))
	assert.Equal(t, http.StatusBadRequest, w.Code, "grant 必须携带 Idempotency-Key")
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

func TestHTTPGrantQuota_Idempotent(t *testing.T) {
	f := newHTTPFixture(t)
	headers := withHdr(hdrAdmin, map[string]string{"Idempotency-Key": "uuid-abc-123"})

	w, resp := f.do(t, http.MethodPost, "/api/v1/patients/P20260001/subscription-quota/grant", `{}`, headers)
	require.Equal(t, http.StatusOK, w.Code)
	var first struct {
		Remaining int  `json:"remaining"`
		IsLow     bool `json:"isLow"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &first))
	assert.Equal(t, model.DefaultQuota+1, first.Remaining)

	// 同 Idempotency-Key 重复回报 → 不重复增额
	w, resp = f.do(t, http.MethodPost, "/api/v1/patients/P20260001/subscription-quota/grant", `{}`, headers)
	require.Equal(t, http.StatusOK, w.Code)
	var second struct {
		Remaining int `json:"remaining"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &second))
	assert.Equal(t, first.Remaining, second.Remaining, "同 Idempotency-Key 不重复增额")
}

func TestHTTPGetQuota_Shape(t *testing.T) {
	f := newHTTPFixture(t)
	f.store.SeedQuota("P20260001", 1)

	w, resp := f.do(t, http.MethodGet, "/api/v1/patients/P20260001/subscription-quota", "", hdrSelf)
	require.Equal(t, http.StatusOK, w.Code)
	var quota model.SubscriptionQuotaDTO
	require.NoError(t, json.Unmarshal(resp.Data, &quota))
	assert.Equal(t, "P20260001", quota.PatientID)
	assert.Equal(t, 1, quota.Remaining)
	assert.True(t, quota.IsLow, "remaining≤1 → isLow=true（引导重新授权）")
}

// ─────────────────────────────────────────────────────────────
// 佩戴提醒
// ─────────────────────────────────────────────────────────────

func TestHTTPWearReminder_PutThenGet(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPut, "/api/v1/patients/P20260001/wear-reminder",
		`{"reminderEnabled":true,"reminderTime":"20:00"}`, hdrSelf)
	require.Equal(t, http.StatusOK, w.Code)
	var updated model.WearReminderDTO
	require.NoError(t, json.Unmarshal(resp.Data, &updated))
	assert.True(t, updated.ReminderEnabled)
	require.NotNil(t, updated.ReminderTime)
	assert.Equal(t, "20:00", *updated.ReminderTime)

	w, resp = f.do(t, http.MethodGet, "/api/v1/patients/P20260001/wear-reminder", "", hdrSelf)
	require.Equal(t, http.StatusOK, w.Code)
	var got model.WearReminderDTO
	require.NoError(t, json.Unmarshal(resp.Data, &got))
	assert.True(t, got.ReminderEnabled, "设置已持久化（直写 patient_preferences，一期偏离声明）")
	require.NotNil(t, got.ReminderTime)
	assert.Equal(t, "20:00", *got.ReminderTime)
}

func TestHTTPWearReminder_InvalidTime_400(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPut, "/api/v1/patients/P20260001/wear-reminder",
		`{"reminderEnabled":true,"reminderTime":"25:00"}`, hdrSelf)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

// ─────────────────────────────────────────────────────────────
// 规则管理
// ─────────────────────────────────────────────────────────────

func TestHTTPNotifyRules_ListAndUpdate(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodGet, "/api/v1/admin/notify-rules", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var rules []model.NotifyRuleDTO
	require.NoError(t, json.Unmarshal(resp.Data, &rules))
	require.Len(t, rules, 1)
	assert.Equal(t, "pressure_high", string(rules[0].Type))

	w, resp = f.do(t, http.MethodPut, "/api/v1/admin/notify-rules/pressure_high",
		`{"channels":["wechat","sms"],"notifyTargets":["patient","doctor","tech"]}`,
		map[string]string{"X-User-Id": "admin01"})
	require.Equal(t, http.StatusOK, w.Code)
	var updated model.NotifyRuleDTO
	require.NoError(t, json.Unmarshal(resp.Data, &updated))
	assert.Equal(t, []string{"wechat", "sms"}, updated.Channels)
	assert.Equal(t, []string{"patient", "doctor", "tech"}, updated.NotifyTargets)
	assert.Equal(t, "admin01", updated.UpdatedBy)
}

func TestHTTPNotifyRules_UnknownType_400(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodPut, "/api/v1/admin/notify-rules/unknown_type",
		`{"channels":["wechat"],"notifyTargets":["patient"]}`, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, "未知告警类型应被拒绝（CHECK 约束兜底）")
	assert.Equal(t, model.CodeInvalidParam, resp.Code)
}

// ─────────────────────────────────────────────────────────────
// 通知记录查询
// ─────────────────────────────────────────────────────────────

func TestHTTPPatientNotifications_Paginated(t *testing.T) {
	f := newHTTPFixture(t)
	// 受理两条告警通知，产生记录
	for i := 0; i < 2; i++ {
		w, _ := f.do(t, http.MethodPost, "/internal/msg/send",
			`{"type":"pressure_high","patientId":"P20260001","detail":"压力偏高"}`,
			map[string]string{"X-Internal-Service": "alert-service"})
		require.Equal(t, http.StatusOK, w.Code)
	}

	w, resp := f.do(t, http.MethodGet, "/api/v1/patients/P20260001/notifications?page=1&pageSize=1", "", hdrSelf)
	require.Equal(t, http.StatusOK, w.Code)
	var page struct {
		List     []model.NotificationRecordDTO `json:"list"`
		Total    int                           `json:"total"`
		Page     int                           `json:"page"`
		PageSize int                           `json:"pageSize"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	assert.Equal(t, 2, page.Total)
	assert.Len(t, page.List, 1, "pageSize=1 分页生效")
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 1, page.PageSize)
	assert.Equal(t, "P20260001", page.List[0].PatientID)
}

func TestHTTPNotificationLogs_FilterValidation(t *testing.T) {
	f := newHTTPFixture(t)

	w, resp := f.do(t, http.MethodGet, "/api/v1/admin/notification-logs?status=bogus", "", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, "非法 status 过滤应被拒绝")
	assert.Equal(t, model.CodeInvalidParam, resp.Code)

	w, _ = f.do(t, http.MethodGet, "/api/v1/admin/notification-logs?status=failed&page=1&pageSize=20", "", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHTTPHealthz(t *testing.T) {
	f := newHTTPFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ─────────────────────────────────────────────────────────────
// T185 患者域水平鉴权
// ─────────────────────────────────────────────────────────────

// TestHTTPPatientScope_HorizontalAuthz 四条患者域端点的水平越权边界
// （口径同 data-service getDailyWear）：本人 200 / 传他人 patientId 403 /
// 缺失身份头 403（fail-closed）/ ROLE_ADMIN 查任意 200。
// 这组端点此前经 gateway 完全不可达（未挂代理），挂载即成为越权面，故同卡强制。
func TestHTTPPatientScope_HorizontalAuthz(t *testing.T) {
	const self = "/api/v1/patients/P20260001"
	const other = "/api/v1/patients/P9999999"
	const putBody = `{"reminderEnabled":true,"reminderTime":"20:00"}`

	cases := []struct {
		name      string
		method    string
		selfPath  string
		otherPath string
		body      string
	}{
		{"wear-reminder读", http.MethodGet, self + "/wear-reminder", other + "/wear-reminder", ""},
		{"wear-reminder写", http.MethodPut, self + "/wear-reminder", other + "/wear-reminder", putBody},
		{"订阅额度读", http.MethodGet, self + "/subscription-quota", other + "/subscription-quota", ""},
		{"通知记录读", http.MethodGet, self + "/notifications", other + "/notifications", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name+"_本人200", func(t *testing.T) {
			f := newHTTPFixture(t)
			w, _ := f.do(t, tc.method, tc.selfPath, tc.body, hdrSelf)
			assert.Equal(t, http.StatusOK, w.Code)
		})
		t.Run(tc.name+"_越权403", func(t *testing.T) {
			f := newHTTPFixture(t)
			w, resp := f.do(t, tc.method, tc.otherPath, tc.body, hdrSelf)
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Equal(t, model.CodeForbidden, resp.Code)
		})
		t.Run(tc.name+"_缺身份头403", func(t *testing.T) {
			f := newHTTPFixture(t)
			w, resp := f.do(t, tc.method, tc.selfPath, tc.body, nil)
			assert.Equal(t, http.StatusForbidden, w.Code, "身份头缺失必须 fail-closed")
			assert.Equal(t, model.CodeForbidden, resp.Code)
		})
		t.Run(tc.name+"_admin任意200", func(t *testing.T) {
			f := newHTTPFixture(t)
			w, _ := f.do(t, tc.method, tc.otherPath, tc.body, hdrAdmin)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

// TestHTTPGrantQuota_AdminOnly 订阅额度授予是权益写操作：
// 若按 self-scope 放行，患者可自行加额度 → 仅 ROLE_ADMIN。
func TestHTTPGrantQuota_AdminOnly(t *testing.T) {
	const grantPath = "/api/v1/patients/P20260001/subscription-quota/grant"
	idem := map[string]string{"Idempotency-Key": "uuid-admin-only"}

	t.Run("患者本人token也403", func(t *testing.T) {
		f := newHTTPFixture(t)
		w, resp := f.do(t, http.MethodPost, grantPath, `{}`, withHdr(hdrSelf, idem))
		assert.Equal(t, http.StatusForbidden, w.Code, "患者不得自行授予订阅额度")
		assert.Equal(t, model.CodeForbidden, resp.Code)
		q, err := f.store.GetQuota(context.Background(), "P20260001")
		require.NoError(t, err)
		assert.Equal(t, model.DefaultQuota, q.Remaining, "403 不得已增额")
	})

	for _, role := range []string{"patient", "ROLE_DOCTOR", "ROLE_CS", "technician"} {
		t.Run("角色"+role+"_403", func(t *testing.T) {
			f := newHTTPFixture(t)
			w, _ := f.do(t, http.MethodPost, grantPath, `{}`,
				withHdr(map[string]string{"X-User-Id": "ADM001", "X-Role": role}, idem))
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}

	t.Run("admin_200", func(t *testing.T) {
		f := newHTTPFixture(t)
		w, _ := f.do(t, http.MethodPost, grantPath, `{}`, withHdr(hdrAdmin, idem))
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
