// Package service msg-service 业务层
//
// notify.go — 告警通知路由 / 订阅额度 / 佩戴提醒 / 发送记录与重试（T017 验收标准 1-4）。
//
// 关键语义（Winner review + Joe 整改后定稿，见 docs/
//  1. 告警通知路由：alert_notify_rules 按 type 路由 channels/targets，未知 type 不发送；
//  2. 订阅额度：grant 幂等（Idempotency-Key）；扣减在实际发送成功时内部执行（无对外扣减接口）；
//     额度耗尽 → accepted=true + degraded=true（降级短信）；accepted=false 仅限服务异常/SMS 不可用；
//     佩戴提醒不降级短信（成本控制，架构 §2.5）；
//  3. 佩戴提醒：reminder_enabled=false 不推送；到点且今日未达标推送；
//  4. 失败重试：记录落库 + 重试队列，成功置 sent+sentAt（不丢通知，对齐 T010 降级模式）。
package service

import (
	"context"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/msg-service/internal/repo"
)

// 重试策略（对齐 T010 降级/补偿模式：指数退避 + 上限放弃，不丢通知）
const (
	// MaxRetries 单条记录最大重试次数，超过后队列项置 failed（记录保留可追溯）
	MaxRetries = 5
	// RetryBaseDelay 首次重试延迟
	RetryBaseDelay = 5 * time.Minute
	// RetryMaxDelay 重试延迟上限
	RetryMaxDelay = 2 * time.Hour
	// RetryBatchSize 单轮排空批量上限
	RetryBatchSize = 100
)

// RetryDelay 第 n 次（1 起）重试的退避延迟：5min × 2^(n-1)，上限 2h
func RetryDelay(n int) time.Duration {
	d := RetryBaseDelay
	for i := 1; i < n && d < RetryMaxDelay; i++ {
		d *= 2
	}
	if d > RetryMaxDelay {
		d = RetryMaxDelay
	}
	return d
}

// WearReminderContent 佩戴提醒推送文案（一期固定模板，模板 ID 配置就绪后接微信模板）
const WearReminderContent = "今日佩戴时长尚未达标，请记得佩戴矫形支具哦"

// NotifyService 消息业务服务
type NotifyService struct {
	store  repo.Store
	wechat WechatSender
	sms    SMSSender

	loc               *time.Location   // 业务时区（Asia/Shanghai，架构 §3.5 切日口径）
	wearTargetMinutes int              // 每日佩戴目标（默认 22h，sys_configs wear_target_hours）
	now               func() time.Time // 可注入时钟（测试）
	log               zerolog.Logger
}

// NewNotifyService 组装 NotifyService；wechat/sms 传 nil 表示该通道不可用（一期 mock 由 main 注入）
func NewNotifyService(store repo.Store, wechat WechatSender, sms SMSSender, log zerolog.Logger) *NotifyService {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil || loc == nil {
		loc = time.FixedZone("CST", 8*3600) // tzdata 缺失兜底（distroless 镜像）
	}
	return &NotifyService{
		store:             store,
		wechat:            wechat,
		sms:               sms,
		loc:               loc,
		wearTargetMinutes: 22 * 60,
		now:               time.Now,
		log:               log,
	}
}

// SetNow 注入时钟（测试用）
func (s *NotifyService) SetNow(f func() time.Time) { s.now = f }

