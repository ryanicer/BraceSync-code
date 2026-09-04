// Package testhelper provides BraceSync integration test utilities
// 对齐：T085 测试计划 · Phase 1 Fixture 搭建
//
// LogCaptureHook 用于 zerolog 结构化日志捕获和审计字段验证
package testhelper

import (
	"sync"

	"github.com/rs/zerolog"
)

// LogCaptureHook 捕获 zerolog 日志条目的 Hook
// 主要用途：
//   - 审计字段落日志断言（场景 E/G Admin 操作）
//   - 验证 structured log fields: operator_id, action, before, after
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

// Run 实现 zerolog.Hook 接口
func (lc *LogCaptureHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	entry := make(map[string]interface{})

	// 提取关键上下文信息（使用传入的 level 参数）
	entry["level"] = level
	entry["msg"] = msg

	lc.entries = append(lc.entries, entry)
}

// Entries 返回所有捕获的日志条目
func (lc *LogCaptureHook) Entries() []map[string]interface{} {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// 深拷贝避免外部修改
	result := make([]map[string]interface{}, len(lc.entries))
	copy(result, lc.entries)
	return result
}

// CountEvent 统计特定 event 类型的日志数量
func (lc *LogCaptureHook) CountEvent(eventType string) int {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	count := 0
	for _, entry := range lc.entries {
		if action, ok := entry["event"]; ok {
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

// HasField 检查指定日志条目是否包含某个字段
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
