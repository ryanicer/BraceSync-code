package calibration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

type fakeStore struct {
	baselines map[string]Baseline
	calls     int
	err       error
}

func (f *fakeStore) GetLatestBaseline(_ context.Context, deviceID string) (Baseline, bool, error) {
	f.calls++
	if f.err != nil {
		return Baseline{}, false, f.err
	}
	bl, ok := f.baselines[deviceID]
	return bl, ok, nil
}

func mkOffsets(values ...float32) [model.PointCount]float32 {
	var out [model.PointCount]float32
	copy(out[:], values)
	return out
}

func TestApply_NoBaseline_RawPassthrough(t *testing.T) {
	store := &fakeStore{baselines: map[string]Baseline{}}
	c := NewCalibrator(store)

	in := mkOffsets(0.6, 0.4)
	res, err := c.Apply(context.Background(), "DEV1", in)
	require.NoError(t, err)
	assert.Equal(t, in, res.Points, "缺基线不减偏移")
	assert.False(t, res.Applied, "缺基线显式未校准，不得静默当 0 偏移")
	assert.Zero(t, res.BaselineID)
}

func TestApply_WithBaseline_SubtractsOffsets(t *testing.T) {
	store := &fakeStore{baselines: map[string]Baseline{
		"DEV1": {BaselineID: 42, Offsets: mkOffsets(0.3, 0.1)},
	}}
	c := NewCalibrator(store)

	res, err := c.Apply(context.Background(), "DEV1", mkOffsets(0.6, 0.4, 1.0))
	require.NoError(t, err)
	assert.True(t, res.Applied)
	assert.Equal(t, int64(42), res.BaselineID)
	assert.InDelta(t, 0.3, res.Points[0], 0.0001)
	assert.InDelta(t, 0.3, res.Points[1], 0.0001)
	assert.InDelta(t, 1.0, res.Points[2], 0.0001, "无偏移点位原值透传")
}

func TestApply_StoreErrorPropagates(t *testing.T) {
	store := &fakeStore{err: errors.New("db down")}
	c := NewCalibrator(store)

	_, err := c.Apply(context.Background(), "DEV1", mkOffsets(1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestCache_HitsWithinTTL_ReReadsAfterExpiry(t *testing.T) {
	store := &fakeStore{baselines: map[string]Baseline{
		"DEV1": {BaselineID: 1, Offsets: mkOffsets(0.1)},
	}}
	c := NewCalibrator(store)
	ctx := context.Background()

	_, err := c.Apply(ctx, "DEV1", mkOffsets(1))
	require.NoError(t, err)
	_, err = c.Apply(ctx, "DEV1", mkOffsets(1))
	require.NoError(t, err)
	assert.Equal(t, 1, store.calls, "TTL 内命中缓存，不重复查库")

	// 规矩 A 下基线不可变，缓存安全；但 TTL 过期仍以最新 baseline_id 为准
	now2 := c.now().Add(DefaultCacheTTL + time.Second)
	c.now = func() time.Time { return now2 }
	_, err = c.Apply(ctx, "DEV1", mkOffsets(1))
	require.NoError(t, err)
	assert.Equal(t, 2, store.calls, "TTL 过期重读 DB")
}

func TestCache_NegativeResultCached(t *testing.T) {
	store := &fakeStore{baselines: map[string]Baseline{}}
	c := NewCalibrator(store)
	ctx := context.Background()

	_, err := c.Apply(ctx, "DEV1", mkOffsets(1))
	require.NoError(t, err)
	res, err := c.Apply(ctx, "DEV1", mkOffsets(1))
	require.NoError(t, err)
	assert.Equal(t, 1, store.calls, "负缓存：无基线结果同样入缓存")
	assert.False(t, res.Applied)
}

func TestApply_DevicesIsolated(t *testing.T) {
	store := &fakeStore{baselines: map[string]Baseline{
		"DEV1": {BaselineID: 1, Offsets: mkOffsets(0.3)},
		"DEV2": {BaselineID: 2, Offsets: mkOffsets(0.9)},
	}}
	c := NewCalibrator(store)
	ctx := context.Background()

	r1, err := c.Apply(ctx, "DEV1", mkOffsets(1))
	require.NoError(t, err)
	r2, err := c.Apply(ctx, "DEV2", mkOffsets(1))
	require.NoError(t, err)
	assert.InDelta(t, 0.7, r1.Points[0], 0.0001)
	assert.InDelta(t, 0.1, r2.Points[0], 0.0001)
}
