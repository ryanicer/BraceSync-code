// T430「压力波动」历史行界面隐藏的展示面用例（PRD §7D.6 历史数据处置已拍 C·Boss 2026-09-27）。
//
// 派发口径：PM 2026-09-27 19:00 裁定「界面隐藏、数据不删，本卡纯前端过滤、无数据迁移」，
// 范围＝admin 三处（告警列表页 / 实时监控事件流 / 异常报告页）+ 导出 CSV 类型列同步过滤。
// 每面各挂各的断言（领卡前在卡内承诺的口径），不共用一套：
//   ① 判据本体在 packages/shared-utils/test/index.test.ts
//   ② 告警列表页在 e2e/tests/admin-alerts.spec.ts（该页静态 import FlowDesigner.vue ⇒
//      @logicflow/core 的 CJS 产物 require ESM 的 lodash-es，CI 的 Node 18 下 ERR_REQUIRE_ESM，
//      整份 spec 会变成「0 用例收集却显示全过」的假绿 —— 同 T351/T275 的教训，不在此 mount）
//   ③④⑤ 本文件：监控事件流（mount）/ 异常报告图表与摘要（纯层）/ mock CSV 正文
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import type { Alert } from '@bracesync/shared-types'
import MonitorPage from '../src/pages/monitor/index.vue'
import { visibleByType, summaryLines, kpiFromReport, type ReportRange } from '../src/utils/abnormal-report'
import { mockAbnormalReport, mockAbnormalReportCsv, type AbnormalReport } from '../src/mock/alerts'
import { HIDDEN_ALERT_TYPES, isHiddenAlertType } from '@bracesync/shared-utils'

vi.mock('../src/api', () => ({
  fetchPatients: vi.fn(async () => ({
    list: [{ patientId: 'PT-001', name: '林小雨', deviceId: 'DEV-A3F312' }],
    total: 1,
  })),
  fetchPatientRealtime: vi.fn(async () => ({
    status: 'abnormal',
    todayHours: 7.2,
    maxPressure: 68.5,
    maxPoint: 'P10',
    events: 3,
    pressureRecords: [],
    alerts: [
      { alertId: 'E-1', type: 'pressure_high', detail: 'P10 压力持续偏高', sensorPoint: 'P10', timestamp: '2026-09-27T10:00:00+08:00' },
      { alertId: 'E-2', type: 'pressure_fluctuation', detail: 'P05 压力波动异常', sensorPoint: 'P05', timestamp: '2026-09-27T09:00:00+08:00' },
      { alertId: 'E-3', type: 'sensor_drift', detail: 'P12 传感器数据漂移', sensorPoint: 'P12', timestamp: '2026-09-27T08:00:00+08:00' },
    ],
    pressureHeatmap: [],
  })),
}))

const range: ReportRange = { start: '2026-09-21', end: '2026-09-27' }

/** 含一条已裁砍除类型历史行的报告（byType 里刻意带着 pressure_fluctuation ⇒ 库里数据不删） */
function report(over: Partial<AbnormalReport> = {}): AbnormalReport {
  return {
    patientId: 'PT-001',
    start: range.start,
    end: range.end,
    total: 10,
    byStatus: [
      { key: 'pending', count: 4 },
      { key: 'processing', count: 2 },
      { key: 'processed', count: 4 },
    ],
    byType: [
      { key: 'pressure_high', count: 4 },
      { key: 'wear_interrupt', count: 2 },
      { key: 'pressure_fluctuation', count: 2 },
      { key: 'sensor_drift', count: 2 },
    ],
    byDay: [{ key: '2026-09-25', count: 10 }],
    ...over,
  }
}

async function settle(times = 8) {
  for (let i = 0; i < times; i++) await flushPromises()
}

