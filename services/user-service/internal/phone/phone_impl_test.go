// Package phone 实现侧测试：AES-GCM 加解密、脱敏、哈希（隐私合规落库支撑）
package phone

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCipher(testKey)
	require.NoError(t, err)

	enc, err := c.Encrypt("13800001111")
	require.NoError(t, err)
	assert.NotContains(t, string(enc), "13800001111") // 密文不含明文

	plain, err := c.Decrypt(enc)
	require.NoError(t, err)
	assert.Equal(t, "13800001111", plain)

	// 每次加密 nonce 随机，密文不同
	enc2, _ := c.Encrypt("13800001111")
	assert.NotEqual(t, enc, enc2)
}

func TestDecryptErrors(t *testing.T) {
	c, err := NewCipher(testKey)
	require.NoError(t, err)
	_, err = c.Decrypt([]byte("short"))
	assert.Error(t, err)
	_, err = c.Decrypt(make([]byte, 32)) // 全零伪造密文
	assert.Error(t, err)
}

func TestNewCipherKeyValidation(t *testing.T) {
	_, err := NewCipher("")
	assert.ErrorIs(t, err, ErrNoKey)
	_, err = NewCipher("zz") // 非 hex
	assert.Error(t, err)
	_, err = NewCipher("0123") // 长度不足 32 字节
	assert.Error(t, err)
}

func TestMask(t *testing.T) {
	assert.Equal(t, "138****1111", Mask("13800001111"))
	assert.Equal(t, "a***c", Mask("abc"))
	assert.Equal(t, "***", Mask("a"))
	assert.Equal(t, "***", Mask(""))
}

func TestMaskedFallback(t *testing.T) {
	c, err := NewCipher(testKey)
	require.NoError(t, err)
	assert.Equal(t, "", c.Masked(nil))
	assert.Equal(t, "***", c.Masked([]byte("corrupted")))
	enc, _ := c.Encrypt("13800001111")
	assert.Equal(t, "138****1111", c.Masked(enc))
}

func TestHashDeterministic(t *testing.T) {
	assert.Equal(t, Hash("13800001111"), Hash("13800001111"))
	assert.NotEqual(t, Hash("13800001111"), Hash("13800001112"))
	assert.Len(t, Hash("x"), 64) // SHA-256 hex，对齐 CHAR(64) 列
}
