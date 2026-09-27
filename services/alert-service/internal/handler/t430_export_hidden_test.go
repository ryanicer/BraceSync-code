// Package handler — T430（PRD §7D.6 历史数据处置拍 C·Boss 2026-09-27）导出侧收口用例。
//
// 裁定口径＝界面隐藏、数据不删；PM 09-27 19:00 裁定四把异常报告 CSV 纳入同一口径。
// 两条用例一头一尾，缺一就成了「只证明了一半」：
//   - 明细导出不含已裁砍除类型的历史行（且同批其他类型照常出，证明滤的是那一行不是整个文件）
//   - 汇总 JSON 仍计入该行（证明收口只发生在导出明细，库里数据没被动、口径没被顺手做成「删数据」）
package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

func t430ExportRows() []repo.AlertRow {
	day := func(h int) time.Time { return time.Date(2026, 9, 2, h, 0, 0, 0, time.UTC) }
	return []repo.AlertRow{
		{AlertID: 201, PatientID: "P001", DeviceID: "DEV01", Type: "pressure_high",
			Detail: "P10 压力持续偏高", SensorPoint: "P10", Ts: day(1), ReadStatus: "unread", ProcessStatus: "pending"},
		{AlertID: 202, PatientID: "P001", DeviceID: "DEV01", Type: "pressure_fluctuation",
			Detail: "P05 压力波动异常，短时间内多次超阈值", SensorPoint: "P05", Ts: day(2), ReadStatus: "read", ProcessStatus: "processed"},
		{AlertID: 203, PatientID: "P001", DeviceID: "DEV01", Type: "sensor_drift",
			Detail: "P12 传感器数据漂移", SensorPoint: "P12", Ts: day(3), ReadStatus: "unread", ProcessStatus: "pending"},
	}
}

func TestT430_ExportSkipsDeprecatedAlertType(t *testing.T) {
	store := &fakePublicStore{exportRows: t430ExportRows()}
	rec := doReport(newPublicHandler(store), t300ExportPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)

	body, records := readT300CSV(t, rec)
	require.Len(t, records, 3, "表头 + 2 条明细：砍除类型那一行不进 CSV，其余两条照旧")
	assert.Equal(t, "201", records[1][0], "按 ts 升序的第一条")
	assert.Equal(t, "203", records[2][0], "被跳过的那一行不得占位")
	assert.NotContains(t, body, "压力波动", "已裁砍除类型的中文标签不得出现在明细里")
	assert.NotContains(t, body, "pressure_fluctuation", "也不得以裸码值形式漏出去")
	assert.Equal(t, "传感器标定异常", records[2][4], "其余类型的标签列不受影响")
}

func TestT430_SummaryStillCountsHistoricalRows(t *testing.T) {
	store := &fakePublicStore{summaryRows: []repo.AlertSummaryRow{
		{Date: "2026-09-02", Type: "pressure_high", ProcessStatus: "pending", Count: 4},
		{Date: "2026-09-02", Type: "pressure_fluctuation", ProcessStatus: "processed", Count: 2},
	}}
	rec := doReport(newPublicHandler(store), t300SummaryPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)

	var env struct {
		Code int                `json:"code"`
		Data abnormalReportData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.EqualValues(t, 6, env.Data.Total, "汇总不收口：本卡裁定只到明细导出，聚合口径不动")
	assert.EqualValues(t, 2, env.Data.ByType[1].Count, "砍除类型仍按类型回传（界面按展示侧判据隐藏，见前端 isHiddenAlertType）")
	assert.Equal(t, "pressure_fluctuation", env.Data.ByType[1].Key)
}
