// T464 错误响应双通道：用户面文案（用户域 1xxxx / 4xxxx / 9xxxx）
//
// 响应体 message 只承载「发生了什么 + 用户该做什么」的中文短句；
// 原始技术 error（AppError.Message，含 %v 拼进来的 SQL/内部路径/英文堆栈文本）
// 由 handler 出口写进服务端日志，绝不进响应体。
//
// 文案按 T402 已裁错误码（域号 + 0 + HTTP 三位）逐码给定，不重开码表；
// 码不在表内一律走 FallbackUserText，不允许回落到技术文本。
package model

// FallbackUserText 未登记错误码的统一用户面兜底文案。
const FallbackUserText = "服务暂时不可用，请稍后重试；若持续失败请联系管理员并告知发生时间"

var userTextByCode = map[int]string{
	CodeInvalidParam:       "请求信息有误，请检查填写内容后重试",
	CodeUnauthorized:       "登录状态已失效，请重新登录",
	CodeForbidden:          "没有权限执行该操作，请联系管理员确认账号权限",
	CodeNotFound:           "未找到对应记录，请刷新后重试",
	CodeConflict:           "该操作与现有记录冲突，请刷新列表后重试",
	CodeWXUnavail:          "微信服务暂时不可用，请稍后重试",
	CodeInvalidPhone:       "手机号获取失败，请重新获取后再试",
	CodeInternal:           FallbackUserText,
	CodeInvalidCredentials: "账号或密码不正确，请重新输入",
	CodePatientNotBound:    "该微信尚未绑定手机号，请先完成手机号绑定",
	CodePatientNotFound:    "未找到与该手机号对应的患者档案，请联系客服核对手机号",
	CodePhoneAlreadyBound:  "该手机号已绑定其他微信，请先在原微信上解绑后再试",
	CodeInvalidPhoneToken:  "手机号授权已过期，请重新获取手机号",
	CodeForbiddenScope:     "当前登录方式不支持该操作，请重新登录后再试",
}

// UserText 取该错误码的用户面中文文案；查不到码走兜底，绝不返回技术文本。
func UserText(code int) string {
	if text, exists := userTextByCode[code]; exists {
		return text
	}
	return FallbackUserText
}
