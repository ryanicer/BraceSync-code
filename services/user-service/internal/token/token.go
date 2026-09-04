// Package token 运营后台 HS256 JWT 签发与校验（T030 #9 登录契约）
//
// 不引入第三方 JWT 库：HS256 = base64url(header).base64url(payload).base64url(HMAC-SHA256)，
// 与标准 RFC 7519 兼容，gateway Phase 1 JWT 中间件可直接复用本包校验。
// JWT_SECRET 经环境变量注入，与 gateway 共享（scripts/deploy/docker-compose.yml）。
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 常见校验失败原因
var (
	ErrMalformed = errors.New("malformed token")
	ErrBadSign   = errors.New("invalid token signature")
	ErrExpired   = errors.New("token expired")
	ErrNoSecret  = errors.New("JWT_SECRET not configured")
)

// Claims token 载荷（sub=用户 ID；role 供 gateway RBAC 使用；team_id 技师端可选）
type Claims struct {
	Subject  string `json:"sub"`
	Username string `json:"username"`
	Name     string `json:"name"`
	RoleID   string `json:"role"`
	TeamID   string `json:"team_id,omitempty"` // T037：技师 JWT 载荷（admin/patient 为空）
	IssuedAt int64  `json:"iat"`
	ExpireAt int64  `json:"exp"`
}

// Signer HS256 签发/校验器
type Signer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewSigner 构造 Signer；secret 为空返回 ErrNoSecret
func NewSigner(secret string, ttl time.Duration) (*Signer, error) {
	if secret == "" {
		return nil, ErrNoSecret
	}
	return &Signer{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

// CloneWithTTL 派生一个复用同一 secret 但 ttl 不同的 Signer。
// 用于用同一 JWT_SECRET 签发不同有效期的 token（如 bindToken 30min vs 正式 JWT 8h）。
func (s *Signer) CloneWithTTL(ttl time.Duration) *Signer {
	return &Signer{secret: s.secret, ttl: ttl, now: s.now}
}

// header HS256 固定头（JSON 序列化保证字段序稳定）
const headerJSON = `{"alg":"HS256","typ":"JWT"}`

func b64(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// Sign 签发 token：Claims 的 iat/exp 由本方法填充（admin 域登录用）
func (s *Signer) Sign(subject, username, name, roleID string) (string, error) {
	return s.signClaims(Claims{
		Subject:  subject,
		Username: username,
		Name:     name,
		RoleID:   roleID,
	})
}

// SignWithTeam 签发含 team_id 的 token（T037 技师/患者登录用；患者 teamID 传空串）
func (s *Signer) SignWithTeam(subject, name, teamID, roleID string) (string, error) {
	return s.signClaims(Claims{
		Subject: subject,
		Name:    name,
		RoleID:  roleID,
		TeamID:  teamID,
	})
}

// signClaims 组装载荷并签名（iat/exp 由本方法填充）
func (s *Signer) signClaims(claims Claims) (string, error) {
	now := s.now()
	claims.IssuedAt = now.Unix()
	claims.ExpireAt = now.Add(s.ttl).Unix()
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64([]byte(headerJSON)) + "." + b64(payload)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64(mac.Sum(nil)), nil
}

// Verify 校验签名与有效期，返回载荷；now 边界：exp 等于当前时刻视为有效
func (s *Signer) Verify(tokenStr string) (*Claims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrMalformed
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(want, mac.Sum(nil)) {
		return nil, ErrBadSign
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrMalformed
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrMalformed
	}
	if claims.ExpireAt < s.now().Unix() {
		return nil, fmt.Errorf("%w: exp=%d", ErrExpired, claims.ExpireAt)
	}
	return &claims, nil
}