// SetWearTargetMinutes 设置每日佩戴目标分钟数（默认 22h）
func (s *NotifyService) SetWearTargetMinutes(m int) {
	if m > 0 {
		s.wearTargetMinutes = m
	}
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────
// 1. 告警通知路由（验收 1）
// ─────────────────────────────────────────────────────────────

// RouteNotify 按告警类型查路由规则；未知 type / 未配置规则返回 nil（不发送，无错误）
func (s *NotifyService) RouteNotify(ctx context.Context, alertType model.AlertType) (*model.NotifyRule, error) {
	if !model.ValidAlertType(alertType) {
		return nil, nil
	}
	rule, err := s.store.FindRuleByType(ctx, alertType)
	if err != nil {
		return nil, model.ErrInternal("route notify: %v", err)
	}
	return rule, nil
}

// SendAlert 告警通知受理（契约 sendAlertNotification，内部接口 POST /internal/msg/send）：
//
//	同步受理 + 记录落库，实际推送异步 goroutine 执行，失败进重试队列（不阻塞 alert-service 主链路）。
//	额度耗尽 → 降级短信（accepted=true + degraded=true）；SMS 通道不可用 → accepted=false。
func (s *NotifyService) SendAlert(ctx context.Context, req model.AlertNotifyRequest) (*model.SendResult, error) {
	rule, err := s.RouteNotify(ctx, req.Type)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		// 未知 type 不发送（验收 1）：受理层静默拒绝，不产生记录
		s.log.Warn().Str("type", string(req.Type)).Msg("send alert rejected: no notify rule for type")
		return &model.SendResult{Accepted: false}, nil
	}

	// 渠道选择：微信优先（消耗订阅额度）；额度耗尽降级短信（仅告警类，架构 §2.5）
	channel := ""
	degraded := false
	reason := ""
	wechatRouted := contains(rule.Channels, model.ChannelWechat)
	smsRouted := contains(rule.Channels, model.ChannelSMS)

	switch {
	case wechatRouted:
		quota, err := s.store.GetQuota(ctx, req.PatientID)
		if err != nil {
			return nil, model.ErrInternal("send alert: get quota: %v", err)
		}
		if quota.Remaining > 0 && s.wechat != nil {
			channel = model.ChannelWechat
		} else if s.sms != nil {
			// 额度耗尽（或微信通道未配置）→ 降级短信（review 修改项 #3：accepted=true + degraded=true）
			channel = model.ChannelSMS
			degraded = true
			reason = model.DegradedReasonQuotaExhausted
		}
	case smsRouted:
		if s.sms != nil {
			channel = model.ChannelSMS
		}
	}
	if channel == "" {
		// 服务异常 / SMS 通道不可用 → accepted=false（区别于额度耗尽的降级）
		s.log.Error().Str("patient_id", req.PatientID).Str("type", string(req.Type)).
			Msg("send alert rejected: no available channel (quota exhausted and sms unavailable)")
		return &model.SendResult{Accepted: false}, nil
	}

	// 记录落库（audit/可追溯，验收 4）；初始 pending，异步推送成功后置 sent/degraded
	rec := &model.NotificationRecord{
		PatientID: req.PatientID,
		AlertType: &req.Type,
		Kind:      model.KindAlert,
		Channel:   channel,
		Status:    model.StatusPending,
		Content:   req.Detail,
	}
	if req.AlertID != "" {
		rec.AlertID = &req.AlertID
	}
	if err := s.store.CreateNotificationRecord(ctx, rec); err != nil {
		return nil, model.ErrInternal("send alert: create record: %v", err)
	}

	// 异步推送：失败进本地重试队列，不阻塞调用方（架构 §1.3/§3.4）
	successStatus := model.StatusSent
	if degraded {
		successStatus = model.StatusDegraded
	}
	go s.deliver(*rec, successStatus)

	return &model.SendResult{
		Accepted:       true,
		Degraded:       degraded,
		DegradedReason: reason,
		RecordID:       strconv.FormatInt(rec.RecordID, 10),
	}, nil
}

