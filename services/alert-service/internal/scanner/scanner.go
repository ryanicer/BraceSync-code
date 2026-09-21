// Package scanner — 定时扫描器（T008 佩戴中断；T257 2.6 增每日佩戴时长不足）
//
// 对齐：架构 §1.3 兜底链路 / §3.6 设备状态扫描 / §4.3 恢复机制 / §4.6 状态机混合计算；
// PRD §8.1 设备状态机（abnormal > offline）；test-plan §3.1 A5/A7/A8。
//
// 职责：
//  1. Scan（每 5min）：扫描绑定设备的 Redis lastseen，超中断阈值生成 wear_interrupt 告警（去重窗口 = 1×阈值）；
//  2. Scan（同轮）：设备恢复上报（lastseen 新鲜，含补传——data-service 补传同样刷新 lastseen）时，
//     自动将该设备 active 的佩戴中断告警置 resolved + resolved_at；
//  3. Scan（同轮）：按 PRD §8.1 状态机推导 devices.status 并落库（仅变更时写）；
//  4. ScanDailyWear（每日一次，独立调度）：上一完整自然日累计佩戴 < wear_target_hours
//     → 生成 wear_duration_short 告警（ts 固定 = 该日 23:59:59，靠自然唯一键每日一发）。
//
// 判定逻辑复用 internal/engine（中断：EvaluateWearInterrupt；时长：EvaluateWearDurationShort），
// 扫描器只做触发源与落库。
package scanner

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

// devices.status 状态机取值（PRD §8.1）
const (
	StatusOnline   = "online"
	StatusOffline  = "offline"
	StatusAbnormal = "abnormal"
)

// Device 绑定设备快照（扫描输入，来自 devices 表 patient_id IS NOT NULL）
type Device struct {
	DeviceID  string
	PatientID string
	Status    string // 当前 devices.status
}

// NewAlert 告警落库请求（alerts 表）
type NewAlert struct {
	AlertID        string // 落库后回填（BIGINT 字符串化），Notify 时使用
	PatientID      string
	DeviceID       string
	Type           engine.AlertType
	SensorPoint    string // 触发告警的采集点（P01–P20），仅压力偏高/漂移时有值；按日类告警（离线/时长不足）为空
	Detail         string
	ThresholdValue float64
	ActualValue    float64
	Ts             time.Time
}

// DeviceStore 设备仓储契约（repo 层实现）
type DeviceStore interface {
	// ListBoundDevices 全部已绑定患者的设备
	ListBoundDevices(ctx context.Context) ([]Device, error)
	// UpdateStatus 状态落库；状态未变化时不写库，返回 changed=false
	UpdateStatus(ctx context.Context, deviceID, status string) (changed bool, err error)
}

// AlertStore 告警仓储契约（repo 层实现）
type AlertStore interface {
	// CreateAlert 落库告警；自然唯一约束冲突时返回 created=false（DB 层保底去重）。
	// alertID 为落库后的主键（BIGINT 字符串化），去重时为空。
	CreateAlert(ctx context.Context, alert NewAlert) (alertID string, created bool, err error)
	// HasAlertSince 同设备同类型告警在 since 之后是否已存在（去重窗口判定）
	HasAlertSince(ctx context.Context, deviceID string, alertType engine.AlertType, since time.Time) (bool, error)
	// HasActiveInterrupt 是否存在未 resolve 的佩戴中断告警
	HasActiveInterrupt(ctx context.Context, deviceID string) (bool, error)
	// ResolveActiveInterrupts 将 active 的佩戴中断告警置 resolved + resolved_at，返回影响行数
	ResolveActiveInterrupts(ctx context.Context, deviceID string, resolvedAt time.Time) (int64, error)
}

// LastSeenReader Redis dev:lastseen:{device_id} 读取契约（data-service 写入，本服务只读）
type LastSeenReader interface {
	// GetLastSeen 无记录返回 ok=false
	GetLastSeen(ctx context.Context, deviceID string) (time.Time, bool, error)
}

