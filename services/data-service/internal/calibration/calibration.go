// Package calibration 患者端监测数据校准读侧（T173）。
//
// 口径（T173-decision D2–D8 保持；单位契约随 T733 方案A 更新，PRD §7A.2）：
//   - 设备上报单位 = 毫牛 mN（固件 1.9 原值），入口零换算，落库前即 mN（见 service.toPendingFrame）；
//   - 基线偏移减法只在本读侧做一处，realtime（DB/Redis 双分支）、records、heatmap、
//     告警评估入参共用同一 Calibrator，保证三端与告警同源；
//   - 基线唯一权威源 = baselines 表（写归 device-service，本服务只读，D3 同库只读 JOIN）；
//   - 规矩 A：单次权威校准、禁现场复校，uk_install_baseline 唯一约束为有意设计——
//     因此「当前基线」= 该设备 baseline_id 最新的一条（一机多装按安装时序取最新）；
//   - 缺基线时不减、显式标记未校准（Result.Applied=false），不得静默当 0 偏移。
package calibration

import (
	"context"
	"sync"
	"time"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// DefaultCacheTTL 基线读缓存 TTL（对齐 repo.ConfigRepo 口径；规矩 A 下基线不可变，缓存安全）
const DefaultCacheTTL = 60 * time.Second

// Baseline 某设备当前生效的权威校准基线（baselines 表行投影）
type Baseline struct {
	BaselineID int64
	Offsets    [model.PointCount]float32 // 单位 mN（T733 方案A：baselines.offset_values 与帧同存储空间，零换算；旧 N 值由迁移 000041 x1000 搬齐）
}

// Store 基线只读契约（repo.BaselineRepo 实现；D3：同库只读，写归 device-service）
type Store interface {
	// GetLatestBaseline 返回设备当前基线（baseline_id 最新）；found=false 表示该设备无基线
	GetLatestBaseline(ctx context.Context, deviceID string) (Baseline, bool, error)
}

// Result 单帧校准结果
type Result struct {
	Points     [model.PointCount]float32 // 已校准值（未命中基线时 = 入参原值）
	BaselineID int64                     // 命中的基线；未命中为 0
	Applied    bool                      // 是否应用了减偏移
}

// Calibrator 校准器：减法单函数 + 设备级基线 TTL 缓存
type Calibrator struct {
	store Store
	ttl   time.Duration
	now   func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	baseline Baseline
	found    bool
	cachedAt time.Time
}

// NewCalibrator 组装校准器
func NewCalibrator(store Store) *Calibrator {
	return &Calibrator{
		store: store,
		ttl:   DefaultCacheTTL,
		now:   time.Now,
		cache: make(map[string]cacheEntry),
	}
}

// Apply 单帧校准：points（原样入库的 mN 值，T733 方案A）减去设备当前基线偏移（同 mN）。
// 缺基线返回原值且 Applied=false（显式未校准，调用方透出 calibrated=false）。
func (c *Calibrator) Apply(ctx context.Context, deviceID string, points [model.PointCount]float32) (Result, error) {
	bl, found, err := c.currentBaseline(ctx, deviceID)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Result{Points: points, Applied: false}, nil
	}
	out := points
	// T733 方案A 口径①③：帧与基线都在 mN 存储空间，直接相减、零换算。
	// 基线若不迁 mN（迁移 000041 漏跑），这里等价于减去 0.001 量级的数——不报错但校准失效，
	// 所以量纲对齐必须靠迁移保证，卡面判据要求改后回读 baselines 读数。
	for i := range out {
		out[i] -= bl.Offsets[i]
	}
	return Result{Points: out, BaselineID: bl.BaselineID, Applied: true}, nil
}

// currentBaseline 读当前基线（TTL 缓存，含负缓存）
func (c *Calibrator) currentBaseline(ctx context.Context, deviceID string) (Baseline, bool, error) {
	c.mu.Lock()
	if e, ok := c.cache[deviceID]; ok && c.now().Sub(e.cachedAt) < c.ttl {
		c.mu.Unlock()
		return e.baseline, e.found, nil
	}
	c.mu.Unlock()

	bl, found, err := c.store.GetLatestBaseline(ctx, deviceID)
	if err != nil {
		return Baseline{}, false, err
	}
	c.mu.Lock()
	c.cache[deviceID] = cacheEntry{baseline: bl, found: found, cachedAt: c.now()}
	c.mu.Unlock()
	return bl, found, nil
}
