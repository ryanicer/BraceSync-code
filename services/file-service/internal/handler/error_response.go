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
	// ErrorCodeForbidden 按 T402 甲-1 口径收为「域号 6 + 0 + HTTP 状态三位 403」。
	// 旧值 60003 与本域 60001/60002 同为「域号 + 序号」形状，数字里不含 HTTP 语义。
	// 六服务里 user/device/msg 三域早就是这个形状，file/data/alert 三域由 T402 对齐。
	ErrorCodeForbidden = 60403

	// 文件业务类（610xx）
	// 本组不参与 T402 的「域号 + 0 + HTTP 三位」口径：它们不是越权拒绝码，取值按域内序号排
	// （Boss 22:07 否决乙-2 的同一口径，见卡 T402）。
	ErrorCodeFileNotFound  = 61001 // 404 文件元数据不存在
	ErrorCodeUploadFailed  = 61002 // 500 上传登记失败
	ErrorCodePresignFailed = 61003 // 500 预签名签发失败
	ErrorCodeInternal      = 69999 // 500 未分类内部错误
)

// errorJSON 统一错误响应体。
// data 字段按 T402 乙-1 补齐：user/device/data/msg 四服务与网关的错误体都带 data（值为 null），
// 文件域原本整个没有这一格，前端统一取 res.data 时会在本域拿到 undefined。
// 注：alert-service 的信封把 Data 写成 json:"data,omitempty"，错误体里这一格是省掉的，
// 属 T402 未收口面（卡内已登记），本卡不动它。
func errorJSON(c *gin.Context, statusCode, code int, message string) {
	c.JSON(statusCode, gin.H{"code": code, "message": message, "data": nil})
}
