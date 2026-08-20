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
