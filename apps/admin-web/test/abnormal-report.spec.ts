// T372 异常报告页的口径单测（纯层 + 抽屉跳转入口）。
// 依据：docs/design/admin/异常报告.html（KPI 口径 :353、快捷范围 :196-201、文字汇总模板 :354）
//       + PRD §7D.15；换算逻辑在 src/utils/abnormal-report.ts，SFC 内测不到 ⇒ 抽出来钉。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createRouter, createMemoryHistory, type RouteRecordRaw } from 'vue-router'
import { defineComponent } from 'vue'
import PatientsPage from '../src/pages/patients/index.vue'
import {
  lastNDays, monthToDate, rangeDays, kpiFromReport, stampText, summaryLines, type ReportRange,
} from '../src/utils/abnormal-report'
import type { AbnormalReport } from '../src/mock/alerts'

const Stub = defineComponent({ render: () => null })

/** mock 模式的列表数据经 setTimeout 落地，假计时器要连推几轮（同 test/patient-qrcode.spec.ts） */
async function flushAll() {
  for (let i = 0; i < 8; i++) {
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
  }
}
const range: ReportRange = { start: '2026-07-15', end: '2026-07-21' } // 稿面示例区间 = 7 天

function report(over: Partial<AbnormalReport> = {}): AbnormalReport {
  return {
    patientId: 'P20260001',
    start: range.start,
    end: range.end,
    total: 29,
    byStatus: [
      { key: 'pending', count: 2 },
      { key: 'processing', count: 2 },
      { key: 'processed', count: 25 },
    ],
    byType: [
      { key: 'pressure_high', count: 15 },
      { key: 'wear_interrupt', count: 9 },
      { key: 'sensor_drift', count: 3 },
      { key: 'wear_duration_short', count: 2 },
    ],
    byDay: [
      { key: '2026-07-15', count: 4 },
      { key: '2026-07-16', count: 5 },
    ],
    ...over,
  }
}

describe('区间换算（稿面「快捷范围」与含端点天数）', () => {
  const today = new Date(2026, 6, 21) // 本地 2026-07-21

  it('近 7 天 = 含今天的 7 个日历日', () => {
    expect(lastNDays(7, today)).toEqual({ start: '2026-07-15', end: '2026-07-21' })
  })

  it('近 30 天跨月正确', () => {
    expect(lastNDays(30, today)).toEqual({ start: '2026-06-22', end: '2026-07-21' })
  })

  it('本月至今 = 1 号到今天', () => {
    expect(monthToDate(today)).toEqual({ start: '2026-07-01', end: '2026-07-21' })
  })

  it('生成时间戳 = 稿面 :310 的「YYYY-MM-DD HH:mm:ss」补零（不用 toLocaleString 的斜杠分隔）', () => {
    expect(stampText(new Date(2026, 6, 21, 16, 4, 12))).toBe('2026-07-21 16:04:12')
    expect(stampText(new Date(2026, 0, 5, 0, 0, 0))).toBe('2026-01-05 00:00:00')
  })

  it('rangeDays 含端点；倒挂与非法日期回 0（调用方按「不算日均」处理）', () => {
    expect(rangeDays(range)).toBe(7)
    expect(rangeDays({ start: '2026-07-21', end: '2026-07-21' })).toBe(1)
    expect(rangeDays({ start: '2026-07-21', end: '2026-07-15' })).toBe(0)
    expect(rangeDays({ start: '2026/07/15', end: '2026-07-21' })).toBe(0)
  })
})

describe('KPI 口径（稿面 :353「同点多次触发按事件计、不合并」）', () => {
  it('日均 = 总次数 / 区间日历天数；未处理 = processed 之外的全部状态', () => {
    const k = kpiFromReport(report(), range)
    expect(k.dailyAvg).toBe(4.1) // 29 / 7 = 4.142… → 4.1（稿面同值）
    expect(k.unprocessed).toBe(4) // 2 + 2，稿面「未处理条数 4」
    expect(k.processedRate).toBe(86.2) // 25 / 29，稿面「处理率 86.2%」
  })

  it('压力偏高次数与占比取自 byType，键缺失记 0 而不是 NaN', () => {
    const k = kpiFromReport(report({ byType: [] }), range)
    expect(k.pressureHigh).toBe(0)
    expect(k.pressureHighShare).toBe(0)
  })

  it('total = 0 时占比与处理率均为 0（空区间不得出现除零）', () => {
    const k = kpiFromReport(report({ total: 0, byType: [], byStatus: [] }), range)
    expect(k).toMatchObject({ total: 0, dailyAvg: 0, pressureHighShare: 0, processedRate: 0, unprocessed: 0 })
  })

  it('区间天数拿不到时日均为 0，不用 total 冒充', () => {
    expect(kpiFromReport(report(), { start: '2026-07-21', end: '2026-07-15' }).dailyAvg).toBe(0)
  })
})

