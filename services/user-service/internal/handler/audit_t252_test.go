// T252 12.3 操作日志实现侧测试：埋点（登录/中间件表驱动）+ GET /admin/audit-logs 查询
//
// 验收对应（T252 ⑤ 关键操作留痕）：
//  1. 登录成功留 login 行，操作人取 admin 行（该路由在 gateway 免 JWT 白名单，无身份头）；
//  2. 表驱动中间件只对 auditRoutes 内的路由、且 HTTP < 400 才留痕；
//  3. 患者详情读取留 data_read（等保 §9.2a「谁看过哪个患者的数据」）；
//  4. 查询端点：date 与 from/to 互斥、单日按 Asia/Shanghai 切半开区间、筛选条件透传 repo；
//  5. 响应行带中文 actionLabel，description 从 detail->>'description' 反解。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// ── 埋点 ──

func TestT252_Audit_LoginWritesLoginRowWithAdminIdentity(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.admin = &repo.AdminRow{AdminID: "A0001", Username: "ops_admin", Name: "运营小张",
		PasswordHash: adminHash(t, "admin123"), RoleID: "ROLE_ADMIN", Status: "enabled"}
	e.store.scope = "all"

	w, resp := e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "ops_admin", "password": "admin123"}, nil)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Equal(t, "login", row.Action)
	assert.Equal(t, "admin", row.TargetType)
	assert.Equal(t, "A0001", row.OperatorID, "登录接口无 X-User-Id，操作人取 admin 行")
	assert.Equal(t, "ROLE_ADMIN", row.OperatorRole)
	assert.Contains(t, row.Description, "ops_admin")
}

func TestT252_Audit_FailedLoginLeavesNoRow(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.admin = &repo.AdminRow{AdminID: "A0001", Username: "ops_admin",
		PasswordHash: adminHash(t, "admin123"), RoleID: "ROLE_ADMIN", Status: "enabled"}

	w, _ := e.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "ops_admin", "password": "wrong-one"}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, e.store.auditRows)
}

func TestT252_Audit_TrailMiddlewareTracksConfiguredRoutesOnlyOnSuccess(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	// 命中表：PUT /admin/settings 成功 → 一条 config_change
	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings", validSettingsBody(),
		map[string]string{"X-User-Id": "A0001", "X-Role": "ROLE_ADMIN"})
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Equal(t, "config_change", row.Action)
	assert.Equal(t, "sys_config", row.TargetType)
	assert.Equal(t, "", row.TargetID)
	assert.Equal(t, "A0001", row.OperatorID)
	assert.Equal(t, "ROLE_ADMIN", row.OperatorRole)

	// 同一路由 400（联动校验拒绝）→ 不留痕，避免噪声
	e.store.auditRows = nil
	bad := validSettingsBody()
	bad["wearInterruptMinutes"] = 30 // 采集间隔 1800s=30min ⇒ 中断阈值须 ≥60min
	w, resp = e.do(http.MethodPut, "/api/v1/admin/settings", bad, map[string]string{"X-User-Id": "A0001"})
	assert.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Empty(t, e.store.auditRows, "失败请求不产生审计噪声")

	// 不在表内的路由（读接口）→ 不留痕
	e.store.auditRows = nil
	w, _ = e.do(http.MethodGet, "/api/v1/admin/settings", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, e.store.auditRows)
}

func TestT252_Audit_PatientDetailReadIsTracked(t *testing.T) {
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	w, resp := e.do(http.MethodGet, "/api/v1/admin/patients/P20260001", nil,
		map[string]string{"X-User-Id": "D0001", "X-Role": "ROLE_DOCTOR"})
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Equal(t, "data_read", row.Action, "等保 §9.2a：谁看过哪个患者的数据")
	assert.Equal(t, "patient", row.TargetType)
	assert.Equal(t, "P20260001", row.TargetID)
	assert.Contains(t, row.Description, "P20260001")

	// 404 读取不留痕
	e.store.auditRows = nil
	e.store.patient = nil
	w, _ = e.do(http.MethodGet, "/api/v1/admin/patients/P99999999", nil, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, e.store.auditRows)
}

// ── 查询端点 ──

