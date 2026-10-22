import { request, USE_MOCK } from '../utils/request'

/**
 * 保存基线（真实：POST /baselines，后端 T084 未实现 → mock 先行）
 * P0-1 时序约束：installId 必填（install 先行）
 *
 * 🔴 T733 方案A（单位链）：`offsetValues` 的**值是 raw mN 原值**（采集层与存储层零换算，
 *   ÷1000 只发生在展示层 `nFromMn`）。T719 拆掉了后端入口的 ÷1000，库里存的就是 mN，
 *   本接口若在客户端先除一次，基线会落进 N 空间、与设备帧（mN）对不齐——这正是 T733 要修的错位。
 *   字段名不改（wire 契约归后端定）。
 * @param offsetValues 20 点零点偏移，单位 mN（原样上报，不做任何换算）
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
