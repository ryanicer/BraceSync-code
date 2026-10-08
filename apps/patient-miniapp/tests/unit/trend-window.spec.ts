/**
 * T620：趋势窗口口径（东八区墙钟）+ 后端桶序列化的纯层判据。
 *
 * 期望值全部按「北京墙上几点 = UTC 几号几点」手工换算写死，不引用实现里的
 * 任何常量或算法：实现改了口径，这里必须响。
 *
 * 现读的日历锚（Intl.DateTimeFormat, timeZone: Asia/Shanghai）：
 *   2026-10-07T18:40:00Z  北京 2026-10-08 02:40 周四   ← 卡面截图的那个时刻
 *   2026-10-04T16:30:00Z  北京 2026-10-05 00:30 周一
 *   2026-10-04T15:30:00Z  北京 2026-10-04 23:30 周日
 *   2026-11-30T15:59:00Z  北京 2026-11-30 23:59 周一（月末日的月档边界）
 */
import { describe, it, expect } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import {
  cstParts,
  cstDateStr,
  trendWindow,
  trendInterval,
  toTrendSeries,
  pickPointValue,
} from '../../src/utils/trend-window'

const ms = (iso: string) => new Date(iso).getTime()
const iso = (epochMs: number) => new Date(epochMs).toISOString()

describe('T620 东八区墙钟', () => {
  it('凌晨 02:40（北京）= 10-07T18:40Z，日期串仍是 10-08', () => {
    const at = ms('2026-10-07T18:40:00Z')
    expect(cstDateStr(at)).toBe('2026-10-08')
    const p = cstParts(at)
    expect([p.year, p.month, p.day, p.hour, p.minute, p.weekday]).toEqual([2026, 9, 8, 2, 40, 4])
  })

  it('UTC 同日但北京已跨日：10-07T15:59Z 属北京的 10-07', () => {
    expect(cstDateStr(ms('2026-10-07T15:59:00Z'))).toBe('2026-10-07')
    expect(cstDateStr(ms('2026-10-07T16:00:00Z'))).toBe('2026-10-08')
  })

  it('日档窗口 = 北京 0:00 起算，不是 UTC 0:00', () => {
    const { start, end } = trendWindow('day', ms('2026-10-07T18:40:00Z'))
    expect(iso(start)).toBe('2026-10-07T16:00:00.000Z')
    expect(iso(end)).toBe('2026-10-08T16:00:00.000Z')
  })

  it('周档窗口 = 北京周日的下一天（周一）起算', () => {
    // 北京 2026-10-08 周四 → 本周一 = 2026-10-05 00:00 北京 = 10-04T16:00Z
    const { start, end } = trendWindow('week', ms('2026-10-07T18:40:00Z'))
    expect(iso(start)).toBe('2026-10-04T16:00:00.000Z')
    expect(iso(end)).toBe('2026-10-11T16:00:00.000Z')
  })

  it('周日 23:30（北京）仍属上一个周一那一周', () => {
    // 2026-10-04T15:30Z = 北京周日 23:30；本周一 = 2026-09-28 00:00 北京 = 09-27T16:00Z
    const { start } = trendWindow('week', ms('2026-10-04T15:30:00Z'))
    expect(iso(start)).toBe('2026-09-27T16:00:00.000Z')
  })

  it('周一 00:30（北京）已属新的一周', () => {
    const { start } = trendWindow('week', ms('2026-10-04T16:30:00Z'))
    expect(iso(start)).toBe('2026-10-04T16:00:00.000Z')
  })

  it('月档窗口 = 北京月初 0:00 到下月初，月末 23:59 仍在本月窗内', () => {
    const { start, end } = trendWindow('month', ms('2026-11-30T15:59:00Z'))
    expect(iso(start)).toBe('2026-10-31T16:00:00.000Z')
    expect(iso(end)).toBe('2026-11-30T16:00:00.000Z')
  })

  it('1 月的月档跨年不出错', () => {
    const { start, end } = trendWindow('month', ms('2026-01-15T02:00:00Z'))
    expect(iso(start)).toBe('2025-12-31T16:00:00.000Z')
    expect(iso(end)).toBe('2026-01-31T16:00:00.000Z')
  })
})

