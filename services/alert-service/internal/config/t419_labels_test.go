// T419 G-6/S-6 后端可见层防回潮门禁 —— 系统配置阈值校验报错文案。
//
// 为什么锁这一条：ValidateThresholds 的 ValidationError.Message 会经 Manager.Update
// （manager.go:119）随「保存配置」失败响应回到 admin 界面，是用户可见文案；
// PRD §7D.12（V3.20）把 wear_interrupt 的显示名收口为「设备离线」，
// 键名 threshold_wear_interrupt_minutes 与判定语义一律不动（改词不改码）。
package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestT419_ThresholdMessages_UseDeviceOfflineTerm(t *testing.T) {
	cases := []struct {
		name      string
		interval  int
		interrupt int
		wantPart  string
	}{
		{"正数分支：设备离线阈值必须为正数", 40, 0, "设备离线阈值必须为正数"},
		{"联动分支：设备离线阈值 < 2×采集间隔", 40, 79, "设备离线阈值 79min"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateThresholds(tc.interval, tc.interrupt)
			require.Error(t, err)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Contains(t, verr.Message, tc.wantPart)
			assert.NotContains(t, verr.Message, "佩戴中断", "旧词回潮：§7D.12 已收口为「设备离线」")
			// 错误码不许随文案变动（前端按 code 分支）
			assert.Equal(t, ErrCodeThresholdLinkage, verr.Code)
		})
	}

	// 通过分支不许被改词顺手改掉（PRD "≥" 语义、边界 =2× 放行）
	require.NoError(t, ValidateThresholds(40, 80))
}

func TestT419_ThresholdMessages_OnlyDisplayLayerChanged(t *testing.T) {
	// 键名与常量仍是旧标识符：这条断言把「改词不改码」钉住，
	// 防止后续把 threshold_wear_interrupt_minutes 一起改名而打断配置回显链路。
	if !strings.Contains(KeyWearInterrupt, "wear_interrupt") {
		t.Fatalf("配置键 %q 已改名 —— T419 只允许动显示层", KeyWearInterrupt)
	}
}
