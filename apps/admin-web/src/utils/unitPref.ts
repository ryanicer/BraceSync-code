// T513 双单位档位记忆（admin 侧，裁定 e = 本地持久化）。
// 🔴 只写 localStorage，不写任何服务端字段、不进任何请求体 —— PRD §7A.2.1 四.8 明令不动
// patient_preferences，kPa 也不落库（Boss 裁定 4「仅展示层换算」）。
import { DEFAULT_PRESSURE_UNIT, pickStartUnit, type PressureUnit } from '@bracesync/shared-utils'

// 键名沿本页 admin_token / admin_user 的前缀族
const UNIT_KEY = 'admin_monitor_unit'

export function readStoredUnit(): PressureUnit {
  if (typeof window === 'undefined') return DEFAULT_PRESSURE_UNIT
  try {
    return pickStartUnit(window.localStorage.getItem(UNIT_KEY))
  } catch {
    // 隐私模式 / 存储被禁用：本次会话仍可用，只是记不住，回落默认档 N
    return DEFAULT_PRESSURE_UNIT
  }
}

export function persistUnit(unit: PressureUnit): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(UNIT_KEY, unit)
  } catch {
    // 写失败只影响下次起版档，当前页面的切换照样生效
  }
}

/** 仅供用例断言键名，不在页面里用 */
export const MONITOR_UNIT_STORAGE_KEY = UNIT_KEY
