// Package main provides unit tests for user-service.
// 对齐：docs/ §1 (单元测试层)
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserServiceCompiles(t *testing.T) {
	// 骨架编译验证：确保 main 包可被测试框架加载
	assert.True(t, true, "user-service skeleton test compiles OK")
}
