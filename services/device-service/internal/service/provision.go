// Package service — 配网密钥派生（T067，硬件清单 §2.1）
//
// GetProvisionKey 复用 GetDeviceSecret（校验 device 存在 → 20404、解密 secret），
// 再 HKDF-SHA256 派生 16B（→32 hex）。鉴权收紧与过期 enforcement 留 TODO（联调优先）。
package service

import (
	"context"
	"encoding/hex"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// ProvisionKeyExpiresInSec 配网密钥有效期秒（硬件清单 §2.1；联调期固定 300）。
// TODO(T068)：真实过期 enforcement——联调对密钥真实过期容忍（硬件侧拿到即用）。
const ProvisionKeyExpiresInSec = 300

// GetProvisionKey 返回 32-hex 配网密钥（HKDF-SHA256 派生 16B）。
// 复用 GetDeviceSecret 校验 device 存在（未注册 → 20404）并解密 secret。
// TODO(T068)：鉴权收紧（本次联调不强制 JWT，清单未定义）。
func (s *DeviceService) GetProvisionKey(ctx context.Context, deviceID string) (string, *model.AppError) {
	secret, appErr := s.GetDeviceSecret(ctx, deviceID)
	if appErr != nil {
		return "", appErr
	}
	key, err := crypto.DeriveProvisionKey([]byte(secret), deviceID)
	if err != nil {
		return "", model.ErrInternal("derive provision key: %v", err)
	}
	return hex.EncodeToString(key), nil
}
