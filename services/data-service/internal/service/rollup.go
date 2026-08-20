// Package service T021：daily rollup 聚合服务
//
// 职责：
//  1. RunDailyRollup — 每日 00:10（Asia/Shanghai）聚合昨日所有患者明细 → daily_wear_stats UPSERT
//  2. ProcessBackfillQueue — 消费 rollup:recompute 队列，按患者+日期重算
//
// 对齐：架构 §3.6 / §4.4 / PRD §7A.11
package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/data-service/internal/metrics"
	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// RollupService daily rollup 聚合编排
type RollupService struct {
	stats   repo.DailyWearStatsStore
	cache   repo.CacheStore
	configs repo.ConfigStore
	now     func() time.Time
}

// NewRollupService 组装 RollupService
func NewRollupService(stats repo.DailyWearStatsStore, cache repo.CacheStore, configs repo.ConfigStore) *RollupService {
	return &RollupService{
		stats:   stats,
		cache:   cache,
		configs: configs,
		now:     time.Now,
	}
}

// RunDailyRollup 聚合昨日（Asia/Shanghai）所有患者明细 → daily_wear_stats UPSERT
func (s *RollupService) RunDailyRollup(ctx context.Context) {
	now := s.now().In(model.CSTZone())
	yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, model.CSTZone())
	dateStr := yesterday.Format("2006-01-02")

	log.Info().Str("date", dateStr).Msg("daily rollup started")

	if err := s.aggregateAndUpsert(ctx, yesterday, dateStr); err != nil {
		metrics.RollupJobTotal.WithLabelValues("error").Inc()
		log.Error().Err(err).Str("date", dateStr).Msg("daily rollup failed")
		return
	}

	metrics.RollupJobTotal.WithLabelValues("ok").Inc()
	log.Info().Str("date", dateStr).Msg("daily rollup completed")
}

// ProcessBackfillQueue 消费 rollup:recompute 队列（补传触发的受影响日期重算）
func (s *RollupService) ProcessBackfillQueue(ctx context.Context) {
	const maxBatch = 50 // 单轮最多处理 50 条，防止无限循环
	processed := 0

	for processed < maxBatch {
		payload, err := s.cache.DequeueRollup(ctx)
		if err != nil {
			log.Error().Err(err).Msg("dequeue rollup task failed")
			return
		}
		if payload == "" {
			break // 队列空
		}

		var task rollupTask
		if err := json.Unmarshal([]byte(payload), &task); err != nil {
			log.Error().Err(err).Str("payload", payload).Msg("unmarshal rollup task failed")
			continue
		}

		day, err := time.ParseInLocation("2006-01-02", task.Date, model.CSTZone())
		if err != nil {
			log.Error().Err(err).Str("date", task.Date).Msg("invalid rollup task date")
			continue
		}

		if err := s.aggregatePatientDate(ctx, day, task.PatientID); err != nil {
			metrics.RollupJobTotal.WithLabelValues("error").Inc()
			log.Error().Err(err).Str("patient_id", task.PatientID).Str("date", task.Date).Msg("backfill rollup recompute failed")
			continue
		}

		processed++
		metrics.RollupJobTotal.WithLabelValues("ok").Inc()
		log.Info().Str("patient_id", task.PatientID).Str("date", task.Date).Msg("backfill rollup recompute done")
	}

	if processed > 0 {
		log.Info().Int("processed", processed).Msg("backfill rollup queue batch done")
	}
}

// aggregateAndUpsert 聚合指定日期的全部患者并 UPSERT
func (s *RollupService) aggregateAndUpsert(ctx context.Context, date time.Time, dateStr string) error {
	interval, _, cfgErr := s.configs.GetDeviceConfig(ctx)
	if cfgErr != nil {
		log.Warn().Err(cfgErr).Msg("read device config failed for rollup, fallback 30min")
		interval = 30
	}

	// UTC 时间窗口：CST 当日 00:00 ~ 次日 00:00
	from := date.UTC()
	to := date.AddDate(0, 0, 1).UTC()

	stats, err := s.stats.AggregateDate(ctx, from, to, interval)
	if err != nil {
		return err
	}

	// 设置日期（CST 切日）
	for i := range stats {
		stats[i].StatDate = date
	}

	if len(stats) == 0 {
		log.Info().Str("date", dateStr).Msg("daily rollup: no records found for date")
		return nil
	}

	if err := s.stats.Upsert(ctx, stats); err != nil {
		return err
	}

	log.Info().Str("date", dateStr).Int("patients", len(stats)).Msg("daily rollup: upserted stats")
	return nil
}

// aggregatePatientDate 聚合指定患者+日期并重算 UPSERT
func (s *RollupService) aggregatePatientDate(ctx context.Context, date time.Time, patientID string) error {
	interval, _, cfgErr := s.configs.GetDeviceConfig(ctx)
	if cfgErr != nil {
		log.Warn().Err(cfgErr).Msg("read device config failed for backfill rollup, fallback 30min")
		interval = 30
	}

	from := date.UTC()
	to := date.AddDate(0, 0, 1).UTC()

	stats, err := s.stats.AggregateDate(ctx, from, to, interval)
	if err != nil {
		return err
	}

	// 过滤出目标患者
	var filtered []model.DailyWearStats
	for _, s := range stats {
		if s.PatientID == patientID {
			s.StatDate = date
			filtered = append(filtered, s)
		}
	}

	if len(filtered) == 0 {
		log.Debug().Str("patient_id", patientID).Str("date", date.Format("2006-01-02")).Msg("backfill rollup: no records for patient on date")
		return nil
	}

	return s.stats.Upsert(ctx, filtered)
}
