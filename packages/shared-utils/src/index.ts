/**
 * BraceSync shared utility functions.
 */

/** Format pressure value with unit (default: N) */
export function formatPressure(value: number, decimals = 1): string {
  return `${value.toFixed(decimals)} N`
}

/** Format wear duration in minutes to human-readable string */
export function formatWearDuration(minutes: number): string {
  if (minutes < 60) return `${minutes} 分钟`
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  return m > 0 ? `${h} 小时 ${m} 分钟` : `${h} 小时`
}

/** Calculate pressure change percentage between two frames */
export function pressureChangeRate(prev: number, curr: number): number {
  if (prev === 0) return curr > 0 ? Infinity : 0
  return Math.abs((curr - prev) / prev) * 100
}

/** Debounce utility for UI interactions */
export function debounce<T extends (...args: unknown[]) => void>(
  fn: T,
  delay: number
): (...args: Parameters<T>) => void {
  let timer: ReturnType<typeof setTimeout>
  return (...args: Parameters<T>) => {
    clearTimeout(timer)
    timer = setTimeout(() => fn(...args), delay)
  }
}

/** Check if a pressure value exceeds threshold */
export function isPressureHigh(value: number, threshold: number): boolean {
  return value > threshold
}

/**
 * T235：按 Alert.type 格式化 actualValue / thresholdValue 显示口径（跨三端共享）
 *
 * Alert.actualValue 是多义字段，含义随 type 变：
 *   pressure_high        → 压力（N）
 *   pressure_fluctuation → 百分比（后端 engine.go 明确用 %%）
 *   sensor_drift         → 空载读数（N，可能负，显示层归零）
 *   wear_interrupt       → 分钟数
 *   wear_duration_short  → 小时数（设计稿 告警管理.html:250「18h / 6.5h」）
 *   未知 type            → 原样数字，不硬编码单位
 *
 * @param type Alert.type
 * @param value actualValue 或 thresholdValue
 * @param opts.prefix 给 thresholdValue 传 '>'，actualValue 不传
 */
export function formatAlertValue(
  type: string,
  value: number,
  opts?: { prefix?: string },
): string {
  const { prefix = '' } = opts ?? {}
  switch (type) {
    case 'pressure_high':
    case 'sensor_drift':
      return `${prefix}${Math.max(0, value).toFixed(2)}N`
    case 'pressure_fluctuation':
      return `${prefix}${value.toFixed(1)}%`
    case 'wear_interrupt':
      return `${prefix}${Math.max(0, Math.round(value))}min`
    case 'wear_duration_short':
      return `${prefix}${value}h`
    default:
      return `${prefix}${value}`
  }
}

/**
 * T289 2.6：告警类型中文术语（跨页唯一来源，设计稿 告警管理.html:248-251 + PRD §7D.6/§10）
 * 码值不改，只统一显示；pressure_fluctuation 仅历史行（T257 2.6 起引擎不再产生）。
 */
export const ALERT_TYPE_LABELS: Record<string, string> = {
  pressure_high: '压力偏高',
  wear_interrupt: '设备离线',
  wear_duration_short: '佩戴时长不足',
  sensor_drift: '传感器标定异常',
  pressure_fluctuation: '压力波动',
}

export function alertTypeLabel(type?: string | null): string {
  if (!type) return '-'
  return ALERT_TYPE_LABELS[type] ?? type
}
