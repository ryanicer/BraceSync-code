// Package main provides unit tests for msg-service.
// 对齐：docs/ §1 (单元测试层)
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMsgServiceCompiles(t *testing.T) {
	assert.True(t, true, "msg-service skeleton test compiles OK")
}
