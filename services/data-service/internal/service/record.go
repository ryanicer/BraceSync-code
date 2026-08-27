// Package service data-service 业务编排层
//
// 对齐：docs/ §1.3 核心数据流 / §3.4 降级策略 /
//
//	§3.5 设备上报接口 / §4.7 Redis Key 设计
package service

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/data-service/internal/metrics"
	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// DefaultAlertTimeout data→alert 内联评估熔断超时（架构 §3.4：100ms）
const DefaultAlertTimeout = 100 * time.Millisecond

// degradeAlarmThreshold 连续降级超过该时长输出可观测告警日志（架构 §3.4：>5min）
const degradeAlarmThreshold = 5 * time.Minute

// realtimeFrame rt:frame:{device_id} 缓存的最新帧 JSON 结构
type realtimeFrame struct {
	DeviceID    string    `json:"deviceId"`
	PatientID   string    `json:"patientId"`
	Timestamp   time.Time `json:"timestamp"`
	Points      []float64 `json:"points"`
	Battery     int       `json:"battery"`
	MaxPressure float64   `json:"maxPressure"`
	MaxPoint    string    `json:"maxPoint"`
	UploadTime  time.Time `json:"uploadTime"`
}

// pendingAlertItem alert:pending 队列元素（帧引用，供 alert-service 常驻消费者补偿评估）
type pendingAlertItem struct {
	QueuedAt time.Time         `json:"queued_at"`
	Frame    *AlertEvalRequest `json:"frame"`
}

// rollupTask rollup:recompute 队列元素（补传受影响日重算，机制幂等）
type rollupTask struct {
	PatientID string `json:"patient_id"`
	Date      string `json:"date"` // YYYY-MM-DD（Asia/Shanghai）
	QueuedAt  string `json:"queued_at"`
}

// RecordService 设备上报主链路编排
type RecordService struct {
	records repo.RecordStore
	devices repo.DeviceStore
	configs repo.ConfigStore
	cache   repo.CacheStore
	alerts  AlertEvaluator
	limiter *RateLimiter

	alertTimeout time.Duration
	now          func() time.Time

	// degradedSinceUnixNano 降级窗口起点（0=正常）；连续降级 >5min 输出告警日志
	degradedSinceUnixNano int64
}

// NewRecordService 组装 RecordService
func NewRecordService(records repo.RecordStore, devices repo.DeviceStore, configs repo.ConfigStore, cache repo.CacheStore, alerts AlertEvaluator, limiter *RateLimiter) *RecordService {
	return &RecordService{
		records:      records,
		devices:      devices,
		configs:      configs,
		cache:        cache,
		alerts:       alerts,
		limiter:      limiter,
		alertTimeout: DefaultAlertTimeout,
		now:          time.Now,
	}
}

// ─────────────────────────────────────────────────────────────
// 1. 单帧实时上报 POST /api/v1/device/records
// ─────────────────────────────────────────────────────────────

// UploadSingle 单帧上报：限流 → 校验 → 幂等落库 → Redis 三写 → 内联告警（超时降级）
func (s *RecordService) UploadSingle(ctx context.Context, headerDeviceID string, req *model.SingleFrameRequest) (*model.SingleFrameResponse, *model.AppError) {
	deviceID, appErr := resolveDeviceID(headerDeviceID, req.DeviceID)
	if appErr != nil {
		return nil, appErr
	}
	if appErr := checkRate(s.limiter.Realtime, deviceID); appErr != nil {
		return nil, appErr
	}
	if len(req.Points) != model.PointCount {
		return nil, model.ErrInvalidParam("points length %d != %d", len(req.Points), model.PointCount)
	}
	ts, appErr := validateTimestamp(req.Timestamp, s.now())
	if appErr != nil {
		return nil, appErr
	}

	patientID, appErr := s.requireBinding(ctx, deviceID)
	if appErr != nil {
		return nil, appErr
	}

	frame := toPendingFrame(ts, req.Points, req.Battery, req.FaultCode)
	recordID, inserted, err := s.records.InsertRecord(ctx, deviceID, patientID, frame)
	if err != nil {
		return nil, s.mapStoreError(err)
	}

	now := s.now()
	// Redis 写入（架构 §4.7）：lastseen / rt:frame / stat:today。
	// 幂等命中（重复帧）不再增量统计，避免佩戴分钟重复累计。
	if appErr := s.applyRealtimeCache(ctx, deviceID, patientID, frame, now, inserted); appErr != nil {
		return nil, appErr
	}

	if inserted {
		s.evaluateInline(ctx, deviceID, patientID, frame, now)
	} else {
		log.Debug().Str("device_id", deviceID).Time("ts", ts).Msg("duplicate frame skipped (idempotent)")
	}

	interval, version, err := s.configs.GetDeviceConfig(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("read device config failed, fallback defaults")
		interval, version = 30, 1
	}
	return &model.SingleFrameResponse{
		RecordID:   strconv.FormatInt(recordID, 10),
		Duplicated: !inserted,
		Config:     model.DeviceConfig{IntervalMinutes: interval, ConfigVersion: version},
	}, nil
}

