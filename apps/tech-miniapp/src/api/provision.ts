import { request, USE_MOCK } from '../utils/request'

/**
 * 申领配网会话密钥（真实：POST /api/v1/devices/:deviceId/provision-key，T067 已实现）
 * @returns provision_key_hex（HKDF-SHA256 派生 16B → 32hex），有效期 expires_in_sec 秒
 */
export async function getProvisionKey(
  deviceId: string
): Promise<{ provision_key_hex: string; expires_in_sec: number }> {
  if (USE_MOCK) {
    // T089-MOCK: 本地生成假 32hex，T067/T068 已上线后走真实
    await new Promise((r) => setTimeout(r, 250))
    const key =
      'a'.repeat(32 - 8) + Math.random().toString(16).slice(2, 10)
    return { provision_key_hex: key, expires_in_sec: 300 }
  }
  return request<{ provision_key_hex: string; expires_in_sec: number }>({
    url: `/api/v1/devices/${deviceId}/provision-key`,
    method: 'POST',
    data: {},
  })
}
