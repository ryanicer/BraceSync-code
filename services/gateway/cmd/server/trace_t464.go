// T464 报错双通道（网关）：request_id 生成与透传 + trace 回传 + 技术细节只进日志。
//
// 网关是外部流量的第一跳：关联号在这里生成（或采信客户端自报的合法形状），
// 写进响应头 + 注入转发请求头，使各后端服务与网关自身的日志行能按同一 request_id 归并。
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// HeaderRequestID 请求关联号的载体头：网关生成、逐跳透传给后端服务。
const HeaderRequestID = "X-Request-Id"

const ctxKeyRequestID = "t464_request_id"

// requestIDPattern 入站关联号形状白名单：允许客户端自报便于全链路对齐，但要防日志注入与超长值。
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// errorTrace 错误响应的定位通道：errorCode 与响应体 code 同值（网关侧取值见各 abortJSON 调用点），
// requestId 与同请求的服务端日志行同值，供用户反馈时反查。
type errorTrace struct {
	ErrorCode int    `json:"errorCode"`
	RequestID string `json:"requestId"`
}

// requestIDMiddleware 挂在最外层：鉴权/限流/路由阶段的拒绝都带得上关联号。
// 反向代理转发的是 c.Request 的头，故这里 Set 即等于逐跳透传。
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(HeaderRequestID)
		if !requestIDPattern.MatchString(rid) {
			rid = newRequestID()
		}
		c.Request.Header.Set(HeaderRequestID, rid)
		c.Set(ctxKeyRequestID, rid)
		c.Header(HeaderRequestID, rid)
		c.Next()
	}
}

// requestIDOf 取本请求关联号；未经中间件（如直接调用中间件的单测）就地生成并固化。
func requestIDOf(c *gin.Context) string {
	if v, exists := c.Get(ctxKeyRequestID); exists {
		if rid, isString := v.(string); isString && rid != "" {
			return rid
		}
	}
	rid := newRequestID()
	if c.Request != nil && c.Request.Header != nil {
		c.Request.Header.Set(HeaderRequestID, rid)
	}
	c.Set(ctxKeyRequestID, rid)
	c.Header(HeaderRequestID, rid)
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

// userTextByCode 键与 abortJSON 的 code 入参同源：网关侧多为裸 HTTP 状态数字，
// 另有设备验签段 20401/20404 与授权范围段 40301（T402 未收口面，本卡不动码表）。
var userTextByCode = map[int]string{
	401:   "登录状态已失效或身份未通过校验，请重新登录后再试",
	403:   "没有权限访问该功能，请联系管理员确认账号权限",
	404:   "该功能接口暂不可用，请刷新页面后重试",
	429:   "操作过于频繁，请稍后重试",
	502:   "后端服务暂时不可用，请稍后重试；若持续失败请联系管理员",
	20401: "设备身份校验未通过，请重新扫描设备后再试",
	20404: "该设备尚未注册，请先在管理端完成设备注册",
	40301: "当前登录方式不支持该操作，请重新登录后再试",
}

// userText 取该错误码的用户面中文文案；查不到码走兜底，绝不返回技术文本。
func userText(code int) string {
	if text, exists := userTextByCode[code]; exists {
		return text
	}
	return FallbackUserText
}

// logTechnical 把原始技术文本写进服务端日志（响应体里给用户的则是同码的中文短句）。
func logTechnical(c *gin.Context, code, httpStatus int, detail, requestID string) {
	log.Warn().
		Str("request_id", requestID).
		Int("code", code).
		Int("http_status", httpStatus).
		Str("method", c.Request.Method).
		Str("path", c.Request.URL.Path).
		Msg(detail)
}

// requestIDFromRequest 反向代理 ErrorHandler 一侧的取号（那边只有 *http.Request，无 gin 上下文）。
func requestIDFromRequest(r *http.Request) string {
	rid := r.Header.Get(HeaderRequestID)
	if !requestIDPattern.MatchString(rid) {
		rid = newRequestID()
	}
	return rid
}

// writeProxy502Body 上游不可达时网关自己的兜底响应体。
// 原 message 是「{serviceName} unavailable」这类英文技术文本，现改为中文短句 + trace；
// 服务名保留在 data.upstream 这一机器可读字段里，保住「502 来自网关兜底 vs 上游主动返 502」的取证判据。
func writeProxy502Body(w http.ResponseWriter, requestID, serviceName string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set(HeaderRequestID, requestID)
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(gin.H{
		"code":    http.StatusBadGateway,
		"message": userText(http.StatusBadGateway),
		"data":    gin.H{"upstream": serviceName},
		"trace":   errorTrace{ErrorCode: http.StatusBadGateway, RequestID: requestID},
	})
}
