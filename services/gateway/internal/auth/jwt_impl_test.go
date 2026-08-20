// Package auth — ParseJWT 实现侧测试（T032；不与 Ella auth_test.go 重叠）
//
// 签发侧同构校验：构造 token 的方式与 user-service/internal/token（T030 #9）一致，
// 保证 gateway 校验与登录签发互通。
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testJWTSecret = "unit-test-jwt-secret"

// buildHS256Token 按 user-service token.Signer 同构方式构造 HS256 token
func buildHS256Token(t *testing.T, secret, headerJSON string, claims any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	signingInput := base64.RawURLEncoding.EncodeToString([]byte(headerJSON)) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func testClaims(exp int64) Claims {
	return Claims{Subject: "ADM001", Username: "admin", Name: "管理员", RoleID: "ROLE_ADMIN",
		IssuedAt: exp - 3600, ExpireAt: exp}
}

func TestParseJWT_Valid(t *testing.T) {
	now := time.Now()
	tok := buildHS256Token(t, testJWTSecret, `{"alg":"HS256","typ":"JWT"}`, testClaims(now.Add(time.Hour).Unix()))

	claims, err := ParseJWT(testJWTSecret, tok, now)
	require.NoError(t, err)
	assert.Equal(t, "ADM001", claims.Subject)
	assert.Equal(t, "ROLE_ADMIN", claims.RoleID)
}

func TestParseJWT_ExpireBoundary_EqualNow_Valid(t *testing.T) {
	now := time.Now()
	tok := buildHS256Token(t, testJWTSecret, `{"alg":"HS256","typ":"JWT"}`, testClaims(now.Unix()))

	claims, err := ParseJWT(testJWTSecret, tok, now)
	require.NoError(t, err, "exp == now 视为有效（对齐签发侧）")
	assert.Equal(t, "ADM001", claims.Subject)
}

func TestParseJWT_Expired(t *testing.T) {
	now := time.Now()
	tok := buildHS256Token(t, testJWTSecret, `{"alg":"HS256","typ":"JWT"}`, testClaims(now.Add(-time.Second).Unix()))

	_, err := ParseJWT(testJWTSecret, tok, now)
	assert.ErrorIs(t, err, ErrJWTExpired)
}

func TestParseJWT_WrongSecret_BadSign(t *testing.T) {
	now := time.Now()
	tok := buildHS256Token(t, "other-secret", `{"alg":"HS256","typ":"JWT"}`, testClaims(now.Add(time.Hour).Unix()))

	_, err := ParseJWT(testJWTSecret, tok, now)
	assert.ErrorIs(t, err, ErrJWTBadSign)
}

func TestParseJWT_TamperedPayload_BadSign(t *testing.T) {
	now := time.Now()
	tok := buildHS256Token(t, testJWTSecret, `{"alg":"HS256","typ":"JWT"}`, testClaims(now.Add(time.Hour).Unix()))

	// 篡改 payload 段（伪造提权 role）
	forged := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"ADM001","role":"ROLE_SUPER","exp":9999999999}`))
	tampered := tok[:len(tok)-43] + forged + tok[len(tok)-43:] // 保留原签名段
	_, err := ParseJWT(testJWTSecret, tampered, now)
	assert.ErrorIs(t, err, ErrJWTBadSign)
}

func TestParseJWT_Malformed(t *testing.T) {
	now := time.Now()
	cases := map[string]string{
		"两段":            "aaa.bbb",
		"单段":            "aaaa",
		"空串":            "",
		"header非base64": "!!!.bbb.ccc",
	}
	for name, tok := range cases {
		_, err := ParseJWT(testJWTSecret, tok, now)
		assert.ErrorIs(t, err, ErrJWTMalformed, name)
	}
}

func TestParseJWT_BadAlg(t *testing.T) {
	now := time.Now()
	for _, header := range []string{`{"alg":"none","typ":"JWT"}`, `{"alg":"RS256","typ":"JWT"}`, `{`} {
		tok := buildHS256Token(t, testJWTSecret, header, testClaims(now.Add(time.Hour).Unix()))
		_, err := ParseJWT(testJWTSecret, tok, now)
		assert.ErrorIs(t, err, ErrJWTAlg, "header=%s", header)
	}
}

func TestParseJWT_PayloadNotJSON_Malformed(t *testing.T) {
	signingInput := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte("not-json"))
	mac := hmac.New(sha256.New, []byte(testJWTSecret))
	mac.Write([]byte(signingInput))
	tok := signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	_, err := ParseJWT(testJWTSecret, tok, time.Now())
	assert.ErrorIs(t, err, ErrJWTMalformed)
}

func TestParseJWT_SignatureNotBase64_BadSign(t *testing.T) {
	tok := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(`{"exp":9999999999}`)) + ".!!!not-base64"

	_, err := ParseJWT(testJWTSecret, tok, time.Now())
	assert.ErrorIs(t, err, ErrJWTBadSign)
}

func TestParseJWT_TechClaims_TeamID(t *testing.T) {
	now := time.Now()
	claims := Claims{
		Subject: "T0001", Name: "技师老陈", RoleID: "technician", TeamID: "TEAM01",
		IssuedAt: now.Unix(), ExpireAt: now.Add(time.Hour).Unix(),
	}
	tok := buildHS256Token(t, testJWTSecret, `{"alg":"HS256","typ":"JWT"}`, claims)

	parsed, err := ParseJWT(testJWTSecret, tok, now)
	require.NoError(t, err)
	assert.Equal(t, "T0001", parsed.Subject)
	assert.Equal(t, "technician", parsed.RoleID)
	assert.Equal(t, "TEAM01", parsed.TeamID)
}

func TestParseJWT_PatientClaims_NoTeamID(t *testing.T) {
	now := time.Now()
	claims := Claims{
		Subject: "P20260001", Name: "患者小明", RoleID: "patient",
		IssuedAt: now.Unix(), ExpireAt: now.Add(time.Hour).Unix(),
	}
	tok := buildHS256Token(t, testJWTSecret, `{"alg":"HS256","typ":"JWT"}`, claims)

	parsed, err := ParseJWT(testJWTSecret, tok, now)
	require.NoError(t, err)
	assert.Equal(t, "P20260001", parsed.Subject)
	assert.Equal(t, "patient", parsed.RoleID)
	assert.Empty(t, parsed.TeamID)
}