// deliver 实际推送（受理 goroutine / 重试 worker 共用）：
// 成功 → 状态置 successStatus + sentAt，微信渠道内部扣减额度（发送时扣减，验收 2）；
// 失败 → 状态置 failed + 进重试队列（不丢通知，验收 4）。
func (s *NotifyService) deliver(rec model.NotificationRecord, successStatus string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	if rec.Channel == model.ChannelWechat {
		if s.wechat == nil {
			err = model.ErrInternal("wechat channel unavailable")
		} else {
			err = s.wechat.SendSubscribe(ctx, rec.PatientID, rec.Content)
		}
	} else {
		if s.sms == nil {
			err = model.ErrInternal("sms channel unavailable")
		} else {
			err = s.sms.SendSMS(ctx, rec.PatientID, rec.Content)
		}
	}

	now := s.now()
	if err == nil {
		if updErr := s.store.UpdateNotificationStatus(ctx, rec.RecordID, successStatus, &now); updErr != nil {
			s.log.Error().Err(updErr).Int64("record_id", rec.RecordID).Msg("deliver: update status failed")
		}
		// 额度扣减发生在实际发送成功时（内部行为，不暴露对外扣减接口，架构 §2.5）
		if rec.Channel == model.ChannelWechat {
			if _, cErr := s.store.ConsumeQuota(ctx, rec.PatientID); cErr != nil {
				s.log.Error().Err(cErr).Str("patient_id", rec.PatientID).Msg("deliver: consume quota failed")
			}
		}
		return
	}

	s.log.Warn().Err(err).Int64("record_id", rec.RecordID).Msg("deliver failed, enqueue retry")
	if updErr := s.store.UpdateNotificationStatus(ctx, rec.RecordID, model.StatusFailed, nil); updErr != nil {
		s.log.Error().Err(updErr).Int64("record_id", rec.RecordID).Msg("deliver: mark failed failed")
	}
	if qErr := s.store.EnqueueRetry(ctx, rec.RecordID, s.now().Add(RetryDelay(1))); qErr != nil {
		s.log.Error().Err(qErr).Int64("record_id", rec.RecordID).Msg("deliver: enqueue retry failed")
	}
}

// DrainRetries 排空到期重试队列（常驻 worker 每轮调用，对齐 T010 A10 补偿模式）：
// 重试成功 → sent+sentAt + 队列项 done（微信渠道补扣额度）；
// 重试失败 → retry_count 递增 + 退避重排，达到 MaxRetries 放弃（队列项 failed，记录保留）。
func (s *NotifyService) DrainRetries(ctx context.Context) (int, error) {
	items, err := s.store.ListDueRetries(ctx, s.now(), RetryBatchSize)
	if err != nil {
		return 0, model.ErrInternal("drain retries: %v", err)
	}
	processed := 0
	for _, item := range items {
		rec, err := s.store.GetNotificationRecord(ctx, item.RecordID)
		if err != nil {
			s.log.Error().Err(err).Int64("record_id", item.RecordID).Msg("drain retries: record missing")
			_ = s.store.FinishRetry(ctx, item.QueueID, model.StatusFailed)
			continue
		}
		processed++

		var sendErr error
		if rec.Channel == model.ChannelWechat {
			if s.wechat == nil {
				sendErr = model.ErrInternal("wechat channel unavailable")
			} else {
				sendErr = s.wechat.SendSubscribe(ctx, rec.PatientID, rec.Content)
			}
		} else {
			if s.sms == nil {
				sendErr = model.ErrInternal("sms channel unavailable")
			} else {
				sendErr = s.sms.SendSMS(ctx, rec.PatientID, rec.Content)
			}
		}

		now := s.now()
		if sendErr == nil {
			if updErr := s.store.UpdateNotificationStatus(ctx, rec.RecordID, model.StatusSent, &now); updErr != nil {
				s.log.Error().Err(updErr).Int64("record_id", rec.RecordID).Msg("drain retries: update status failed")
			}
			if rec.Channel == model.ChannelWechat {
				if _, cErr := s.store.ConsumeQuota(ctx, rec.PatientID); cErr != nil {
					s.log.Error().Err(cErr).Str("patient_id", rec.PatientID).Msg("drain retries: consume quota failed")
				}
			}
			_ = s.store.FinishRetry(ctx, item.QueueID, "done")
			continue
		}

		// 重试失败：计数递增 + 退避重排；达上限放弃（不丢记录）
		_ = s.store.IncrementRetryCount(ctx, rec.RecordID)
		next := item.RetryCount + 1
		if next >= MaxRetries {
			s.log.Error().Int64("record_id", rec.RecordID).Int("retry", next).Msg("retry exhausted, giving up")
			_ = s.store.FinishRetry(ctx, item.QueueID, model.StatusFailed)
			continue
		}
		_ = s.store.RescheduleRetry(ctx, item.QueueID, next, now.Add(RetryDelay(next+1)))
	}
	return processed, nil
}