describe('T620 桶宽与序列化', () => {
  it('日档 30m、周/月档 1d（与后端 historyIntervals 白名单同名）', () => {
    expect(trendInterval('day')).toBe('30m')
    expect(trendInterval('week')).toBe('1d')
    expect(trendInterval('month')).toBe('1d')
  })

  it('零值桶留在序列里（旧实现的 filter(value>0) 会把断码窗打成空洞）', () => {
    const rows = [
      { timestamp: '2026-10-07T16:00:00Z', points: [{ pointId: 'P01', pressureValue: 30 }, { pointId: 'P02', pressureValue: 5 }] },
      { timestamp: '2026-10-07T16:30:00Z', points: [{ pointId: 'P01', pressureValue: 0 }, { pointId: 'P02', pressureValue: 0 }] },
      { timestamp: '2026-10-07T17:00:00Z', points: [{ pointId: 'P01', pressureValue: 28 }] },
    ]
    expect(toTrendSeries(rows, 'P01')).toEqual([
      { timestamp: '2026-10-07T16:00:00Z', value: 30 },
      { timestamp: '2026-10-07T16:30:00Z', value: 0 },
      { timestamp: '2026-10-07T17:00:00Z', value: 28 },
    ])
  })

  it('后端已升序，乱序入参也要排回时间升序（X 轴按真实时间定位的前提）', () => {
    const rows = [
      { timestamp: '2026-10-07T17:00:00Z', points: [{ pointId: 'P01', pressureValue: 1 }] },
      { timestamp: '2026-10-07T16:00:00Z', points: [{ pointId: 'P01', pressureValue: 2 }] },
    ]
    expect(toTrendSeries(rows, 'P01').map(p => p.timestamp)).toEqual([
      '2026-10-07T16:00:00Z',
      '2026-10-07T17:00:00Z',
    ])
  })

  it('时间戳解析不行的行丢掉（旧实现 NaN 会污染 X 轴比例）', () => {
    const rows = [
      { timestamp: 'not-a-date', points: [{ pointId: 'P01', pressureValue: 9 }] },
      { timestamp: '2026-10-07T16:00:00Z', points: [{ pointId: 'P01', pressureValue: 3 }] },
    ]
    expect(toTrendSeries(rows, 'P01')).toEqual([{ timestamp: '2026-10-07T16:00:00Z', value: 3 }])
  })

  it('选中点位不在该行时退到该行最大值；无点位入参也走最大值', () => {
    const row = { timestamp: '2026-10-07T16:00:00Z', points: [{ pointId: 'P01', pressureValue: 12 }, { pointId: 'P02', pressureValue: 40 }] }
    expect(pickPointValue(row, 'P99')).toBe(40)
    expect(pickPointValue(row)).toBe(40)
    const emptyRow = { timestamp: '2026-10-07T16:00:00Z', points: [] as { pointId: string; pressureValue: number }[] }
    expect(pickPointValue(emptyRow, 'P01')).toBe(0)
  })

  it('空窗口返回空序列（由页面决定 fallback 单点，序列化层不造点）', () => {
    expect(toTrendSeries([], 'P01')).toEqual([])
  })
})

describe('T620 接线门禁：页面必须引本 util，不得再自带一套窗口', () => {
  const here = dirname(fileURLToPath(import.meta.url))
  const pagePath = join(here, '..', '..', 'src', 'pages', 'monitor', 'index.vue')

  it('monitor 页 import 了 util 的四个出口', () => {
    expect(existsSync(pagePath)).toBe(true)
    const src = readFileSync(pagePath, 'utf8')
    expect(src).toContain('utils/trend-window')
    for (const name of ['cstDateStr', 'trendInterval', 'trendWindow', 'toTrendSeries']) {
      expect(src).toContain(name)
    }
  })

  it('旧病灶（设备本地时区窗口、前端二次分桶、翻页凑数）已从页面消失', () => {
    const src = readFileSync(pagePath, 'utf8')
    expect(src).not.toContain('aggregateByPeriod')
    expect(src).not.toContain('fetchRecordsByPeriod')
    expect(src).not.toContain("now.getFullYear()")
    expect(src).not.toContain('Math.floor(ts / bucketMs)')
  })
})
