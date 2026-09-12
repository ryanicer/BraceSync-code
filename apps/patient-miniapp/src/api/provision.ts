/**
 * 配网密钥管理（T094b）
 *
 * POST /api/v1/devices/:deviceId/provision-key
 * - 技师/管理员可签发；患者 JWT 会被 403（后端 RBAC）
 * - 60s 内存缓存 + 节流（T119）
 */

import { request } from '../utils/request'

interface ProvisionKeyResp {
  provision_key_hex: string
  expires_in: number
  seq: number
}

interface CachedEntry {
  key: string
  expiresAt: number
}

// 内存缓存：deviceId → { key, expiresAt }
const cache = new Map<string, CachedEntry>()
// 节流：最近一次请求时间戳
const lastRequestAt = new Map<string, number>()
const THROTTLE_MS = 3000 // 3s 最小间隔
const CACHE_TTL_MS = 60_000 // 60s 缓存

function nowMs(): number {
  return Date.now()
}

/**
 * 获取配网密钥。
 * 优先 60s 内存缓存；缓存未命中时请求后端，且 3s 内不重复请求。
 */
export async function getProvisionKey(deviceId: string): Promise<{ key: string; seq: number }> {
  // 1. 命中缓存
  const cached = cache.get(deviceId)
  if (cached && cached.expiresAt > nowMs()) {
    return { key: cached.key, seq: 0 }
  }

  // 2. 节流检查
  const lastReq = lastRequestAt.get(deviceId) || 0
  if (nowMs() - lastReq < THROTTLE_MS) {
    // 仍返回缓存（即使过期也复用，避免 429）
    if (cached) return { key: cached.key, seq: 0 }
  }

  lastRequestAt.set(deviceId, nowMs())

  const resp = await request<ProvisionKeyResp>({
    url: `/api/v1/devices/${deviceId}/provision-key`,
    method: 'POST',
  })

  const entry: CachedEntry = {
    key: resp.provision_key_hex,
    expiresAt: nowMs() + Math.min(resp.expires_in * 1000, CACHE_TTL_MS),
  }
  cache.set(deviceId, entry)

  return { key: resp.provision_key_hex, seq: resp.seq || 0 }
}

/** 清除缓存（配网失败时可调用） */
export function clearProvisionCache(deviceId?: string) {
  if (deviceId) {
    cache.delete(deviceId)
    lastRequestAt.delete(deviceId)
  } else {
    cache.clear()
    lastRequestAt.clear()
  }
}
