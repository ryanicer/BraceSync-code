// Package service — 设备验签密钥查询（T032：gateway 设备验签密钥来源）
//
// 架构边界：device_secret 写归 device-service（devices 表 owner，§4.2），
// gateway 验签需要明文密钥时经 /internal/devices/:deviceId/secret 服务间
// 直连获取（§5.2 内部信任链，不经网关、不对前端暴露）。
package service

import (
	"context"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// GetDeviceSecret 解密返回设备 HMAC 验签密钥（仅 internal 通道使用）。
// 设备未注册 → 20404；密文损坏/密钥版本异常按系统错误。
func (s *DeviceService) GetDeviceSecret(ctx context.Context, deviceID string) (string, *model.AppError) {
	if deviceID == "" {
		return "", model.ErrInvalidParam("device_id is required")
	}
	dev, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return "", mapRepoErr(err, model.ErrNotFound("device %q not registered", deviceID))
	}
	secret, err := s.enc.Decrypt(dev.DeviceSecretEnc)
	if err != nil {
		return "", model.ErrInternal("decrypt device secret: %v", err)
	}
	return string(secret), nil
}
