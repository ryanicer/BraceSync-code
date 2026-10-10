/**
 * T634 — 佩戴统计「按日 / 按周」两视图的共用派生层。
 *
 * 周界口径＝值班席 weide-duty 第 115 轮裁定第四节的明文：周一起算、时区按 Asia/Shanghai，
 * 与 utils/trend-window.ts 既有实现同源 ⇒ 本周界只从 trendWindow('week') 那一档取，不按运行设备的时区推。
 * 时长换算式沿用佩戴管理页既有实现（T224），本模块只把它收成一处，
 * 两视图都从这里取数 ⇒ 切换只换聚合维度，不新增口径、不换数据源。
 */
import { cstDateStr, trendWindow } from './trend-window'

const MS_PER_DAY = 24 * 60 * 60 * 1000

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

/** 设备本地日键：今日环形图那份仍用它（裁定第二节把「今日」键迁 CST 判为独立事项，不在本卡迁移） */
export function dateKey(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

export function toHours(wearMinutes: number): number {
  return Math.round(wearMinutes / 6) / 10
}

/** 东八区当日在本周数组里的下标：周一 = 0，周日 = 6 */
export function todayIndexInWeek(now: Date): number {
  return weekDayKeys(now).indexOf(cstTodayKey(now))
}

/** 东八区的「今日」键，按日视图的默认选中与窗口右界都取它 */
export function cstTodayKey(now: Date): string {
  return cstDateStr(now.getTime())
}

/**
 * 汇总视图取数窗口两界（都按东八区）：start = 本周一，end = 今日。
 * 后端 daily-wear 的 date 键本身就是 Asia/Shanghai 切日，窗口跟着东八区走才对得上。
 * 上面「今日」环形图那份仍用设备本地键（迁 CST 属独立事项，见裁定第二节）：设备钟快于 +8 时（或恰在周边附近）
 * 那枚键会落在本窗口之外，环形图届时读 0 —— 这一格的残留差不在本卡射程内。
 */
export function summaryWindowKeys(now: Date): { start: string; end: string } {
  return { start: weekDayKeys(now)[0], end: cstTodayKey(now) }
}

/** 本周 7 枚日期键，周一 → 周日（东八区，与 trendWindow 的 week 档同一枚起点） */
export function weekDayKeys(now: Date): string[] {
  const { start } = trendWindow('week', now.getTime())
  const out: string[] = []
  for (let i = 0; i < 7; i++) {
    out.push(cstDateStr(start + i * MS_PER_DAY))
  }
  return out
}

/** 按日视图的可选日：东八区本周一 → 东八区今日（未来的日子不列；取数窗口的右界可能比这枚「日」更宽，见 summaryWindowKeys） */
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
