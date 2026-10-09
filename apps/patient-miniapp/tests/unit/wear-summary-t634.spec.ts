/**
 * T634 — 佩戴统计按日/按周汇总视图的派生层尺
 *
 * 三类判据：① 换算式与周界锚点未变（与本页改造前的内联实现逐日对平）
 *          ② 按日/按周两视图读的是同一份 days（无第二处口径）
 *          ③ 仓内机械尺：切档不重新取数、周界与时长换算在页面里只剩一处
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import {
  WEEK_LABELS,
  buildDaySummary,
  dateKey,
  mondayOf,
  visibleDayKeys,
  weekAggregate,
  weekDayKeys,
  weekSlots,
  toHours,
} from '../../src/utils/wear-summary'

const SRC = fileURLToPath(new URL('../../src', import.meta.url))
const PAGE = 'pages/wearing/index.vue'

interface Row {
  date: string
  wearMinutes: number
  frameCount?: number
  abnormalCount?: number
}

/** 改造前佩戴管理页的内联实现，逐字搬来当参照：口径只能不变，不能被我改形 */
function legacyTodayHours(days: Row[], now: Date): number {
  const pad = (n: number) => (n < 10 ? '0' + n : '' + n)
  const todayKey = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
  const rec = days.find((d) => d.date === todayKey)
  return rec ? Math.round(rec.wearMinutes / 6) / 10 : 0
}

function legacyWeekSlots(days: Row[], now: Date): (number | null)[] {
  const pad = (n: number) => (n < 10 ? '0' + n : '' + n)
  const arr: (number | null)[] = [null, null, null, null, null, null, null]
  const byDate = new Map(days.map((d) => [d.date, Math.round(d.wearMinutes / 6) / 10]))
  const dow = now.getDay()
  const todayIdx = dow === 0 ? 6 : dow - 1
  const monday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - todayIdx)
  for (let i = 0; i <= todayIdx; i++) {
    const d = new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + i)
    const key = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
    arr[i] = byDate.has(key) ? (byDate.get(key) as number) : null
  }
  return arr
}

function legacyFormatted(days: Row[], now: Date) {
  const slots = legacyWeekSlots(days, now)
  const withData = slots.filter((v): v is number => v != null)
  return {
    avg: withData.length === 0 ? '0.0' : (withData.reduce((a, b) => a + b, 0) / withData.length).toFixed(1),
    max: withData.length === 0 ? '0.0' : Math.max(...withData).toFixed(1),
    total: withData.length === 0 ? '0' : String(Math.round(withData.reduce((a, b) => a + b, 0))),
  }
}

const WEEK_DAYS = [5, 6, 7, 8, 9, 10, 11]
const sampleRows = (): Row[] => [
  { date: '2026-10-05', wearMinutes: 600, frameCount: 1200, abnormalCount: 3 },
  { date: '2026-10-06', wearMinutes: 138, frameCount: 300, abnormalCount: 0 },
  { date: '2026-10-08', wearMinutes: 1000, frameCount: 2000, abnormalCount: 11 },
  // 窗口外（上一周）的一行：用来证明槽位不会被越界取用
  { date: '2026-09-28', wearMinutes: 900 },
]

describe('T634 — 换算式与日期键', () => {
  it('dateKey 补零，形同 YYYY-MM-DD', () => {
    expect(dateKey(new Date(2026, 9, 5))).toBe('2026-10-05')
    expect(dateKey(new Date(2026, 11, 1))).toBe('2026-12-01')
  })

  it('toHours 与页面既有换算式对平（分钟→小时，一位小数）', () => {
    for (const m of [0, 6, 60, 138, 600, 999, 1440]) {
      expect(toHours(m), String(m)).toBe(Math.round(m / 6) / 10)
    }
  })
})

describe('T634 — 周界锚点：周一起算', () => {
  it('本周七天里每一天（含周日）锚点都是 10-05 周一', () => {
    for (const d of WEEK_DAYS) {
      expect(dateKey(mondayOf(new Date(2026, 9, d))), `10-${d}`).toBe('2026-10-05')
    }
  })

  it('跨周两枚对照：上周日归上一周，下周一自成一周边', () => {
    expect(dateKey(mondayOf(new Date(2026, 9, 4)))).toBe('2026-09-28')
    expect(dateKey(mondayOf(new Date(2026, 9, 12)))).toBe('2026-10-12')
  })

  it('weekDayKeys 首枚周一、末枚周日', () => {
    const keys = weekDayKeys(new Date(2026, 9, 9))
    expect(keys).toEqual([
      '2026-10-05', '2026-10-06', '2026-10-07', '2026-10-08', '2026-10-09', '2026-10-10', '2026-10-11',
    ])
    expect(WEEK_LABELS[keys.length - 1]).toBe('日')
  })

  it('visibleDayKeys 只列到当日：周一 1 枚、周五 5 枚、周日 7 枚', () => {
    expect(visibleDayKeys(new Date(2026, 9, 5))).toEqual(['2026-10-05'])
    expect(visibleDayKeys(new Date(2026, 9, 9))).toHaveLength(5)
    expect(visibleDayKeys(new Date(2026, 9, 11))).toHaveLength(7)
  })
})

