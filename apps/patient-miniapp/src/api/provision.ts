/**
 * 配网密钥管理（T182 对齐技师端）
 *
 * POST /api/v1/devices/:deviceId/provision-key
 * - 技师/管理员可签发；患者 JWT 会被 403 → 真机实测确认，撞缺口上报 Winner
 * - 内存缓存 + 按 expires_in_sec 失效（T119）
 *
 * 返回值字段名对齐技师端：{ provision_key_hex, expires_in_sec }
 */

import { request } from '../utils/request'

interface ProvisionKeyResp {
  provision_key_hex: string
  expires_in_sec: number
}

interface CachedEntry {
  provision_key_hex: string
  expiresAt: number
}

// 内存缓存：deviceId → { provision_key_hex, expiresAt }
const cache = new Map<string, CachedEntry>()

/**
 * 获取配网密钥。
 * 优先内存缓存；缓存未命中时请求后端。
 */
export async function getProvisionKey(deviceId: string): Promise<ProvisionKeyResp> {
  // 1. 命中缓存
  const cached = cache.get(deviceId)
  if (cached && cached.expiresAt > Date.now()) {
    const remainingSec = Math.max(1, Math.ceil((cached.expiresAt - Date.now()) / 1000))
    return { provision_key_hex: cached.provision_key_hex, expires_in_sec: remainingSec }
  }

  // 2. 请求后端
  const resp = await request<ProvisionKeyResp>({
    url: `/api/v1/devices/${deviceId}/provision-key`,
    method: 'POST',
  })

  const entry: CachedEntry = {
    provision_key_hex: resp.provision_key_hex,
    expiresAt: Date.now() + resp.expires_in_sec * 1000,
  }
  cache.set(deviceId, entry)

  return resp
}

/** 清除缓存（配网失败时可调用） */
export function clearProvisionKeyCache(deviceId?: string) {
  if (deviceId) {
    cache.delete(deviceId)
  } else {
    cache.clear()
  }
}
