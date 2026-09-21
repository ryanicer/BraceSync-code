// Package handler — T300 异常报告汇总/导出端点测试。
//
// 覆盖：staff-only 判定、参数校验、北京日历日 → ts 半开区间换算、
// 汇总三视角派生（纯函数 + HTTP 契约）、CSV 表头/行值/BOM/文件名/截断、
// 表格式注入消毒、存储错误不得写成半截 CSV。
package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

const (
	t300SummaryPath = "/api/v1/admin/abnormal-reports"
	t300ExportPath  = "/api/v1/admin/abnormal-reports/export"
	t300Query       = "?patientId=P001&start=2026-09-01&end=2026-09-03"
)

func doReport(h *Handler, target string, role string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if role != "" {
		req.Header.Set(headerRole, role)
	}
	req.Header.Set(headerUserID, "ADMIN-001")
	h.Router().ServeHTTP(rec, req)
	return rec
}

// ── 鉴权与参数 ─────────────────────────────────────────────────

// 跨患者聚合端点：患者 token 与匿名（X-Role 缺失）一律 403，且不触达存储
func TestT300_SummaryStaffOnly(t *testing.T) {
	for _, tc := range []struct{ name, role string }{
		{"患者", "ROLE_PATIENT"}, {"匿名", ""},
	} {
		store := &fakePublicStore{}
		rec := doReport(newPublicHandler(store), t300SummaryPath+t300Query, tc.role)
		assert.Equal(t, http.StatusForbidden, rec.Code, "%s 应 403", tc.name)
		assert.Zero(t, store.sumHits, "%s 不得触达存储", tc.name)
	}
	for _, role := range []string{roleAdmin, "ROLE_DOCTOR", "ROLE_CS", "technician"} {
		store := &fakePublicStore{}
		rec := doReport(newPublicHandler(store), t300SummaryPath+t300Query, role)
		assert.Equal(t, http.StatusOK, rec.Code, "role=%s 应放行", role)
		assert.Equal(t, 1, store.sumHits)
	}
}

func TestT300_InvalidParams(t *testing.T) {
	cases := []struct{ name, target string }{
		{"缺 patientId", "?start=2026-09-01&end=2026-09-03"},
		{"缺 start", "?patientId=P001&end=2026-09-03"},
		{"缺 end", "?patientId=P001&start=2026-09-01"},
		{"start 非日期", "?patientId=P001&start=2026/09/01&end=2026-09-03"},
		{"start 非法月日", "?patientId=P001&start=2026-13-40&end=2026-09-03"},
		{"end 早于 start", "?patientId=P001&start=2026-09-05&end=2026-09-03"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakePublicStore{}
			rec := doReport(newPublicHandler(store), t300SummaryPath+tc.target, roleAdmin)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			var env envelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			assert.Equal(t, codeInvalidParam, env.Code)
			assert.Zero(t, store.sumHits, "校验失败不应查库")
		})
	}
	// 同口径校验也适用于导出端点
	store := &fakePublicStore{}
	rec := doReport(newPublicHandler(store), t300ExportPath+"?patientId=P001&start=2026-09-01", roleAdmin)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Zero(t, store.exportHits)
}

// 北京日历日 → UTC 时刻半开区间：[9-1 00:00 CST, 9-4 00:00 CST)
// 含 9-3 全天、不含 9-4；换算错了导出范围就会整体偏移 8 小时。
func TestT300_DateBoundsAreBeijingHalfOpen(t *testing.T) {
	store := &fakePublicStore{}
	rec := doReport(newPublicHandler(store), t300SummaryPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)

	f := store.sumFilter
	require.NotNil(t, f.StartTs)
	require.NotNil(t, f.EndTs)
	assert.Equal(t, "2026-08-31T16:00:00Z", f.StartTs.UTC().Format(time.RFC3339), "start 应为 9-1 00:00 北京")
	assert.Equal(t, "2026-09-03T16:00:00Z", f.EndTs.UTC().Format(time.RFC3339), "end 应为末日次日 00:00 北京（不含）")
	assert.True(t, f.EndTs.After(*f.StartTs))
	assert.Equal(t, "P001", f.PatientID)
	assert.Equal(t, 72*time.Hour, f.EndTs.Sub(*f.StartTs), "3 个自然日 = 72 小时")
}

// 单日范围也必须覆盖全天（start==end 时不能退化成空区间）
func TestT300_SingleDayRange(t *testing.T) {
	store := &fakePublicStore{}
	rec := doReport(newPublicHandler(store), t300SummaryPath+"?patientId=P001&start=2026-09-03&end=2026-09-03", roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 24*time.Hour, store.sumFilter.EndTs.Sub(*store.sumFilter.StartTs))
}

// ── 汇总派生 ───────────────────────────────────────────────────

