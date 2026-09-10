import { request, USE_MOCK } from '../utils/request'

/**
 * 配网会话密钥的内存缓存（T119 方案 A）。
 *
 * 背景：provision key 由 HKDF(device_secret + device_id) 确定性派生，
 * 同一设备永远同一把；expires_in_sec 仅为建议窗口。失败重试时无需重新申领，
 * 复用本地 key 即可绕过后端 60s reissue 限流（20429）。
 *
 * 安全约束（等同 Secret 处理）：
 * - 仅存内存，不落盘、不进 localStorage、不写日志、不进自报
 * - 按 expires_in_sec 失效；换设备天然隔离（Map key = deviceId）
 */
interface ProvisionKeyCacheEntry {
  provision_key_hex: string
  /** 绝对过期时间戳（ms） */
  expiresAt: number
}

const provisionKeyCache = new Map<string, ProvisionKeyCacheEntry>()

/** 清空缓存（切换安装会话 / 测试用）。 */
export function clearProvisionKeyCache(): void {
  provisionKeyCache.clear()
}

/**
 * 申领配网会话密钥（真实：POST /api/v1/devices/:deviceId/provision-key，T067 已实现）
 * @returns provision_key_hex（HKDF-SHA256 派生 16B → 32hex），有效期 expires_in_sec 秒
 */
export async function getProvisionKey(
  deviceId: string
): Promise<{ provision_key_hex: string; expires_in_sec: number }> {
  // 命中未过期缓存：复用本地 key，不调 /provision-key
  const cached = provisionKeyCache.get(deviceId)
  if (cached && cached.expiresAt > Date.now()) {
    const remainingSec = Math.max(1, Math.ceil((cached.expiresAt - Date.now()) / 1000))
    return { provision_key_hex: cached.provision_key_hex, expires_in_sec: remainingSec }
  }

  let result: { provision_key_hex: string; expires_in_sec: number }
  if (USE_MOCK) {
    // T089-MOCK: 本地生成假 32hex，T067/T068 已上线后走真实
    await new Promise((r) => setTimeout(r, 250))
    const key = 'a'.repeat(32 - 8) + Math.random().toString(16).slice(2, 10)
    result = { provision_key_hex: key, expires_in_sec: 300 }
  } else {
    result = await request<{ provision_key_hex: string; expires_in_sec: number }>({
      url: `/api/v1/devices/${deviceId}/provision-key`,
      method: 'POST',
      data: {},
    })
  }

  provisionKeyCache.set(deviceId, {
    provision_key_hex: result.provision_key_hex,
    expiresAt: Date.now() + result.expires_in_sec * 1000,
  })
  return result
}
