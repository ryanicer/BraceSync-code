import { ref } from 'vue'
import { request } from './request'

/**
 * T579：佩戴期望时长的唯一取值入口（两页共用，兜底常量只准住在这一处）。
 *
 * 下发载体（winner 2026-10-05 14:32 现读三条）：GET /api/v1/patient/profile 的 data 顶层
 * 同名标量 `dailyWearTargetHours`，JSON number，与 patientId / name / status 同层，不嵌套。
 * 真源 sys_configs.wear_target_hours，后端读不到键或值非法时自退默认 22，故 profile 面上
 * 这一枚恒在场；前端的 22 只兜「请求本身没成功」那一格（未登录 / bind 态被网关 403 / 网络失败）。
 */
export const FALLBACK_TARGET_HOURS = 22

const MIN_TARGET_HOURS = 1
const MAX_TARGET_HOURS = 24

export const wearTargetHours = ref(FALLBACK_TARGET_HOURS)

/** 合法域与后端同宽（validateSettings 收 1..24 的 float64，允许小数）；其余一律算「后端没给」 */
export function normalizeTargetHours(raw: unknown): number | null {
  const n = typeof raw === 'number' ? raw : typeof raw === 'string' ? Number(raw) : Number.NaN
  if (!Number.isFinite(n) || n < MIN_TARGET_HOURS || n > MAX_TARGET_HOURS) return null
  return n
}

/** 取下发字段并落到共用读数；任何取不到都退兜底，不向上抛（页面不该因为口径拿不到而空屏） */
export async function loadWearTarget(): Promise<number> {
  let raw: unknown = null
  try {
    const data = await request<{ dailyWearTargetHours?: number | null }>({
      url: '/api/v1/patient/profile',
      method: 'GET',
    })
    raw = data?.dailyWearTargetHours
  } catch {
    raw = null
  }
  wearTargetHours.value = normalizeTargetHours(raw) ?? FALLBACK_TARGET_HOURS
  return wearTargetHours.value
}
