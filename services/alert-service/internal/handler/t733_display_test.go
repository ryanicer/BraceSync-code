// T733 方案A · 展示出口量纲门禁（alert-service GET /api/v1/alerts）
//
// alerts.threshold_value / actual_value 自 T733 起按 **mN** 存（引擎产出即 mN），
// N 只在本服务的 HTTP 展示响应里出现（÷config.MnPerN）。
// 这条用例钉住「哪些类型换算、哪些类型原样」，防止 346mN 又被当成 346N 显示，
// 也防止分钟/百分比类被误除 1000。
package handler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bracesync/bracesync/services/alert-service/internal/config"
	"github.com/bracesync/bracesync/services/alert-service/internal/repo"
)

func TestT733_AlertItemConvertsPressureMnToN(t *testing.T) {
	ts := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		typ         string
		thrStored   float64
		actStored   float64
		wantThrN    float64
		wantActN    float64
		wantDivided bool
	}{
		{"现网误报值 346mN", "pressure_high", 5000, 346, 5, 0.346, true},
		{"压力偏高落库值", "pressure_high", 45000, 52500, 45, 52.5, true},
		{"标定异常负向读数", "sensor_drift", 300, -350, 0.3, -0.35, true},
		{"设备离线（分钟）", "wear_interrupt", 60, 35, 60, 35, false},
		{"历史波动（百分比）", "pressure_fluctuation", 30, 47.2, 30, 47.2, false},
		{"佩戴时长不足（分钟）", "wear_duration_short", 1320, 600, 1320, 600, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			item := toAlertItem(repo.AlertRow{
				AlertID: 1, Type: c.typ, Ts: ts,
				ThresholdValue: c.thrStored, ActualValue: c.actStored,
			})
			assert.InDelta(t, c.wantThrN, item.ThresholdValue, 1e-9)
			assert.InDelta(t, c.wantActN, item.ActualValue, 1e-9)
			if c.wantDivided {
				assert.InDelta(t, c.thrStored/config.MnPerN, item.ThresholdValue, 1e-9,
					"压力量纲必须按 mN→N 出口换算")
			} else {
				assert.Equal(t, c.thrStored, item.ThresholdValue, "非压力量纲类型不得参与换算")
			}
		})
	}
}
