/**
 * T620：趋势视图的时间窗口口径（患者端）。
 *
 * 为什么不用设备本地时区：后端 records 端点的 day/week/month 边界由
 * `periodRange()` 按 Asia/Shanghai 切日（service/record.go），返回的每条
 * timestamp 又是 UTC 瞬时；而本页此前的实现用 `new Date()` 的本地 getter
 * 造 `date` 参数与 X 轴窗口。设备时区不是东八区时，两处口径会错开一天内
 * 的任意时长，曲线窗口与查询窗口不是同一天。
 *
 * 这里把「东八区墙钟」算成纯算术（固定 +08:00，中国无夏令时），不依赖
 * Intl/timezone 数据库——微信小程序的 JS 运行时上 Intl 不完整。
 */

/** 趋势页的三档时间范围 */
export type TrendSegment = 'day' | 'week' | 'month'

/** 东八区相对 UTC 的固定偏移（毫秒）。中国自 1991 年起不再用夏令时。 */
export const CST_OFFSET_MS = 8 * 60 * 60 * 1000

export interface CstParts {
  year: number
  /** 0-11，与 Date.getUTCMonth 同口径 */
  month: number
  day: number
  /** 0=周日 … 6=周六 */
  weekday: number
  hour: number
  minute: number
}

/** 把瞬时读成东八区墙钟：先加偏移，再用 UTC getter 取分量。 */
export function cstParts(nowMs: number): CstParts {
  const shifted = new Date(nowMs + CST_OFFSET_MS)
  return {
    year: shifted.getUTCFullYear(),
    month: shifted.getUTCMonth(),
    day: shifted.getUTCDate(),
    weekday: shifted.getUTCDay(),
    hour: shifted.getUTCHours(),
    minute: shifted.getUTCMinutes(),
  }
}

/** 东八区墙钟 (y,m,d hh:mm) → 瞬时毫秒。 */
export function cstToEpochMs(year: number, month: number, day: number, hour = 0, minute = 0): number {
  return Date.UTC(year, month, day, hour, minute, 0, 0) - CST_OFFSET_MS
}

/** `date` 查询参数：东八区的今天，形如 2026-10-08 */
export function cstDateStr(nowMs: number): string {
  const p = cstParts(nowMs)
  const mm = String(p.month + 1).padStart(2, '0')
  const dd = String(p.day).padStart(2, '0')
  return `${p.year}-${mm}-${dd}`
}

/**
 * 趋势 X 轴窗口，与后端 periodRange 的同档边界对齐：
 * day = 东八区今日 0:00 → 次日 0:00；week = 本周一 0:00 → 下周一；
 * month = 本月 1 日 0:00 → 下月 1 日。
 */
export function trendWindow(segment: TrendSegment, nowMs: number): { start: number; end: number } {
  const p = cstParts(nowMs)
  if (segment === 'day') {
    const start = cstToEpochMs(p.year, p.month, p.day)
    return { start, end: cstToEpochMs(p.year, p.month, p.day + 1) }
  }
  if (segment === 'week') {
    // 周一为一周起点：周日（weekday=0）是本周最后一天，要回退 6 天而不是 7 天
    const monday = p.day - (p.weekday === 0 ? 6 : p.weekday - 1)
    const start = cstToEpochMs(p.year, p.month, monday)
    return { start, end: cstToEpochMs(p.year, p.month, monday + 7) }
  }
  const start = cstToEpochMs(p.year, p.month, 1)
  return { start, end: cstToEpochMs(p.year, p.month + 1, 1) }
}

/**
 * 每档对应的后端桶宽（T620 新增的 interval 参数，口径见 data-service
 * service/record.go 的 historyIntervals 白名单）：
 * 日 = 30 分钟（一天 48 桶，连续铺到当前时刻）；周/月 = 1 天（7 / 30 桶）。
 * 明细分页在这里永远铺不满一天：设备约 31 秒一帧，pageSize 上限 100 只有约 50 分钟。
 */
export function trendInterval(segment: TrendSegment): string {
  return segment === 'day' ? '30m' : '1d'
}

/** 曲线点位（PressureCurve 的 data 项）。value 恒为 N 读数；kpa 是同一选点在同响应里派生的 kPa 值，null = 不可换算 */
export interface TrendPoint {
  timestamp: string
  value: number
  kpa: number | null
}

/** 后端桶行里参与取数的字段（pressureKpa 由 data-service T643 A 路同源派生；前端不换算） */
interface TrendSourcePoint {
  pointId: string
  pressureValue: number
  pressureKpa?: number | null
}

/**
 * 后端桶行 → 曲线点。
 *
 * 与旧实现（本页的 aggregateByPeriod）的三点差别，都是 T620 的病灶：
 * 1. 不再自己分桶——桶宽与桶界归后端（epoch 取余会把东八区的日切成 08:00 起，
 *    导致每天 0:00-8:00 结构性缺一块）；
 * 2. 不再丢弃 0 值——0N 是「未受压」的真实读数，丢掉就等于把连续曲线打洞；
 * 3. 只按时间升序排列，越界的桶不夹到端点（前端坐标轴自己会按窗口定位）。
 *
 * T643：N 读数与 kPa 派生值必须取自同一枚选点（见 `pickPointReading`），
 * 否则两档曲线画的是两条线。
 */
export function toTrendSeries(
  records: { timestamp: string; points?: TrendSourcePoint[] }[],
  pointId?: string
): TrendPoint[] {
  const points = records
    .map(r => ({ timestamp: r.timestamp, ...pickPointReading(r, pointId) }))
    .filter(p => !Number.isNaN(new Date(p.timestamp).getTime()))
  points.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
  return points
}

/** 取某行（帧或桶）里指定点位的压力读数；点位缺失时退到该行最大值。 */
export function pickPointValue(
  r: { points?: TrendSourcePoint[] },
  pointId?: string
): number {
  return pickPointReading(r, pointId).value
}

/**
 * 同一行里 N 与 kPa 的一对读数，二者必须出自同一枚选点：
 * 指定点位命中就用那一个点位，否则用该行压力最大的点位（与旧 pickPointValue 的退路一致）。
 * 行里没有点位 ⇒ N 回 0、kPa 回 null（kPa 没有「0」这个兜底含义，缺值只以 null 表达）。
 */
export function pickPointReading(
  r: { points?: TrendSourcePoint[] },
  pointId?: string
): { value: number; kpa: number | null } {
  const pts = r.points || []
  if (pointId) {
    const hit = pts.find(pt => pt.pointId === pointId)
    // 命中指定点位时 N 逐字沿用旧口径（原样回那一点位的读数，含 0 与负值），本卡不动数值
    if (hit) return { value: hit.pressureValue, kpa: hit.pressureKpa ?? null }
  }
  let rep: TrendSourcePoint | undefined
  for (const p of pts) {
    if (!rep || p.pressureValue > rep.pressureValue) rep = p
  }
  // 退路旧口径：从 0 起比，全行没有正读数就回 0（负读数不参与「取最大」），本卡同样不改数值
  return { value: rep && rep.pressureValue > 0 ? rep.pressureValue : 0, kpa: rep ? rep.pressureKpa ?? null : null }
}