func t300SummaryRows() []repo.AlertSummaryRow {
	return []repo.AlertSummaryRow{
		{Date: "2026-09-03", Type: "pressure_high", ProcessStatus: "pending", Count: 2},
		{Date: "2026-09-03", Type: "wear_interrupt", ProcessStatus: "processed", Count: 1},
		{Date: "2026-09-02", Type: "pressure_high", ProcessStatus: "processing", Count: 5},
		{Date: "2026-09-01", Type: "sensor_drift", ProcessStatus: "pending", Count: 4},
	}
}

func TestT300_SummaryThreeViews(t *testing.T) {
	s := &fakePublicStore{summaryRows: t300SummaryRows()}
	rec := doReport(newPublicHandler(s), t300SummaryPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)

	var env struct {
		Code int                `json:"code"`
		Data abnormalReportData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, codeSuccess, env.Code)
	assert.Equal(t, "P001", env.Data.PatientID)
	assert.Equal(t, "2026-09-01", env.Data.Start)
	assert.Equal(t, "2026-09-03", env.Data.End)
	assert.EqualValues(t, 12, env.Data.Total, "总数 = 各格计数之和")

	assert.Equal(t, []reportCount{
		{Key: "pending", Count: 6}, {Key: "processing", Count: 5}, {Key: "processed", Count: 1},
	}, env.Data.ByStatus, "状态视角固定三档顺序")
	assert.Equal(t, []reportCount{
		{Key: "pressure_high", Count: 7}, {Key: "sensor_drift", Count: 4},
		{Key: "wear_interrupt", Count: 1},
	}, env.Data.ByType, "类型视角按计数降序")
	assert.Equal(t, []reportCount{
		{Key: "2026-09-01", Count: 4}, {Key: "2026-09-02", Count: 5}, {Key: "2026-09-03", Count: 3},
	}, env.Data.ByDay, "按日视角按日期升序")
}

// 空范围：三个视角都要出 [] 而非 null（前端直接渲染，不需要判空）
func TestT300_SummaryEmptyIsNotNull(t *testing.T) {
	rec := doReport(newPublicHandler(&fakePublicStore{}), t300SummaryPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `"byStatus":[{"key":"pending","count":0},{"key":"processing","count":0},{"key":"processed","count":0}]`)
	assert.Contains(t, body, `"byType":[]`)
	assert.Contains(t, body, `"byDay":[]`)
}

// 白名单外的历史状态值不能被吞掉（计数守恒）
func TestT300_SummaryKeepsUnknownStatus(t *testing.T) {
	rows := []repo.AlertSummaryRow{{Date: "2026-09-01", Type: "pressure_high", ProcessStatus: "archived", Count: 3}}
	data := buildAbnormalReport(reportQuery{patientID: "P001"}, rows)
	assert.EqualValues(t, 3, data.Total)
	var found int64
	for _, c := range data.ByStatus {
		found += c.Count
	}
	assert.EqualValues(t, 3, found, "未知状态也要计入 byStatus，否则三个视角加总对不上")
}

// ── CSV 导出 ───────────────────────────────────────────────────

func t300Time(y int, mo time.Month, d, hh, mm, ss int) *time.Time {
	v := time.Date(y, mo, d, hh, mm, ss, 0, time.UTC)
	return &v
}

func t300ExportRows() []repo.AlertRow {
	note := "已电话指导"
	by := "tech01"
	return []repo.AlertRow{
		{ // 后一条：ts 更晚，但 repo 返回顺序不定，导出需按时间升序
			AlertID: 12, PatientID: "P001", PatientName: "林小雨", DeviceID: "DEV01",
			Type: "wear_interrupt", Detail: "中断 35 分钟", Ts: time.Date(2026, 9, 2, 1, 30, 0, 0, time.UTC),
			ReadStatus: "read", ProcessStatus: "processed", ProcessNote: &note, ProcessedBy: &by,
			ProcessedAt: t300Time(2026, 9, 2, 2, 0, 0),
		},
		{
			AlertID: 11, PatientID: "P001", DeviceID: "DEV01", Type: "pressure_high",
			SensorPoint: "P03", ThresholdValue: 45, ActualValue: 52.5,
			Ts:         time.Date(2026, 9, 1, 16, 5, 0, 0, time.UTC), // 北京 9-2 00:05
			ReadStatus: "unread", ProcessStatus: "pending", InProgressAt: t300Time(2026, 9, 1, 17, 0, 0),
		},
	}
}

func readT300CSV(t *testing.T, rec *httptest.ResponseRecorder) (body string, records [][]string) {
	t.Helper()
	body = rec.Body.String()
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, csvBOM)))
	records, err := r.ReadAll()
	require.NoError(t, err, "CSV 必须可被标准解析器读出")
	return body, records
}

