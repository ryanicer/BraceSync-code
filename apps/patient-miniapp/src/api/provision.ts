/**
 * 配网密钥管理（T182 对齐技师端）
 *
 * POST /api/v1/devices/:deviceId/provision-key
 * - 技师/管理员可签发；患者 JWT 会被 403 → 真机实测确认，撞缺口上报 Winner
 * - 内存缓存 + 按 expires_in_sec 失效（T119）
 *
 * 返回值字段名对齐技师端：{ provision_key_hex, expires_in_sec }
 */

import { request, USE_MOCK } from '../utils/request'

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
 * USE_MOCK=true 时（H5 开发 / E2E）直接返回假 key，绕过 request() throw。
 */
export async function getProvisionKey(deviceId: string): Promise<ProvisionKeyResp> {
  // 命中缓存
  const cached = cache.get(deviceId)
  if (cached && cached.expiresAt > Date.now()) {
    const remainingSec = Math.max(1, Math.ceil((cached.expiresAt - Date.now()) / 1000))
    return { provision_key_hex: cached.provision_key_hex, expires_in_sec: remainingSec }
  }

  let result: ProvisionKeyResp

  // H5 环境（dev/E2E）强制 mock：患者端 vite 默认 USE_MOCK=false 与技师端相反，
  // 条件编译确保 H5 不打真 request（dev 无登录态会 401 炸整条链路）。
  // #ifdef H5
  {
    await new Promise((r) => setTimeout(r, 250))
    const key = 'a'.repeat(24) + Math.random().toString(16).slice(2, 10)
    result = { provision_key_hex: key, expires_in_sec: 300 }
  }
  // #endif
  // #ifndef H5
  if (USE_MOCK) {
    await new Promise((r) => setTimeout(r, 250))
    const key = 'a'.repeat(24) + Math.random().toString(16).slice(2, 10)
    result = { provision_key_hex: key, expires_in_sec: 300 }
  } else {
    result = await request<ProvisionKeyResp>({
      url: `/api/v1/devices/${deviceId}/provision-key`,
      method: 'POST',
    })
  }
  // #endif

  cache.set(deviceId, {
    provision_key_hex: result.provision_key_hex,
    expiresAt: Date.now() + result.expires_in_sec * 1000,
  })

  return result
}

/** 清除缓存（配网失败时可调用） */
export function clearProvisionKeyCache(deviceId?: string) {
  if (deviceId) {
    cache.delete(deviceId)
  } else {
    cache.clear()
  }
}
