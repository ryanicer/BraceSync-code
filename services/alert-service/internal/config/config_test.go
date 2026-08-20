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
// 测试替身：内存 Store（记录写入次数，可注入读错误）
// ─────────────────────────────────────────────────────────────

type fakeStore struct {
	data     map[string]string
	upserts  int
	fetchErr error
}

func newFakeStore(data map[string]string) *fakeStore {
	if data == nil {
		data = map[string]string{}
	}
	return &fakeStore{data: data}
}

func (s *fakeStore) FetchAll(_ context.Context) (map[string]string, error) {
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	out := make(map[string]string, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, nil
}

func (s *fakeStore) Upsert(_ context.Context, values map[string]string) error {
	s.upserts++
	for k, v := range values {
		s.data[k] = v
	}
	return nil
}

func iPtr(v int) *int         { return &v }
func fPtr(v float64) *float64 { return &v }

// ─────────────────────────────────────────────────────────────
// ValidateThresholds 纯函数（test-plan §3.1 A6 三分支 + 非法输入）
// ─────────────────────────────────────────────────────────────

func TestValidateThresholds_A6(t *testing.T) {
	cases := []struct {
		name      string
		interval  int
		interrupt int
		wantOK    bool
	}{
		{"A6 拒绝分支：采集间隔 40min + 中断阈值 60min（<80min）", 40, 60, false},
		{"A6 拒绝分支（差 1min）：40min + 79min", 40, 79, false},
		{"A6 通过分支：采集间隔 40min + 中断阈值 80min", 40, 80, true},
		{"边界：恰好 =2× 通过（PRD \"≥\" 语义）", 40, 80, true},
		{"通过分支（富余）：40min + 90min", 40, 90, true},
		{"默认口径自洽：30min + 60min", 30, 60, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateThresholds(tc.interval, tc.interrupt)
			if tc.wantOK {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, ErrCodeThresholdLinkage, verr.Code, "错误码归 9xxxx 系统级（架构 §3.5）")
			assert.Contains(t, verr.Message, "2×采集间隔", "错误信息须给出明确联动约束")
		})
	}
}

func TestValidateThresholds_InvalidInputs(t *testing.T) {
	assert.Error(t, ValidateThresholds(0, 60), "采集间隔零值拒绝")
	assert.Error(t, ValidateThresholds(-5, 60), "采集间隔负值拒绝")
	assert.Error(t, ValidateThresholds(40, 0), "中断阈值零值拒绝")
	assert.Error(t, ValidateThresholds(40, -1), "中断阈值负值拒绝")
}

// ─────────────────────────────────────────────────────────────
// ParseThresholds：缺失/非法值回退默认
// ─────────────────────────────────────────────────────────────

func TestParseThresholds(t *testing.T) {
	th := ParseThresholds(map[string]string{
		KeyPressureHigh:    "50",
		KeyFluctuationPct:  "25",
		KeyWearInterrupt:   "90",
		KeySensorDrift:     "3",
		KeyCollectInterval: "40",
	})
	assert.Equal(t, 50.0, th.PressureHighN)
	assert.Equal(t, 25.0, th.FluctuationPct)
	assert.Equal(t, 90, th.WearInterruptMinutes)
	assert.Equal(t, 3.0, th.SensorDriftN)
	assert.Equal(t, 40, th.CollectIntervalMinutes)

	// 非法值（非数值/非正）与缺失键均回退 PRD 默认
	def := DefaultThresholds()
	got := ParseThresholds(map[string]string{
		KeyPressureHigh:  "abc",
		KeyWearInterrupt: "-5",
		KeySensorDrift:   "0",
	})
	assert.Equal(t, def, got)
}

// ─────────────────────────────────────────────────────────────
// Manager：加载 / 热更新 / 缓存失效 / 引擎读最新值
// ─────────────────────────────────────────────────────────────

func TestManager_Load_DefaultsWhenEmpty(t *testing.T) {
	mgr := NewManager(newFakeStore(nil))
	th, err := mgr.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, DefaultThresholds(), th, "空表回退 PRD 默认口径（默认 30/60 自洽通过联动校验）")
}

func TestManager_Load_FromStoreValues(t *testing.T) {
	store := newFakeStore(map[string]string{
		KeyPressureHigh:    "50",
		KeyWearInterrupt:   "90",
		KeyCollectInterval: "40",
	})
	mgr := NewManager(store)
	th, err := mgr.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 50.0, th.PressureHighN)
	assert.Equal(t, 90, th.WearInterruptMinutes)
	assert.Equal(t, 40, th.CollectIntervalMinutes)
	assert.Equal(t, 30.0, th.FluctuationPct, "缺失键回退默认")
}

func TestManager_Load_LinkageInvalid_NotCached(t *testing.T) {
	// DB 中存在非法配置（40/60 违反联动）→ Load 拒绝缓存并返回 90712
	store := newFakeStore(map[string]string{
		KeyCollectInterval: "40",
		KeyWearInterrupt:   "60",
	})
	mgr := NewManager(store)
	_, err := mgr.Load(context.Background())
	require.Error(t, err)
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, ErrCodeThresholdLinkage, verr.Code)
	_, err = mgr.Current(context.Background())
	assert.Error(t, err, "非法配置不入缓存，Current 重新加载仍拒绝")
}

