package config

import (
	"context"
	"sync"
	"time"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
)

// Store sys_configs 持久化契约（repo.PGConfigRepo 实现）
type Store interface {
	// FetchAll 读取本包管理的配置键（缺失行不返回）
	FetchAll(ctx context.Context) (map[string]string, error)
	// Upsert 批量写入（存在则覆盖，单事务）
	Upsert(ctx context.Context, values map[string]string) error
}

// DefaultCacheTTL 配置读缓存 TTL（与 data-service ConfigRepo 口径一致；过期重读 DB = 热更新生效）
const DefaultCacheTTL = 60 * time.Second

// Manager 阈值配置统一读取入口 + 热更新入口（T009）。
// 校验失败（ValidateThresholds）的配置不缓存、不写库，引擎保持上一份合法值。
type Manager struct {
	store    Store
	cacheTTL time.Duration
	now      func() time.Time

	mu       sync.Mutex
	cached   *Thresholds
	cachedAt time.Time
}

// NewManager 组装配置管理器（生产调用方使用，缓存 TTL = DefaultCacheTTL）
func NewManager(store Store) *Manager {
	return &Manager{store: store, cacheTTL: DefaultCacheTTL, now: time.Now}
}

// Load 配置加载入口：全量读 sys_configs → 解析 → 联动校验 → 缓存生效。
// 校验失败不缓存并返回 *ValidationError（调用方决定兜底，如保持引擎默认口径）。
func (m *Manager) Load(ctx context.Context) (Thresholds, error) {
	raw, err := m.store.FetchAll(ctx)
	if err != nil {
		return Thresholds{}, err
	}
	th := ParseThresholds(raw)
	if err := ValidateThresholds(th.CollectIntervalMinutes, th.WearInterruptMinutes); err != nil {
		return th, err
	}
	m.mu.Lock()
	m.cached, m.cachedAt = &th, m.now()
	m.mu.Unlock()
	return th, nil
}

// Current 配置读取统一入口：缓存未过期直接返回；过期自动重读（热更新生效）。
func (m *Manager) Current(ctx context.Context) (Thresholds, error) {
	m.mu.Lock()
	if m.cached != nil && m.now().Sub(m.cachedAt) < m.cacheTTL {
		th := *m.cached
		m.mu.Unlock()
		return th, nil
	}
	m.mu.Unlock()
	return m.Load(ctx)
}

// Update 配置修改/热更新入口（运营后台 §7D.12 系统配置变更路径）：
// 合并补丁 → 联动校验（采集间隔变化时自动检查受影响的中断阈值）→
// 通过则写库 + 缓存失效；校验失败拒绝写入（不落库、缓存不变）并返回 *ValidationError。
func (m *Manager) Update(ctx context.Context, patch ThresholdPatch) (Thresholds, error) {
	cur, err := m.Current(ctx)
	if err != nil {
		return Thresholds{}, err
	}
	if patch.IsEmpty() {
		return cur, nil
	}
	next := cur.apply(patch)
	if err := ValidateThresholds(next.CollectIntervalMinutes, next.WearInterruptMinutes); err != nil {
		return cur, err // 拒绝写入：现有配置保持不变
	}
	if err := m.store.Upsert(ctx, next.ToValues()); err != nil {
		return cur, err // 写库失败即回滚：缓存仍为 cur
	}
	m.mu.Lock()
	m.cached, m.cachedAt = &next, m.now()
	m.mu.Unlock()
	return next, nil
}

// Refresh 将最新生效配置热更新到评估器（引擎读最新值）；
// 读取/校验失败不改动 eval（保持上一份生效值），仅返回错误。
func (m *Manager) Refresh(ctx context.Context, eval *engine.RuleEvaluator) (Thresholds, error) {
	th, err := m.Current(ctx)
	if err != nil {
		return Thresholds{}, err
	}
	ApplyThresholds(th, eval)
	return th, nil
}

// ApplyThresholds 阈值快照写入评估器（采集间隔同步注入，扫描器状态机推导依赖）
func ApplyThresholds(t Thresholds, eval *engine.RuleEvaluator) {
	eval.PressureHighThreshold = t.PressureHighN
	eval.FluctuationThresholdPct = t.FluctuationPct
	eval.WearInterruptMinutes = t.WearInterruptMinutes
	eval.SensorDriftThreshold = t.SensorDriftN
	eval.CollectionIntervalMin = t.CollectIntervalMinutes
}
