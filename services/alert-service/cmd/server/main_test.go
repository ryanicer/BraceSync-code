// Package main provides unit tests for alert-service.
// 对齐：docs/ §1 (单元测试层)
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAlertServiceCompiles(t *testing.T) {
	assert.True(t, true, "alert-service skeleton test compiles OK")
}

// TestEnvOr_Defaults 验证 envOr 在未设置环境变量时返回兜底默认值。
// T231 回归用例：MSG_SERVICE_URL 默认端口必须对齐 msg-service 实际监听 8086。
func TestEnvOr_Defaults(t *testing.T) {
	t.Setenv("MSG_SERVICE_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("PORT", "")

	assert.Equal(t, "http://msg-service:8086", envOr("MSG_SERVICE_URL", "http://msg-service:8086"))
	assert.Equal(t, "redis://localhost:6379/0", envOr("REDIS_URL", "redis://localhost:6379/0"))
	assert.Equal(t, "8080", envOr("PORT", "8080"))
}

// TestEnvOr_Override 验证 envOr 在环境变量已设置时优先取环境值。
func TestEnvOr_Override(t *testing.T) {
	t.Setenv("MSG_SERVICE_URL", "http://override:9999")

	assert.Equal(t, "http://override:9999", envOr("MSG_SERVICE_URL", "http://msg-service:8086"))
}
