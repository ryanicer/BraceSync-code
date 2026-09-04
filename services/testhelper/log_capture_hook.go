// Package testhelper provides BraceSync integration test utilities
// 对齐：T085 测试计划 · Phase 1 Fixture 搭建
//
// LogCaptureHook 用于 zerolog 结构化日志捕获和审计字段验证。
//
// 实现方式：zerolog.Hook.Run 只能拿到 level 与 msg，拿不到结构化字段
// （action/operator_id/before/after）。因此本 Hook 同时实现 io.Writer，
// 逐行解析 zerolog 输出的 JSON，把每个字段都存入 entries。
//
// 使用方式（替换全局 logger 并在测试结束还原）：
//
//	lc := testhelper.NewLogCaptureHook()
//	orig := log.Logger
//	log.Logger = zerolog.New(lc).With().Timestamp().Logger()
//	t.Cleanup(func() { log.Logger = orig })
package testhelper

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/rs/zerolog"
)

// LogCaptureHook 捕获 zerolog 日志条目的 Hook（兼 io.Writer 解析 JSON 行）
type LogCaptureHook struct {
	entries         []map[string]interface{}
	mu              sync.Mutex
	eventTypeFilter string // 可选过滤 event 类型
}

// NewLogCaptureHook 创建新的日志捕获 hook
func NewLogCaptureHook(eventTypeFilter ...string) *LogCaptureHook {
	hook := &LogCaptureHook{}
	if len(eventTypeFilter) > 0 {
		hook.eventTypeFilter = eventTypeFilter[0]
	}
	return hook
}

// Run 实现 zerolog.Hook 接口（兜底：如果调用方仍用 Hook 模式，至少记录 level/msg）
func (lc *LogCaptureHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	entry := make(map[string]interface{})
	entry["level"] = level.String()
	entry["msg"] = msg
	lc.entries = append(lc.entries, entry)
}

// Write 实现 io.Writer：逐行解析 zerolog 输出的 JSON，把全部字段存入 entries。
// 一行可能分多次 Write 到达（zerolog 一次性写一行，通常单次就完整），
// 这里用内部缓冲按换行切分。
func (lc *LogCaptureHook) Write(p []byte) (int, error) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lines := bytes.Split(p, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal(line, &entry); err != nil {
			// 非 JSON 行（如 zerolog console 模式）原样保存为 msg
			entry = map[string]interface{}{"msg": string(line)}
		}
		lc.entries = append(lc.entries, entry)
	}
	return len(p), nil
}

// Entries 返回所有捕获的日志条目（深拷贝）
func (lc *LogCaptureHook) Entries() []map[string]interface{} {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	result := make([]map[string]interface{}, len(lc.entries))
	copy(result, lc.entries)
	return result
}

// CountEvent 统计特定 event/action 类型的日志数量
func (lc *LogCaptureHook) CountEvent(eventType string) int {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	count := 0
	for _, entry := range lc.entries {
		if action, ok := entry["action"]; ok {
			if actionStr, ok := action.(string); ok && actionStr == eventType {
				count++
			}
		}
	}
	return count
}

// FindEventByAction 根据 action 字段查找日志条目
func (lc *LogCaptureHook) FindEventByAction(actionValue string) map[string]interface{} {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	for _, entry := range lc.entries {
		if action, ok := entry["action"]; ok {
			if actionStr, ok := action.(string); ok && actionStr == actionValue {
				return entry
			}
		}
	}
	return nil
}

// HasField 检查是否存在任意条目包含指定字段
func (lc *LogCaptureHook) HasField(fieldName string) bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	for _, entry := range lc.entries {
		if _, exists := entry[fieldName]; exists {
			return true
		}
	}
	return false
}

// Clear 清空所有捕获的日志条目
func (lc *LogCaptureHook) Clear() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.entries = nil
}

// GetLastEntry 返回最后一条日志条目
func (lc *LogCaptureHook) GetLastEntry() map[string]interface{} {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if len(lc.entries) == 0 {
		return nil
	}
	return lc.entries[len(lc.entries)-1]
}
