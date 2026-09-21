// Package service — T257 2.6 新告警类型 wear_duration_short 的 msg-service 侧验证。
//
// 为什么单独一个文件：类型枚举一旦与 alert-service / DB CHECK 漂移，症状是
// 「后台能配、推送静默不发」（notify.go 无规则即 accepted=false），最难查。
// 这里把三条都钉住：枚举收录 / 规则可写 / 有规则时能按规则推送。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
)

func TestT257_WearDurationShortIsKnownAlertType(t *testing.T) {
	assert.True(t, model.ValidAlertType(model.AlertTypeWearDurationShort),
		"新类型必须进 KnownAlertTypes，否则 UpdateNotifyRule 直接拒写")
	// 波动类型自 alert-service 停产生，但**仍是合法枚举**（DB CHECK 保留、admin 可查看历史规则）
	assert.True(t, model.ValidAlertType(model.AlertTypePressureFluctuation),
		"停产生 ≠ 从枚举删除：历史告警与既有规则行仍需可读")
	assert.False(t, model.ValidAlertType(model.AlertType("wear_duration_shortish")),
		"近似拼写不得被接受")
	assert.Contains(t, model.AlertTypeList(), "wear_duration_short",
		"报错文案必须由枚举生成，含新类型")
}

func TestT257_UpdateNotifyRuleAcceptsWearDurationShort(t *testing.T) {
	f := newImplFixture(t)

	rule, err := f.svc.UpdateNotifyRule(context.Background(), model.AlertTypeWearDurationShort,
		[]string{model.ChannelWechat, model.ChannelSMS},
		[]string{model.TargetPatient, model.TargetDoctor}, "admin")
	require.NoError(t, err)
	require.NotNil(t, rule)
	assert.Equal(t, model.AlertTypeWearDurationShort, rule.Type)

	got, err := f.svc.RouteNotify(context.Background(), model.AlertTypeWearDurationShort)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.ElementsMatch(t, []string{model.ChannelWechat, model.ChannelSMS}, got.Channels)
}

// 无规则行 ⇒ 不发送（既有语义）；配了规则 ⇒ 按渠道发出。T257 只关心后者不再被枚举挡掉。
func TestT257_SendAlertRoutesWearDurationShort(t *testing.T) {
	f := newImplFixture(t)
	_, err := f.svc.UpdateNotifyRule(context.Background(), model.AlertTypeWearDurationShort,
		[]string{model.ChannelWechat}, []string{model.TargetDoctor}, "admin")
	require.NoError(t, err)

	req := f.alertReq(model.AlertTypeWearDurationShort)
	req.Detail = "佩戴时长不足：P20260001 于 2026-08-09 累计佩戴 6.5 小时，低于目标 18.0 小时"
	res, err := f.svc.SendAlert(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, res.Accepted, "已配置规则的新类型必须被接受推送")
}
