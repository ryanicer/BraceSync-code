/**
 * T634 — 佩戴统计「按日 / 按周」两视图的共用派生层。
 *
 * 换算式与周界锚点逐字沿用佩戴管理页既有实现（T224），本模块只把它收成一处，
 * 两视图都从这里取数 ⇒ 切换只换聚合维度，不新增口径、不换数据源。
 */

export interface WearDayRecord {
  date: string
  wearMinutes: number
  frameCount?: number
  abnormalCount?: number
}

export const WEEK_LABELS = ['一', '二', '三', '四', '五', '六', '日']

export function pad2(n: number): string {
  return n < 10 ? '0' + n : '' + n
}

export function dateKey(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

export function toHours(wearMinutes: number): number {
  return Math.round(wearMinutes / 6) / 10
}

/** 当日在本周数组里的下标：周一 = 0，周日（getDay() === 0）回退到 6 */
export function todayIndexInWeek(now: Date): number {
  const dow = now.getDay()
  return dow === 0 ? 6 : dow - 1
}

export function mondayOf(now: Date): Date {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() - todayIndexInWeek(now))
}

/** 本周 7 枚日期键，周一 → 周日 */
export function weekDayKeys(now: Date): string[] {
  const monday = mondayOf(now)
  const out: string[] = []
  for (let i = 0; i < 7; i++) {
    out.push(dateKey(new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + i)))
  }
  return out
}

/** 按日视图的可选日：周一 → 当日，与本页取数窗口（start = 周一、end = 当日）同界，未来的日子不列 */
export function visibleDayKeys(now: Date): string[] {
  return weekDayKeys(now).slice(0, todayIndexInWeek(now) + 1)
}

/** 逐日时长槽位：有记录给小时数，无记录/未到为 null */
export function weekSlots(days: WearDayRecord[], now: Date): (number | null)[] {
  const byDate = new Map(days.map((d) => [d.date, toHours(d.wearMinutes)]))
  const keys = weekDayKeys(now)
  const out: (number | null)[] = [null, null, null, null, null, null, null]
  for (let i = 0; i <= todayIndexInWeek(now); i++) {
    out[i] = byDate.has(keys[i]) ? (byDate.get(keys[i]) as number) : null
  }
  return out
}

export interface DaySummary {
  key: string
  hasRecord: boolean
  hours: number
  wearMinutes: number
  frameCount: number | null
  abnormalCount: number | null
}

export function buildDaySummary(days: WearDayRecord[], key: string): DaySummary {
  const rec = days.find((d) => d.date === key)
  if (!rec) {
    return { key, hasRecord: false, hours: 0, wearMinutes: 0, frameCount: null, abnormalCount: null }
  }
  return {
    key,
    hasRecord: true,
    hours: toHours(rec.wearMinutes),
    wearMinutes: rec.wearMinutes,
    frameCount: typeof rec.frameCount === 'number' ? rec.frameCount : null,
    abnormalCount: typeof rec.abnormalCount === 'number' ? rec.abnormalCount : null,
  }
}

export interface WeekAggregate {
  coveredDays: number
  sumHours: number
  maxHours: number
}

export function weekAggregate(slots: (number | null)[]): WeekAggregate {
  const withData = slots.filter((v): v is number => v != null)
  return {
    coveredDays: withData.length,
    sumHours: withData.reduce((a, b) => a + b, 0),
    maxHours: withData.length === 0 ? 0 : Math.max(...withData),
  }
}

export type SummaryViewMode = 'day' | 'week'
