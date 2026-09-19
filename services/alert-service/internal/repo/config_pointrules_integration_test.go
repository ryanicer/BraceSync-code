//go:build integration
// +build integration

// T252 2.2 集成测试：alert-service 经共享 DB 读逐采集点规则并热更新到引擎。
//
// 覆盖单元层无法验证的三件事：
//  1. alert_point_rules（migration 000016）真实列/类型可读，NUMERIC(6,2) → float64 保真；
//  2. 稀疏存放语义：未落库的点不出现条目 ⇒ 引擎跟随统一上限；
//  3. config.Manager + engine 端到端：monitored=false 的点超阈值不告警、独立上限的点按点判定。
//
// 运行：make test-integration（需 Docker）
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/config"
	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

func TestIT_T252_PointRules_ReaderAndEngineHotRefresh(t *testing.T) {
	ctx := context.Background()
	cleanITConfigs(ctx, t) // 空 sys_configs = 引擎回退 PRD 默认（统一上限 45N）
	_, err := itPool.Exec(ctx, `DELETE FROM alert_point_rules`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = itPool.Exec(context.Background(), `DELETE FROM alert_point_rules`)
	})

	_, err = itPool.Exec(ctx, `
		INSERT INTO alert_point_rules (point_id, monitored, upper_n, lower_n, updated_by) VALUES
		  ('P03', false, NULL,  NULL,  'A0001'),
		  ('P07', true,  20.50, 5.25,  'A0001'),
		  ('P11', true,  NULL,  8.00,  'A0002')`)
	require.NoError(t, err)

	r := NewConfigRepo(itPool)
	rules, err := r.FetchPointRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 3, "只返回已落库的点")
	assert.Equal(t, engine.PointRule{Monitored: false}, rules["P03"])
	assert.InDelta(t, 20.5, rules["P07"].UpperN, 1e-9)
	assert.True(t, rules["P07"].Monitored)
	assert.InDelta(t, 0, rules["P11"].UpperN, 1e-9, "upper_n 为 NULL ⇒ 跟随统一上限")

	// 端到端：Manager 一份快照同时把阈值与逐点规则推进引擎
	eval := &engine.RuleEvaluator{}
	th, err := config.NewManagerWithPointRules(r, r).Refresh(ctx, eval)
	require.NoError(t, err)
	assert.Equal(t, 45.0, th.PressureHighN)

	// P03 未勾选：50N 不告警
	assert.Nil(t, eval.Evaluate(pointRuleFrame(2, 50), nil))
	// P07 独立上限 20.5N：30N 触发，且阈值回显该点生效值
	res := eval.Evaluate(pointRuleFrame(6, 30), nil)
	require.NotNil(t, res)
	assert.Equal(t, "P07", res.SensorPoint)
	assert.InDelta(t, 20.5, res.ThresholdValue, 1e-9)
	// P11 未设独立上限：30N 不触发、50N 按统一 45N 触发
	assert.Nil(t, eval.Evaluate(pointRuleFrame(10, 30), nil))
	res = eval.Evaluate(pointRuleFrame(10, 50), nil)
	require.NotNil(t, res)
	assert.InDelta(t, 45.0, res.ThresholdValue, 1e-9)
}

func TestIT_T252_PointRules_EmptyTableMeansNoOverride(t *testing.T) {
	ctx := context.Background()
	_, err := itPool.Exec(ctx, `DELETE FROM alert_point_rules`)
	require.NoError(t, err)

	rules, err := NewConfigRepo(itPool).FetchPointRules(ctx)
	require.NoError(t, err)
	assert.Nil(t, rules, "空表返回 nil = 全点位跟随统一阈值")
}

func pointRuleFrame(idx int, v float64) engine.PressureFrame {
	frame := engine.PressureFrame{DeviceID: "DEV-IT-T252", Timestamp: time.Now(), Wearing: true}
	frame.Pressures[idx] = v
	return frame
}