func TestT300_ExportHeadersAndRows(t *testing.T) {
	store := &fakePublicStore{exportRows: t300ExportRows()}
	rec := doReport(newPublicHandler(store), t300ExportPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)

	assert.Equal(t, "text/csv; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, `attachment; filename="abnormal-report-P001-2026-09-01_2026-09-03.csv"`,
		rec.Header().Get("Content-Disposition"), "文件名带患者与日期范围，且无斜杠（防路径注入）")
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, repo.MaxExportRows, store.exportLimit, "导出必须带上限，不能让一次请求物化全表")
	require.Equal(t, 1, store.exportHits)

	body, records := readT300CSV(t, rec)
	assert.True(t, strings.HasPrefix(body, csvBOM), "Excel 打开中文需要 UTF-8 BOM")
	require.Len(t, records, 3, "表头 + 2 条明细")
	assert.Equal(t, "告警ID", records[0][0])
	assert.Len(t, records[0], len(records[1]), "每行列数须与表头一致")

	// 时间升序：alert 11（北京 9-2 00:05）在前，12（北京 9-2 09:30）在后
	assert.Equal(t, "11", records[1][0])
	assert.Equal(t, "2026-09-02 00:05:00", records[1][9], "采集时间按北京时间，不是 UTC")
	assert.Equal(t, "2026-09-02 01:00:00", records[1][12], "开始处理时间同样转北京")
	assert.Equal(t, "", records[1][13], "未处理 ⇒ 处理时间留空")
	assert.Equal(t, "压力超阈值", records[1][4])
	assert.Equal(t, "45.00", records[1][7])
	assert.Equal(t, "52.50", records[1][8])
	assert.Equal(t, "未读", records[1][10])
	assert.Equal(t, "待处理", records[1][11])

	assert.Equal(t, "12", records[2][0])
	assert.Equal(t, "佩戴中断", records[2][4])
	assert.Equal(t, "林小雨", records[2][2])
	assert.Equal(t, "已处理", records[2][11])
	assert.Equal(t, "2026-09-02 10:00:00", records[2][13])
	assert.Equal(t, "tech01", records[2][14])
	assert.Equal(t, "已电话指导", records[2][15])
}

// 截断必须显式写在文件里，否则使用者会把局部当全量上报
func TestT300_ExportTruncationNotice(t *testing.T) {
	store := &fakePublicStore{exportTrunc: true}
	rec := doReport(newPublicHandler(store), t300ExportPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "范围内数据超过 "+strconv.Itoa(repo.MaxExportRows)+" 条")
}

// 自由文本以 = 开头会被 Excel 当公式求值 ⇒ 前导单引号消毒
func TestT300_ExportSanitizesFormulaCells(t *testing.T) {
	note := "=cmd|'/c calc'!A0"
	name := "-1+1"
	store := &fakePublicStore{exportRows: []repo.AlertRow{{
		AlertID: 1, PatientID: "P001", PatientName: name, Type: "pressure_high",
		Ts: time.Unix(0, 0).UTC(), ReadStatus: "unread", ProcessStatus: "pending", ProcessNote: &note,
	}}}
	rec := doReport(newPublicHandler(store), t300ExportPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	_, records := readT300CSV(t, rec)
	require.Len(t, records, 2)
	assert.Equal(t, "'=cmd|'/c calc'!A0", records[1][15])
	assert.Equal(t, "'-1+1", records[1][2])
}

// patientId 进 Content-Disposition 前必须收敛字符集（防响应头注入 / 引号逃逸）
func TestT300_ExportFilenameSanitized(t *testing.T) {
	store := &fakePublicStore{}
	rec := doReport(newPublicHandler(store),
		t300ExportPath+"?patientId=P001%22%0d%0aX-Evil%3A1&start=2026-09-01&end=2026-09-03", roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	cd := rec.Header().Get("Content-Disposition")
	assert.Equal(t, `attachment; filename="abnormal-report-P001___X-Evil_1-2026-09-01_2026-09-03.csv"`,
		cd, `引号/CR/LF/冒号全部退化为 _，不产生第二个头或提前闭合的 filename`)
}

func TestT300_ExportStoreErrorIsJSONNotHalfCSV(t *testing.T) {
	store := &fakePublicStore{exportErr: errors.New("db down")}
	rec := doReport(newPublicHandler(store), t300ExportPath+t300Query, roleAdmin)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.NotContains(t, rec.Body.String(), "告警ID")
}

func TestT300_ReportNilStore(t *testing.T) {
	h := New(nil, nil, nil)
	for _, p := range []string{t300SummaryPath, t300ExportPath} {
		rec := doReport(h, p+t300Query, roleAdmin)
		assert.Equal(t, http.StatusInternalServerError, rec.Code, p)
	}
}

// 存储错误：500 且 code 一致（汇总端点）
func TestT300_SummaryStoreError(t *testing.T) {
	store := &fakePublicStore{sumErr: errors.New("db down")}
	rec := doReport(newPublicHandler(store), t300SummaryPath+t300Query, roleAdmin)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// 方法边界：导出端点不接受 POST（网关只转发 GET）
func TestT300_ExportMethodMismatch(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, t300ExportPath+t300Query, nil)
	req.Header.Set(headerRole, roleAdmin)
	newPublicHandler(&fakePublicStore{}).Router().ServeHTTP(rec, req)
	assert.Contains(t, []int{http.StatusMethodNotAllowed, http.StatusNotFound}, rec.Code)
}
