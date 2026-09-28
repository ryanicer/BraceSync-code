// 设备域用户面文案表（T464 报错双通道）：错误码 → 面向终端用户的中文短句。
//
// 与 Message 的分工：model.AppError.Message 是技术文本（字段名、ID、SQL 片段），
// 只写服务端日志；响应体 message 一律由本表给出，用户据此知道「发生了什么 + 该做什么」。
package model

// FallbackUserText 未在表内的错误码兜底：宁可给一句通用中文，也不让技术文本漏到用户面。
const FallbackUserText = "服务暂时不可用，请稍后重试；若持续失败请联系管理员并告知发生时间"

var userTextByCode = map[int]string{
	CodeInvalidParam:    "设备信息有误，请重新扫描设备后重试",
	CodeNotFound:        "未找到该设备或对应记录，请确认设备编号后重试",
	CodeForbidden:       "该设备不属于当前账号可操作范围，请联系技师或管理员确认绑定关系",
	CodeConflict:        "设备绑定状态与当前操作不一致，请刷新设备列表后重试",
	CodeTooMany:         "操作过于频繁，请稍等片刻后重试",
	CodeUserResNotFound: "未找到关联的患者或技师档案，请联系管理员核对账号信息",
	CodeInternal:        FallbackUserText,
}

// UserText 取该错误码的用户面中文文案；查不到码走兜底，绝不返回技术文本。
func UserText(code int) string {
	if text, exists := userTextByCode[code]; exists {
		return text
	}
	return FallbackUserText
}