// applyRealtimeCache 单帧上报后的 Redis 三写
func (s *RecordService) applyRealtimeCache(ctx context.Context, deviceID, patientID string, frame repo.PendingFrame, now time.Time, countStat bool) *model.AppError {
	if err := s.cache.SetLastSeen(ctx, deviceID, frame.Ts); err != nil {
		log.Error().Err(err).Str("device_id", deviceID).Msg("redis set lastseen failed")
		return model.ErrInternal("redis unavailable")
	}

	maxP, maxIdx := maxPoint(frame.Points)
	rtJSON, err := json.Marshal(&realtimeFrame{
		DeviceID:    deviceID,
		PatientID:   patientID,
		Timestamp:   frame.Ts.UTC(),
		Points:      pointsToFloat64(frame.Points),
		Battery:     frame.Battery,
		MaxPressure: maxP,
		MaxPoint:    model.PointID(maxIdx),
		UploadTime:  now.UTC(),
	})
	if err != nil {
		return model.ErrInternal("marshal realtime frame: %v", err)
	}
	if err := s.cache.SetRealtimeFrame(ctx, deviceID, string(rtJSON)); err != nil {
		log.Error().Err(err).Str("device_id", deviceID).Msg("redis set rt:frame failed")
		return model.ErrInternal("redis unavailable")
	}

	if countStat {
		wearMinutes := 0
		if maxP > model.WearingThresholdN { // PRD §8.1 佩戴帧判定
			interval, _, cfgErr := s.configs.GetDeviceConfig(ctx)
			if cfgErr != nil {
				interval = 30
			}
			wearMinutes = interval
		}
		if err := s.cache.ApplyStatToday(ctx, patientID, wearMinutes, maxP, model.PointID(maxIdx), 0, endOfTodayCST(now)); err != nil {
			log.Error().Err(err).Str("patient_id", patientID).Msg("redis update stat:today failed")
			return model.ErrInternal("redis unavailable")
		}
	}
	return nil
}

// evaluateInline 内联告警评估（100ms 熔断 → alert:pending 降级，架构 §3.4）
func (s *RecordService) evaluateInline(ctx context.Context, deviceID, patientID string, frame repo.PendingFrame, uploadTime time.Time) {
	evalReq := &AlertEvalRequest{
		DeviceID:   deviceID,
		PatientID:  patientID,
		Timestamp:  frame.Ts.UTC(),
		Points:     pointsToFloat64(frame.Points),
		UploadTime: uploadTime.UTC(),
		IsBackfill: false,
	}

	evalCtx, cancel := context.WithTimeout(ctx, s.alertTimeout)
	defer cancel()
	result, err := s.alerts.Evaluate(evalCtx, evalReq)
	if err != nil {
		s.degradeToPending(ctx, evalReq, err)
		return
	}
	s.markRecovered()

	if result.ShouldAlert {
		// 命中告警：今日异常数 +1（stat:today 增量，monitor/摘要卡展示）
		if incErr := s.cache.ApplyStatToday(ctx, patientID, 0, 0, "", 1, endOfTodayCST(s.now())); incErr != nil {
			log.Warn().Err(incErr).Str("patient_id", patientID).Msg("incr abnormal_count failed")
		}
		log.Info().Str("device_id", deviceID).Str("alert_type", result.AlertType).Msg("inline alert hit")
	}
}