describe('T634 — 口径未变：与改造前内联实现对平', () => {
  for (const d of WEEK_DAYS) {
    const now = new Date(2026, 9, d)
    it(`10-${d} 逐日槽位与当日读数与旧实现逐枚相等`, () => {
      const rows = sampleRows()
      expect(weekSlots(rows, now), `slots 10-${d}`).toEqual(legacyWeekSlots(rows, now))
      expect(buildDaySummary(rows, dateKey(now)).hours, `today 10-${d}`).toBe(legacyTodayHours(rows, now))
    })
  }

  it('按周三读数（日均/最高/累计）与旧格式化串逐字相等', () => {
    const rows = sampleRows()
    const now = new Date(2026, 9, 9)
    const agg = weekAggregate(weekSlots(rows, now))
    const legacy = legacyFormatted(rows, now)
    expect([
      agg.coveredDays === 0 ? '0.0' : (agg.sumHours / agg.coveredDays).toFixed(1),
      agg.coveredDays === 0 ? '0.0' : agg.maxHours.toFixed(1),
      agg.coveredDays === 0 ? '0' : String(Math.round(agg.sumHours)),
    ]).toEqual([legacy.avg, legacy.max, legacy.total])
    expect(agg.coveredDays).toBe(3)
  })

  it('空窗（本周一条记录都没有）三读数仍按旧形出 0.0/0.0/0', () => {
    const agg = weekAggregate(weekSlots([], new Date(2026, 9, 9)))
    expect(agg).toEqual({ coveredDays: 0, sumHours: 0, maxHours: 0 })
    const legacy = legacyFormatted([], new Date(2026, 9, 9))
    expect([legacy.avg, legacy.max, legacy.total]).toEqual(['0.0', '0.0', '0'])
  })
})

describe('T634 — 两视图同一份取数面', () => {
  it('按日选中当日时，读数与「今日」环形图那份完全相等', () => {
    const rows = sampleRows()
    const now = new Date(2026, 9, 8)
    const todayKey = dateKey(now)
    expect(buildDaySummary(rows, todayKey).hours).toBe(legacyTodayHours(rows, now))
    expect(buildDaySummary(rows, todayKey).hours).toBeCloseTo(1000 / 60, 1)
    // 同一份 rows 里换一天，槽位数不变（切换的是维度不是数据源）
    expect(buildDaySummary(rows, '2026-10-05').hours).toBe(10)
  })

  it('无记录的日期 hasRecord=false、帧数为 null，不拿 0 冒充有数据', () => {
    const empty = buildDaySummary(sampleRows(), '2026-10-07')
    expect(empty.hasRecord).toBe(false)
    expect(empty.hours).toBe(0)
    expect(empty.frameCount).toBeNull()
    expect(empty.abnormalCount).toBeNull()
    const hit = buildDaySummary(sampleRows(), '2026-10-06')
    expect(hit.frameCount).toBe(300)
    expect(hit.abnormalCount).toBe(0)
  })
})

describe('T634 — 仓内机械尺（判据的可复跑形）', () => {
  const page = fs.readFileSync(path.join(SRC, PAGE), 'utf8')

  it('页面从共用派生层取数', () => {
    expect(page.includes("from '../../utils/wear-summary'")).toBe(true)
    expect(page.includes('buildDaySummary'), '按日与「今日」共用同一份派生').toBe(true)
    expect(page.includes('weekSlots')).toBe(true)
  })

  it('切档不重新取数：loadDailyWear 除定义外只有一处调用点', () => {
    const defs = page.match(/function loadDailyWear\(\)/g) || []
    expect(defs, '定义那一枚要数得出来（尺子的牙）').toHaveLength(1)
    const calls = page.match(/(?<!function\s)loadDailyWear\(\)/g) || []
    expect(calls, '调用点只许 onMounted 那一发').toHaveLength(1)
    const toggle = page.match(/function switchViewMode\([\s\S]*?\n\}/)
    expect(toggle, 'switchViewMode 定义要在页面上找得到').not.toBeNull()
    expect(/\b(request|loadDailyWear)\s*\(/.test(toggle[0]), '切档体内不许有取数').toBe(false)
  })

  it('周界与时长换算在页面里只剩一处：都不许再内联出现', () => {
    expect(page.includes('getDay()'), '周界锚点只住 wear-summary').toBe(false)
    expect(page.includes('wearMinutes / 6'), '时长换算只住 wear-summary').toBe(false)
    // 尺子的牙：同两枚针在共用层里必须命中，否则上面的「不含」是自证空白
    const util = fs.readFileSync(path.join(SRC, 'utils/wear-summary.ts'), 'utf8')
    expect(util.includes('getDay()')).toBe(true)
    expect(util.includes('wearMinutes / 6')).toBe(true)
  })

  it('两档都在页面上有入口，且默认按日', () => {
    expect(page.includes("viewMode === 'day'")).toBe(true)
    expect(page.includes("viewMode === 'week'")).toBe(true)
    expect(/const viewMode = ref<SummaryViewMode>\('day'\)/.test(page)).toBe(true)
  })
})
