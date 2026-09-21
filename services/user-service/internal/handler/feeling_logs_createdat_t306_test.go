// T306：feeling_logs.created_at（提交时间）透出到 FeelingLogDTO。
//
// 缺陷原貌：DB 列一直存在（000001_init_schema.up.sql feeling_logs.created_at TIMESTAMPTZ
// NOT NULL DEFAULT now()），但 repo 两条 SELECT 未查该列、DTO 也没有字段 ⇒ admin-web
// 「矫形日志」的「提交时间」列整列回落占位「—」（前端 Iris T289 已按 row.createdAt 渲染）。
// 与 patientName（T290-A）同族：链路通了、最后一跳没落值。
//
// 本文件守三件事：值到得了、格式是 RFC3339、且值来自 created_at 而不是拿 logDate 冒充。
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

// feelingRowWithCreated 业务日期与提交时刻刻意取不同值：log_date 只有日期（09-05），
// created_at 带当天 18:30 的时刻 —— 这样「前端拿 logDate 冒充」或「后端只透出日期」都会判红。
func feelingRowWithCreated(logID int64, patientID, patientName string) repo.FeelingLogRow {
	level := "fitted"
	created := time.Date(2026, 9, 5, 18, 30, 0, 0, time.UTC)
	return repo.FeelingLogRow{
		LogID:        logID,
		PatientID:    patientID,
		PatientName:  patientName,
		LogDate:      time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		ComfortLevel: &level,
		CreatedAt:    created,
	}
}

const t306WantCreated = "2026-09-05T18:30:00Z"

// TestFeelingLogsAdmin_CreatedAtInList 跨患者流：每行都带 createdAt。
func TestFeelingLogsAdmin_CreatedAtInList(t *testing.T) {
	e := newEnv(t, false, false)
	e.store.feelingsAdmin = []repo.FeelingLogRow{
		feelingRowWithCreated(31, "P0000001", "甲"),
		feelingRowWithCreated(32, "P0000002", "乙"),
	}
	e.store.feelingsAdminTotal = 2

	w, resp := e.do(http.MethodGet, "/api/v1/admin/feeling-logs?page=1&pageSize=20", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var page struct {
		List []model.FeelingLogDTO `json:"list"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &page))
	require.Len(t, page.List, 2)
	for i, row := range page.List {
		assert.Equal(t, t306WantCreated, row.CreatedAt, "第 %d 行 createdAt 应为 created_at 的 RFC3339", i)
		assert.Equal(t, "2026-09-05", row.LogDate, "logDate 仍是业务日期，不被提交时刻污染")
	}
	// 键名与 shared-types FeelingLog.createdAt 对齐，且真实有值（非 null）
	assert.Contains(t, string(resp.Data), `"createdAt":"`+t306WantCreated+`"`)
}

// TestFeelingLogs_SinglePatientHasCreatedAt 单患者端点同口径：补字段要在两条路径上都成立，
// 否则后台「患者工作台」里看同一份日志仍是空列。
func TestFeelingLogs_SinglePatientHasCreatedAt(t *testing.T) {
	e := newEnv(t, false, false)
	e.store.feelings = []repo.FeelingLogRow{feelingRowWithCreated(33, "P0000001", "")}

	w, resp := e.do(http.MethodGet, "/api/v1/patients/P0000001/feeling-logs", nil,
		map[string]string{"X-Role": "ROLE_ADMIN", "X-User-Id": "ADM001"})
	require.Equal(t, http.StatusOK, w.Code)

	var list []model.FeelingLogDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, t306WantCreated, list[0].CreatedAt)
	assert.Nil(t, list[0].PatientName, "单患者端点不 join，patientName 仍为 null（T290-A 口径未变）")
}

// TestFeelingLogs_CreatedAtIsRFC3339WithZone 后端一律按 UTC 出值并带时区标记：
// 前端 formatDateTime 是字符串切片实现，不认「裸的 8 位日期 + 无时区时刻」。
// 输入刻意用 +08:00，验证透出时被归一化，而不是原样吐出本地偏移。
func TestFeelingLogs_CreatedAtIsRFC3339WithZone(t *testing.T) {
	e := newEnv(t, false, false)
	row := feelingRowWithCreated(34, "P0000001", "")
	row.CreatedAt = time.Date(2026, 9, 5, 18, 30, 0, 0, time.FixedZone("CST", 8*3600))
	e.store.feelings = []repo.FeelingLogRow{row}

	w, resp := e.do(http.MethodGet, "/api/v1/patients/P0000001/feeling-logs", nil,
		map[string]string{"X-Role": "ROLE_ADMIN", "X-User-Id": "ADM001"})
	require.Equal(t, http.StatusOK, w.Code)

	var list []model.FeelingLogDTO
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "2026-09-05T10:30:00Z", list[0].CreatedAt)

	parsed, err := time.Parse(time.RFC3339, list[0].CreatedAt)
	require.NoError(t, err, "createdAt 必须是可解析的 RFC3339")
	assert.True(t, parsed.UTC().Equal(row.CreatedAt.UTC()), "归一化不得改变时刻本身")
}
