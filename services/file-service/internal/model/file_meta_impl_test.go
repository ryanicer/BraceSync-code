// Package model 实现侧测试（T022：文件类型/状态枚举与合法性校验）
package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidFileType(t *testing.T) {
	valid := []FileType{FileTypeSignature, FileTypeInstallPhoto, FileTypeCommPhoto, FileTypeLogPhoto}
	for _, ft := range valid {
		assert.True(t, ValidFileType(ft), "file_type=%s should be valid", ft)
	}

	invalid := []FileType{"", "exe", "SIGNATURE", "signature ", FileType("video/mp4")}
	for _, ft := range invalid {
		assert.False(t, ValidFileType(ft), "file_type=%q should be invalid", ft)
	}
}

func TestFileStatusConstants(t *testing.T) {
	// 状态枚举与 migration CHECK 约束口径一致（000006_file_service.up.sql）
	assert.Equal(t, FileStatus("pending"), FileStatusPending)
	assert.Equal(t, FileStatus("uploaded"), FileStatusUploaded)
	assert.Equal(t, FileStatus("failed"), FileStatusFailed)
}
