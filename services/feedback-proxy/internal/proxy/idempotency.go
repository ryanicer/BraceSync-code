package proxy

import (
	"sync"
	"time"
)

// idemWindow 幂等窗（D-2：同标题＋提交人＋数分钟时间窗 → 同受理号；无本地持久状态）。
const idemWindow = 5 * time.Minute

type idemEntry struct {
	receipt string
	bugID   int64
	expires time.Time
}

// idemCache 进程内存幂等表：重启即清——与「无库无存储」口径一致（窗口 5 分钟内有效）。
type idemCache struct {
	mu  sync.Mutex
	m   map[string]idemEntry
	now func() time.Time
}

func newIdemCache() *idemCache {
	return &idemCache{m: make(map[string]idemEntry), now: time.Now}
}

// get 命中且未过期返回 true；过期顺手清。
func (c *idemCache) get(key string) (idemEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return idemEntry{}, false
	}
	if c.now().After(e.expires) {
		delete(c.m, key)
		return idemEntry{}, false
	}
	return e, true
}

// put 写入；顺带清扫过期项防膨胀。
func (c *idemCache) put(key, receipt string, bugID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, e := range c.m {
		if now.After(e.expires) {
			delete(c.m, k)
		}
	}
	c.m[key] = idemEntry{receipt: receipt, bugID: bugID, expires: now.Add(idemWindow)}
}
