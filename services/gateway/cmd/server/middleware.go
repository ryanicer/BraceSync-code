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

// abortJSON 统一响应体中止（架构 §3.5 code/message）
func abortJSON(c *gin.Context, status, code int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"code": code, "message": message, "data": nil})
}

// authWhitelisted JWT 免鉴权白名单（方法 + 完整路径）
// T030 admin 登录 + T037 技师/患者登录接口本身免 JWT
func authWhitelisted(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	switch path {
	case "/api/v1/auth/login", "/api/v1/tech/login", "/api/v1/patient/login":
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

// deviceSigAuth 设备验签中间件：挂载于设备域路由组（单帧上报/批量补传/校时）。
// 失败统一 HTTP 401，body code 区分 20401（签名）/20402（时间窗）/20404（未注册，协议 §4.4）。
func deviceSigAuth(agt *gatewayAuth) gin.HandlerFunc {
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

		res := agt.verifier.VerifySignature(c.Request.Method, c.Request.URL.Path, string(body),
			c.GetHeader("X-Timestamp"), c.GetHeader("X-Signature"), deviceID, secret, agt.now())
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