describe('③ 实时监控事件流（mount 真实 DOM）', () => {
  let wrapper: VueWrapper | null = null

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    document.body.innerHTML = ''
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
  })

  async function mountMonitor() {
    wrapper = mount(MonitorPage, { global: { plugins: [ElementPlus] } })
    await settle()
    return wrapper
  }

  it('事件流只剩未砍除的两类，压力波动那行不渲染（mount 后按 .event-type 逐个取文本）', async () => {
    const w = await mountMonitor()
    const labels = w.findAll('.events-table .event-type').map((n) => n.text())
    expect(labels).toEqual(['压力偏高', '传感器标定异常'])
    expect(labels.join()).not.toContain('压力波动')
    // 裸码值也不许出现在页面上（alertTypeLabel 查不到键才吐裸码，本卡保留键 ⇒ 两头都不露）
    expect(w.text()).not.toContain('pressure_fluctuation')
  })

  it('隐藏的只是这一类：详情正文一并消失，且行数少于接口给的 events=3', async () => {
    const w = await mountMonitor()
    // 作用域限定到事件表：页上另有一张「采集点」points-table，不限定会把它的行算进来
    const rows = w.findAll('.events-table tbody tr')
    expect(rows).toHaveLength(2)
    expect(w.text()).not.toContain('P05 压力波动异常')
    // 页头「异常事件数」取 /realtime 的 events（后端今日异常值，本卡裁定不改后端）⇒ 3 与 2 并存是登记过的口径，
    // 数字本身随轮询状态变，这里只钉「隐藏不会去改这个计数」这条不变式。
    expect(w.text()).toContain('今日异常事件')
    const eventsKpi = w.text().match(/今日异常事件\s*(\d+)/)?.[1]
    expect(eventsKpi, '页头异常事件数仍按接口 events 显示（前端不扣）').toBe('3')
  })

  it('反证：判据对本面真生效 —— 全为砍除类型时落「无异常事件」空态而不是空表壳', async () => {
    const api = await import('../src/api')
    // mockResolvedValue（非 Once）：进页会连打两次 /realtime（onMounted 一次 + 选患者 watch 一次），
    // 用 Once 只有第一次吃到这份数据，第二次落回默认三类样本 ⇒ 假绿。
    vi.mocked(api.fetchPatientRealtime).mockResolvedValue({
      status: 'abnormal',
      todayHours: 7.2,
      maxPressure: 68.5,
      maxPoint: 'P10',
      events: 1,
      pressureRecords: [],
      alerts: [
        { alertId: 'E-9', type: 'pressure_fluctuation', detail: '只剩历史行', sensorPoint: 'P05', timestamp: '2026-09-27T07:00:00+08:00' },
      ] as unknown as Alert[],
      pressureHeatmap: [],
    } as never)
    const w = await mountMonitor()
    expect(w.findAll('.events-table .event-type')).toHaveLength(0)
    expect(w.findAll('.events-table tbody tr')).toHaveLength(1)
    expect(w.text()).toContain('无异常事件')
  })
})

describe('④ 异常报告页：图表数据源与文字汇总（纯层，同 T372 抽层理由）', () => {
  it('visibleByType 剔掉砍除类型，其余类型原序原值（图表扇区少了这一格，不是被并入他格）', () => {
    const visible = visibleByType(report())
    expect(visible.map((x) => x.key)).toEqual(['pressure_high', 'wear_interrupt', 'sensor_drift'])
    expect(visible.find((x) => x.key === 'pressure_high')?.count).toBe(4)
  })

  it('文字汇总 ② 构成不含「压力波动」，也不含裸码值', () => {
    const line = summaryLines(report(), range)[1]
    expect(line).not.toContain('压力波动')
    expect(line).not.toContain('pressure_fluctuation')
    expect(line).toContain('压力偏高 4 次')
  })

  it('数据不删：total / byStatus / byDay 一律不动（前端没有类型维度，硬算就是编数据）', () => {
    const r = report()
    expect(visibleByType(r).reduce((s, x) => s + x.count, 0)).toBe(8)
    expect(r.total).toBe(10)
    expect(kpiFromReport(r, range).total).toBe(10)
    expect(summaryLines(r, range)[0]).toContain('共产生异常 10 次')
  })

  it('区间内只剩砍除类型时：图表数据源为空（页面据此不画图），② 落到「无异常记录」', () => {
    const only = report({
      total: 2,
      byType: [{ key: 'pressure_fluctuation', count: 2 }],
    })
    expect(visibleByType(only)).toEqual([])
    expect(summaryLines(only, range)[1]).toBe('② 构成：区间内无异常记录')
  })
})

describe('⑤ 导出 CSV（mock 通道与后端同律）', () => {
  const q = { patientId: 'PT-001', start: '2026-09-01', end: '2026-09-27' }

  it('CSV 正文既无「压力波动」中文标签也无裸码值', () => {
    const csv = mockAbnormalReportCsv(q)
    expect(csv).not.toContain('压力波动')
    expect(csv).not.toContain('pressure_fluctuation')
  })

  it('判别力：同一无参数调用在有过滤时必有行 ⇒ 断空不是靠明细本就为空', () => {
    const csv = mockAbnormalReportCsv(q)
    const lines = csv.replace(/^\ufeff/, '').trim().split('\r\n')
    expect(lines.length).toBeGreaterThan(1)
    const typeCol = new Set(lines.slice(1).map((l) => l.split(',')[4]))
    expect(typeCol.size).toBeGreaterThan(0)
    for (const t of typeCol) {
      expect(isHiddenAlertType(HIDDEN_ALERT_TYPES.includes(t) ? t : null), `CSV 类型列出现被隐藏项 ${t}`).toBe(false)
    }
    expect([...typeCol].every((t) => t !== '压力波动')).toBe(true)
  })

  it('报告 JSON（汇总视图）仍带该类型 ⇒ 收口只发生在展示/导出面，库里数据不动', () => {
    const r = mockAbnormalReport(q)
    expect(r.byType.some((x) => x.key === 'pressure_fluctuation')).toBe(true)
  })
})
