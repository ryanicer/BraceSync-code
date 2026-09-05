// Package service — 配网密钥派生（T067，硬件清单 §2.1；T091 安全收紧）
//
// GetProvisionKey 复用 GetDeviceSecret（校验 device 存在 → 20404、解密 secret），
// 再 HKDF-SHA256 派生 16B（→32 hex）。
//
// T091 安全收紧：
//   - 重发间隔防护：同设备在 provisionInterval 内重复领卡 → 20429（防脚本刷领）。
//   - 审计日志：每次领卡（成功/拒绝）记录 user_id/device_id/时间/结果。
//   - expires_in_sec 语义：密钥本身是 HKDF 确定性派生（同设备同 secret 永不变），
//     "过期"并非密钥失效，而是领卡窗口受限——网关 JWT+RBAC+限流 + 本服务重发间隔
//     共同约束谁、何时能领卡。硬件侧拿到即用，密钥本身不失效。
//
// 降级说明（第 5 条领卡资格）：绑定关系为 device→patient，无 device→tech 直接关联，
// 且 Store 无"按 device 查 install"方法，故资格校验降级为"已注册即可 + 审计日志
// 记录领卡人/设备/时间"，admin 可领任意设备（运维兜底）。
package service

import (
	"context"
	"encoding/hex"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// ProvisionKeyExpiresInSec 配网密钥有效期秒（硬件清单 §2.1；固定 300）。
//
// 语义说明（T091）：此值仅告知调用方"建议在该窗口内使用"，密钥本身由 HKDF 确定性
// 派生（同 device+secret 恒定），不会随时间失效。真正的"过期"= 领卡窗口受限：
// gateway JWT+RBAC 决定谁能领、限流决定频率、本服务重发间隔决定同设备最短领卡周期。
// 硬件侧拿到即用，无需校验密钥时效。
const ProvisionKeyExpiresInSec = 300

// defaultProvisionReissueInterval 默认同设备最短重发间隔（秒）
const defaultProvisionReissueIntervalSec = 60

// loadProvisionInterval 从 env PROVISION_REISSUE_INTERVAL_SEC 读取间隔（非法/空回退默认）
func loadProvisionInterval() time.Duration {
	sec := defaultProvisionReissueIntervalSec
	if v := os.Getenv("PROVISION_REISSUE_INTERVAL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			sec = n
		}
	}
	return time.Duration(sec) * time.Second
}

// GetProvisionKey 返回 32-hex 配网密钥（HKDF-SHA256 派生 16B）。
// 复用 GetDeviceSecret 校验 device 存在（未注册 → 20404）并解密 secret。
//
// T091：
//   - operatorID 为网关注入的 X-User-Id（领卡人），用于审计日志。
//   - 同设备在 provisionInterval 内重复领卡 → 20429（防脚本刷领）。
//   - 领卡成功/拒绝均写结构化审计日志。
func (s *DeviceService) GetProvisionKey(ctx context.Context, deviceID, operatorID string) (string, *model.AppError) {
	now := s.now()

	// 重发间隔校验（防脚本刷领同设备密钥）
	s.provisionMu.Lock()
	if last, ok := s.lastProvision[deviceID]; ok && now.Sub(last) < s.provisionInterval {
		s.provisionMu.Unlock()
		log.Warn().Str("user_id", operatorID).Str("device_id", deviceID).
			Str("result", "reissue_too_soon").Time("last_issued_at", last).
			Msg("provision-key reissue within interval denied")
		return "", model.ErrTooMany("provision key reissue too soon, retry after %ds",
			int(s.provisionInterval.Seconds()))
	}
	s.lastProvision[deviceID] = now
	s.provisionMu.Unlock()

	secret, appErr := s.GetDeviceSecret(ctx, deviceID)
	if appErr != nil {
		// 未注册等错误：回滚时间戳记录，避免错误请求占用间隔窗口
		s.provisionMu.Lock()
		delete(s.lastProvision, deviceID)
		s.provisionMu.Unlock()
		log.Warn().Str("user_id", operatorID).Str("device_id", deviceID).
			Str("result", "device_error").Int("code", appErr.Code).
			Msg("provision-key request denied")
		return "", appErr
	}

	key, err := crypto.DeriveProvisionKey([]byte(secret), deviceID)
	if err != nil {
		s.provisionMu.Lock()
		delete(s.lastProvision, deviceID)
		s.provisionMu.Unlock()
		log.Error().Str("user_id", operatorID).Str("device_id", deviceID).
			Str("result", "derive_failed").Err(err).Msg("provision-key derive failed")
		return "", model.ErrInternal("derive provision key: %v", err)
	}

	log.Info().Str("user_id", operatorID).Str("device_id", deviceID).
		Str("result", "issued").Msg("provision-key issued")
	return hex.EncodeToString(key), nil
}
