// 消息域用户面文案表（T464 报错双通道）：错误码 → 面向终端用户的中文短句。
//
// 与 Message 的分工：model.AppError.Message 是技术文本（规则 ID、渠道名、内部接口头），
// 只写服务端日志；响应体 message 一律由本表给出。
package model

// FallbackUserText 未在表内的错误码兜底：宁可给一句通用中文，也不让技术文本漏到用户面。
const FallbackUserText = "服务暂时不可用，请稍后重试；若持续失败请联系管理员并告知发生时间"

var userTextByCode = map[int]string{
	CodeInvalidParam:     "通知设置填写有误，请检查告警类型与接收渠道后重试",
	CodeForbidden:        "没有权限执行该操作，请联系管理员确认账号权限",
	CodeNotFound:         "未找到对应的通知规则或发送记录，请刷新后重试",
	CodePatientNotFound:  "未找到该患者的档案，请联系客服核对患者信息",
	CodeQuotaExhausted:   "本月消息额度已用完，请升级套餐或稍后再试",
	CodeInternal:         FallbackUserText,
	CodeInternalDisabled: "该接口仅限内部服务调用",
}

// UserText 取该错误码的用户面中文文案；查不到码走兜底，绝不返回技术文本。
func UserText(code int) string {
	if text, exists := userTextByCode[code]; exists {
		return text
	}
	return FallbackUserText
}
