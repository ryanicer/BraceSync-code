// Package testutil msg-service 测试辅助：内存 Store 实现（无 DB 依赖）
//
// 对齐 device-service/internal/testutil 模式：单测/HTTP 层测试以 FakeStore 替换 PGStore，
// 语义与 repo.PGStore 对齐（幂等 grant / 额度下限 0 / 时间倒序分页 / 重试队列状态机）。
package testutil

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/bracesync/bracesync/services/msg-service/internal/model"
	"github.com/bracesync/bracesync/services/msg-service/internal/repo"
)

// FakeStore repo.Store 的内存实现（并发安全）
type FakeStore struct {
	mu sync.Mutex

	rules       map[model.AlertType]model.NotifyRule
	quotas      map[string]int  // patientID → subscription_quota（无行 = 默认 3）
	grantKeys   map[string]bool // patientID:idempotencyKey → 已授予
	granted     map[string]int  // patientID → 台账增量合计
	prefs       map[string]model.WearReminderSettings
	prefsExists map[string]bool
	records     map[int64]model.NotificationRecord
	nextID      int64
	queue       map[int64]model.RetryQueueItem
	nextQueueID int64
	wearMinutes map[string]int // patientID:bizDate → 佩戴分钟数
}

// 编译期断言：FakeStore 实现 repo.Store
var _ repo.Store = (*FakeStore)(nil)

// NewFakeStore 创建空 FakeStore
func NewFakeStore() *FakeStore {
	return &FakeStore{
		rules:       map[model.AlertType]model.NotifyRule{},
		quotas:      map[string]int{},
		grantKeys:   map[string]bool{},
		granted:     map[string]int{},
		prefs:       map[string]model.WearReminderSettings{},
		prefsExists: map[string]bool{},
		records:     map[int64]model.NotificationRecord{},
		nextID:      1,
		queue:       map[int64]model.RetryQueueItem{},
		nextQueueID: 1,
		wearMinutes: map[string]int{},
	}
}

// SeedRule 测试预置通知规则
func (s *FakeStore) SeedRule(rule model.NotifyRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[rule.Type] = rule
}

// SeedWearMinutes 测试预置当日佩戴分钟数
func (s *FakeStore) SeedWearMinutes(patientID, bizDate string, minutes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wearMinutes[patientID+":"+bizDate] = minutes
}

// SeedQuota 测试预置患者额度（模拟 patient_preferences.subscription_quota 现存值）
func (s *FakeStore) SeedQuota(patientID string, quota int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quotas[patientID] = quota
	s.prefsExists[patientID] = true
}

func (s *FakeStore) quotaOf(patientID string) int {
	if q, ok := s.quotas[patientID]; ok {
		return q
	}
	return model.DefaultQuota
}

func (s *FakeStore) totalOf(patientID string) int {
	return model.DefaultQuota + s.granted[patientID]
}

func (s *FakeStore) buildQuota(patientID string) *model.SubscriptionQuota {
	remaining := s.quotaOf(patientID)
	q := &model.SubscriptionQuota{
		PatientID: patientID,
		Remaining: remaining,
		Total:     s.totalOf(patientID),
		IsLow:     remaining <= model.QuotaLowThreshold,
	}
	if s.prefsExists[patientID] {
		now := time.Now()
		q.UpdatedAt = &now
	}
	return q
}

// ── 通知规则 ──

// FindRules 全量规则（type 排序）
func (s *FakeStore) FindRules(_ context.Context) ([]model.NotifyRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.NotifyRule, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out, nil
}