// ─────────────────────────────────────────────────────────────
// 2. 订阅额度（验收 2）
// ─────────────────────────────────────────────────────────────

// GrantQuota 授予额度（契约 grantSubscriptionQuota）：幂等，同 Idempotency-Key 不重复增额
func (s *NotifyService) GrantQuota(ctx context.Context, patientID, idempotencyKey string) (*model.SubscriptionQuota, error) {
	quota, err := s.store.GrantQuota(ctx, patientID, idempotencyKey, 1)
	if err != nil {
		return nil, model.ErrInternal("grant quota: %v", err)
	}
	return quota, nil
}

// GetQuota 查询额度快照（契约 getSubscriptionQuota；isLow 由 repo 层按 ≤1 判定）
func (s *NotifyService) GetQuota(ctx context.Context, patientID string) (*model.SubscriptionQuota, error) {
	quota, err := s.store.GetQuota(ctx, patientID)
	if err != nil {
		return nil, model.ErrInternal("get quota: %v", err)
	}
	return quota, nil
}

// ─────────────────────────────────────────────────────────────
// 3. 佩戴提醒（验收 3）
// ─────────────────────────────────────────────────────────────

// GetWearReminder 读取佩戴提醒设置（契约 getWearReminder）
func (s *NotifyService) GetWearReminder(ctx context.Context, patientID string) (*model.WearReminderSettings, error) {
	settings, err := s.store.GetWearReminder(ctx, patientID)
	if err != nil {
		return nil, model.ErrInternal("get wear reminder: %v", err)
	}
	return settings, nil
}

// UpdateWearReminder 更新佩戴提醒设置（契约 updateWearReminder，直写 patient_preferences 一期偏离）
func (s *NotifyService) UpdateWearReminder(ctx context.Context, patientID string, enabled bool, reminderTime *string) (*model.WearReminderSettings, error) {
	if reminderTime != nil && !validHHmm(*reminderTime) {
		return nil, model.ErrInvalidParam("reminderTime must be HH:mm, got %q", *reminderTime)
	}
	settings, err := s.store.UpdateWearReminder(ctx, patientID, enabled, reminderTime)
	if err != nil {
		return nil, model.ErrInternal("update wear reminder: %v", err)
	}
	return settings, nil
}

func validHHmm(v string) bool {
	if len(v) != 5 || v[2] != ':' {
		return false
	}
	h := (v[0]-'0')*10 + (v[1] - '0')
	m := (v[3]-'0')*10 + (v[4] - '0')
	return v[0] >= '0' && v[1] >= '0' && v[3] >= '0' && v[4] >= '0' && h <= 23 && m <= 59
}