// WearStore 每日佩戴时长 rollup 读取契约（T257 2.6 新增；daily_wear_stats，data-service 写、本服务只读）。
// 与 msg-service 佩戴提醒读同一张 rollup 表，不另算一套口径（架构 §5 禁扫明细）。
type WearStore interface {
	// DailyWearMinutes 业务日累计佩戴分钟数；无 rollup 行返回 0（=整日无有效佩戴，与 msg-service 同判定）
	DailyWearMinutes(ctx context.Context, patientID string, bizDate time.Time) (int, error)
}

// DailyReport 单日佩戴时长扫描结果统计
type DailyReport struct {
	Patients     int // 判定的患者数（按绑定设备去重后）
	AlertCreated int // 新生成「佩戴时长不足」告警数
	Deduped      int // 当日已有同类告警 / 唯一约束抑制数
	AboveTarget  int // 达标跳过数
	WearErrors   int // 单患者 rollup 读失败数（不中断整轮）
}

// Report 单轮扫描结果统计
type Report struct {
	Scanned        int   // 扫描的绑定设备数
	MissedLastSeen int   // 无 lastseen 记录跳过数（无判定依据）
	RedisErrors    int   // 单设备 lastseen 读失败数（不中断整轮）
	AlertCreated   int   // 新生成告警数
	Deduped        int   // 去重窗口/active 告警/唯一约束抑制数
	Resolved       int64 // 恢复上报自动 resolve 的告警数
	StatusChanged  int   // devices.status 实际变更数
}

// Scanner 佩戴中断定时扫描器
type Scanner struct {
	devices  DeviceStore
	alerts   AlertStore
	lastseen LastSeenReader
	wear     WearStore // T257 2.6：可选（SetWearStore 注入）；nil 时跳过每日佩戴时长扫描
	eval     *engine.RuleEvaluator
	now      func() time.Time
	log      zerolog.Logger
}

// New 组装扫描器；eval 提供阈值口径（中断分钟数 / 采集间隔）与判定逻辑
func New(devices DeviceStore, alerts AlertStore, lastseen LastSeenReader, eval *engine.RuleEvaluator) *Scanner {
	return &Scanner{
		devices:  devices,
		alerts:   alerts,
		lastseen: lastseen,
		eval:     eval,
		now:      time.Now,
		log:      zerolog.Nop(),
	}
}

// SetLogger 注入日志器（生产使用；默认 Nop）
func (s *Scanner) SetLogger(l zerolog.Logger) { s.log = l }

// SetWearStore 注入每日佩戴时长来源（T257 2.6）。未注入 ⇒ ScanDailyWear 直接空转返回，
// 保持 scanner.New 四参数签名不变（冻结契约 engine_test.go 直接调用该构造函数）。
func (s *Scanner) SetWearStore(w WearStore) { s.wear = w }

