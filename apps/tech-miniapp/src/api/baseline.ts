import { request, USE_MOCK } from '../utils/request'

/**
 * 保存基线（真实：POST /baselines，后端 T084 未实现 → mock 先行）
 * P0-1 时序约束：installId 必填（install 先行）
 * @returns baselineId 已保存基线的编号
 */
export async function saveBaseline(
  installId: string,
  offsetValues: number[],
  deviceId?: string
): Promise<{ baselineId: string }> {
  if (!installId) throw new Error('installId 必填（install 先行）')
  if (USE_MOCK) {
    // T089-MOCK: 等后端 T084 POST /baselines 就绪后切换
    await new Promise((r) => setTimeout(r, 500))
    const suffix = Math.random().toString(36).slice(2, 8)
    return { baselineId: `BSL-${Date.now().toString().slice(-6)}-${suffix}` }
  }
  return request<{ baselineId: string }>({
    url: '/api/v1/baselines',
    method: 'POST',
    data: { installId, offsetValues, deviceId },
  })
}
