// T464 报错双通道（告警域）：错误码 → 面向终端用户的中文短句 + trace 回传
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

// HeaderRequestID 请求关联号的载体头：网关生成、逐跳透传。
const HeaderRequestID = "X-Request-Id"

// requestIDPattern 入站关联号形状白名单：防日志注入与超长值。
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// errorTrace 错误响应的定位通道：错误码沿用本域码表（T402 丁-2 保留了 400/500 两格的中间态取值），
// request_id 与同请求的服务端日志行同值，供用户反馈时反查。
type errorTrace struct {
	ErrorCode int    `json:"errorCode"`
	RequestID string `json:"requestId"`
}

// requestIDOf 取本请求关联号：优先用网关透传值，形状不合或缺失就地生成。
func requestIDOf(r *http.Request) string {
	rid := r.Header.Get(HeaderRequestID)
	if !requestIDPattern.MatchString(rid) {
		rid = newRequestID()
	}
	return rid
}

func newRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(buf[:])
}

// FallbackUserText 未在表内的错误码兜底：宁可给一句通用中文，也不让技术文本漏到用户面。
const FallbackUserText = "服务暂时不可用，请稍后重试；若持续失败请联系管理员并告知发生时间"

// userTextByCode 与 reject 的 code 入参同源（本域包内常量）。
var userTextByCode = map[int]string{
	codeInvalidParam:  "告警查询条件有误，请调整筛选条件后重试",
	codeInternalError: FallbackUserText,
	codeNotFound:      "未找到该条告警，请刷新告警列表后重试",
	codeForbidden:     "没有权限查看该患者的告警，请联系管理员确认账号权限",
	codeConflict:      "该告警已处理完成，无需重复处理",
}

// userText 取该错误码的用户面中文文案；查不到码走兜底，绝不返回技术文本。
func userText(code int) string {
	if text, exists := userTextByCode[code]; exists {
		return text
	}
	return FallbackUserText
}
