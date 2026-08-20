// Package crypto 单元测试：AES-GCM 加解密（架构 §5.2 加密列）
package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 测试密钥（32 字节 hex）：仅测试用途，非生产密钥
const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestNewEncryptor_InvalidKey(t *testing.T) {
	for _, bad := range []string{"", "abcd", "xyz-not-hex-0123456789abcdef0123456789abcdef0123456789abcdef0123"} {
		_, err := NewEncryptor(bad)
		assert.ErrorIs(t, err, ErrInvalidKey, "key %q should be rejected", bad)
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	enc, err := NewEncryptor(testKey)
	require.NoError(t, err)

	plaintext := []byte("device_secret_material_64hex")
	ct, err := enc.Encrypt(plaintext)
	require.NoError(t, err)
	assert.False(t, bytes.Contains(ct, plaintext), "密文中不得出现明文")

	got, err := enc.Decrypt(ct)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

func TestEncrypt_NonceRandomized(t *testing.T) {
	enc, err := NewEncryptor(testKey)
	require.NoError(t, err)

	ct1, err := enc.Encrypt([]byte("same"))
	require.NoError(t, err)
	ct2, err := enc.Encrypt([]byte("same"))
	require.NoError(t, err)
	assert.False(t, bytes.Equal(ct1, ct2), "同明文两次加密密文必须不同（随机 nonce）")
}

func TestDecrypt_TamperAndWrongKey(t *testing.T) {
	enc, err := NewEncryptor(testKey)
	require.NoError(t, err)
	ct, err := enc.Encrypt([]byte("secret"))
	require.NoError(t, err)

	// 篡改末字节（GCM tag 校验失败）
	tampered := append([]byte(nil), ct...)
	tampered[len(tampered)-1] ^= 0xff
	_, err = enc.Decrypt(tampered)
	assert.Error(t, err)

	// 错误密钥
	other, err := NewEncryptor("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	_, err = other.Decrypt(ct)
	assert.Error(t, err)

	// 密文过短
	_, err = enc.Decrypt(ct[:3])
	assert.ErrorIs(t, err, ErrCiphertextTooShort)
}

func TestRandomSecret(t *testing.T) {
	s1, err := RandomSecret(32)
	require.NoError(t, err)
	s2, err := RandomSecret(32)
	require.NoError(t, err)
	assert.Len(t, s1, 64, "32 字节 hex 编码长度=64")
	assert.NotEqual(t, s1, s2)
}
