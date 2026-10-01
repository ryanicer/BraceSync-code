/**
 * 患者端实时监测页的显示档位（T513，N / kPa）。
 *
 * 档位记忆 = **本地持久化**（Boss 2026-09-30 08:58:12 裁定 e），🔴 不动
 * `patient_preferences` schema、不新增任何服务端「单位」字段（PRD §7A.2.1 四.8）。
 * 键名沿用本页 token 类键的前缀风格；跨设备不保证一致是本地存储的既有边界。
 */
import { pickStartUnit, DEFAULT_PRESSURE_UNIT, type PressureUnit } from '@bracesync/shared-utils'

const UNIT_KEY = 'bracesync_monitor_unit'

export function readStoredUnit(): PressureUnit {
  try {
    return pickStartUnit(uni.getStorageSync(UNIT_KEY))
  } catch {
    return DEFAULT_PRESSURE_UNIT
  }
}

export function persistUnit(unit: PressureUnit): void {
  try {
    uni.setStorageSync(UNIT_KEY, unit)
  } catch {
    // 存储写失败只影响下次起版档，当前档已生效，不打断读帧展示
  }
}
