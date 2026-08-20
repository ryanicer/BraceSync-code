// Package service T021：分区预建单元测试（实现侧）
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPartitionService_EnsureFuturePartitions_CreatesTwo(t *testing.T) {
	store := newFakePartitionStore()
	svc := NewPartitionService(store)
	svc.now = func() time.Time { return time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC) } // CST 08-25 08:00

	svc.EnsureFuturePartitions(context.Background())

	assert.Len(t, store.created, 2)
	assert.Contains(t, store.created, "pressure_records_202609")
	assert.Contains(t, store.created, "pressure_records_202610")
}

func TestPartitionService_EnsureFuturePartitions_ExistsSkip(t *testing.T) {
	store := newFakePartitionStore()
	store.existing["pressure_records_202609"] = true // 9 月已存在
	svc := NewPartitionService(store)
	svc.now = func() time.Time { return time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC) }

	svc.EnsureFuturePartitions(context.Background())

	assert.Len(t, store.created, 1) // 仅 10 月
	assert.Contains(t, store.created, "pressure_records_202610")
}

func TestPartitionService_EnsureFuturePartitions_AllExist(t *testing.T) {
	store := newFakePartitionStore()
	store.existing["pressure_records_202609"] = true
	store.existing["pressure_records_202610"] = true
	svc := NewPartitionService(store)
	svc.now = func() time.Time { return time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC) }

	svc.EnsureFuturePartitions(context.Background())
	assert.Empty(t, store.created, "全部已存在不应创建")
}

func TestPartitionService_EnsureFuturePartitions_ExistsError(t *testing.T) {
	store := newFakePartitionStore()
	store.existsErr = errors.New("db error")
	svc := NewPartitionService(store)
	svc.now = func() time.Time { return time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC) }

	svc.EnsureFuturePartitions(context.Background()) // 不应 panic
	assert.Empty(t, store.created)
}

func TestPartitionService_EnsureFuturePartitions_CreateError(t *testing.T) {
	store := newFakePartitionStore()
	store.ensureErr = errors.New("create failed")
	svc := NewPartitionService(store)
	svc.now = func() time.Time { return time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC) }

	svc.EnsureFuturePartitions(context.Background()) // 不应 panic
	assert.Empty(t, store.created)
}

func TestPartitionService_YearRollover(t *testing.T) {
	store := newFakePartitionStore()
	svc := NewPartitionService(store)
	svc.now = func() time.Time { return time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC) } // CST 11-25

	svc.EnsureFuturePartitions(context.Background())

	assert.Len(t, store.created, 2)
	assert.Contains(t, store.created, "pressure_records_202612") // +1 月
	assert.Contains(t, store.created, "pressure_records_202701") // +2 月（跨年）
}
