/**
 * T625（类 4 / T620 趋势档）盲区用例：日・周・月三档窗口选择 + 连续多日 24h 上报的
 * 连续点位（无孤点）+ 空采集点/断裂场景交给图表的输入形状。
 *
 * 与既有 tests/unit/trend-window.spec.ts（T620 首建）不重叠：那边钉的是
 * 「东八区墙钟换算」与「旧病灶已从页面消失」的接线门禁；本轮补的是
 * **窗口宽度 ↔ 点位密度**这一格 —— T620 的 interval 桶宽（日 30m / 周・月 1d）
 * 决定了窗口里应有多少个点、点与点是否等距，而孤点/空洞正是患者端曲线的原病灶。
 *
 * 只使用 src/utils/trend-window.ts 实际导出的符号：
 *   cstParts / cstDateStr / trendWindow / trendInterval / toTrendSeries / pickPointValue
 * 断言里不引用实现里的常量，期望值全部手算写死（实现改口径这里必须响）。
 */
import { describe, it, expect } from 'vitest'
import {
  cstDateStr,
  trendWindow,
  trendInterval,
  toTrendSeries,
  pickPointValue,
} from '../../src/utils/trend-window'

const ms = (iso: string) => new Date(iso).getTime()
const iso = (epochMs: number) => new Date(epochMs).toISOString()

const MINUTE = 60 * 1000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** 日历锚（北京墙钟）：10-08 周四凌晨 02:40 = 2026-10-07T18:40Z，即 T620 卡面截图的时刻 */
const ANCHOR = ms('2026-10-07T18:40:00Z')

/** 桶行：points 可选，正对后端「该行没有点位读数」的两种写法（空数组 / 字段缺失） */
type BucketRow = {
  timestamp: string
  points?: { pointId: string; pressureValue: number }[]
}

/** 从窗口起点按 step 铺满到 end（不含 end），模拟后端按桶宽返回的行 */
function fillBuckets(start: number, end: number, step: number, valueOf: (index: number) => number): BucketRow[] {
  const rows: BucketRow[] = []
  for (let t = start; t < end; t += step) {
    rows.push({ timestamp: iso(t), points: [{ pointId: 'P01', pressureValue: valueOf(rows.length) }] })
  }
  return rows
}

/** 相邻点位的时间差序列（毫秒），用于看「是否等距 / 有几处断裂」 */
function deltas(points: { timestamp: string }[]): number[] {
  const out: number[] = []
  for (let i = 1; i < points.length; i++) {
    out.push(ms(points[i].timestamp) - ms(points[i - 1].timestamp))
  }
  return out
}

describe('T625 三档窗口选择：宽度与桶宽必须成对', () => {
  it('日档 = 一个自然日（北京 0:00→次日 0:00），配 30m 桶宽刚好 48 桶', () => {
    const { start, end } = trendWindow('day', ANCHOR)
    expect(iso(start)).toBe('2026-10-07T16:00:00.000Z')
    expect(iso(end)).toBe('2026-10-08T16:00:00.000Z')
    expect(end - start).toBe(DAY)
    expect(trendInterval('day')).toBe('30m')
    expect((end - start) / (30 * MINUTE)).toBe(48)
  })

  it('周档 = 7 个自然日（北京周一起算），配 1d 桶宽刚好 7 桶', () => {
    const { start, end } = trendWindow('week', ANCHOR)
    expect(iso(start)).toBe('2026-10-04T16:00:00.000Z')
    expect(iso(end)).toBe('2026-10-11T16:00:00.000Z')
    expect(end - start).toBe(7 * DAY)
    expect(trendInterval('week')).toBe('1d')
    expect((end - start) / DAY).toBe(7)
  })

  it('月档按当月真实天数铺开：10 月 31 桶、2 月 28 桶，桶宽同为 1d', () => {
    const oct = trendWindow('month', ANCHOR)
    expect(iso(oct.start)).toBe('2026-09-30T16:00:00.000Z')
    expect(iso(oct.end)).toBe('2026-10-31T16:00:00.000Z')
    expect(oct.end - oct.start).toBe(31 * DAY)

    const feb = trendWindow('month', ms('2026-02-10T02:00:00Z'))
    expect(iso(feb.start)).toBe('2026-01-31T16:00:00.000Z')
    expect(iso(feb.end)).toBe('2026-02-28T16:00:00.000Z')
    expect(feb.end - feb.start).toBe(28 * DAY)
    expect(trendInterval('month')).toBe('1d')
  })

  it('页面发给后端的 date 参数与窗口起点属同一个北京日（两处口径不能错开一天）', () => {
    const dateParam = cstDateStr(ANCHOR)
    const { start, end } = trendWindow('day', ANCHOR)
    expect(dateParam).toBe('2026-10-08')
    // 窗口两端按墙钟读回，必须都落回 dateParam 那一天：起点是当天 0:00，终点是次日 0:00 前一刻
    expect(cstDateStr(start)).toBe(dateParam)
    expect(cstDateStr(end - 1)).toBe(dateParam)
    // 该北京日的 0:00 对应 UTC 瞬时是前一天 16:00Z（+08:00 固定偏移）
    expect(iso(start)).toBe('2026-10-07T16:00:00.000Z')
  })
})

