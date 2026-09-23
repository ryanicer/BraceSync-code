// T344 患者工作台「数据视图」纯层：滚动区间计算、日佩戴聚合契约镜像、缺日对齐。
// 抽成纯模块的两个原因：① SFC 里的逻辑 vitest 测不到（T298 教训）；
// ② 稿面要求 7/14/30 天可选 + 虚线取配置不写死，这两条都靠这里落准。
// 后端日切是 Asia/Shanghai（services/data-service/internal/model/model.go:374 注释），
// 这里一律按东八区取「今天」，用本地时区或 UTC 都会整体错一天。

export type WearRangeDays = 7 | 14 | 30

export interface DateRange {
  start: string
  end: string
}

/** 后端 DailyWearDayDTO 镜像（services/data-service/internal/model/model.go:373-381） */
export interface DailyWearDay {
  date: string
  wearMinutes: number
  avgPressure: number
  maxPressure: number
  maxPoint: string
  frameCount: number
  abnormalCount: number
}

const CST_DATE = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
})

/** YYYY-MM-DD（东八区） */
export function cstDate(d: Date): string {
  return CST_DATE.format(d)
}

function addDays(isoDate: string, delta: number): string {
  const [y, m, d] = isoDate.split('-').map(Number)
  const t = new Date(Date.UTC(y, m - 1, d))
  t.setUTCDate(t.getUTCDate() + delta)
  return t.toISOString().slice(0, 10)
}

function dayDiff(from: string, to: string): number {
  return Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86_400_000)
}

/**
 * 滚动 N 天闭区间（含今日）。稿面「最近 N 天」是滚动窗口，
 * 后端 records 的 period 只有 day/week/month 三档自然周期（record.go:689-708），对不上，
 * 故工作台走 daily-wear 的 start/end。
 */
export function rangeForDays(days: WearRangeDays, today: Date = new Date()): DateRange {
  const end = cstDate(today)
  return { start: addDays(end, -(days - 1)), end }
}

/** 闭区间逐日枚举；start 晚于 end 时返回空数组 */
export function listDates(range: DateRange): string[] {
  const span = dayDiff(range.start, range.end)
  if (span < 0) return []
  return Array.from({ length: span + 1 }, (_, i) => addDays(range.start, i))
}

/** 佩戴分钟 → 小时，保留 1 位小数（与患者端「日均」口径一致，不额外凑整） */
export function toWearHours(wearMinutes: number): number {
  return Math.round((wearMinutes / 60) * 10) / 10
}

export interface WearSeries {
  dates: string[]
  /** 日最大压力（N），无统计行 ⇒ null，不填 0 */
  avgPressure: (number | null)[]
  maxPressure: (number | null)[]
  wearHours: (number | null)[]
}

/**
 * 把 daily-wear 返回的稀疏行对齐到区间每一天。
 * 缺日 = 该日无统计行 ⇒ null。填 0 会把「没数据」说成「佩戴 0 小时」，
 * 后者在告警口径里是要被判佩戴时长不足的实数据。
 */
export function alignWearSeries(rows: DailyWearDay[], range: DateRange): WearSeries {
  const dates = listDates(range)
  const byDate = new Map(rows.map((r) => [r.date, r]))
  return {
    dates,
    avgPressure: dates.map((d) => byDate.get(d)?.avgPressure ?? null),
    maxPressure: dates.map((d) => byDate.get(d)?.maxPressure ?? null),
    wearHours: dates.map((d) => {
      const row = byDate.get(d)
      return row ? toWearHours(row.wearMinutes) : null
    }),
  }
}

/** 序列是否全无数据（图表空态判据，避免拿空数组渲染出一张坐标轴齐全的假图） */
export function seriesIsEmpty(series: WearSeries): boolean {
  return series.avgPressure.every((v) => v === null) && series.wearHours.every((v) => v === null)
}

/** 画常量参考线（稿面「压力上限线 / 佩戴目标线，值取 §7D.12 配置 ⇒ 不写死」） */
export function constantLine(value: number, length: number): number[] {
  return Array.from({ length }, () => value)
}