describe('文字汇总（稿面固定 6 段模板的可用 4 段）', () => {
  it('只出 ①②⑤⑥ 四段，③集中点位 / ④佩戴依从性 不产出（现读端点无点位与时长维度）', () => {
    const lines = summaryLines(report(), range)
    expect(lines).toHaveLength(4)
    expect(lines[0]).toMatch(/^① 总体：.*共产生异常 29 次，日均 4\.1 次/)
    // ② 用新四类名（Boss 2026-09-20 裁定，与「告警管理」页同一 alertTypeLabel 单一来源）
    expect(lines[1]).toContain('设备离线 9 次')
    expect(lines[1]).toContain('传感器标定异常 3 次')
    expect(lines[1]).not.toContain('传感器漂移') // 旧词不得在本页回潮
    expect(lines[2]).toMatch(/^⑤ 处理情况：已处理 25 次，未处理 4 次/)
  })

  it('⑥ 免责提示是固定文案，不随数据变化（稿面 :354）', () => {
    expect(summaryLines(report(), range)[3]).toBe(summaryLines(report({ total: 0 }), range)[3])
    expect(summaryLines(report(), range)[3]).toContain('不构成诊疗结论')
  })

  it('空区间：② 明写「无异常记录」而不是留下一串顿号', () => {
    const lines = summaryLines(report({ total: 0, byType: [], byStatus: [] }), range)
    expect(lines[1]).toBe('② 构成：区间内无异常记录')
  })
})

describe('患者管理抽屉的异常报告入口（T372 拆页后的处置）', () => {
  let wrapper: VueWrapper | null = null
  const routes: RouteRecordRaw[] = [
    { path: '/', component: Stub },
    { path: '/patients', component: Stub },
    { path: '/abnormal-report', component: Stub },
  ]

  async function mountPage() {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/patients')
    await router.isReady()
    wrapper = mount(PatientsPage, { global: { plugins: [router, ElementPlus] } })
    await flushAll()
    return router
  }

  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.useRealTimers()
  })

  async function openDrawer() {
    await flushAll()
    const rows = wrapper!.findAll('.el-table__body tr')
    if (!rows[0]) throw new Error(`列表没有数据（mock 模式应有患者行）：${rows.length} 行`)
    await rows[0].trigger('click') // 行点击 = 「查看」打开详情抽屉（同 test/patient-qrcode.spec.ts）
    await flushAll()
    // el-drawer 可能 teleport 到 body，也可能就地渲染 ⇒ 两处都找（同 test/patient-qrcode.spec.ts）
    const drawer = document.querySelector('.el-drawer') ?? wrapper!.element.querySelector('.el-drawer')
    expect(drawer, '患者详情抽屉应打开').not.toBeNull()
    return drawer as HTMLElement
  }

  it('抽屉不再内嵌异常报告区块（稿面已拆独立页，避免双实现）', async () => {
    await mountPage()
    const drawer = await openDrawer()
    expect(drawer.querySelector('.abnormal-report'), '抽屉内的汇总区块应已移除').toBeNull()
    expect(drawer.textContent, '基本信息仍要在').toContain('建档时间')
  })

  it('点「异常报告」跳独立页并把患者带过去（稿「进入条件①」）', async () => {
    const router = await mountPage()
    const drawer = await openDrawer()
    const btn = [...drawer.querySelectorAll('button')].find((b) => b.textContent?.includes('异常报告'))
    expect(btn, '抽屉要有「异常报告」跳转按钮').not.toBeUndefined()
    btn!.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/abnormal-report')
    expect(String(router.currentRoute.value.query.patient), '患者须自动定位').toMatch(/^P/)
  })
})
