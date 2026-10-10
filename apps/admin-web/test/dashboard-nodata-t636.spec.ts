// T636 防回潮：后端把「窗口内查不到」回成 null（不再 COALESCE 成 0）之后，
// 页面不得把 null 渲染成「0h / 0%」，也不得把全 null 的序列画成一条贴零假线。
// 现场依据：staging today 窗口实测 avgWearHours=0、deviceOnlineRate=0、7 日趋势全 0，
// 而库里当日确实没有 daily_wear_stats 行 —— 0 是被造出来的，不是量出来的。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import { Chart } from 'chart.js'
import type { DashboardKPI } from '@bracesync/shared-types'

const NULL_KPI: DashboardKPI = {
  totalPatients: 0,
  todayActiveWear: 0,
  todayAlerts: 0,
  avgWearHours: null,
  deviceOnlineRate: null,
  monthNewPatients: 0,
}

const NUMERIC_KPI: DashboardKPI = { ...NULL_KPI, avgWearHours: 7.4, deviceOnlineRate: 33.33 }

// 缺行日（09-17、09-19）库里没有聚合行 ⇒ 后端回 null，不是 0
const MIXED_TREND = [
  { date: '09-17', avgHours: null },
  { date: '09-18', avgHours: 2.5 },
  { date: '09-19', avgHours: null },
]
const ALL_NULL_TREND = MIXED_TREND.map((d) => ({ ...d, avgHours: null }))

vi.mock('../src/api', () => ({
  fetchDashboardKPI: vi.fn(async (): Promise<DashboardKPI> => NULL_KPI),
  fetchWearTrend: vi.fn(async () => MIXED_TREND),
  fetchAlertTrend: vi.fn(async () => [{ date: '09-17', count: 0 }]),
  fetchTeamRanking: vi.fn(async () => []),
  fetchDoctorRanking: vi.fn(async () => []),
  fetchWearDistribution: vi.fn(async () => []),
}))

import * as api from '../src/api'
import DashboardPage from '../src/pages/dashboard/index.vue'

async function settle() {
  for (let i = 0; i < 5; i++) await flushPromises()
}

function mountPage() {
  return mount(DashboardPage, { global: { plugins: [createPinia(), ElementPlus] } })
}

describe('Dashboard 数据概览 · 无数据两态（T636）', () => {
  // 🔴 每个用例自带喂数：clearAllMocks 只清调用记录、不清 mockResolvedValue，
  // 上一笔喂的数组会漏到下一格（实测把「混排」那格读成只有 1 张图）。
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
    document.body.innerHTML = ''
    vi.mocked(api.fetchDashboardKPI).mockResolvedValue(NULL_KPI)
    vi.mocked(api.fetchWearTrend).mockResolvedValue(MIXED_TREND)
  })

  it('KPI 的 null 项渲染「—」＋「暂无数据」，count 类 0 仍显示 0', async () => {
    const wrapper = mountPage()
    await settle()

    const cards = wrapper.findAll('.kpi-card')
    expect(cards).toHaveLength(6)
    const notes = wrapper.findAll('.kpi-note')
    expect(notes).toHaveLength(2) // 只有平均佩戴时长与设备在线率两项缺数据
    expect(notes.map((n) => n.text()).join('|')).toBe('暂无数据|暂无数据')
    const values = cards.map((c) => c.find('.kpi-value').text())
    expect(values[3]).toBe('—')
    expect(values[4]).toBe('—')
    // 🔴 反证：count 类无行时后端回 0 是实测事实，不能跟着变 null 而被画成「—」
    expect(values[0]).toBe('0')
    expect(values[2]).toBe('0')
    expect(wrapper.text()).not.toContain('0h')
    expect(wrapper.text()).not.toContain('0%')
    wrapper.unmount()
  })

  it('佩戴趋势整窗全 null ⇒ 不画图，换「暂无数据」占位', async () => {
    vi.mocked(api.fetchWearTrend).mockResolvedValue(ALL_NULL_TREND)
    const wrapper = mountPage()
    await settle()

    // 4 张图里只有告警趋势有数据（mock 回 1 条 count），其余 3 张缺数据 ⇒ 3 个占位
    expect(wrapper.findAll('canvas')).toHaveLength(1)
    const empties = wrapper.findAll('.el-empty')
    expect(empties).toHaveLength(3)
    expect(empties.map((e) => e.text()).join('|')).toBe(Array(3).fill('暂无数据').join('|'))
    wrapper.unmount()
  })

  it('混排时缺行日以 null 断线，不补 0，且曲线不做平滑', async () => {
    vi.mocked(api.fetchDashboardKPI).mockResolvedValue(NUMERIC_KPI)
    const wrapper = mountPage()
    await settle()

    // 佩戴趋势 + 告警趋势各一张（排行/分布 mock 回空数组 ⇒ 走占位）
    const canvases = wrapper.findAll('canvas')
    expect(canvases).toHaveLength(2)
    const chart = Chart.getChart(canvases[0]!.element as HTMLCanvasElement)
    if (!chart) throw new Error('Chart.getChart 取不到佩戴趋势实例，本格的 dataset 断言无从落脚')
    // 尺的落点＝Chart.js 真正收到的那份 dataset 载荷（v4 解析后的 dataset.options 在这里不在场，
    // 押它会读成 undefined 而不是读成行为）。
    const ds = chart.data.datasets[0] as { data: (number | null)[]; tension?: number; spanGaps?: boolean }
    expect(ds.data).toEqual([null, 2.5, null])
    // tension 缺省（0）：旧值 0.4 是凭空造的钟形平滑，两个真实点之间不该被抹成弧线
    expect(ds.tension).toBeUndefined()
    expect(ds.spanGaps).toBe(false)
    // 有数据的 KPI 卡恢复正常读数，且不再出现空态副文案
    expect(wrapper.findAll('.kpi-note')).toHaveLength(0)
    expect(wrapper.text()).toContain('7.4h')
    expect(wrapper.text()).toContain('33.33%')
    wrapper.unmount()
  })
})
