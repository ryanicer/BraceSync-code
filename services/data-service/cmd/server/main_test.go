// Package main provides unit tests for data-service.
// 对齐：docs/ §1 (单元测试层)
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDataServiceCompiles(t *testing.T) {
	assert.True(t, true, "data-service skeleton test compiles OK")
}
