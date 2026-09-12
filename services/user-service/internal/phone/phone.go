// Package phone 手机号加密存储与脱敏展示（隐私合规 §3：未成年人健康数据关联手机号加密落库）
//
// 对齐 device-service internal/crypto 的 AES-256-GCM 模式：密文格式 nonce(12B)||ciphertext，
// 密钥为 64 位 hex（32 字节），经环境变量 PHONE_ENC_KEY 注入。
// phone_hash = SHA-256(明文手机号) hex，用于登录/查重（与 technicians.phone_hash 语义一致）。
//
// T156：哈希前必须先规范化（NormalizeHash）。原因：微信 phonenumber.getPhoneNumber 在不同
// AppID/客户端/SDK 版本下返回的 purePhoneNumber 可能带 "+86" / "86" 前缀或不可见字符
// （零宽空格 / BOM），与 DB 中按明文 "18607101885" 算的 hash 不等，触发 10602 patient_not_found。
// 规范化策略：trim 前后空白 → 去 +86 / 86 前缀 → 去所有非数字字符 → SHA-256。
// 兼容输入空串/全空字符（返回 sha256("")，由上层判定）。
package phone

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrNoKey 加密密钥未配置（PHONE_ENC_KEY 缺失时写入路径返回）
var ErrNoKey = errors.New("phone encryption key not configured")

// Cipher 手机号 AES-256-GCM 加密器
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher 以 64 位 hex 密钥构造加密器；密钥非法返回错误
func NewCipher(hexKey string) (*Cipher, error) {
	if hexKey == "" {
		return nil, ErrNoKey
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("PHONE_ENC_KEY must be 64-char hex (32 bytes)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt 加密明文手机号 → nonce||ciphertext
func (c *Cipher) Encrypt(plain string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, []byte(plain), nil), nil
}

// Decrypt 解密 nonce||ciphertext；密文损坏返回错误
func (c *Cipher) Decrypt(data []byte) (string, error) {
	ns := c.aead.NonceSize()
	if len(data) < ns {
		return "", errors.New("ciphertext too short")
	}
	plain, err := c.aead.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Masked 解密失败或明文过短时返回 "***"，避免泄漏密文
func (c *Cipher) Masked(enc []byte) string {
	if len(enc) == 0 {
		return ""
	}
	plain, err := c.Decrypt(enc)
	if err != nil {
		return "***"
	}
	return Mask(plain)
}

// Mask 手机号脱敏（138****5678）；非 11 位号码首尾各留 1 位，过短返回 "***"
func Mask(plain string) string {
	r := []rune(plain)
	switch {
	case len(r) >= 11:
		return string(r[:3]) + "****" + string(r[len(r)-4:])
	case len(r) >= 2:
		return string(r[:1]) + "***" + string(r[len(r)-1:])
	default:
		return "***"
	}
}

// Hash 手机号查重哈希（SHA-256 hex，对齐 technicians.phone_hash CHAR(64)）
func Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// Normalize 手机号规范化（不哈希）：
//   - 去除前后空白（含 \t \n \r 空格 中文空格 零宽字符）
//   - 去前缀 +86 / 86（兼容微信 getPhoneNumber 返回带国家码的边界）
//   - 过滤掉所有非数字字符（避免 BOM / 零宽空格 / 全角数字等污染）
//
// 仅当输入为有效手机号时返回 11 位数字字符串；否则返回原 trim 结果（保持兼容）。
func Normalize(plain string) string {
	// 去前后空白（包括零宽 \u200b、零宽非连接符 \u200c、不间断空格 \u00a0 等）
	s := strings.TrimSpace(plain)
	s = strings.Trim(s, "\u200b\u200c\u200d\ufeff\u00a0")

	// 去前缀：+86 / +86- / 86 / 86-
	switch {
	case strings.HasPrefix(s, "+86"):
		s = strings.TrimLeft(s[3:], "- ")
	case strings.HasPrefix(s, "86"):
		s = strings.TrimLeft(s[2:], "- ")
	}

	// 过滤非数字（保留 ASCII 0-9；全角数字由调用方自行转）
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizeHash 先规范化再哈希。bind-phone 等外部输入走此路径；
// 对内已知干净的 phoneHash（如 verifyPhoneToken 取出的 claims.phone_hash）可直接用 Hash。
func NormalizeHash(plain string) string {
	return Hash(Normalize(plain))
}
