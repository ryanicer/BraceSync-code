package repo

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// ConfigStore 设备配置查询契约（sys_configs 只读）
type ConfigStore interface {
	// GetDeviceConfig 返回采集间隔（分钟）与配置版本号（响应捎带下发，协议 §4.1）
	GetDeviceConfig(ctx context.Context) (intervalMinutes, configVersion int, err error)
}

// ThresholdStore 压力量纲阈值查询契约（T173 可配置化：PRD §7D.12，sys_configs 驱动）。
// 可选接口：RecordService 经类型断言使用，未实现时回退 model.DefaultPressureThresholds。
type ThresholdStore interface {
	// GetPressureThresholds 返回佩戴/热力图/压力偏高阈值（占位值待 Boss 重定，见 T173-decision）
	GetPressureThresholds(ctx context.Context) (model.PressureThresholds, error)
}

const (
	defaultIntervalMinutes = 30 // PRD 默认采集间隔
	defaultConfigVersion   = 1
	configCacheTTL         = 60 * time.Second // 配置读缓存，避免逐帧查库
)

// sys_configs 阈值键（与 scripts/db/seed/seed.sql 一致；值均为占位，待按 mN/÷1000 量级重定）
const (
	keyWearingThreshold = "wearing_pressure_threshold"
	keyHeatmapMax       = "heatmap_max_n"
	keyPressureHigh     = "threshold_pressure_high"
)

// configSnapshot 一次 sys_configs 读取的缓存负载
type configSnapshot struct {
	interval int
	version  int
	th       model.PressureThresholds
}

// ConfigRepo ConfigStore/ThresholdStore 的 pgx 实现（带 60s 内存缓存）
type ConfigRepo struct {
	pool *pgxpool.Pool

	mu       sync.Mutex
	cachedAt time.Time
	snap     configSnapshot
	hasCache bool
	now      func() time.Time
}

// NewConfigRepo 创建 ConfigRepo
func NewConfigRepo(pool *pgxpool.Pool) *ConfigRepo {
	return &ConfigRepo{pool: pool, now: time.Now}
}

// load 读 sys_configs（缺失/非法键回退默认），结果缓存 60s
func (r *ConfigRepo) load(ctx context.Context) (configSnapshot, error) {
	r.mu.Lock()
	if r.hasCache && r.now().Sub(r.cachedAt) < configCacheTTL {
		snap := r.snap
		r.mu.Unlock()
		return snap, nil
	}
	r.mu.Unlock()

	snap := configSnapshot{
		interval: defaultIntervalMinutes,
		version:  defaultConfigVersion,
		th:       model.DefaultPressureThresholds(),
	}

	rows, err := r.pool.Query(ctx,
		`SELECT config_key, config_value FROM sys_configs WHERE config_key IN
		 ('collect_interval_minutes','device_config_version',
		  'wearing_pressure_threshold','heatmap_max_n','threshold_pressure_high')`)
	if err != nil {
		return snap, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return snap, err
		}
		applyConfigValue(key, value, &snap)
	}
	if err := rows.Err(); err != nil {
		return snap, err
	}

	r.mu.Lock()
	r.cachedAt, r.snap, r.hasCache = r.now(), snap, true
	r.mu.Unlock()
	return snap, nil
}

// GetDeviceConfig 读采集间隔与配置版本（协议 §4.1 捎带下发）
func (r *ConfigRepo) GetDeviceConfig(ctx context.Context) (int, int, error) {
	snap, err := r.load(ctx)
	if err != nil {
		return defaultIntervalMinutes, defaultConfigVersion, err
	}
	return snap.interval, snap.version, nil
}

// GetPressureThresholds 读压力量纲阈值（T173 可配置化，PRD §7D.12）
func (r *ConfigRepo) GetPressureThresholds(ctx context.Context) (model.PressureThresholds, error) {
	snap, err := r.load(ctx)
	if err != nil {
		return model.DefaultPressureThresholds(), err
	}
	return snap.th, nil
}

// applyConfigValue 解析单条 sys_configs 记录（非法值保持默认，不覆盖）
func applyConfigValue(key, value string, snap *configSnapshot) {
	switch key {
	case "collect_interval_minutes":
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			snap.interval = n
		}
	case "device_config_version":
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			snap.version = n
		}
	case keyWearingThreshold:
		if v, err := strconv.ParseFloat(value, 64); err == nil && v > 0 {
			snap.th.WearingN = v
		}
	case keyHeatmapMax:
		if v, err := strconv.ParseFloat(value, 64); err == nil && v > 0 {
			snap.th.HeatmapMaxN = v
		}
	case keyPressureHigh:
		if v, err := strconv.ParseFloat(value, 64); err == nil && v > 0 {
			snap.th.PressureHighN = v
		}
	}
}
