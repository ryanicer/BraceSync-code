// T464 报错双通道（文件域）：错误码 → 面向终端用户的中文短句。
//
// 与 errorJSON 的 message 入参分工：调用方传的是技术文本（错误原因、字段名、底层报错），
// 只写服务端日志；响应体 message 一律由本表给出。
// 表键用本包错误码常量（架构 §3.5 文件域 6xxxx 分段）。
package handler

// FallbackUserText 未在表内的错误码兜底：宁可给一句通用中文，也不让技术文本漏到用户面。
const FallbackUserText = "服务暂时不可用，请稍后重试；若持续失败请联系管理员并告知发生时间"

var userTextByCode = map[int]string{
	ErrorCodeInvalidRequest: "文件信息填写有误，请确认文件类型与大小后重新上传",
	ErrorCodeUnauthorized:   "登录状态已失效，请重新登录后再上传",
	ErrorCodeForbidden:      "没有权限访问该文件，请联系管理员确认账号权限",
	ErrorCodeFileNotFound:   "未找到该文件，请刷新页面后重试；若仍缺失请重新上传",
	ErrorCodeUploadFailed:   "文件上传登记失败，请重新上传该文件",
	ErrorCodePresignFailed:  "文件上传通道暂时不可用，请稍后重试",
	ErrorCodeInternal:       FallbackUserText,
}

// UserText 取该错误码的用户面中文文案；查不到码走兜底，绝不返回技术文本。
func UserText(code int) string {
	if text, exists := userTextByCode[code]; exists {
		return text
	}
	return FallbackUserText
}