func decodeAuditPage(t *testing.T, raw json.RawMessage) (model.PageData, []model.AuditLogDTO) {
	t.Helper()
	// PageData.List 是 any，直接解会得到 []interface{}；这里按契约字段再解一次
	var envelope struct {
		List     []model.AuditLogDTO `json:"list"`
		Total    int64               `json:"total"`
		Page     int                 `json:"page"`
		PageSize int                 `json:"pageSize"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	return model.PageData{Total: envelope.Total, Page: envelope.Page, PageSize: envelope.PageSize}, envelope.List
}

func TestT252_GetAuditLogs_MapsFiltersAndDTO(t *testing.T) {
	name, role, ip := "运营小张", "ROLE_ADMIN", "10.1.2.3"
	tid := "P20260001"
	tt := "patient"
	detail := `{"description":"查看患者档案 P20260001（等保 §9.2a 读留痕）","before":1}`
	e := newEnv(t, true, true)
	e.store.auditLogTotal = 137
	e.store.auditLogRows = []repo.AuditLogRow{
		{LogID: 9, OperatorID: strPtr("A0001"), OperatorName: &name, OperatorRole: &role,
			Action: "data_read", TargetType: &tt, TargetID: &tid, Detail: &detail, IP: &ip,
			Ts: time.Date(2026, 9, 18, 2, 30, 0, 0, time.UTC)},
		{LogID: 8, Action: "device_calibrate"}, // 未知 action：标签原样回显，detail 为空
	}

	w, resp := e.do(http.MethodGet,
		"/api/v1/admin/audit-logs?page=2&pageSize=20&action=data_read&operator=%E5%B0%8F%E5%BC%A0&targetType=patient&targetId=P20260001&from=2026-09-17T00:00:00&to=2026-09-19",
		nil, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	f := e.store.lastAuditFilter
	assert.Equal(t, 2, f.Page)
	assert.Equal(t, 20, f.PageSize)
	assert.Equal(t, "data_read", f.Action)
	assert.Equal(t, "小张", f.Operator)
	assert.Equal(t, "patient", f.TargetType)
	assert.Equal(t, "P20260001", f.TargetID)
	// 无时区入参按业务时区解释（否则切日偏移 8 小时）
	require.NotNil(t, f.From)
	require.NotNil(t, f.To)
	assert.Equal(t, "2026-09-16T16:00:00Z", f.From.UTC().Format(time.RFC3339), "2026-09-17T00:00 Asia/Shanghai")
	assert.Equal(t, "2026-09-18T16:00:00Z", f.To.UTC().Format(time.RFC3339), "2026-09-19 零点为半开区间右端")

	page, list := decodeAuditPage(t, resp.Data)
	assert.Equal(t, int64(137), page.Total)
	assert.Equal(t, 2, page.Page)
	assert.Equal(t, 20, page.PageSize)
	require.Len(t, list, 2)
	assert.Equal(t, int64(9), list[0].LogID)
	assert.Equal(t, "数据查看", list[0].ActionLabel)
	assert.Equal(t, "运营小张", *list[0].OperatorName)
	assert.Equal(t, "查看患者档案 P20260001（等保 §9.2a 读留痕）", list[0].Description)
	assert.JSONEq(t, detail, string(list[0].Detail))
	assert.Equal(t, "2026-09-18T02:30:00Z", list[0].Ts)

	assert.Equal(t, "device_calibrate", list[1].ActionLabel, "未知 action 原样回显，不吞数据")
	assert.Empty(t, list[1].Description)
	assert.Equal(t, json.RawMessage("null"), list[1].Detail, "无 detail 的行输出 null，不给前端空对象")
}

func TestT252_GetAuditLogs_SingleDayWindowUsesShanghai(t *testing.T) {
	e := newEnv(t, true, true)
	w, resp := e.do(http.MethodGet, "/api/v1/admin/audit-logs?date=2026-09-18", nil, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	f := e.store.lastAuditFilter
	require.NotNil(t, f.From)
	require.NotNil(t, f.To)
	assert.Equal(t, "2026-09-17T16:00:00Z", f.From.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-09-18T16:00:00Z", f.To.UTC().Format(time.RFC3339), "单日 = [day, day+1) 半开区间")
}

func TestT252_GetAuditLogs_RejectsBadQuery(t *testing.T) {
	cases := []struct {
		name, qs, msg string
	}{
		{"日期与区间互斥", "date=2026-09-18&from=2026-09-17", "mutually exclusive"},
		{"日期非法", "date=2026/09/18", "invalid date"},
		{"from 非法", "from=yesterday", "invalid from"},
		{"右端早于左端", "from=2026-09-18&to=2026-09-17", "to must be after from"},
		{"分页越界", "pageSize=101", "pageSize"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			w, resp := e.do(http.MethodGet, "/api/v1/admin/audit-logs?"+tc.qs, nil, t252AdminHdr())
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			assert.Contains(t, resp.Message, tc.msg)
			assert.Nil(t, e.store.lastAuditFilter.From, "拒绝时不查库")
		})
	}
}

func TestT252_GetAuditLogs_StoreFailureReturns500(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.auditLogErr = errors.New("db")
	w, resp := e.do(http.MethodGet, "/api/v1/admin/audit-logs", nil, t252AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)
}
