// T597 —— 六处时间列改走东八区公共出口（原 UTC 串切片早 8 小时）。
//
// 两层断言：
//  ① 纯函数口径：卡面实测样本（提交 12:11 北京、库侧 04:11Z、页面曾显示 04:11）修后必须显示 12:11；
//     跨日、北京凌晨段（卡面待核实的「日期切前一天」推论）、非法输入。
//     CI runner 系统时区是 UTC —— 实现若漏写显式 timeZone，这里必然红，断言自带牙。
//  ② 源码门禁：六页不得再出现 ISO 串切片（slice(11, 16) 形态），防止有人改回字面切片。
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { formatCstDateTime, formatCstMonthDayTime } from '../src/utils/formatTime'

const srcOf = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), 'utf8')

// 卡面（T597 第一节）：患者端 12:11 提交，接口回 2026-10-06T04:11:46Z，页面曾渲染 04:11
const t597Sample = '2026-10-06T04:11:46Z'

describe('formatCstDateTime / formatCstMonthDayTime：东八区渲染（T597）', () => {
  it('卡面实测样本：页面必须显示北京时间 12:11，不是 UTC 面 04:11', () => {
    expect(formatCstDateTime(t597Sample)).toBe('2026-10-06 12:11')
    expect(formatCstMonthDayTime(t597Sample)).toBe('10-06 12:11')
    expect(formatCstDateTime(t597Sample)).not.toContain('04:11')
  })

  it('跨日：UTC 16:30 = 北京次日 00:30，日期进位且不出现 24:00（hourCycle h23）', () => {
    expect(formatCstDateTime('2026-10-06T16:30:00Z')).toBe('2026-10-07 00:30')
    expect(formatCstMonthDayTime('2026-10-06T16:30:00Z')).toBe('10-07 00:30')
  })

  it('北京 00:00-07:59 段：日期取东八区（卡面「日期切成前一天」推论就此封口）', () => {
    // UTC 10-05 20:00 = 北京 10-06 04:00：旧切片会显示 10-05 04:00（日期早一天）
    expect(formatCstDateTime('2026-10-05T20:00:00Z')).toBe('2026-10-06 04:00')
  })

  it('带时区偏移的输入同样换算（非 Z 结尾的 RFC3339）', () => {
    expect(formatCstDateTime('2026-10-06T12:11:46+08:00')).toBe('2026-10-06 12:11')
  })

  it('非法输入回 "-"（对齐各页原守卫的占位，不抛错）', () => {
    expect(formatCstDateTime(null)).toBe('-')
    expect(formatCstDateTime(undefined)).toBe('-')
    expect(formatCstDateTime('')).toBe('-')
    expect(formatCstDateTime('not-a-date')).toBe('-')
    expect(formatCstMonthDayTime(null)).toBe('-')
    expect(formatCstMonthDayTime('not-a-date')).toBe('-')
  })
})

describe('源码门禁：六页不得再对 ISO 串做切片（T597 缺陷形态不得回潮）', () => {
  const pages = [
    '../src/pages/alerts/index.vue',
    '../src/pages/communication/index.vue',
    '../src/pages/devices/index.vue',
    '../src/pages/install-records/index.vue',
    '../src/pages/orthosis-log/index.vue',
    '../src/pages/settings/index.vue',
  ]

  for (const p of pages) {
    it(`${p.replace('../src/pages/', '')} 走公共出口、无 ISO 切片`, () => {
      const src = srcOf(p)
      expect(src).not.toMatch(/slice\(11,\s*16\)/)
      expect(src).not.toMatch(/slice\(0,\s*10\)\s*\$\{/)
      expect(src).not.toMatch(/slice\(5,\s*10\)\s*\$\{/)
      expect(src).toMatch(/from '\.\.\/\.\.\/utils\/formatTime'/)
    })
  }
})
