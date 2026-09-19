// Package config — 逐采集点规则热更新测试（T252 2.2）
//
// 覆盖 config.PointRuleStore 注入路径：阈值与逐点规则同轮加载/同轮生效，
// 读取失败时引擎整体保持上一份生效值（不出现「阈值新、逐点旧」的半更新态）。
package config

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

// ─────────────────────────────────────────────────────────────
// 测试替身：内存 PointRuleStore（可注入读错误）
// ─────────────────────────────────────────────────────────────

type fakePointStore struct {
	rules    map[string]engine.PointRule
	fetchErr error
	fetches  int
}

func (s *fakePointStore) FetchPointRules(_ context.Context) (map[string]engine.PointRule, error) {
	s.fetches++
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	out := make(map[string]engine.PointRule, len(s.rules))
	for k, v := range s.rules {
		out[k] = v
	}
	return out, nil
}

// newEval 空评估器（DedupWindowMinutes=0 不去重），阈值全部经 Manager.Refresh 注入
func newEval() *engine.RuleEvaluator { return &engine.RuleEvaluator{} }

func pointFrameAt(idx int, v float64) engine.PressureFrame {
	frame := engine.PressureFrame{DeviceID: "DEV-1", Timestamp: time.Now(), Wearing: true}
	frame.Pressures[idx] = v
	return frame
}

func newPointManager(t *testing.T) (*Manager, *fakeStore, *fakePointStore, func()) {
	t.Helper()
	clock := time.Now()
	st := newFakeStore(map[string]string{
		KeyPressureHigh:    "45",
		KeyWearInterrupt:   "60",
		KeyCollectInterval: "30",
	})
	ps := &fakePointStore{rules: map[string]engine.PointRule{"P03": {Monitored: false}}}
	m := NewManagerWithPointRules(st, ps)
	m.now = func() time.Time { return clock }
	return m, st, ps, func() { clock = clock.Add(DefaultCacheTTL + time.Second) }
}

func TestPointRules_RefreshInjectsIntoEvaluator(t *testing.T) {
	m, _, ps, _ := newPointManager(t)
	ps.rules["P05"] = engine.PointRule{Monitored: true, UpperN: 20}

	eval := newEval()
	th, err := m.Refresh(context.Background(), eval)
	require.NoError(t, err)
	assert.Equal(t, 45.0, th.PressureHighN)

	assert.Nil(t, eval.Evaluate(pointFrameAt(2, 50), nil), "P03 未勾选应被跳过")
	res := eval.Evaluate(pointFrameAt(4, 30), nil)
	require.NotNil(t, res, "P05 独立上限 20N 应触发")
	assert.Equal(t, 20.0, res.ThresholdValue)
}

func TestPointRules_WithoutStoreKeepsUnifiedOnly(t *testing.T) {
	// 旧构造入口（NewManager）不接逐点规则：引擎全点位跟随统一上限
	m, _, _, _ := newPointManager(t)
	m.points = nil
	eval := newEval()
	_, err := m.Refresh(context.Background(), eval)
	require.NoError(t, err)
	res := eval.Evaluate(pointFrameAt(2, 50), nil)
	require.NotNil(t, res)
	assert.Equal(t, "P03", res.SensorPoint)
}

func TestPointRules_FetchFailureKeepsPreviousEffectiveConfig(t *testing.T) {
	m, st, ps, advance := newPointManager(t)
	eval := newEval()
	_, err := m.Refresh(context.Background(), eval)
	require.NoError(t, err)

	// DB 侧把统一上限改成 10N，但逐点规则读失败 ⇒ 两份配置都不生效
	ps.fetchErr = errors.New("boom")
	st.data[KeyPressureHigh] = "10"
	advance()
	_, err = m.Refresh(context.Background(), eval)
	assert.Error(t, err)
	res := eval.Evaluate(pointFrameAt(0, 50), nil)
	require.NotNil(t, res)
	assert.Equal(t, 45.0, res.ThresholdValue, "逐点规则读取失败时阈值不应半更新")

	// 恢复后下一轮整体生效（上限 10N + P03 重新参与监控）
	ps.fetchErr = nil
	ps.rules = map[string]engine.PointRule{}
	advance()
	_, err = m.Refresh(context.Background(), eval)
	require.NoError(t, err)
	res = eval.Evaluate(pointFrameAt(2, 50), nil)
	require.NotNil(t, res)
	assert.Equal(t, 10.0, res.ThresholdValue)
}

func TestPointRules_CacheTTLReusesSnapshot(t *testing.T) {
	m, _, ps, advance := newPointManager(t)
	eval := newEval()
	_, err := m.Refresh(context.Background(), eval)
	require.NoError(t, err)
	before := ps.fetches

	_, err = m.Refresh(context.Background(), eval)
	require.NoError(t, err)
	assert.Equal(t, before, ps.fetches, "TTL 内不重复读库")

	advance()
	_, err = m.Refresh(context.Background(), eval)
	require.NoError(t, err)
	assert.Equal(t, before+1, ps.fetches, "TTL 过期后同轮重读两份配置")
}