// SendWearReminder 佩戴提醒推送（验收 2/3，架构 §2.5/§7）：
//   - reminder_enabled=false → 不推送（Accepted=false）；
//   - 额度耗尽 → 静默跳过，不降级短信（成本控制）；
//   - 当日已推送 → 去重跳过（防 15min 扫描窗口重复推送）；
//   - 微信推送：成功置 sent + 内部扣减额度；失败置 failed + 进重试队列。
func (s *NotifyService) SendWearReminder(ctx context.Context, patientID string) (*model.SendResult, error) {
	settings, err := s.store.GetWearReminder(ctx, patientID)
	if err != nil {
		return nil, model.ErrInternal("send wear reminder: get settings: %v", err)
	}
	if !settings.ReminderEnabled {
		return &model.SendResult{Accepted: false}, nil
	}

	quota, err := s.store.GetQuota(ctx, patientID)
	if err != nil {
		return nil, model.ErrInternal("send wear reminder: get quota: %v", err)
	}
	if quota.Remaining <= 0 {
		// 佩戴提醒不降级短信（架构 §2.5 成本控制）：静默跳过
		s.log.Info().Str("patient_id", patientID).Msg("wear reminder skipped: quota exhausted (no sms degrade)")
		return &model.SendResult{Accepted: false}, nil
	}

	dayStart := s.bizDayStart()
	sent, err := s.store.ReminderSentToday(ctx, patientID, dayStart)
	if err != nil {
		return nil, model.ErrInternal("send wear reminder: dedup check: %v", err)
	}
	if sent {
		return &model.SendResult{Accepted: false}, nil
	}
	if s.wechat == nil {
		s.log.Error().Str("patient_id", patientID).Msg("wear reminder rejected: wechat channel unavailable")
		return &model.SendResult{Accepted: false}, nil
	}

	rec := &model.NotificationRecord{
		PatientID: patientID,
		Kind:      model.KindWearReminder,
		Channel:   model.ChannelWechat,
		Status:    model.StatusPending,
		Content:   WearReminderContent,
	}
	if err := s.store.CreateNotificationRecord(ctx, rec); err != nil {
		return nil, model.ErrInternal("send wear reminder: create record: %v", err)
	}

	now := s.now()
	if err := s.wechat.SendSubscribe(ctx, patientID, rec.Content); err != nil {
		// 失败不丢通知：记录置 failed + 进重试队列
		s.log.Warn().Err(err).Str("patient_id", patientID).Msg("wear reminder send failed, enqueue retry")
		_ = s.store.UpdateNotificationStatus(ctx, rec.RecordID, model.StatusFailed, nil)
		_ = s.store.EnqueueRetry(ctx, rec.RecordID, now.Add(RetryDelay(1)))
		return &model.SendResult{Accepted: true, RecordID: strconv.FormatInt(rec.RecordID, 10)}, nil
	}

	if err := s.store.UpdateNotificationStatus(ctx, rec.RecordID, model.StatusSent, &now); err != nil {
		s.log.Error().Err(err).Int64("record_id", rec.RecordID).Msg("wear reminder: update status failed")
	}
	// 发送成功 → 内部扣减额度（验收 2）
	if _, err := s.store.ConsumeQuota(ctx, patientID); err != nil {
		s.log.Error().Err(err).Str("patient_id", patientID).Msg("wear reminder: consume quota failed")
	}
	return &model.SendResult{Accepted: true, RecordID: strconv.FormatInt(rec.RecordID, 10)}, nil
}

