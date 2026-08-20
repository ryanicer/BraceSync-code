// Package crypto 敏感列 AES-GCM 加密（架构 §5.2 加密列方案）
//
// 用于 device_secret 等密钥材料的落库加密：
// 密文格式 = 12B 随机 nonce || GCM 密文（含 16B tag），BYTEA 存储。
// 一期密钥经环境变量注入（32 字节 hex）；生产密钥托管演进为 KMS 时只需替换 KeySource。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// KeySize AES-256 密钥字节数
const KeySize = 32

// 错误哨兵
var (
	ErrInvalidKey         = errors.New("crypto: key must be 64 hex chars (32 bytes)")
	ErrCiphertextTooShort = errors.New("crypto: ciphertext shorter than nonce")
)

// Encryptor AES-256-GCM 加解密器
type Encryptor struct {
	aead cipher.AEAD
}

// NewEncryptor 从 64 位 hex 字符串（32 字节）构造 Encryptor
func NewEncryptor(hexKey string) (*Encryptor, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: gcm: %w", err)
	}
	return &Encryptor{aead: aead}, nil
}

// Encrypt 加密明文，返回 nonce||密文；nonce 每次随机，同明文密文不同
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: rand nonce: %w", err)
	}
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt 解密 Encrypt 产出的密文；篡改/错钥返回错误（GCM tag 校验）
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	ns := e.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, ErrCiphertextTooShort
	}
	plaintext, err := e.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}

// RandomSecret 生成 n 字节加密安全随机数（hex 编码），用于出厂 device_secret
func RandomSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto: rand secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