func TestManager_Update_Reject_KeepsConfig(t *testing.T) {
	store := newFakeStore(nil) // 空表 → 默认 30/60
	mgr := NewManager(store)
	ctx := context.Background()
	_, err := mgr.Load(ctx)
	require.NoError(t, err)

	// A6 联动修改拒绝路径：仅改采集间隔 30→40min，中断阈值 60min < 2×40=80min
	_, err = mgr.Update(ctx, ThresholdPatch{CollectIntervalMinutes: iPtr(40)})
	require.Error(t, err)
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, ErrCodeThresholdLinkage, verr.Code)
	assert.Equal(t, 0, store.upserts, "校验失败拒绝写入：不落库")

	cur, err := mgr.Current(ctx)
	require.NoError(t, err)
	assert.Equal(t, 30, cur.CollectIntervalMinutes, "拒绝后现有配置不变")
	assert.Equal(t, 60, cur.WearInterruptMinutes)
}

func TestManager_Update_Accept_InvalidatesCache(t *testing.T) {
	store := newFakeStore(nil)
	mgr := NewManager(store)
	ctx := context.Background()
	_, err := mgr.Load(ctx)
	require.NoError(t, err)

	// A6 联动修改通过路径：采集间隔 40min + 中断阈值 80min（=2× 边界，PRD ≥ 语义）
	th, err := mgr.Update(ctx, ThresholdPatch{
		PressureHighN:          fPtr(50),
		CollectIntervalMinutes: iPtr(40),
		WearInterruptMinutes:   iPtr(80),
	})
	require.NoError(t, err)
	assert.Equal(t, 40, th.CollectIntervalMinutes)
	assert.Equal(t, 80, th.WearInterruptMinutes)
	assert.Equal(t, 50.0, th.PressureHighN, "浮点阈值补丁同步生效")
	assert.Equal(t, 1, store.upserts, "校验通过后写库一次")
	assert.Equal(t, "40", store.data[KeyCollectInterval])
	assert.Equal(t, "80", store.data[KeyWearInterrupt])
	assert.Equal(t, "50", store.data[KeyPressureHigh])

	// 缓存失效：TTL 内 Current 立即读到新值（无需等 60s）
	cur, err := mgr.Current(ctx)
	require.NoError(t, err)
	assert.Equal(t, th, cur, "更新后缓存失效，读取立即生效")
}

func TestManager_Update_EmptyPatch_NoWrite(t *testing.T) {
	store := newFakeStore(nil)
	mgr := NewManager(store)
	ctx := context.Background()
	_, err := mgr.Load(ctx)
	require.NoError(t, err)

	cur, err := mgr.Update(ctx, ThresholdPatch{})
	require.NoError(t, err)
	assert.Equal(t, DefaultThresholds(), cur)
	assert.Equal(t, 0, store.upserts, "空补丁不写库")
}

func TestManager_Current_TTLHotReload(t *testing.T) {
	store := newFakeStore(map[string]string{
		KeyCollectInterval: "30",
		KeyWearInterrupt:   "60",
	})
	mgr := NewManager(store)
	now := time.Now()
	mgr.now = func() time.Time { return now }
	ctx := context.Background()

	_, err := mgr.Load(ctx)
	require.NoError(t, err)

	// 外部路径改库后 TTL 内仍读缓存（旧值）
	store.data[KeyWearInterrupt] = "90"
	cur, err := mgr.Current(ctx)
	require.NoError(t, err)
	assert.Equal(t, 60, cur.WearInterruptMinutes, "TTL 内命中缓存")

	// TTL 过期后自动重读（热更新生效）
	now = now.Add(DefaultCacheTTL + time.Second)
	cur, err = mgr.Current(ctx)
	require.NoError(t, err)
	assert.Equal(t, 90, cur.WearInterruptMinutes, "缓存过期重读 DB = 热更新生效")
}

func TestManager_Refresh_EngineReadsLatest(t *testing.T) {
	store := newFakeStore(map[string]string{
		KeyCollectInterval: "30",
		KeyWearInterrupt:   "60",
	})
	mgr := NewManager(store)
	ctx := context.Background()
	eval := engine.NewDefaultRuleEvaluator()

	_, err := mgr.Refresh(ctx, eval)
	require.NoError(t, err)
	assert.Equal(t, 60, eval.WearInterruptMinutes)
	assert.Equal(t, 30, eval.CollectionIntervalMin)

	// 热更新：联动修改采集间隔 40min + 中断 80min
	_, err = mgr.Update(ctx, ThresholdPatch{
		CollectIntervalMinutes: iPtr(40),
		WearInterruptMinutes:   iPtr(80),
	})
	require.NoError(t, err)
	_, err = mgr.Refresh(ctx, eval)
	require.NoError(t, err)
	assert.Equal(t, 80, eval.WearInterruptMinutes, "引擎读取最新配置值")
	assert.Equal(t, 40, eval.CollectionIntervalMin)

	// 引擎按新阈值判定：70min 间隔不再触发（新阈值 80min），90min 触发
	now := time.Now()
	assert.Nil(t, eval.EvaluateWearInterrupt("DEV001", now.Add(-70*time.Minute), now),
		"阈值调大后原触发间隔不再命中")
	assert.NotNil(t, eval.EvaluateWearInterrupt("DEV001", now.Add(-90*time.Minute), now))
}

func TestManager_Refresh_LoadFailureKeepsEvaluator(t *testing.T) {
	// DB 中为非法联动配置 → Refresh 失败，评估器保持默认口径不被污染
	store := newFakeStore(map[string]string{
		KeyCollectInterval: "40",
		KeyWearInterrupt:   "60",
	})
	mgr := NewManager(store)
	eval := engine.NewDefaultRuleEvaluator()

	_, err := mgr.Refresh(context.Background(), eval)
	require.Error(t, err)
	assert.Equal(t, 60, eval.WearInterruptMinutes, "校验失败评估器保持默认口径")
	assert.Equal(t, 30, eval.CollectionIntervalMin)
}

func TestManager_Load_StoreError(t *testing.T) {
	store := newFakeStore(nil)
	store.fetchErr = errors.New("db down")
	mgr := NewManager(store)
	_, err := mgr.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}