// bizDayStart 业务日零点（Asia/Shanghai 切日，架构 §3.5）
func (s *NotifyService) bizDayStart() time.Time {
	now := s.now().In(s.loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
}

// ScanReminders 佩戴提醒定时扫描（架构 §7 每 15min）：
// ① reminder_enabled=true 且 reminder_time 已到 → ② 今日佩戴未达标 → ③ 推送（去重/额度在 SendWearReminder 内）
func (s *NotifyService) ScanReminders(ctx context.Context) (int, error) {
	now := s.now().In(s.loc)
	candidates, err := s.store.ListReminderCandidates(ctx, now.Format("15:04"))
	if err != nil {
		return 0, model.ErrInternal("scan reminders: %v", err)
	}

	bizDate := now.Format("2006-01-02")
	pushed := 0
	for _, c := range candidates {
		minutes, err := s.store.TodayWearMinutes(ctx, c.PatientID, bizDate)
		if err != nil {
			s.log.Error().Err(err).Str("patient_id", c.PatientID).Msg("scan reminders: wear minutes failed")
			continue
		}
		if minutes >= s.wearTargetMinutes {
			continue // 今日已达标 → 跳过
		}
		res, err := s.SendWearReminder(ctx, c.PatientID)
		if err != nil {
			s.log.Error().Err(err).Str("patient_id", c.PatientID).Msg("scan reminders: send failed")
			continue
		}
		if res.Accepted {
			pushed++
		}
	}
	return pushed, nil
}

// ─────────────────────────────────────────────────────────────
// 4. 规则管理 / 记录查询（验收 1/4）
// ─────────────────────────────────────────────────────────────

// GetNotifyRules 全量通知规则（契约 getNotifyRules；ROLE_DOCTOR 只读全量，规则表无团队维度）
func (s *NotifyService) GetNotifyRules(ctx context.Context) ([]model.NotifyRule, error) {
	rules, err := s.store.FindRules(ctx)
	if err != nil {
		return nil, model.ErrInternal("get notify rules: %v", err)
	}
	return rules, nil
}

// UpdateNotifyRule 更新通知规则（契约 updateNotifyRule，直写 alert_notify_rules 一期偏离）：
// 未知告警类型拒绝（CHECK 约束兜底）；channels/notify_targets 枚举逐项校验。
func (s *NotifyService) UpdateNotifyRule(ctx context.Context, alertType model.AlertType, channels, notifyTargets []string, operator string) (*model.NotifyRule, error) {
	if !model.ValidAlertType(alertType) {
		return nil, model.ErrInvalidParam("unknown alert type %q (must be one of pressure_high/pressure_fluctuation/wear_interrupt/sensor_drift)", alertType)
	}
	if len(channels) == 0 {
		return nil, model.ErrInvalidParam("channels must not be empty")
	}
	for _, ch := range channels {
		if !model.ValidChannel(ch) {
			return nil, model.ErrInvalidParam("invalid channel %q (must be wechat/sms)", ch)
		}
	}
	if len(notifyTargets) == 0 {
		return nil, model.ErrInvalidParam("notify_targets must not be empty")
	}
	for _, t := range notifyTargets {
		if !model.ValidTarget(t) {
			return nil, model.ErrInvalidParam("invalid notify target %q (must be patient/doctor/tech/ops)", t)
		}
	}

	rule := &model.NotifyRule{
		Type:          alertType,
		Channels:      channels,
		NotifyTargets: notifyTargets,
		UpdatedBy:     operator,
		UpdatedAt:     s.now(),
	}
	if err := s.store.UpsertRule(ctx, rule); err != nil {
		return nil, model.ErrInternal("update notify rule: %v", err)
	}
	return rule, nil
}

// GetNotificationLogs 通知记录分页过滤查询（契约 getNotificationLogs / getPatientNotifications）
func (s *NotifyService) GetNotificationLogs(ctx context.Context, f repo.RecordFilter) ([]model.NotificationRecord, int, error) {
	if f.AlertType != "" && !model.ValidAlertType(model.AlertType(f.AlertType)) {
		return nil, 0, model.ErrInvalidParam("invalid alertType filter %q", f.AlertType)
	}
	if f.Channel != "" && !model.ValidChannel(f.Channel) {
		return nil, 0, model.ErrInvalidParam("invalid channel filter %q", f.Channel)
	}
	if f.Status != "" && !validStatus(f.Status) {
		return nil, 0, model.ErrInvalidParam("invalid status filter %q", f.Status)
	}
	records, total, err := s.store.ListNotificationRecords(ctx, f)
	if err != nil {
		return nil, 0, model.ErrInternal("get notification logs: %v", err)
	}
	return records, total, nil
}

func validStatus(v string) bool {
	switch v {
	case model.StatusPending, model.StatusSent, model.StatusFailed, model.StatusDegraded:
		return true
	}
	return false
}