// FindRuleByType 按类型查规则；未配置返回 nil
func (s *FakeStore) FindRuleByType(_ context.Context, alertType model.AlertType) (*model.NotifyRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rules[alertType]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

// UpsertRule 规则 upsert
func (s *FakeStore) UpsertRule(_ context.Context, rule *model.NotifyRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rule.UpdatedAt = time.Now()
	s.rules[rule.Type] = *rule
	return nil
}

// ── 订阅额度 ──

// GetQuota 额度快照（无偏好行 = 默认 3）
func (s *FakeStore) GetQuota(_ context.Context, patientID string) (*model.SubscriptionQuota, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buildQuota(patientID), nil
}

// GrantQuota 幂等增额：同 (patientID, idempotencyKey) 仅增额一次
func (s *FakeStore) GrantQuota(_ context.Context, patientID, idempotencyKey string, increment int) (*model.SubscriptionQuota, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := patientID + ":" + idempotencyKey
	if !s.grantKeys[key] {
		if _, ok := s.quotas[patientID]; !ok {
			s.quotas[patientID] = model.DefaultQuota
		}
		s.quotas[patientID] += increment
		s.granted[patientID] += increment
		s.grantKeys[key] = true
		s.prefsExists[patientID] = true
	}
	return s.buildQuota(patientID), nil
}

// ConsumeQuota 发送时内部扣减（下限 0）
func (s *FakeStore) ConsumeQuota(_ context.Context, patientID string) (*model.SubscriptionQuota, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.quotas[patientID]; !ok {
		s.quotas[patientID] = model.DefaultQuota
	}
	if s.quotas[patientID] > 0 {
		s.quotas[patientID]--
	}
	s.prefsExists[patientID] = true
	return s.buildQuota(patientID), nil
}

// ── 佩戴提醒 ──

// GetWearReminder 提醒设置（无偏好行 = 默认关闭）
func (s *FakeStore) GetWearReminder(_ context.Context, patientID string) (*model.WearReminderSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.prefsExists[patientID] {
		return &model.WearReminderSettings{ReminderEnabled: false}, nil
	}
	w := s.prefs[patientID]
	return &w, nil
}

// UpdateWearReminder upsert 提醒设置
func (s *FakeStore) UpdateWearReminder(_ context.Context, patientID string, enabled bool, reminderTime *string) (*model.WearReminderSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefs[patientID] = model.WearReminderSettings{ReminderEnabled: enabled, ReminderTime: reminderTime}
	s.prefsExists[patientID] = true
	return &model.WearReminderSettings{ReminderEnabled: enabled, ReminderTime: reminderTime}, nil
}

// ListReminderCandidates 到点候选（reminder_enabled 且 reminder_time ≤ nowHM）
func (s *FakeStore) ListReminderCandidates(_ context.Context, nowHM string) ([]repo.ReminderCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []repo.ReminderCandidate{}
	for patientID, w := range s.prefs {
		if !s.prefsExists[patientID] || !w.ReminderEnabled || w.ReminderTime == nil {
			continue
		}
		if *w.ReminderTime <= nowHM {
			out = append(out, repo.ReminderCandidate{PatientID: patientID, ReminderTime: *w.ReminderTime})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PatientID < out[j].PatientID })
	return out, nil
}

// ReminderSentToday 当日是否已推送（kind=wear_reminder 且非 failed）
func (s *FakeStore) ReminderSentToday(_ context.Context, patientID string, dayStart time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.records {
		if rec.PatientID == patientID && rec.Kind == model.KindWearReminder &&
			rec.Status != model.StatusFailed && !rec.CreatedAt.Before(dayStart) {
			return true, nil
		}
	}
	return false, nil
}

// ── 发送记录 ──

// CreateNotificationRecord 落库并回填 RecordID/CreatedAt
func (s *FakeStore) CreateNotificationRecord(_ context.Context, rec *model.NotificationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.RecordID = s.nextID
	s.nextID++
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	s.records[rec.RecordID] = *rec
	return nil
}

// GetNotificationRecord 按 ID 查记录；不存在返回 repo.ErrNotFound
func (s *FakeStore) GetNotificationRecord(_ context.Context, recordID int64) (*model.NotificationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[recordID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return &rec, nil
}

// UpdateNotificationStatus 状态流转
func (s *FakeStore) UpdateNotificationStatus(_ context.Context, recordID int64, status string, sentAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[recordID]
	if !ok {
		return repo.ErrNotFound
	}
	rec.Status = status
	rec.SentAt = sentAt
	s.records[recordID] = rec
	return nil
}

// IncrementRetryCount 重试失败递增计数
func (s *FakeStore) IncrementRetryCount(_ context.Context, recordID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[recordID]
	if !ok {
		return repo.ErrNotFound
	}
	rec.RetryCount++
	s.records[recordID] = rec
	return nil
}

// ListNotificationRecords 分页过滤（created_at DESC）
func (s *FakeStore) ListNotificationRecords(_ context.Context, f repo.RecordFilter) ([]model.NotificationRecord, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	matched := []model.NotificationRecord{}
	for _, rec := range s.records {
		if f.PatientID != "" && rec.PatientID != f.PatientID {
			continue
		}
		if f.AlertType != "" && (rec.AlertType == nil || string(*rec.AlertType) != f.AlertType) {
			continue
		}
		if f.Channel != "" && rec.Channel != f.Channel {
			continue
		}
		if f.Status != "" && rec.Status != f.Status {
			continue
		}
		matched = append(matched, rec)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].CreatedAt.After(matched[j].CreatedAt) })

	page, pageSize := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	start := (page - 1) * pageSize
	if start > len(matched) {
		start = len(matched)
	}
	end := start + pageSize
	if end > len(matched) {
		end = len(matched)
	}
	return matched[start:end], len(matched), nil
}

// ── 重试队列 ──

// EnqueueRetry 失败记录入队（同记录已有 pending 项时幂等跳过）
func (s *FakeStore) EnqueueRetry(_ context.Context, recordID int64, nextRetryAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.queue {
		if item.RecordID == recordID && item.Status == "pending" {
			return nil
		}
	}
	rec, ok := s.records[recordID]
	if !ok {
		return repo.ErrNotFound
	}
	s.queue[s.nextQueueID] = model.RetryQueueItem{
		QueueID:     s.nextQueueID,
		RecordID:    recordID,
		RetryCount:  rec.RetryCount,
		NextRetryAt: nextRetryAt,
		Status:      "pending",
	}
	s.nextQueueID++
	return nil
}

// ListDueRetries 到期 pending 队列项（next_retry_at 升序）
func (s *FakeStore) ListDueRetries(_ context.Context, now time.Time, limit int) ([]model.RetryQueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.RetryQueueItem{}
	for _, item := range s.queue {
		if item.Status == "pending" && !item.NextRetryAt.After(now) {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextRetryAt.Before(out[j].NextRetryAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// FinishRetry 队列项终态（done / failed）
func (s *FakeStore) FinishRetry(_ context.Context, queueID int64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.queue[queueID]
	if !ok {
		return repo.ErrNotFound
	}
	item.Status = status
	s.queue[queueID] = item
	return nil
}

// RescheduleRetry 重试失败后更新计数与下次重试时刻
func (s *FakeStore) RescheduleRetry(_ context.Context, queueID int64, retryCount int, nextRetryAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.queue[queueID]
	if !ok {
		return repo.ErrNotFound
	}
	item.RetryCount = retryCount
	item.NextRetryAt = nextRetryAt
	s.queue[queueID] = item
	return nil
}

// ── 佩戴达标只读 ──

// TodayWearMinutes 业务日佩戴分钟数（无预置 = 0，视为未达标）
func (s *FakeStore) TodayWearMinutes(_ context.Context, patientID string, bizDate string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wearMinutes[patientID+":"+bizDate], nil
}
