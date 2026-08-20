// Package auth — HS256 JWT 校验（T032：gateway 统一鉴权中间件）
//
// 与 user-service internal/token（T030 #9 登录签发）同构：
// HS256 = base64url(header).base64url(payload).base64url(HMAC-SHA256)，
// JWT_SECRET 经环境变量在 user-service 与 gateway 间共享（不入库）。
// gateway 只做校验不签发；Claims 字段序与 user-service token.Claims 一致。
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// JWT 校验失败原因
var (
	ErrJWTMalformed = errors.New("malformed token")
	ErrJWTBadSign   = errors.New("invalid token signature")
	ErrJWTExpired   = errors.New("token expired")
	ErrJWTAlg       = errors.New("unsupported token algorithm")
)

// Claims token 载荷（镜像 user-service/internal/token.Claims）
type Claims struct {
	Subject  string `json:"sub"`
	Username string `json:"username"`
	Name     string `json:"name"`
	RoleID   string `json:"role"`
	TeamID   string `json:"team_id,omitempty"` // T037：技师 JWT 载荷（admin/patient 为空）
	IssuedAt int64  `json:"iat"`
	ExpireAt int64  `json:"exp"`
}

// jwtHeader 仅校验 alg 字段（HS256 之外一律拒绝，防 alg=none 降级攻击）
type jwtHeader struct {
	Alg string `json:"alg"`
}

// ParseJWT 校验 HS256 签名与有效期，返回载荷。
// now 为校验基准时刻（测试可注入）；exp 等于当前时刻视为有效（对齐签发侧）。
func ParseJWT(secret, tokenStr string, now time.Time) (*Claims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrJWTMalformed
	}

	headRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrJWTMalformed
	}
	var head jwtHeader
	if err := json.Unmarshal(headRaw, &head); err != nil || head.Alg != "HS256" {
		return nil, ErrJWTAlg
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(want, mac.Sum(nil)) {
		return nil, ErrJWTBadSign
	}

	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrJWTMalformed
	}
	var claims Claims
	if err := json.Unmarshal(payloadRaw, &claims); err != nil {
		return nil, ErrJWTMalformed
	}
	if claims.ExpireAt < now.Unix() {
		return nil, ErrJWTExpired
	}
	return &claims, nil
}
