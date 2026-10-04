// Package main — gateway 统一鉴权中间件（T032，架构 §3.3）
//
// JWT 鉴权（/api/v1 路由组）：
//   - 验证 Authorization: Bearer <HS256 JWT>（user-service 登录签发，JWT_SECRET 共享），
//     缺失/非法/过期 → 401 统一响应体
//   - 白名单：POST /api/v1/auth/login（登录入口免鉴权）；/healthz 不入路由组；
//     设备上报走独立路由组 + 设备验签（不走 JWT，协议 §3）
//   - 通过后注入 X-User-Id / X-Role（架构 §5.2），并先剥离外部伪造的同名头
//
// 设备验签（设备域路由组）：
//   - X-Device-Id + X-Timestamp + X-Signature（HMAC-SHA256，对齐 T007/模拟器签名串）
//   - 密钥经 SecretProvider 查 device-service（未注册 → 20401/20404 拒绝）
//   - 验签通过后恢复请求体并注入 X-Device-Id 身份头（data-service 以此为准，不越权）
package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/gateway/internal/auth"
)

// gatewayAuth 鉴权依赖集合（setupRouter 组装，中间件共享）
type gatewayAuth struct {
	jwtSecret string // JWT_SECRET（T039-H1：空 = fail-closed 拒绝请求，main 启动即校验必填）
	verifier  *auth.DeviceSigVerifier
	secrets   SecretProvider
	now       func() time.Time
}

// newGatewayAuth 组装鉴权依赖（secrets 为 nil 时设备验签路由组不可用）
func newGatewayAuth(jwtSecret string, secrets SecretProvider) *gatewayAuth {
	return &gatewayAuth{
		jwtSecret: jwtSecret,
		verifier:  &auth.DeviceSigVerifier{},
		secrets:   secrets,
		now:       time.Now,
	}
}

// abortJSON 统一响应体中止（架构 §3.5 code/message）。
// T464：message 入参是技术文本（如 "missing X-Device-Id header"），一律只进服务端日志；
// 响应体给用户的是按码映射的中文短句 + trace（错误码 + 请求关联号）。
func abortJSON(c *gin.Context, status, code int, message string) {
	requestID := requestIDOf(c)
	logTechnical(c, code, status, message, requestID)
	c.AbortWithStatusJSON(status, gin.H{
		"code":    code,
		"message": userText(code),
		"data":    nil,
		"trace":   errorTrace{ErrorCode: code, RequestID: requestID},
	})
}

// authWhitelisted JWT 免鉴权白名单（方法 + 完整路径）
// T030 admin 登录 + T037 技师/患者登录接口本身免 JWT
func authWhitelisted(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	switch path {
	case "/api/v1/auth/login", "/api/v1/tech/login", "/api/v1/patient/login", "/api/v1/patient/wx-login":
		return true
	}
	return false
}

// jwtAuth JWT 鉴权中间件：挂载于 /api/v1 路由组（白名单放行，其余强制校验）
func jwtAuth(agt *gatewayAuth) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 剥离外部伪造的身份头（架构 §5.2：身份头仅由网关注入）
		c.Request.Header.Del("X-User-Id")
		c.Request.Header.Del("X-Role")

		if authWhitelisted(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		if agt.jwtSecret == "" {
			// T039-H1（T023-H1 fail-open 修复）：JWT_SECRET 未配置时 fail-closed，
			// 非白名单请求一律 401，绝不降级放行（main 启动即校验，此为纵深兜底）
			log.Error().Msg("JWT_SECRET not configured: rejecting request (fail-closed)")
			abortJSON(c, http.StatusUnauthorized, http.StatusUnauthorized,
				"unauthorized: gateway JWT secret not configured")
			return
		}

		const bearerPrefix = "Bearer "
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, bearerPrefix) {
			abortJSON(c, http.StatusUnauthorized, http.StatusUnauthorized,
				"unauthorized: missing or malformed Authorization header")
			return
		}
		claims, err := auth.ParseJWT(agt.jwtSecret, strings.TrimPrefix(header, bearerPrefix), agt.now())
		if err != nil {
			abortJSON(c, http.StatusUnauthorized, http.StatusUnauthorized,
				"unauthorized: invalid or expired token")
			return
		}

		c.Request.Header.Set("X-User-Id", claims.Subject)
		c.Request.Header.Set("X-Role", claims.RoleID)
		c.Next()
	}
}

// maxDeviceBodyBytes 设备上报请求体上限：批量补传 ≤100 帧 × ~600B + 冗余（4MB 足够）
const maxDeviceBodyBytes = 4 << 20

// deviceForensics T564 取证面：20402（时钟异常）拒签时挂在 gin 上下文上的读数。
//
// 现网被拒的那批请求，X-Timestamp 原值在边缘面（nginx combined 无 $http_*）与网关
// 既有日志行里都取不到，所以这一格只能由网关在拒签那一刻自己写出来（详见 T564 报告）。
// 记 X-Timestamp 原值，加上 X-Timestamp / X-Nonce / X-Signature 三个头名的在场与否；
// 签名与 nonce 的值本身是凭据形状，一律不落日志面。
type deviceForensics struct {
	deviceID         string
	timestamp        string // X-Timestamp 原值（经 timestampForLog 过一道形状闸）
	skewSec          int64
	skewKnown        bool // false = 没测出来（戳不可解析），此时日志行不出 skew_sec
	timestampPresent bool
	noncePresent     bool
	signaturePresent bool
}

