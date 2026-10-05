/**
 * T579 佩戴期望时长同源（wearing / anomaly 两页唯一取值入口）
 *
 * 真源是后端下发字段 dailyWearTargetHours（sys_configs.wear_target_hours，下发腿在 T576 甲案）。
 * 页面一律经本模块取数，不得再各写一份目标时长常量；兜底那颗也只住这里（T579 判据 3）。
 * 兜底值与后端 msg-service DefaultWearTargetHours 同口径，改一处即两页同步。
 */
import { ref } from 'vue'

/** 后端字段未到齐时的兜底目标时长（小时）：后端现值口径 22 */
export const FALLBACK_TARGET_HOURS = 22

// 合法区间与 admin-web 系统配置页那条 :min=1 / :max=24 同口径；越界一律按「未下发」处理
const MIN_TARGET_HOURS = 1
const MAX_TARGET_HOURS = 24

export const wearTargetHours = ref(FALLBACK_TARGET_HOURS)

function pickField(row: unknown): unknown {
  return (row as { dailyWearTargetHours?: unknown } | null | undefined)?.dailyWearTargetHours
}

/** 只认 [1,24] 的整数；0、空串、null、超上限都算「后端没给」 */
export function normalizeTargetHours(raw: unknown): number | null {
  const n = typeof raw === 'number' ? raw : typeof raw === 'string' ? Number(raw) : Number.NaN
  if (!Number.isInteger(n) || n < MIN_TARGET_HOURS || n > MAX_TARGET_HOURS) return null
  return n
}

/**
 * 从页面已经取到的响应面上读这枚字段：数组（daily-wear 的 data）自最近一天往回找第一个有效值，
 * 对象（profile 那类响应）直接取顶层。取不到 = null，由调用方落兜底。
 */
export function readTargetHours(source: unknown): number | null {
  if (Array.isArray(source)) {
    for (let i = source.length - 1; i >= 0; i--) {
      const h = normalizeTargetHours(pickField(source[i]))
      if (h != null) return h
    }
    return null
  }
  return normalizeTargetHours(pickField(source))
}

/** 把一页的响应面喂进来，刷新两页共用的那枚读数并返回它 */
export function applyTargetHours(source: unknown): number {
  wearTargetHours.value = readTargetHours(source) ?? FALLBACK_TARGET_HOURS
  return wearTargetHours.value
}
