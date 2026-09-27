// Package handler — T421（G-6 后端镜像）告警类型显示口径锁定用例。
//
// 全站唯一口径源是 packages/shared-utils/src/index.ts 的 ALERT_TYPE_LABELS；
// reportAlertTypeLabels 是它在导出 CSV 侧的镜像。两侧任一处改名字而另一处没改，
// 后台「告警管理」页与异常报告 CSV 就会对同一告警叫两个名。
package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

type t421LabelCase struct {
	code  string
	label string
}

// t421UnifiedLabels 新四类 + 一个仅历史行的旧类（Boss Q1=方案A：改名不改判定，码值不动）。
func t421UnifiedLabels() []t421LabelCase {
	return []t421LabelCase{
		{"pressure_high", "压力偏高"},
		{"wear_interrupt", "设备离线"},
		{"wear_duration_short", "佩戴时长不足"},
		{"sensor_drift", "传感器标定异常"},
		{"pressure_fluctuation", "压力波动"},
	}
}

func TestT421_LabelTableMatchesUnifiedCaliber(t *testing.T) {
	cases := t421UnifiedLabels()
	require.Len(t, reportAlertTypeLabels, len(cases), "标签表键数须与全站口径源一致")
	for _, tc := range cases {
		v, ok := reportAlertTypeLabels[tc.code]
		assert.True(t, ok, "码值 %s 必须在标签表里，否则它在 CSV 里退化成裸码值", tc.code)
		assert.Equal(t, tc.label, v, "码值 %s 的显示名", tc.code)
	}
	// 旧文案不得回流：改名只发生在展示层，码值（上面的键）一个都不动
	for _, stale := range []string{"佩戴中断", "传感器漂移"} {
		for code, v := range reportAlertTypeLabels {
			assert.NotEqual(t, stale, v, "码值 %s 仍挂着旧文案", code)
		}
	}
	assert.Equal(t, "some_future_type", labelOf(reportAlertTypeLabels, "some_future_type"),
		"白名单外的码值原样输出，不吞不猜")
}

// 常量改对了但没接上渲染 = 假修：这条走真实导出路径看类型列。
func TestT421_ExportTypeColumnUsesUnifiedLabels(t *testing.T) {
	cases := t421UnifiedLabels()
	rows := make([]repo.AlertRow, 0, len(cases)+1)
	for i, tc := range cases {
		rows = append(rows, repo.AlertRow{
			AlertID: int64(101 + i), PatientID: "P001", DeviceID: "DEV01",
			Type: tc.code, Ts: time.Date(2026, 9, 2, i, 0, 0, 0, time.UTC),
			ReadStatus: "unread", ProcessStatus: "pending",
		})
	}
	// 白名单外的历史码值也要出现在明细里（透传，不是空串）
	rows = append(rows, repo.AlertRow{
		AlertID: 199, PatientID: "P001", DeviceID: "DEV01",
		Type: "legacy_type", Ts: time.Date(2026, 9, 2, 23, 0, 0, 0, time.UTC),
		ReadStatus: "unread", ProcessStatus: "pending",
	})

	store := &fakePublicStore{exportRows: rows}
	rec := doReport(newPublicHandler(store), t300ExportPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)

	_, records := readT300CSV(t, rec)
	require.Len(t, records, len(rows)+1, "表头 + 每条明细，按 ts 升序")
	for i, tc := range cases {
		assert.Equal(t, tc.label, records[i+1][4], "明细第 %d 行（码值 %s）的类型列", i+1, tc.code)
	}
	assert.Equal(t, "legacy_type", records[len(rows)][4], "未知码值透传裸码，不得显示成空")
}
