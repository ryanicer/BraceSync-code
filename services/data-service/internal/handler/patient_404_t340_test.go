// Package handler — T340：患者档案不存在必须回 HTTP 404，与「档案存在但暂无数据」的 200 空响应区分开。
//
// 缺陷原貌（staging 一手实测，卡内评论 1133005753001000713）：
// NOPE 型 / 笔误型不存在 / 真实但未绑定设备，四种输入响应一字不差 —— http 200、code 0、228 字节。
// 根因是读侧只问过「有没有绑定设备」（service/record.go:469 取 exists、:477 为假即 return 空快照），
// 从没问过「patients 表里有没有这个人」。
//
// 本文件守三条：
//  1. 不存在 → HTTP 状态位与业务码**双断**（只断业务码正是本缺陷成因：user-service 的
//     ErrPatientNotFound 就配着 HTTP 200，见 services/user-service/internal/model/model.go:93）；
//  2. 存在但未绑定 / 无帧 → 仍 200 空快照（T325「无真帧不伪造网格」不能被改成报错）；
//  3. 鉴权先于存在性判定 —— 状态码差不得向无权调用方泄露「这个患者存不存在」。
package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/service"
)

// stubPatientLookup 患者档案存在性替身。known 为 nil 表示「一律存在」（供既有测试镜像生产装配）；
// 非 nil 时只有名单内的 patient_id 算存在。
type stubPatientLookup struct {
	known    []string
	err      error
	lastSeen string
}

func (s *stubPatientLookup) PatientExists(_ context.Context, patientID string) (bool, error) {
	s.lastSeen = patientID
	if s.err != nil {
		return false, s.err
	}
	if s.known == nil {
		return true, nil
	}
	for _, k := range s.known {
		if k == patientID {
			return true, nil
		}
	}
	return false, nil
}

type lookupError struct{}

func (lookupError) Error() string { return "patients lookup failed" }

// t340Server 自建装配：绑定关系与存在性名单都由用例决定（newTestServer 走的是「已绑定 + 一律存在」）
func t340Server(lookup PatientLookup, bound bool) *testServer {
	records := &stubRecords{}
	devices := &stubDevices{}
	if bound {
		devices = &stubDevices{patientByDevice: map[string]string{hDevice: hPatient}}
	}
	svc := service.NewRecordService(records, devices, stubConfigs{}, stubCache{}, stubAlerts{},
		service.NewRateLimiter(1e9, 1e9, 1e9, 1e9))
	h := New(svc)
	if lookup != nil {
		h.SetPatientLookup(lookup)
	}
	h.SetReportLister(&fakeReportLister{})
	h.SetDailyWearQuerier(&fakeDailyWearQuerier{})
	return &testServer{router: h.Router(), records: records, svc: svc}
}

// TestT340_PatientNotFound_Is404 四条患者域 GET 端点：档案不存在一律 404 + 业务码 10404。
func TestT340_PatientNotFound_Is404(t *testing.T) {
	cases := []struct{ name, path string }{
		{"realtime", "/api/v1/patients/P-NOPE/realtime"},
		{"records", "/api/v1/patients/P-NOPE/records?date=2026-08-08"},
		{"health-reports", "/api/v1/patients/P-NOPE/health-reports"},
		{"daily-wear", "/api/v1/patients/P-NOPE/daily-wear"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &stubPatientLookup{known: []string{hPatient}}
			srv := t340Server(lookup, true)

			w := doReq(t, srv, http.MethodGet, tc.path, "", map[string]string{headerRole: roleAdmin})

			require.Equal(t, http.StatusNotFound, w.Code,
				"%s 档案不存在必须回 404；此前是 200+空，与「有此人无数据」不可区分。响应：%s", tc.name, w.Body.String())
			code, _ := decodeBody(t, w)
			assert.Equal(t, model.CodePatientNotFound, code, "业务码用患者域 10404，不借 device 域 20404")
		})
	}
}

// TestT340_ExistingButNoData_Still200Empty 档案存在但未绑定设备 → 200 + 空快照，
// 且必须真的走到业务层（存在性判定放行后由读侧决定空数据）。
func TestT340_ExistingButNoData_Still200Empty(t *testing.T) {
	lookup := &stubPatientLookup{known: []string{hPatient}}
	srv := t340Server(lookup, false) // 存在，但没有任何绑定设备

	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/realtime", "",
		map[string]string{headerRole: roleAdmin})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	code, data := decodeBody(t, w)
	assert.Equal(t, model.CodeOK, code)
	assert.Equal(t, "", data["deviceId"], "未绑定设备时 deviceId 为空串，前端按「无设备」处理")
	assert.NotNil(t, data["pressureRecords"], "无真帧回空数组，不下发伪造网格（T325 口径）")
}

// TestT340_AuthzRunsBeforeExistenceCheck 越权调用方拿 403，且不该触发存在性查询。
func TestT340_AuthzRunsBeforeExistenceCheck(t *testing.T) {
	lookup := &stubPatientLookup{known: []string{hPatient}}
	srv := t340Server(lookup, true)

	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/P-OTHER/realtime", "",
		map[string]string{headerRole: "ROLE_PATIENT", headerUserID: "P-SELF"})

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Empty(t, lookup.lastSeen, "鉴权未通过时不应发生存在性查询（否则状态码差会泄露档案是否存在）")
}

// TestT340_LookupNotConfigured_Is500 漏装配必须显性失败，不能退化成「200 + 空数据」。
func TestT340_LookupNotConfigured_Is500(t *testing.T) {
	srv := t340Server(nil, true)

	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/realtime", "",
		map[string]string{headerRole: roleAdmin})

	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	code, _ := decodeBody(t, w)
	assert.Equal(t, model.CodeInternal, code)
}

// TestT340_LookupError_Is500 查库失败必须 500，不得吞成 200 空快照。
func TestT340_LookupError_Is500(t *testing.T) {
	srv := t340Server(&stubPatientLookup{err: lookupError{}}, true)

	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/realtime", "",
		map[string]string{headerRole: roleAdmin})

	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// 编译期确认替身实现的是 handler 侧契约（与 repo.PatientRepo 的签名一致）
var _ PatientLookup = (*stubPatientLookup)(nil)
