// T344 患者工作台「数据视图」纯层单测 + mock 日佩戴聚合。
// 判据要点：区间必须按东八区取今日（否则整张图错一天）、缺日必须是 null 而不是 0
// （0 = 「佩戴 0 小时」是一条会被判佩戴时长不足的实数据，null 才是「无统计」）。
import { describe, it, expect } from 'vitest'
import {
  alignWearSeries, constantLine, cstDate, listDates, rangeForDays, seriesIsEmpty, toWearHours,
  type DailyWearDay,
} from '../src/utils/workbenchData'
import { mockPatientDailyWear } from '../src/mock/patients'

const row = (date: string, wearMinutes: number, avgPressure = 30): DailyWearDay => ({
  date, wearMinutes, avgPressure, maxPressure: avgPressure + 8, maxPoint: 'P07', frameCount: 800, abnormalCount: 0,
})

describe('cstDate 东八区切日', () => {
  it('UTC 17:00 已是东八区次日 01:00 ⇒ 日期进一天（用 UTC 或本地取日都会错一天）', () => {
    expect(cstDate(new Date('2026-09-23T17:00:00Z'))).toBe('2026-09-24')
    expect(cstDate(new Date('2026-09-23T15:59:00Z'))).toBe('2026-09-23')
  })
})

describe('rangeForDays 滚动区间', () => {
  const today = new Date('2026-09-23T06:00:00Z') // 东八区 09-23 14:00

  it('7/14/30 天都是「含今日」的闭区间，首末差 = N-1 天', () => {
    for (const days of [7, 14, 30] as const) {
      const r = rangeForDays(days, today)
      expect(r.end).toBe('2026-09-23')
      expect(listDates(r)).toHaveLength(days)
    }
    expect(rangeForDays(7, today).start).toBe('2026-09-17')
    expect(rangeForDays(14, today).start).toBe('2026-09-10')
    expect(rangeForDays(30, today).start).toBe('2026-08-25')
  })

  it('跨月倒推不出错（月初往回借上月天数）', () => {
    expect(rangeForDays(7, new Date('2026-03-03T06:00:00Z')).start).toBe('2026-02-25')
    expect(rangeForDays(30, new Date('2026-01-05T06:00:00Z')).start).toBe('2025-12-07')
  })
})

describe('listDates 闭区间枚举', () => {
  it('首末含端点', () => {
    const dates = listDates({ start: '2026-09-17', end: '2026-09-23' })
    expect(dates).toHaveLength(7)
    expect(dates[0]).toBe('2026-09-17')
    expect(dates[6]).toBe('2026-09-23')
  })

  it('start 晚于 end ⇒ 空数组，不吐出负长度', () => {
    expect(listDates({ start: '2026-09-23', end: '2026-09-17' })).toEqual([])
  })
})

describe('toWearHours 分钟折小时', () => {
  it('保留 1 位小数，不向上取整', () => {
    expect(toWearHours(90)).toBe(1.5)
    expect(toWearHours(511)).toBe(8.5) // 8.5166…
    expect(toWearHours(0)).toBe(0)
  })
})

describe('alignWearSeries 稀疏行对齐', () => {
  const range = { start: '2026-09-20', end: '2026-09-23' }

  it('缺日是 null 不是 0（后端只回有统计行的日子）', () => {
    const series = alignWearSeries([row('2026-09-21', 600)], range)
    expect(series.dates).toEqual(['2026-09-20', '2026-09-21', '2026-09-22', '2026-09-23'])
    expect(series.wearHours).toEqual([null, 10, null, null])
    expect(series.avgPressure).toEqual([null, 30, null, null])
  })

  it('乱序返回的统计行按日期落位，区间外的行丢弃', () => {
    const series = alignWearSeries([row('2026-09-23', 120), row('2026-09-12', 999)], range)
    expect(series.wearHours).toEqual([null, null, null, 2])
  })

  it('空数组 ⇒ 整条序列全 null（页面上走空态而不是画一张空轴假图）', () => {
    const series = alignWearSeries([], range)
    expect(series.avgPressure).toEqual([null, null, null, null])
    expect(seriesIsEmpty(series)).toBe(true)
  })
})

describe('constantLine 参考线', () => {
  it('按点位长度铺同一个配置值', () => {
    expect(constantLine(45, 3)).toEqual([45, 45, 45])
    expect(constantLine(45, 0)).toEqual([])
  })
})

describe('mockPatientDailyWear 日佩戴聚合', () => {
  it('已绑定设备的患者：逐日出数，且同一区间两次调用结果一致（用例才钉得住）', () => {
    const range = { start: '2026-09-17', end: '2026-09-23' }
    const a = mockPatientDailyWear('PT-001', range.start, range.end)
    const b = mockPatientDailyWear('PT-001', range.start, range.end)
    expect(a).toHaveLength(7)
    expect(a.map((d) => d.date)).toEqual(listDates(range))
    expect(a).toEqual(b)
    expect(a[0].wearMinutes).toBeGreaterThan(0)
  })

  it('未绑定设备的患者（PT-005 赵欣然）⇒ 空数组，对齐后端无统计行', () => {
    expect(mockPatientDailyWear('PT-005', '2026-09-17', '2026-09-23')).toEqual([])
  })
})
