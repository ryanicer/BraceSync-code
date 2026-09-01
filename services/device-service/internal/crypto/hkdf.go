// Package crypto — HKDF-SHA256 配网密钥派生（T067，硬件清单 §2.1）
//
// DeriveProvisionKey 按 RFC 5869 HKDF-SHA256 从 device_secret 派生 16B 配网密钥：
//
//	ikm   = device_secret（32B）
//	salt  = nil（RFC 5869：salt 未提供时用 HashLen 零字节，即 32B 零）
//	info  = "provision" + device_id
//	L     = 16（128 bit）
//
// 派生为确定性函数：相同 (secret, device_id) → 相同输出；不同 device_id 或 secret → 不同输出。
package crypto

import (
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/hkdf"
)

// DeriveProvisionKey HKDF-SHA256 派生 16B 配网密钥（硬件清单 §2.1）。
// 返回 16 字节；调用方 hex.EncodeToString 得 32 hex 字符。
func DeriveProvisionKey(deviceSecret []byte, deviceID string) ([]byte, error) {
	info := []byte("provision" + deviceID)
	r := hkdf.New(sha256.New, deviceSecret, nil, info)
	key := make([]byte, 16)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}