// bizDayOf 给定时刻 → 所属业务日零点（loc 由调用方给 Asia/Shanghai，架构 §3.5）
func bizDayOf(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// PreviousBizDay 上一个**完整**自然日零点（T257 2.6 判定对象）。
// 不用「今天到此刻」：那样每天阈值时刻之前人人不达标，等于天天误报。
func PreviousBizDay(now time.Time, loc *time.Location) time.Time {
	return bizDayOf(now, loc).AddDate(0, 0, -1)
}

// ScanDailyWear 每日「佩戴时长不足」扫描（T257 2.6 新增第四类告警）。
//
// 判定对象 = targetDay 所在业务日（调用方传 PreviousBizDay）；阈值 targetHours 来自
// sys_configs wear_target_hours（与 §7D.12 系统配置页同一键，PM 裁定不另设）。
//
// 幂等（每日一发）：告警 ts 固定写该业务日 23:59:59（业务时区），
// 直接命中 alerts 自然唯一键 uk_alerts_natural(patient_id, device_id, type, ts)
// ⇒ 同一业务日重复执行不产生第二条，无需新去重表；执行时刻由调度器决定（00:30 + 启动补扫）。
func (s *Scanner) ScanDailyWear(ctx context.Context, targetDay time.Time, targetHours float64) (DailyReport, error) {
	var report DailyReport
	if s.wear == nil {
		return report, nil // 未注入 rollup 来源：规则不启用（不误报）
	}
	if targetHours <= 0 {
		return report, nil // 阈值零值 = 规则未启用（与引擎其他规则同口径），整轮不查库
	}
	devices, err := s.devices.ListBoundDevices(ctx)
	if err != nil {
		return report, err
	}
	// 一名患者可绑多设备，但佩戴时长是**患者级**指标 ⇒ 按患者去重，设备取字典序最小者
	// （固定选取，保证 ts 相同的重复扫描命中同一唯一键）。
	patientDevice := make(map[string]string, len(devices))
	for _, dev := range devices {
		if dev.PatientID == "" {
			continue
		}
		if cur, ok := patientDevice[dev.PatientID]; !ok || dev.DeviceID < cur {
			patientDevice[dev.PatientID] = dev.DeviceID
		}
	}
	report.Patients = len(patientDevice)

	loc := targetDay.Location()
	alertTs := time.Date(targetDay.Year(), targetDay.Month(), targetDay.Day(),
		23, 59, 59, 0, loc)
	bizDate := targetDay.Format("2006-01-02")

	for patientID, deviceID := range patientDevice {
		// 去重一：该业务日已有同类告警 ⇒ 不再查 rollup（多数轮次走此快路径）
		recent, err := s.alerts.HasAlertSince(ctx, deviceID, engine.TypeWearDurationShort, targetDay)
		if err != nil {
			report.WearErrors++
			s.log.Error().Err(err).Str("patient_id", patientID).Msg("daily wear: dedup query failed, skip patient")
			continue
		}
		if recent {
			report.Deduped++
			continue
		}
		minutes, err := s.wear.DailyWearMinutes(ctx, patientID, targetDay)
		if err != nil {
			report.WearErrors++
			s.log.Error().Err(err).Str("patient_id", patientID).Msg("daily wear: read rollup failed, skip patient")
			continue
		}
		result := s.eval.EvaluateWearDurationShort(patientID, targetDay, float64(minutes), targetHours)
		if result == nil {
			report.AboveTarget++
			continue
		}
		_, created, err := s.alerts.CreateAlert(ctx, NewAlert{
			PatientID:      patientID,
			DeviceID:       deviceID,
			Type:           result.AlertType,
			Detail:         result.Message,
			ThresholdValue: result.ThresholdValue,
			ActualValue:    result.ActualValue,
			Ts:             alertTs,
		})
		if err != nil {
			report.WearErrors++
			s.log.Error().Err(err).Str("patient_id", patientID).Msg("daily wear: create alert failed")
			continue
		}
		if !created {
			report.Deduped++ // 唯一约束保底（并发副本等极端场景）
			continue
		}
		report.AlertCreated++
		s.log.Warn().Str("patient_id", patientID).Str("device_id", deviceID).
			Str("biz_date", bizDate).Int("wear_minutes", minutes).
			Float64("target_hours", targetHours).
			Msg("daily wear: wear_duration_short alert created")
	}
	return report, nil
}

// DeriveStatus 按 PRD §8.1 状态机推导 devices.status（优先级 abnormal > offline）：
//   - gap ≥ 3×采集间隔（缺数 3 个周期）→ abnormal
//   - gap > 2h → offline
//   - 其余（≤2h）→ online（与 data-service 查询时推导口径一致）
//
// collectionIntervalMin 零值时 abnormal 规则不启用（offline 兜底）。
func DeriveStatus(gap time.Duration, collectionIntervalMin int) string {
	if collectionIntervalMin > 0 && gap >= 3*time.Duration(collectionIntervalMin)*time.Minute {
		return StatusAbnormal
	}
	if gap > 2*time.Hour {
		return StatusOffline
	}
	return StatusOnline
}

// Scan 执行一轮扫描（幂等：重复执行不产生重复告警/重复状态写）
func (s *Scanner) Scan(ctx context.Context) (Report, error) {
	var report Report
	now := s.now()

	devices, err := s.devices.ListBoundDevices(ctx)
	if err != nil {
		return report, err // 设备清单不可得：整轮失败，由调度层记录
	}
	report.Scanned = len(devices)

	for _, dev := range devices {
		s.scanDevice(ctx, dev, now, &report)
	}
	return report, nil
}

// scanDevice 单设备扫描：中断告警/恢复 resolve/状态联动
func (s *Scanner) scanDevice(ctx context.Context, dev Device, now time.Time, report *Report) {
	lastSeen, ok, err := s.lastseen.GetLastSeen(ctx, dev.DeviceID)
	if err != nil {
		report.RedisErrors++
		s.log.Warn().Err(err).Str("device_id", dev.DeviceID).Msg("scan: read lastseen failed, skip device")
		return
	}
	if !ok {
		report.MissedLastSeen++ // 从未上报：无判定依据，状态由查询时推导兜底
		return
	}

	if s.eval.EvaluateWearInterrupt(dev.DeviceID, lastSeen, now) == nil {
		// A8 恢复上报（含补传，二者都刷新 lastseen）→ active 中断告警自动 resolve
		s.recoverDevice(ctx, dev, now, report)
	} else {
		s.raiseInterrupt(ctx, dev, lastSeen, now, report)
	}

	s.syncStatus(ctx, dev, now.Sub(lastSeen), report)
}

// recoverDevice 设备恢复上报：resolve 其 active 佩戴中断告警（无命中则幂等空转）
func (s *Scanner) recoverDevice(ctx context.Context, dev Device, now time.Time, report *Report) {
	n, err := s.alerts.ResolveActiveInterrupts(ctx, dev.DeviceID, now)
	if err != nil {
		s.log.Error().Err(err).Str("device_id", dev.DeviceID).Msg("scan: resolve active interrupts failed")
		return
	}
	if n > 0 {
		s.log.Info().Str("device_id", dev.DeviceID).Int64("resolved", n).Msg("scan: wear interrupt resolved on recovery")
	}
	report.Resolved += n
}

// raiseInterrupt 超阈值 → 生成 wear_interrupt 告警（去重窗口 = 1×中断阈值）
func (s *Scanner) raiseInterrupt(ctx context.Context, dev Device, lastSeen, now time.Time, report *Report) {
	// 去重一：已有未 resolve 的中断告警（告警生命周期维度）
	active, err := s.alerts.HasActiveInterrupt(ctx, dev.DeviceID)
	if err != nil {
		s.log.Error().Err(err).Str("device_id", dev.DeviceID).Msg("scan: query active interrupt failed")
		return
	}
	if active {
		report.Deduped++
		return
	}
	// 去重二：1×阈值窗口内已产生过同类型告警（时间窗维度，PRD §7D.6 / test-plan A7）
	window := time.Duration(s.eval.WearInterruptMinutes) * time.Minute
	recent, err := s.alerts.HasAlertSince(ctx, dev.DeviceID, engine.TypeWearInterrupt, now.Add(-window))
	if err != nil {
		s.log.Error().Err(err).Str("device_id", dev.DeviceID).Msg("scan: query dedup window failed")
		return
	}
	if recent {
		report.Deduped++
		return
	}

	result := s.eval.EvaluateWearInterrupt(dev.DeviceID, lastSeen, now)
	_, created, err := s.alerts.CreateAlert(ctx, NewAlert{
		PatientID:      dev.PatientID,
		DeviceID:       dev.DeviceID,
		Type:           result.AlertType,
		Detail:         result.Message,
		ThresholdValue: result.ThresholdValue,
		ActualValue:    result.ActualValue,
		Ts:             now,
	})
	if err != nil {
		s.log.Error().Err(err).Str("device_id", dev.DeviceID).Msg("scan: create alert failed")
		return
	}
	if !created {
		report.Deduped++ // 唯一约束保底命中（并发扫描等极端场景）
		return
	}
	report.AlertCreated++
	s.log.Warn().Str("device_id", dev.DeviceID).
		Float64("gap_minutes", result.ActualValue).
		Msg("scan: wear_interrupt alert created")
}

// syncStatus 状态机联动：推导结果与当前状态不一致时落库
func (s *Scanner) syncStatus(ctx context.Context, dev Device, gap time.Duration, report *Report) {
	want := DeriveStatus(gap, s.eval.CollectionIntervalMin)
	if want == dev.Status {
		return
	}
	changed, err := s.devices.UpdateStatus(ctx, dev.DeviceID, want)
	if err != nil {
		s.log.Error().Err(err).Str("device_id", dev.DeviceID).
			Str("from", dev.Status).Str("to", want).Msg("scan: update device status failed")
		return
	}
	if changed {
		report.StatusChanged++
		s.log.Info().Str("device_id", dev.DeviceID).
			Str("from", dev.Status).Str("to", want).Msg("scan: device status migrated")
	}
}