// degradeToPending 降级：帧引用写 Redis alert:pending，不阻塞上报成功返回
func (s *RecordService) degradeToPending(ctx context.Context, evalReq *AlertEvalRequest, cause error) {
	payload, err := json.Marshal(&pendingAlertItem{QueuedAt: s.now().UTC(), Frame: evalReq})
	if err != nil {
		log.Error().Err(err).Msg("marshal pending alert item failed")
		return
	}
	// 兜底 ctx：上报请求 ctx 可能临近超时，队列写入用独立短超时
	pushCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := s.cache.PushAlertPending(pushCtx, string(payload)); err != nil {
		// Redis 也写不进去：帧明细已落 PG，仅告警评估缺失（已知取舍，架构 §4.1）
		log.Error().Err(err).Str("device_id", evalReq.DeviceID).Msg("push alert:pending failed, frame alert may be lost")
		return
	}

	now := s.now()
	metrics.AlertDegradeTotal.Inc()
	if s.degradedSinceUnixNano == 0 {
		s.degradedSinceUnixNano = now.UnixNano()
		log.Warn().Err(cause).Str("device_id", evalReq.DeviceID).Msg("alert-service unavailable, degraded to alert:pending")
	} else if dur := now.Sub(time.Unix(0, s.degradedSinceUnixNano)); dur >= degradeAlarmThreshold {
		// 连续降级 >5min 的可观测信号（架构 §3.4；一期以结构化日志承载，监控按 degrade_duration_s 采集）
		log.Error().Err(cause).Dur("degrade_duration", dur).Msg("alert-service degraded >5min, ops attention required")
	}
	metrics.AlertDegradedSeconds.Set(now.Sub(time.Unix(0, s.degradedSinceUnixNano)).Seconds())
}

// markRecovered 降级恢复打点
func (s *RecordService) markRecovered() {
	metrics.AlertDegradedSeconds.Set(0)
	if s.degradedSinceUnixNano != 0 {
		dur := s.now().Sub(time.Unix(0, s.degradedSinceUnixNano))
		s.degradedSinceUnixNano = 0
		log.Info().Dur("degrade_duration", dur).Msg("alert-service recovered, degraded window closed")
	}
}

// ─────────────────────────────────────────────────────────────
// 2. 批量补传 POST /api/v1/device/records/batch
// ─────────────────────────────────────────────────────────────