describe('T625 连续多日 24h 上报：点位必须连续、无孤点', () => {
  it('日档全天 30m 上报 ⇒ 48 个等距点，首尾都落在窗口内', () => {
    const { start, end } = trendWindow('day', ANCHOR)
    const points = toTrendSeries(fillBuckets(start, end, 30 * MINUTE, () => 20), 'P01')

    expect(points).toHaveLength(48)
    expect(deltas(points).every((d) => d === 30 * MINUTE)).toBe(true)
    expect(ms(points[0].timestamp)).toBe(start)
    expect(ms(points[47].timestamp)).toBe(end - 30 * MINUTE)
  })

  it('跨 3 个自然日连续上报 ⇒ 144 点全等距，跨日处不产生孤点也不重复', () => {
    const dayA = trendWindow('day', ms('2026-10-06T18:00:00Z'))
    const step = 30 * MINUTE
    const rows = fillBuckets(dayA.start, dayA.start + 3 * DAY, step, () => 15)
    const points = toTrendSeries(rows, 'P01')

    expect(points).toHaveLength(144)
    expect(points[points.length - 1].timestamp).toBe(iso(dayA.start + 143 * step))
    expect(deltas(points).every((d) => d === step)).toBe(true)
    // 无重复时间戳：后端按桶界返回，前端不得再合并出第二种密度
    expect(new Set(points.map((p) => p.timestamp)).size).toBe(points.length)
  })

  it('周档 7 个日日桶 ⇒ 7 个 24h 等距点（东八区无夏令时，日宽恒 24h）', () => {
    const { start, end } = trendWindow('week', ANCHOR)
    const points = toTrendSeries(fillBuckets(start, end, DAY, (i) => 10 + i), 'P01')

    expect(points).toHaveLength(7)
    expect(deltas(points).every((d) => d === DAY)).toBe(true)
    expect(points.map((p) => p.value)).toEqual([10, 11, 12, 13, 14, 15, 16])
  })

  it('点位乱序到达也仍排成升序连续序列（后端已升序，前端不得依赖到达顺序）', () => {
    const { start, end } = trendWindow('day', ANCHOR)
    const rows = fillBuckets(start, end, 30 * MINUTE, () => 1)
    const shuffled = [...rows.slice(24), ...rows.slice(0, 24)]
    const points = toTrendSeries(shuffled, 'P01')

    expect(points).toHaveLength(48)
    expect(deltas(points).every((d) => d === 30 * MINUTE)).toBe(true)
  })
})

describe('T625 空采集点与断裂：交给图表的输入形状', () => {
  it('空采集点（points 为空数组 / 整字段缺失）保留为 0N 点位，不被打洞', () => {
    const { start, end } = trendWindow('day', ANCHOR)
    const rows = fillBuckets(start, end, 30 * MINUTE, () => 30)
    rows[3] = { timestamp: rows[3].timestamp, points: [] }
    rows[4] = { timestamp: rows[4].timestamp }
    rows[5] = { timestamp: rows[5].timestamp, points: [{ pointId: 'P07', pressureValue: 12 }] }

    const points = toTrendSeries(rows, 'P01')
    expect(points).toHaveLength(48)
    expect(points[3].value).toBe(0)
    expect(points[4].value).toBe(0)
    // 选中点位 P01 不在该行时退到该行最大值（现读 util 口径），仍是数字而不是 NaN
    expect(points[5].value).toBe(12)
    expect(points.every((p) => Number.isFinite(p.value))).toBe(true)
  })

  it('设备停报 4 小时 ⇒ 序列只剩上报的那些点，断裂只呈现为一处超宽间隔，前端不得补 0 造点', () => {
    const { start, end } = trendWindow('day', ANCHOR)
    const rows = fillBuckets(start, end, 30 * MINUTE, () => 25)
    const broken = rows.filter((_, i) => i < 10 || i >= 18) // 第 10-17 共 8 个桶（4 小时）无上报
    const points = toTrendSeries(broken, 'P01')

    expect(points).toHaveLength(48 - 8)
    const gaps = deltas(points).filter((d) => d !== 30 * MINUTE)
    expect(gaps).toHaveLength(1)
    // 空洞是 8 个桶（4 小时）没上报，前后两个真实点相隔 9 个桶宽
    expect(gaps[0]).toBe(4 * HOUR + 30 * MINUTE)
    // 补 0 造点会把断裂抹平成「真的没受压」，这条必须钉住不出现
    expect(points.filter((p) => p.value === 0)).toHaveLength(0)
  })

  it('整日零上报 ⇒ 空数组（由页面出空态占位，序列化层不虚构点位）', () => {
    expect(toTrendSeries([], 'P01')).toEqual([])
    const allUnparsable = [
      { timestamp: '', points: [{ pointId: 'P01', pressureValue: 5 }] },
      { timestamp: '2026-13-40T99:99:99Z', points: [{ pointId: 'P01', pressureValue: 6 }] },
    ]
    expect(toTrendSeries(allUnparsable, 'P01')).toEqual([])
  })

  it('只给单点上报时序列长度 1：孤点的判据是「前后无邻居」，不是被前端复制成两点', () => {
    const { start } = trendWindow('day', ANCHOR)
    const single = [{ timestamp: iso(start), points: [{ pointId: 'P01', pressureValue: 7 }] }]
    const points = toTrendSeries(single, 'P01')
    expect(points).toEqual([{ timestamp: iso(start), value: 7, kpa: null }])
    expect(deltas(points)).toEqual([])
  })

  it('不传 pointId（全点位取最大）与传点位取不到时的取值口径一致，序列形状不变', () => {
    const row = {
      timestamp: '2026-10-07T16:00:00Z',
      points: [
        { pointId: 'P01', pressureValue: 8 },
        { pointId: 'P02', pressureValue: 33 },
      ],
    }
    expect(pickPointValue(row, 'P01')).toBe(8)
    expect(pickPointValue(row, 'P99')).toBe(33)
    expect(pickPointValue(row)).toBe(33)
    expect(toTrendSeries([row], 'P99')).toEqual([{ timestamp: '2026-10-07T16:00:00Z', value: 33, kpa: null }])
  })
})
