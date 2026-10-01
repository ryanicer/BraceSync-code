/**
 * BraceSync shared utility functions.
 */

export * from './errorCopy'
export * from './feelings'
export * from './pressureUnit'

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
 * Alert.actualValue 是多义字段，含义随 type 变。左边是**落库口径**（engine.go 写什么），
 * 右边是**显示口径**（本函数吐什么），两者只在小时/分钟这一档上不同：
 *   pressure_high        → 压力（N）            → 原样 + 'N'
 *   pressure_fluctuation → 百分比               → 原样 + '%'
 *   sensor_drift         → 空载读数（N，可负）  → 归零 + 'N'
 *   wear_interrupt       → 分钟数               → 取整 + 'min'
 *   wear_duration_short  → 分钟数               → 除以 60 + 'h'
 *     落库=分钟的依据：engine.go EvaluateWearDurationShort 写 need = targetHours*60、
 *     actual = wearMinutes；engine_supplement_test.go:246 逐值钉「阈值以分钟口径落库」。
 *     显示=小时的依据：设计稿 admin/告警管理.html:252 同一行写 18h / 6.5h，
 *     而 seed.sql:193 该条落库 1080.0 / 390.0（分钟）。
 *     改前这里按分钟直标 'h' ⇒ 540 分钟显示成「540h」，与同弹窗「低于目标 9 小时」自相矛盾（T433 缺陷三）。
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
    case 'wear_duration_short': {
      const rounded = Math.round(Math.max(0, value) / 6) / 10 // 分钟 → 小时，保留 1 位小数
      return `${prefix}${Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1)}h`
    }
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

/**
 * T430（PRD §7D.6 历史数据处置已拍 C·Boss 2026-09-27）：已裁砍除类型的**展示侧**隐藏集合。
 * 口径＝界面不展示、数据不删。各消费点判据只认这一处，后续 PRD 待下线清单收敛时只动这里。
 * 🔴 上面的 ALERT_TYPE_LABELS 键一律保留：alertTypeLabel 查不到键会回退裸码值，删键等于把历史行露成 pressure_fluctuation。
 */
export const HIDDEN_ALERT_TYPES: readonly string[] = ['pressure_fluctuation']

export function isHiddenAlertType(type?: string | null): boolean {
  return !!type && HIDDEN_ALERT_TYPES.includes(type)
}