// UploadBatch 批量补传：独立限流 → 逐帧校验 → 单事务幂等批量落库 →
// 跳过实时告警 → 受影响日期投递 rollup 重算
func (s *RecordService) UploadBatch(ctx context.Context, headerDeviceID string, req *model.BatchRequest) (*model.BatchResponse, *model.AppError) {
	deviceID, appErr := resolveDeviceID(headerDeviceID, req.DeviceID)
	if appErr != nil {
		return nil, appErr
	}
	if appErr := checkRate(s.limiter.Batch, deviceID); appErr != nil {
		return nil, appErr
	}
	if len(req.Frames) == 0 {
		return nil, model.ErrInvalidParam("frames is empty")
	}
	if len(req.Frames) > model.MaxBatchFrames {
		return nil, model.ErrInvalidParam("frames count %d exceeds %d", len(req.Frames), model.MaxBatchFrames)
	}

	patientID, appErr := s.requireBinding(ctx, deviceID)
	if appErr != nil {
		return nil, appErr
	}

	now := s.now()
	valid := make([]repo.PendingFrame, 0, len(req.Frames))
	rejected := make([]model.RejectedFrame, 0)
	for i, f := range req.Frames {
		if len(f.Points) != model.PointCount {
			rejected = append(rejected, model.RejectedFrame{Index: i, Code: model.CodeInvalidParam, Reason: "points length != 20"})
			continue
		}
		ts, tsErr := validateTimestamp(f.Timestamp, now)
		if tsErr != nil {
			rejected = append(rejected, model.RejectedFrame{Index: i, Code: tsErr.Code, Reason: tsErr.Message})
			continue
		}
		if now.Sub(ts) > model.BackfillMaxAge {
			// 补传时间窗上限 7 天（设备缓存容量口径，协议 §6 / test-plan A10）
			rejected = append(rejected, model.RejectedFrame{Index: i, Code: model.CodeBadTimestamp, Reason: "backfill frame older than 7 days"})
			continue
		}
		valid = append(valid, toPendingFrame(ts, f.Points, f.Battery, f.FaultCode))
	}

	acceptedTS, err := s.records.BatchInsert(ctx, deviceID, patientID, valid)
	if err != nil {
		return nil, s.mapStoreError(err)
	}
	// 补传不更新 rt:frame/stat:today（历史数据）；lastseen 置为当前时刻：设备刚恢复联网
	if err := s.cache.SetLastSeen(ctx, deviceID, now); err != nil {
		log.Error().Err(err).Str("device_id", deviceID).Msg("redis set lastseen failed (batch)")
		return nil, model.ErrInternal("redis unavailable")
	}

	s.enqueueRollup(ctx, patientID, acceptedTS)

	interval, version, cfgErr := s.configs.GetDeviceConfig(ctx)
	if cfgErr != nil {
		log.Warn().Err(cfgErr).Msg("read device config failed, fallback defaults")
		interval, version = 30, 1
	}
	return &model.BatchResponse{
		Accepted:   len(acceptedTS),
		Duplicated: len(valid) - len(acceptedTS), // ON CONFLICT DO NOTHING 幂等去重
		Rejected:   rejected,
		Config:     model.DeviceConfig{IntervalMinutes: interval, ConfigVersion: version},
	}, nil
}

// enqueueRollup 收集受影响日期（Asia/Shanghai 切日）→ 幂等投递 rollup 重算任务
func (s *RecordService) enqueueRollup(ctx context.Context, patientID string, acceptedTS []time.Time) {
	seen := make(map[string]struct{})
	for _, ts := range acceptedTS {
		date := ts.In(model.CSTZone()).Format("2006-01-02")
		if _, ok := seen[date]; ok {
			continue
		}
		seen[date] = struct{}{}

		payload, err := json.Marshal(&rollupTask{PatientID: patientID, Date: date, QueuedAt: s.now().UTC().Format(time.RFC3339)})
		if err != nil {
			continue
		}
		queued, qErr := s.cache.EnqueueRollup(ctx, patientID, date, string(payload))
		if qErr != nil {
			log.Warn().Err(qErr).Str("date", date).Msg("enqueue rollup task failed")
			continue
		}
		if queued {
			log.Info().Str("patient_id", patientID).Str("date", date).Msg("rollup recompute enqueued (backfill)")
		}
	}
}

// ─────────────────────────────────────────────────────────────
// 3/4. 历史查询 + 实时快照
// ─────────────────────────────────────────────────────────────

