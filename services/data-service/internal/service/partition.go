// Package service T021：分区预建服务
//
// 职责：每月 25 日预建 pressure_records 未来 2 个月分区
// 对齐：架构 §3.6 / §4.4
package service

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/data-service/internal/metrics"
	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// PartitionService 分区预建编排
type PartitionService struct {
	partitions repo.PartitionStore
	now        func() time.Time
}

// NewPartitionService 组装 PartitionService
func NewPartitionService(partitions repo.PartitionStore) *PartitionService {
	return &PartitionService{
		partitions: partitions,
		now:        time.Now,
	}
}

// EnsureFuturePartitions 预建未来 2 个月的 pressure_records 分区
func (s *PartitionService) EnsureFuturePartitions(ctx context.Context) {
	now := s.now().In(model.CSTZone())
	log.Info().Msg("partition pre-creation started")

	created := 0
	for i := 1; i <= 2; i++ {
		target := time.Date(now.Year(), now.Month()+time.Month(i), 1, 0, 0, 0, 0, model.CSTZone())
		name := target.Format("200601")

		exists, err := s.partitions.PartitionExists(ctx, "pressure_records_"+name)
		if err != nil {
			metrics.PartitionJobTotal.WithLabelValues("error").Inc()
			log.Error().Err(err).Str("partition", name).Msg("check partition existence failed")
			continue
		}
		if exists {
			log.Info().Str("partition", name).Msg("partition already exists, skip")
			continue
		}

		if err := s.partitions.EnsurePartition(ctx, target); err != nil {
			metrics.PartitionJobTotal.WithLabelValues("error").Inc()
			log.Error().Err(err).Str("partition", name).Msg("create partition failed")
			continue
		}
		created++
		log.Info().Str("partition", name).Msg("partition created")
	}

	metrics.PartitionJobTotal.WithLabelValues("ok").Inc()
	log.Info().Int("created", created).Msg("partition pre-creation completed")
}
