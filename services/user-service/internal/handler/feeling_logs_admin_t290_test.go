// T290-A：/api/v1/admin/feeling-logs 的 patientName 回填，与 /api/v1/admin/teams/stats
// 两条 T256 端点的行为断言（此前 services 侧零测试覆盖）。
//
// 缺陷原貌：repo 已把 patients.name 扫进 FeelingLogRow.PatientName，但 FeelingLogDTO
// 无 patientName 字段 ⇒ toFeelingDTO 丢弃，后台跨患者流「患者」列只剩编号（同 T278-③）。
package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

func feelingAdminRow(logID int64, patientID, patientName string, day int, comfortLevel string) repo.FeelingLogRow {
	level := comfortLevel
	return repo.FeelingLogRow{
		LogID:        logID,
		PatientID:    patientID,
		PatientName:  patientName,
		LogDate:      time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC),
		ComfortLevel: &level,
	}
}

// TestFeelingLogsAdmin_PatientNameInList 跨患者流必须把 join 出的患者姓名传到 DTO。
func TestFeelingLogsAdmin_PatientNameInList(t *testing.T) {
	e := newEnv(t, false, false)
	e.store.feelingsAdmin = []repo.FeelingLogRow{
		feelingAdminRow(11, "P0000001", "张阿姨", 3, "discomfort"),
		feelingAdminRow(12, "P0000002", "李大爷", 2, "fitted"),
	}
	e.store.feelingsAdminTotal = 2

	w, resp := e.do(http.MethodGet, "/api/v1/admin/feeling-logs?page=1&pageSize=20", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	var page struct {
		List     []model.FeelingLogDTO `json:"list"`
		Total    int64                 `json:"total"`
		Page     int                   `json:"page"`
		PageSize int                   `json:"pageSize"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	require.Len(t, page.List, 2)
	assert.Equal(t, int64(2), page.Total)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 20, page.PageSize)

	// 编号与姓名并存：编号给跳转，姓名给人看
	assert.Equal(t, "P0000001", page.List[0].PatientID)
	require.NotNil(t, page.List[0].PatientName, "跨患者流必须带 patientName，否则前端患者列只剩编号")
	require.NotNil(t, page.List[1].PatientName)
	assert.Equal(t, "张阿姨", *page.List[0].PatientName)
	assert.Equal(t, "李大爷", *page.List[1].PatientName)
	require.NotNil(t, page.List[0].Feeling)
	require.NotNil(t, page.List[1].Feeling)
	assert.Equal(t, "discomfort", *page.List[0].Feeling)
	assert.Equal(t, "fitted", *page.List[1].Feeling)
}

// TestFeelingLogsAdmin_FiltersPassthrough 四个筛选条件与分页原样交给 repo
// （keyword 匹配患者姓名、日期区间闭区间、feeling 为 comfort_level 两档）。
func TestFeelingLogsAdmin_FiltersPassthrough(t *testing.T) {
	e := newEnv(t, false, false)
	w, _ := e.do(http.MethodGet,
		"/api/v1/admin/feeling-logs?keyword=%E5%BC%A0&startDate=2026-09-01&endDate=2026-09-07&feeling=fitted&page=2&pageSize=5",
		nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, repo.FeelingLogAdminFilter{
		Keyword:   "张",
		StartDate: "2026-09-01",
		EndDate:   "2026-09-07",
		Feeling:   "fitted",
		Page:      2,
		PageSize:  5,
	}, e.store.lastFeelingAdminFilter)
}

// TestFeelingLogsAdmin_RejectsUnknownFeeling feeling 只有两档；旧口径 comfortable/uncomfortable
// 一律 400，防止前端拿旧枚举静默筛出空列表。
func TestFeelingLogsAdmin_RejectsUnknownFeeling(t *testing.T) {
	e := newEnv(t, false, false)
	for _, v := range []string{"comfortable", "uncomfortable", "any"} {
		w, resp := e.do(http.MethodGet, "/api/v1/admin/feeling-logs?feeling="+v, nil, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code, "feeling=%s 应被拒", v)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
		assert.Empty(t, e.store.lastFeelingAdminFilter.Feeling, "非法枚举不得进 DB")
	}
}

// TestFeelingLogs_SinglePatientHasNoName 单患者端点不 join patients ⇒ patientName 为 null，
// 前端回落 patientId；同时守住「补字段没在没查姓名的路径上凭空造值」。
func TestFeelingLogs_SinglePatientHasNoName(t *testing.T) {
	e := newEnv(t, false, false)
	e.store.patient = &repo.PatientRow{PatientID: "P0000001"} // T353：列表端点先判患者存在
	e.store.feelings = []repo.FeelingLogRow{feelingAdminRow(21, "P0000001", "", 5, "fitted")}

	w, resp := e.do(http.MethodGet, "/api/v1/patients/P0000001/feeling-logs", nil,
		map[string]string{"X-Role": "ROLE_ADMIN", "X-User-Id": "ADM001"})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, model.CodeOK, resp.Code)

	var list []model.FeelingLogDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Nil(t, list[0].PatientName)
	// 键必须存在且为 null（不带 omitempty），口径同 msg-service 的 patientName
	assert.Contains(t, string(resp.Data), `"patientName":null`)
	assert.Equal(t, "P0000001", list[0].PatientID)
}

// TestTeamStats 四张统计卡原样透出，键名与契约一致。
func TestTeamStats(t *testing.T) {
	e := newEnv(t, false, false)
	e.store.teamCount = 7
	e.store.memberCount = 23
	e.store.managedPatientCount = 156
	e.store.unassignedPatientCount = 12

	w, resp := e.do(http.MethodGet, "/api/v1/admin/teams/stats", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t,
		`{"teamCount":7,"memberCount":23,"managedPatientCount":156,"unassignedPatientCount":12}`,
		string(resp.Data))
}
