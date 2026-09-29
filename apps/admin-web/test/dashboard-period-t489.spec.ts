// T489：数据概览的 period 必须贯通全部数据区。
//
// 复现的现场（Boss 2026-09-29 21:33 亲报）：切「本周/本月」只有 KPI 跟随，两块排行、佩戴分布
// 固定近 7 日窗口，两条趋势固定 days=7 —— 因为 loadData 里后三个 fetch 根本不带 period。
// 这张 spec 锁的是「前端到底把什么参数发出去」，接口是否真按 period 过滤由后端 Go 用例负责。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus, { ElSelect } from 'element-plus'
import type { DashboardKPI, DoctorRanking, TeamRanking } from '@bracesync/shared-types'

vi.mock('../src/api', () => ({
  fetchDashboardKPI: vi.fn(async (): Promise<DashboardKPI> => ({
    totalPatients: 8, todayActiveWear: 3, todayAlerts: 1,
    avgWearHours: 7.4, deviceOnlineRate: 33.33, monthNewPatients: 2,
  })),
  fetchWearTrend: vi.fn(async (): Promise<{ date: string; avgHours: number }[]> => [
    { date: '09-23', avgHours: 7.5 },
  ]),
  fetchAlertTrend: vi.fn(async (): Promise<{ date: string; count: number }[]> => [
    { date: '09-23', count: 2 },
  ]),
  fetchTeamRanking: vi.fn(async (): Promise<TeamRanking[]> => [
    { rank: 1, teamName: '脊柱矫形一组', patientCount: 4, avgDailyWear: 9.1, complianceRate: 66.67 },
  ]),
  fetchDoctorRanking: vi.fn(async (): Promise<DoctorRanking[]> => [
    { rank: 1, doctorName: '李医师', teamName: '测试组1', patientCount: 3, complianceRate: 0 },
  ]),
  fetchWearDistribution: vi.fn(async (): Promise<{ range: string; count: number }[]> => [
    { range: '< 4小时', count: 1 },
  ]),
}))

import * as api from '../src/api'
import DashboardPage from '../src/pages/dashboard/index.vue'

type Period = 'today' | 'week' | 'month'

function mountPage() {
  return mount(DashboardPage, { global: { plugins: [createPinia(), ElementPlus] } })
}

async function settle() {
  for (let i = 0; i < 5; i++) await flushPromises()
}

async function switchPeriod(wrapper: ReturnType<typeof mountPage>, next: Period) {
  const select = wrapper.findComponent(ElSelect)
  // 真实交互里选中一项会连发两条：update:modelValue（喂 v-model）与 change（挂 @change）
  await select.vm.$emit('update:modelValue', next)
  await select.vm.$emit('change', next)
  await settle()
}

const SIX_REGIONS = [
  api.fetchDashboardKPI, api.fetchWearTrend, api.fetchAlertTrend,
  api.fetchTeamRanking, api.fetchDoctorRanking, api.fetchWearDistribution,
]

describe('Dashboard 数据概览 · period 贯通（T489）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
    document.body.innerHTML = ''
  })

  it('首屏（缺省 today）：六个数据区各发一次，趋势按 days=7', async () => {
    const wrapper = mountPage()
    await settle()

    expect(api.fetchDashboardKPI).toHaveBeenCalledWith('today')
    expect(api.fetchWearTrend).toHaveBeenCalledWith(7)
    expect(api.fetchAlertTrend).toHaveBeenCalledWith(7)
    expect(api.fetchTeamRanking).toHaveBeenCalledWith('today')
    expect(api.fetchDoctorRanking).toHaveBeenCalledWith('today')
    expect(api.fetchWearDistribution).toHaveBeenCalledWith('today')
    for (const fn of SIX_REGIONS) expect(fn).toHaveBeenCalledTimes(1)
  })

  it('切「本月」：六个数据区全部重发，period=month、趋势 days=30', async () => {
    const wrapper = mountPage()
    await settle()
    await switchPeriod(wrapper, 'month')

    expect(api.fetchDashboardKPI).toHaveBeenLastCalledWith('month')
    expect(api.fetchWearTrend).toHaveBeenLastCalledWith(30)
    expect(api.fetchAlertTrend).toHaveBeenLastCalledWith(30)
    expect(api.fetchTeamRanking).toHaveBeenLastCalledWith('month')
    expect(api.fetchDoctorRanking).toHaveBeenLastCalledWith('month')
    expect(api.fetchWearDistribution).toHaveBeenLastCalledWith('month')
    for (const fn of SIX_REGIONS) expect(fn).toHaveBeenCalledTimes(2)
  })

  it('切「本周」：period=week，趋势仍是 days=7（派发单口径：本周维持近 7 日趋势现状）', async () => {
    const wrapper = mountPage()
    await settle()
    await switchPeriod(wrapper, 'week')

    expect(api.fetchTeamRanking).toHaveBeenLastCalledWith('week')
    expect(api.fetchDoctorRanking).toHaveBeenLastCalledWith('week')
    expect(api.fetchWearDistribution).toHaveBeenLastCalledWith('week')
    expect(api.fetchWearTrend).toHaveBeenLastCalledWith(7)
    expect(api.fetchAlertTrend).toHaveBeenLastCalledWith(7)
  })

  it('趋势卡标题跟随窗口：默认「近7天」，切本月后是「近30天」', async () => {
    const wrapper = mountPage()
    await settle()
    expect(wrapper.text()).toContain('近7天日均佩戴时长')
    expect(wrapper.text()).toContain('近7天告警趋势')

    await switchPeriod(wrapper, 'month')
    expect(wrapper.text()).toContain('近30天日均佩戴时长')
    expect(wrapper.text()).toContain('近30天告警趋势')
    expect(wrapper.text()).not.toContain('近7天日均佩戴时长')
  })

  it('反证：任一数据区不跟周期就停在早先的调用次数（三次拉取 × 六个区）', async () => {
    const wrapper = mountPage()
    await settle()
    await switchPeriod(wrapper, 'month')
    await switchPeriod(wrapper, 'today')

    for (const fn of SIX_REGIONS) expect(fn).toHaveBeenCalledTimes(3)
  })
})
