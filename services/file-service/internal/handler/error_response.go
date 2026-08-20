// Package handler — 统一错误响应（架构 §3.5：{code, message}，文件域错误码段 6xxxx）
package handler

import (
	"github.com/gin-gonic/gin"
)

// 错误码（架构 §3.5 分段：6xxxx 文件域）
const (
	ErrorCodeSuccess = 0

	// 请求/权限类（600xx）
	ErrorCodeInvalidRequest = 60001 // 400 参数非法
	ErrorCodeUnauthorized   = 60002 // 401 身份缺失/无效
	ErrorCodeForbidden      = 60003 // 403 角色无权

	// 文件业务类（610xx）
	ErrorCodeFileNotFound  = 61001 // 404 文件元数据不存在
	ErrorCodeUploadFailed  = 61002 // 500 上传登记失败
	ErrorCodePresignFailed = 61003 // 500 预签名签发失败
	ErrorCodeInternal      = 69999 // 500 未分类内部错误
)

// errorJSON 统一错误响应体
func errorJSON(c *gin.Context, statusCode, code int, message string) {
	c.JSON(statusCode, gin.H{"code": code, "message": message})
}
