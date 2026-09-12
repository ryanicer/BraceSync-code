package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// phoneToken 用途常量（claims.purpose 必须等于此值）
const phoneTokenPurpose = "phone_token"

// phoneTokenTTL phoneToken 有效期 7 天（设计源 T088-V2 §5.2）
const phoneTokenTTL = 7 * 24 * time.Hour

// phoneTokenHeader JWT 头（与测试 signPhoneToken 同构，字段序固定）
const phoneTokenHeader = `{"alg":"HS256","typ":"JWT"}`

// phoneTokenClaims phoneToken 载荷
type phoneTokenClaims struct {
	Purpose   string `json:"purpose"`
	PhoneHash string `json:"phone_hash"`
	OpenID    string `json:"openid"`
	Iat       int64  `json:"iat"`
	Exp       int64  `json:"exp"`
}

// issuePhoneToken 签发 phoneToken（HS256，7d）。
// secret 为空时返回空串（fail-closed：handler 层据此返回 500）。
func issuePhoneToken(secret, phoneHash, openID string, now time.Time) string {
	if secret == "" {
		return ""
	}
	iat := now.Unix()
	exp := now.Add(phoneTokenTTL).Unix()
	payload, _ := json.Marshal(phoneTokenClaims{
		Purpose:   phoneTokenPurpose,
		PhoneHash: phoneHash,
		OpenID:    openID,
		Iat:       iat,
		Exp:       exp,
	})
	signingInput := base64.RawURLEncoding.EncodeToString([]byte(phoneTokenHeader)) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifyPhoneToken 校验 phoneToken：签名 / purpose / openid / 过期。
// expectedOpenID 为绑定态 JWT sub（openid）；不匹配返回错误。
func verifyPhoneToken(secret, tokenStr, expectedOpenID string, now time.Time) (*phoneTokenClaims, error) {
	if secret == "" {
		return nil, errors.New("phone token secret not configured")
	}
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed phone token")
	}
	// 签名校验
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(want, mac.Sum(nil)) {
		return nil, errors.New("invalid phone token signature")
	}
	// 载荷解析
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("malformed phone token payload")
	}
	var claims phoneTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.New("malformed phone token claims")
	}
	// purpose 校验
	if claims.Purpose != phoneTokenPurpose {
		return nil, errors.New("invalid phone token purpose")
	}
	// openid 一致性校验
	if claims.OpenID != expectedOpenID {
		return nil, errors.New("phone token openid mismatch")
	}
	// 过期校验（exp <= now 视为过期）
	if claims.Exp <= now.Unix() {
		return nil, errors.New("phone token expired")
	}
	return &claims, nil
}