const ctxKeyDeviceForensics = "t564_device_forensics"

// maxRawTimestampLogLen X-Timestamp 原值可整段入日志的长度上限（正常是 10 位 Unix 秒）。
const maxRawTimestampLogLen = 32

// timestampForLog 原值入日志前的一道形状闸：可打印 ASCII 且不长才原样落，
// 否则只报长度——既挡住超长头值灌日志面，也挡住把非法字节截进 JSON 日志行
// （那会破坏整条日志行，比少一个读数糟得多）。
func timestampForLog(raw string) string {
	if len(raw) > maxRawTimestampLogLen {
		return "<len=" + strconv.Itoa(len(raw)) + ">"
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < 0x20 || raw[i] == 0x7f {
			return "<unprintable len=" + strconv.Itoa(len(raw)) + ">"
		}
	}
	return raw
}

// deviceSigAuth 设备验签中间件（默认 ±SignatureTimeWindow 时间窗）：挂载于上报路由组。
// 失败统一 HTTP 401，body code 区分 20401（签名）/20402（时间窗）/20404（未注册，协议 §4.4）。
func deviceSigAuth(agt *gatewayAuth) gin.HandlerFunc {
	return deviceSigAuthWindow(agt, auth.SignatureTimeWindow)
}

// deviceSigAuthWindow 设备验签中间件，时间窗按分钟数入参（T550：校时端点用更宽的窗）。
// 只有窗口这一维可变——密钥查询、body 上限、HMAC 比对与注入逻辑全部同一条路径。
func deviceSigAuthWindow(agt *gatewayAuth, windowMinutes int) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID := c.GetHeader("X-Device-Id")
		if deviceID == "" {
			abortJSON(c, http.StatusUnauthorized, 20401, "missing X-Device-Id header")
			return
		}

		secret, err := agt.secrets.GetDeviceSecret(c.Request.Context(), deviceID)
		if errors.Is(err, ErrDeviceNotRegistered) {
			abortJSON(c, http.StatusUnauthorized, 20404, "device not registered")
			return
		}
		if err != nil {
			log.Warn().Err(err).Str("device", deviceID).Msg("device secret lookup failed")
			abortJSON(c, http.StatusBadGateway, 502, "device-service unavailable")
			return
		}

		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxDeviceBodyBytes+1))
		if err != nil {
			abortJSON(c, http.StatusUnauthorized, 20401, "read request body failed")
			return
		}
		if len(body) > maxDeviceBodyBytes {
			abortJSON(c, http.StatusUnauthorized, 20401, "request body too large")
			return
		}

		// T067：X-Nonce 参与 6 行签名串（硬件清单 §2.2）；缺失则 nonce=""，与客户端不一致 → 20401。
		// VerifyNonce 防重放仍恒放行（TODO：Redis 接入后实现）。
		res := agt.verifier.VerifySignatureWindowed(c.Request.Method, c.Request.URL.Path, string(body),
			c.GetHeader("X-Timestamp"), c.GetHeader("X-Signature"), deviceID, secret, c.GetHeader("X-Nonce"), agt.now(), windowMinutes)
		if res == nil || !res.Valid {
			code := 20401
			if res != nil && res.ErrorCode != "" {
				if parsed, convErr := strconv.Atoi(res.ErrorCode); convErr == nil {
					code = parsed
				}
			}
			msg := "signature verification failed"
			if res != nil && res.ErrorMessage != "" {
				msg = res.ErrorMessage
			}
			// T564：只有超窗那一支补取证面——这一支的拒签理由本身就是「设备说的时刻
			// 对不上」，把那条时刻与偏差写进同一条日志行，才谈得上判它是阶跃还是漂移。
			// 挂在上下文上由 logTechnical 统一出，响应体一侧的形状不变（见 T464 双通道）。
			if code == 20402 {
				rawTS := c.GetHeader("X-Timestamp")
				c.Set(ctxKeyDeviceForensics, deviceForensics{
					deviceID:         deviceID,
					timestamp:        timestampForLog(rawTS),
					skewSec:          res.SkewSec,
					skewKnown:        res.SkewMeasured,
					timestampPresent: rawTS != "",
					noncePresent:     c.GetHeader("X-Nonce") != "",
					signaturePresent: c.GetHeader("X-Signature") != "",
				})
			}
			abortJSON(c, http.StatusUnauthorized, code, msg)
			return
		}

		// 验签通过：恢复请求体供反代转发，注入设备身份头（data-service 以此为准）
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.ContentLength = int64(len(body))
		c.Request.Header.Set("X-Device-Id", deviceID)
		c.Next()
	}
}
