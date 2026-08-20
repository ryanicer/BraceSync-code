// Package phone 手机号加密存储与脱敏展示（隐私合规 §3：未成年人健康数据关联手机号加密落库）
//
// 对齐 device-service internal/crypto 的 AES-256-GCM 模式：密文格式 nonce(12B)||ciphertext，
// 密钥为 64 位 hex（32 字节），经环境变量 PHONE_ENC_KEY 注入。
// phone_hash = SHA-256(明文手机号) hex，用于登录/查重（与 technicians.phone_hash 语义一致）。
package phone

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
