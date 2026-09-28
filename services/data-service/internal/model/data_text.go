// 数据域用户面文案表（T464 报错双通道）：错误码 → 面向终端用户的中文短句。
//
// 与 Message 的分工：model.AppError.Message 是技术文本（字段名、设备号、SQL 片段），
// 只写服务端日志；响应体 message 一律由本表给出。
// 上报类错误（204xx/30400）的读者是固件与网关侧运维，中文短句同样够用，
// 且卡内点名的那条「设备信息不匹配」即本表 30400 一行。
package model

// FallbackUserText 未在表内的错误码兜底：宁可给一句通用中文，也不让技术文本漏到用户面。
const FallbackUserText = "服务暂时不可用，请稍后重试；若持续失败请联系管理员并告知发生时间"

var userTextByCode = map[int]string{
	CodeInvalidParam:     "上报数据格式不正确，请确认采集点数量后重新上报",
	CodeBadTimestamp:     "数据时间超出可上报范围，请检查设备时间后重新上报",
	CodeDeviceIDMismatch: "设备信息不匹配，请重新扫描设备后重试",
	CodeDeviceNotFound:   "该设备尚未注册，请先在管理端完成设备注册",
	CodeDeviceUnbound:    "该设备还未绑定患者，请先完成绑定后再上报数据",
	CodeRateLimited:      "上报过于频繁，请稍候片刻后重试",
	CodePatientNotFound:  "未找到该患者的档案，请联系客服核对患者信息",
	CodeQueryParam:       "查询条件有误，请调整筛选条件后重试",
	CodeForbidden:        "没有权限查看该患者的数据，请联系管理员确认账号权限",
	CodeInternal:         FallbackUserText,
}

// UserText 取该错误码的用户面中文文案；查不到码走兜底，绝不返回技术文本。
func UserText(code int) string {
	if text, exists := userTextByCode[code]; exists {
		return text
	}
	return FallbackUserText
}
