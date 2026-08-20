// Package token 实现侧测试：HS256 JWT 签发/校验（T030 #9 登录契约支撑）
package token

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignAndVerify(t *testing.T) {
	s, err := NewSigner("unit-secret", time.Hour)
	require.NoError(t, err)

	tk, err := s.Sign("A0001", "ops_admin", "运营小张", "ROLE_ADMIN")
	require.NoError(t, err)
	assert.Equal(t, 3, len(strings.Split(tk, ".")))

	claims, err := s.Verify(tk)
	require.NoError(t, err)
	assert.Equal(t, "A0001", claims.Subject)
	assert.Equal(t, "ops_admin", claims.Username)
	assert.Equal(t, "运营小张", claims.Name)
	assert.Equal(t, "ROLE_ADMIN", claims.RoleID)
	assert.Greater(t, claims.ExpireAt, claims.IssuedAt)
}

func TestVerifyRejects(t *testing.T) {
	s, err := NewSigner("unit-secret", time.Hour)
	require.NoError(t, err)
	tk, _ := s.Sign("A1", "u", "n", "ROLE_CS")

	// 篡改载荷 → 签名不匹配
	parts := strings.Split(tk, ".")
	tampered := parts[0] + "." + parts[1] + "x." + parts[2]
	_, err = s.Verify(tampered)
	assert.True(t, errors.Is(err, ErrBadSign))

	// 结构非法
	_, err = s.Verify("only.two")
	assert.True(t, errors.Is(err, ErrMalformed))
	_, err = s.Verify("a.b.!!!")
	assert.True(t, errors.Is(err, ErrBadSign))

	// 密钥不一致
	other, _ := NewSigner("other-secret", time.Hour)
	_, err = other.Verify(tk)
	assert.True(t, errors.Is(err, ErrBadSign))
}

func TestVerifyExpired(t *testing.T) {
	s, err := NewSigner("unit-secret", -time.Hour) // 负 TTL：签发即过期
	require.NoError(t, err)
	tk, err := s.Sign("A1", "u", "n", "ROLE_CS")
	require.NoError(t, err)
	_, err = s.Verify(tk)
	assert.True(t, errors.Is(err, ErrExpired))
}

func TestNewSignerRequiresSecret(t *testing.T) {
	_, err := NewSigner("", time.Hour)
	assert.True(t, errors.Is(err, ErrNoSecret))
}

func TestSignWithTeam(t *testing.T) {
	s, err := NewSigner("unit-secret", time.Hour)
	require.NoError(t, err)

	// 技师 token：含 team_id
	tk, err := s.SignWithTeam("T0001", "技师老陈", "TEAM01", "technician")
	require.NoError(t, err)
	claims, err := s.Verify(tk)
	require.NoError(t, err)
	assert.Equal(t, "T0001", claims.Subject)
	assert.Equal(t, "技师老陈", claims.Name)
	assert.Equal(t, "technician", claims.RoleID)
	assert.Equal(t, "TEAM01", claims.TeamID)
	assert.Empty(t, claims.Username) // 技师/患者无 username

	// 患者 token：team_id 为空（omitempty 不出现在 JSON 中）
	tk2, err := s.SignWithTeam("P20260001", "患者小明", "", "patient")
	require.NoError(t, err)
	claims2, err := s.Verify(tk2)
	require.NoError(t, err)
	assert.Equal(t, "P20260001", claims2.Subject)
	assert.Equal(t, "patient", claims2.RoleID)
	assert.Empty(t, claims2.TeamID)
}
