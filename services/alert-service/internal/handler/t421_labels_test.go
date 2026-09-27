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
//
// T430（PRD §7D.6 拍 C）：本用例原来把第五格 pressure_fluctuation 也当「会出现在 CSV 里的行」断言，
// 裁定四把导出纳入隐藏口径后那一行不再进明细 —— 键（上一条用例）与行（本条 + t430_export_hidden_test.go）
// 就此分家，两侧各自钉住：标签表仍逐键同形，明细不再出现砍除类型。
func TestT421_ExportTypeColumnUsesUnifiedLabels(t *testing.T) {
	cases := exportVisibleLabelCases(t421UnifiedLabels())
	rows := make([]repo.AlertRow, 0, len(cases)+2)
	for i, tc := range cases {
		rows = append(rows, repo.AlertRow{
			AlertID: int64(101 + i), PatientID: "P001", DeviceID: "DEV01",
			Type: tc.code, Ts: time.Date(2026, 9, 2, i, 0, 0, 0, time.UTC),
			ReadStatus: "unread", ProcessStatus: "pending",
		})
	}
	// 砍除类型那一行也要进 store：它证明本用例看到的是「被滤掉」而不是「造数据时漏了」
	rows = append(rows, repo.AlertRow{
		AlertID: 198, PatientID: "P001", DeviceID: "DEV01",
		Type: "pressure_fluctuation", Ts: time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC),
		ReadStatus: "unread", ProcessStatus: "pending",
	})
	// 白名单外的历史码值仍要出现在明细里（透传，不是空串）
	rows = append(rows, repo.AlertRow{
		AlertID: 199, PatientID: "P001", DeviceID: "DEV01",
		Type: "legacy_type", Ts: time.Date(2026, 9, 2, 23, 0, 0, 0, time.UTC),
		ReadStatus: "unread", ProcessStatus: "pending",
	})

	store := &fakePublicStore{exportRows: rows}
	rec := doReport(newPublicHandler(store), t300ExportPath+t300Query, roleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)

	_, records := readT300CSV(t, rec)
	require.Len(t, records, len(cases)+2, "表头 + 每条可见明细（砍除类型那一行不进 CSV），按 ts 升序")
	for i, tc := range cases {
		assert.Equal(t, tc.label, records[i+1][4], "明细第 %d 行（码值 %s）的类型列", i+1, tc.code)
	}
	assert.Equal(t, "legacy_type", records[len(cases)+1][4], "未知码值透传裸码，不得显示成空")
}

// exportVisibleLabelCases 从全站口径源里挑出「会出现在导出明细里」的那些码值，
// 判据与 hiddenReportAlertTypes 同一处（不复制一份名单，防止两边各自腐烂）。
func exportVisibleLabelCases(all []t421LabelCase) []t421LabelCase {
	visible := make([]t421LabelCase, 0, len(all))
	for _, tc := range all {
		if isHiddenReportAlertType(tc.code) {
			continue
		}
		visible = append(visible, tc)
	}
	return visible
}
