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

// TestNormalize T156：手机号规范化各分支
func TestNormalize(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		output string
	}{
		{"纯数字", "13800001111", "13800001111"},
		{"带 +86 前缀", "+8613800001111", "13800001111"},
		{"带 +86- 前缀", "+86-13800001111", "13800001111"},
		{"带 86 前缀（无加号）", "8613800001111", "13800001111"},
		{"带 86- 前缀", "86-13800001111", "13800001111"},
		{"前后空格", "  13800001111  ", "13800001111"},
		{"前后 Tab/换行", "\t13800001111\n", "13800001111"},
		{"零宽空格", "\u200b13800001111\u200d", "13800001111"},
		{"BOM", "\ufeff13800001111", "13800001111"},
		{"不间断空格", "\u00a013800001111\u00a0", "13800001111"},
		{"混合 +86 + 空格", "  +86 13800001111  ", "13800001111"},
		{"非数字字符混入", "138-0000-1111", "13800001111"},
		{"空字符串", "", ""},
		{"全空格", "   ", ""},
		{"全非数字", "abc-def", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.output, Normalize(tc.input))
		})
	}
}

// TestNormalizeHash T156：normalize + hash 一致性，覆盖真实场景
func TestNormalizeHash(t *testing.T) {
	// 核心契约：无论微信返回什么形式，"18607101885" 和它的变体必须得到同一个 hash
	want := Hash("18607101885")
	cases := []string{
		"18607101885",
		"+8618607101885",
		"+86-18607101885",
		"8618607101885",
		"86-18607101885",
		"  18607101885  ",
		"\u200b18607101885\u200d",
		"\ufeff18607101885",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			assert.Equal(t, want, NormalizeHash(c), "NormalizeHash(%q) should equal Hash(%q)", c, "18607101885")
		})
	}

	// 负例：11 位数字但不同号 → hash 必须不同
	assert.NotEqual(t, NormalizeHash("18607101885"), NormalizeHash("18607101886"))
	// 边界：空串返回 sha256("")，不 panic
	assert.Len(t, NormalizeHash(""), 64)
}
