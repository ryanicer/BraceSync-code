// T464 错误响应双通道（文件域）：request_id 关联号 + trace 回传 + 技术细节只进日志
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// HeaderRequestID 请求关联号的载体头：网关生成、逐跳透传，服务侧同时回写响应头。
const HeaderRequestID = "X-Request-Id"

const ctxKeyRequestID = "t464_request_id"

// requestIDPattern 入站关联号形状白名单：允许客户端自报便于全链路对齐，但要防日志注入与超长值。
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// errorTrace 错误响应的定位通道：错误码沿用 T402「域号 + 0 + HTTP 三位」及其域内序号段，
// request_id 与同请求的服务端日志行同值，供用户反馈时反查。
type errorTrace struct {
	ErrorCode int    `json:"errorCode"`
	RequestID string `json:"requestId"`
}

// requestIDMiddleware 需尽早挂载（先于任何会返回错误的中间件），
// 使鉴权阶段的错误也带得上关联号。
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
		if rid, isString := v.(string); isString && rid != "" {
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
func logTechnical(c *gin.Context, code, httpStatus int, detail, requestID string) {
	log.Warn().
		Str("request_id", requestID).
		Int("code", code).
		Int("http_status", httpStatus).
		Str("method", c.Request.Method).
		Str("path", c.Request.URL.Path).
		Msg(detail)
}
