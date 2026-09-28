// T464 错误响应双通道（用户域）：request_id 关联号 + trace 回传 + 技术细节只进日志
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
)

// HeaderRequestID 请求关联号的载体头：网关生成、逐跳透传，服务侧同时回写响应头。
const HeaderRequestID = "X-Request-Id"

const ctxKeyRequestID = "t464_request_id"

// requestIDPattern 入站关联号形状白名单：允许客户端自报便于全链路对齐，但要防日志注入与超长值。
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// errorTrace 错误响应的定位通道：错误码沿用 T402「域号 + 0 + HTTP 三位」，
// request_id 与同请求的服务端日志行同值，供用户反馈时反查。
type errorTrace struct {
	ErrorCode int    `json:"errorCode"`
	RequestID string `json:"requestId"`
}

// requestIDMiddleware 需尽早挂载（先于任何会返回错误的中间件），
// 使鉴权/限流阶段的错误也带得上关联号。
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(HeaderRequestID)
		if !requestIDPattern.MatchString(rid) {
			rid = newRequestID()
		}
		c.Set(ctxKeyRequestID, rid)
		c.Header(HeaderRequestID, rid)
		c.Next()
	}
}

// requestIDOf 取本请求关联号；未经中间件（如直接调用 handler 的单测）就地生成并固化。
func requestIDOf(c *gin.Context) string {
	if v, exists := c.Get(ctxKeyRequestID); exists {
		if rid, ok := v.(string); ok && rid != "" {
			return rid
		}
	}
	rid := newRequestID()
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

// logTechnical 把原始技术文本写进服务端日志（响应体里给用户的则是同码的中文短句）。
// 走 ctxLogger：与本请求其它日志同一个 logger，便于按 request_id 归并。
func logTechnical(c *gin.Context, code, httpStatus int, detail, requestID string) {
	ctxLogger(c).Warn().
		Str("request_id", requestID).
		Int("code", code).
		Int("http_status", httpStatus).
		Str("method", c.Request.Method).
		Str("path", c.Request.URL.Path).
		Msg(detail)
}

// writeErrorJSON 错误响应统一出口：非 0 码 + 中文 message + trace。
// detail 是原始技术文本（只进日志）；data 供机器可读附带字段（如 bindToken/phoneToken）。
// 不经 model.AppError 的手写响应分支走这里，避免绕开双通道。
func writeErrorJSON(c *gin.Context, httpStatus, code int, detail string, data any) {
	requestID := requestIDOf(c)
	logTechnical(c, code, httpStatus, detail, requestID)
	c.JSON(httpStatus, jsonResp{
		Code:    code,
		Message: model.UserText(code),
		Data:    data,
		Trace:   &errorTrace{ErrorCode: code, RequestID: requestID},
	})
}