// GetHistory 压力历史查询（period+date 决定时间范围，Asia/Shanghai 切日，分页）
func (s *RecordService) GetHistory(ctx context.Context, patientID, period, date string, page, pageSize int) (*model.HistoryPage, *model.AppError) {
	from, to, appErr := periodRange(period, date)
	if appErr != nil {
		return nil, appErr
	}
	records, total, err := s.records.QueryHistory(ctx, patientID, from, to, page, pageSize)
	if err != nil {
		return nil, model.ErrInternal("query history: %v", err)
	}
	list := make([]model.PressureRecordDTO, 0, len(records))
	for i := range records {
		list = append(list, records[i].ToDTO())
	}
	return &model.HistoryPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetRealtime 实时快照：读 Redis（lastseen / rt:frame / stat:today），零 DB 明细命中
func (s *RecordService) GetRealtime(ctx context.Context, patientID string) (*model.RealtimeSnapshot, *model.AppError) {
	snapshot := &model.RealtimeSnapshot{
		Status:          "offline",
		MaxPoint:        "",
		PressureRecords: []model.PressureRecordDTO{},
		Alerts:          []any{}, // 今日告警明细由 alert-service 提供；此处仅 Redis 摘要
	}

	deviceID, dbStatus, exists, err := s.devices.GetDeviceByPatient(ctx, patientID)
	if err != nil {
		return nil, model.ErrInternal("lookup device: %v", err)
	}
	if !exists {
		snapshot.PressureHeatmap = model.SeedHeatmap(patientID)
		return snapshot, nil // 未绑定设备：返回空快照，heatmap 走 seed
	}

	// 状态推导（架构 §4.6 查询时实时推导）：abnormal 优先，其次 lastseen ≤2h 判 online
	if dbStatus == "abnormal" {
		snapshot.Status = "abnormal"
	} else if lastseen, ok, lsErr := s.cache.GetLastSeen(ctx, deviceID); lsErr == nil && ok && s.now().Sub(lastseen) <= 2*time.Hour {
		snapshot.Status = "online"
	}

	// 最新帧（rt:frame，零 DB）→ 同时产出 PressureRecords 与热力图 20 点
	frameJSON, err := s.cache.GetRealtimeFrame(ctx, deviceID)
	if err != nil {
		return nil, model.ErrInternal("read rt:frame: %v", err)
	}
	var hmPoints [model.PointCount]float32
	heatmapReady := false
	if frameJSON != "" {
		var rf realtimeFrame
		if jsonErr := json.Unmarshal([]byte(frameJSON), &rf); jsonErr == nil {
			if len(rf.Points) >= model.PointCount {
				allDead := true
				for i := 0; i < model.PointCount; i++ {
					v := float32(rf.Points[i])
					hmPoints[i] = v
					if v >= model.WearingThresholdN {
						allDead = false
					}
				}
				if !allDead {
					heatmapReady = true
				}
			}
			// PressureRecords 总是构建（即便 points 短，PressureRecord 逻辑保留原有对不齐能力）
			var points [model.PointCount]float32
			for i := 0; i < model.PointCount && i < len(rf.Points); i++ {
				points[i] = float32(rf.Points[i])
			}
			snapshot.PressureRecords = append(snapshot.PressureRecords, model.PressureRecordDTO{
				DeviceID:   rf.DeviceID,
				PatientID:  rf.PatientID,
				Timestamp:  rf.Timestamp.UTC().Format(time.RFC3339),
				Points:     model.BuildSensorPoints(points),
				UploadTime: rf.UploadTime.UTC().Format(time.RFC3339),
			})
		} else {
			log.Warn().Err(jsonErr).Str("device_id", deviceID).Msg("invalid rt:frame json, heatmap fall back to seed")
		}
	}
	if heatmapReady {
		snapshot.PressureHeatmap = model.BuildHeatmap(hmPoints)
	} else {
		snapshot.PressureHeatmap = model.SeedHeatmap(patientID)
	}

	// 今日统计（stat:today）
	stats, err := s.cache.GetStatToday(ctx, patientID)
	if err != nil {
		return nil, model.ErrInternal("read stat:today: %v", err)
	}
	if v, convErr := strconv.Atoi(stats["wear_minutes"]); convErr == nil {
		snapshot.TodayHours = float64(v) / 60.0
	}
	if v, convErr := strconv.ParseFloat(stats["max_pressure"], 64); convErr == nil {
		snapshot.MaxPressure = v
	}
	snapshot.MaxPoint = stats["max_point"]
	if v, convErr := strconv.Atoi(stats["abnormal_count"]); convErr == nil {
		snapshot.Events = v
	}
	return snapshot, nil
}

// ─────────────────────────────────────────────────────────────
// 公共辅助
// ─────────────────────────────────────────────────────────────

// resolveDeviceID 身份解析：gateway 注入的 X-Device-Id 优先（验签归 gateway）；
// 头与体同时存在且不一致 → 20403；均缺失 → 20400。
// gateway 设备验签未上线前允许仅用 body device_id 联调（缺口已在自报标注）。
func resolveDeviceID(headerID, bodyID string) (string, *model.AppError) {
	if headerID != "" {
		if bodyID != "" && bodyID != headerID {
			return "", model.ErrDeviceIDMismatch()
		}
		return headerID, nil
	}
	if bodyID == "" {
		return "", model.ErrInvalidParam("device_id missing (no X-Device-Id header and empty body device_id)")
	}
	return bodyID, nil
}

// checkRate 通道限流检查
func checkRate(ch *Channel, deviceID string) *model.AppError {
	if !ch.Allow(deviceID) {
		return model.ErrRateLimited(ch.RetryAfterSec())
	}
	return nil
}

// validateTimestamp 协议 §4.1：timestamp 须落在 [2026-01-01, now+1h]
func validateTimestamp(unixSec int64, now time.Time) (time.Time, *model.AppError) {
	if unixSec <= 0 {
		return time.Time{}, model.ErrBadTimestamp("timestamp missing")
	}
	ts := time.Unix(unixSec, 0).UTC()
	if ts.Before(model.MinValidTime) || ts.After(now.Add(model.MaxFutureDrift)) {
		return time.Time{}, model.ErrBadTimestamp("timestamp %d outside valid range", unixSec)
	}
	return ts, nil
}

// requireBinding 校验设备已注册且已绑定患者（协议错误码 20404 / 20409）
func (s *RecordService) requireBinding(ctx context.Context, deviceID string) (string, *model.AppError) {
	patientID, _, exists, err := s.devices.GetBinding(ctx, deviceID)
	if err != nil {
		return "", model.ErrInternal("lookup device binding: %v", err)
	}
	if !exists {
		return "", model.ErrDeviceNotFound(deviceID)
	}
	if patientID == "" {
		return "", model.ErrDeviceUnbound(deviceID)
	}
	return patientID, nil
}

// mapStoreError 存储错误 → 业务错误（分区缺失映射 20402，其余 90001）
func (s *RecordService) mapStoreError(err error) *model.AppError {
	if repo.IsNoPartitionError(err) {
		return model.ErrBadTimestamp("timestamp outside existing partitions")
	}
	log.Error().Err(err).Msg("record store error")
	return model.ErrInternal("record store error")
}

// periodRange period+date → UTC 时间范围（Asia/Shanghai 切日）
func periodRange(period, date string) (from, to time.Time, appErr *model.AppError) {
	day, err := time.ParseInLocation("2006-01-02", date, model.CSTZone())
	if err != nil {
		return time.Time{}, time.Time{}, model.ErrQueryParam("invalid date %q, expect YYYY-MM-DD", date)
	}
	switch period {
	case "day":
		from, to = day, day.AddDate(0, 0, 1)
	case "week":
		// ISO 周：周一为起点
		from = day.AddDate(0, 0, -int((day.Weekday()+6)%7))
		to = from.AddDate(0, 0, 7)
	case "month":
		from = time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, model.CSTZone())
		to = from.AddDate(0, 1, 0)
	default:
		return time.Time{}, time.Time{}, model.ErrQueryParam("invalid period %q, expect day|week|month", period)
	}
	return from.UTC(), to.UTC(), nil
}

// endOfTodayCST 今日 24:00（Asia/Shanghai），用于 stat:today 过期
func endOfTodayCST(now time.Time) time.Time {
	local := now.In(model.CSTZone())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, model.CSTZone()).AddDate(0, 0, 1)
}

// maxPoint 20 点最大值与下标（并列取首个）
func maxPoint(points [model.PointCount]float32) (float64, int) {
	idx := 0
	for i := 1; i < model.PointCount; i++ {
		if points[i] > points[idx] {
			idx = i
		}
	}
	return float64(points[idx]), idx
}

// pointsToFloat64 [20]float32 → []float64（JSON 传输）
func pointsToFloat64(points [model.PointCount]float32) []float64 {
	out := make([]float64, model.PointCount)
	for i, v := range points {
		out[i] = float64(v)
	}
	return out
}

// toPendingFrame 请求参数 → 待落库帧
func toPendingFrame(ts time.Time, points []float64, battery, faultCode int) repo.PendingFrame {
	var arr [model.PointCount]float32
	for i := 0; i < model.PointCount && i < len(points); i++ {
		arr[i] = float32(points[i])
	}
	return repo.PendingFrame{Ts: ts, Points: arr, Battery: battery, FaultCode: faultCode}
}
