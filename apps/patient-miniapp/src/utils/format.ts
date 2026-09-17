/**
 * 患者端数值格式化工具（T206 / T233 / T235）
 *
 * - formatPressureValue: 本地实现，monitor 热力图/单元格/hero 等压力值显示。
 *   T233：负值归零（物理上压力不可为负）。
 *
 * - formatAlertValue: T235 已迁移至 packages/shared-utils，此处 re-export。
 *   按 Alert.type 映射单位（N / % / min / 兜底）。
 */
export function formatPressureValue(val: number | undefined | null): string {
  if (val === undefined || val === null || isNaN(val)) return '--'
  return Math.max(0, val).toFixed(2)
}

// T235: 告警值格式化已抽到 shared-utils，三端共用。
export { formatAlertValue } from '@bracesync/shared-utils'
