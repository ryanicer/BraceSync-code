package repo

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConfigStore 设备配置查询契约（sys_configs 只读）
type ConfigStore interface {
	// GetDeviceConfig 返回采集间隔（分钟）与配置版本号（响应捎带下发，协议 §4.1）
	GetDeviceConfig(ctx context.Context) (intervalMinutes, configVersion int, err error)
}

const (
	defaultIntervalMinutes = 30 // PRD 默认采集间隔
	defaultConfigVersion   = 1
	configCacheTTL         = 60 * time.Second // 配置读缓存，避免逐帧查库
)

// ConfigRepo ConfigStore 的 pgx 实现（带 60s 内存缓存）
type ConfigRepo struct {
	pool *pgxpool.Pool

	mu        sync.Mutex
	cachedAt  time.Time
	interval  int
	version   int
	hasCached bool
	now       func() time.Time
}

// NewConfigRepo 创建 ConfigRepo
func NewConfigRepo(pool *pgxpool.Pool) *ConfigRepo {
	return &ConfigRepo{pool: pool, now: time.Now}
}

// GetDeviceConfig 读 sys_configs（collect_interval_minutes / device_config_version），
// 缺失或非法值回退默认；结果缓存 60s
func (r *ConfigRepo) GetDeviceConfig(ctx context.Context) (int, int, error) {
	r.mu.Lock()
	if r.hasCached && r.now().Sub(r.cachedAt) < configCacheTTL {
		interval, version := r.interval, r.version
		r.mu.Unlock()
		return interval, version, nil
	}
	r.mu.Unlock()

	interval := defaultIntervalMinutes
	version := defaultConfigVersion

	rows, err := r.pool.Query(ctx,
		`SELECT config_key, config_value FROM sys_configs WHERE config_key IN ('collect_interval_minutes','device_config_version')`)
	if err != nil {
		return defaultIntervalMinutes, defaultConfigVersion, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return defaultIntervalMinutes, defaultConfigVersion, err
		}
		applyConfigValue(key, value, &interval, &version)
	}
	if err := rows.Err(); err != nil {
		return defaultIntervalMinutes, defaultConfigVersion, err
	}

	r.mu.Lock()
	r.cachedAt, r.interval, r.version, r.hasCached = r.now(), interval, version, true
	r.mu.Unlock()
	return interval, version, nil
}

// applyConfigValue 解析单条 sys_configs 记录（非法值保持默认，不覆盖）
func applyConfigValue(key, value string, interval, version *int) {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return
	}
	switch key {
	case "collect_interval_minutes":
		*interval = n
	case "device_config_version":
		*version = n
	}
}
