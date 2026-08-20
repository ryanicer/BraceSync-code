// Package repo 实现侧测试（T030）：纯逻辑辅助（不依赖 PG）
package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTrimPhoneHash(t *testing.T) {
	// CHAR(64) 定长列：PG 读取带尾部空格填充，查重/回写前必须去除
	assert.Equal(t, "hash-it-1", TrimPhoneHash("hash-it-1                    "))
	assert.Equal(t, "abc", TrimPhoneHash("abc"))
	assert.Equal(t, "", TrimPhoneHash(""))
	assert.Equal(t, "", TrimPhoneHash("    "))
	// 仅去尾部空格，中间空格保留
	assert.Equal(t, "a b", TrimPhoneHash("a b  "))
}
