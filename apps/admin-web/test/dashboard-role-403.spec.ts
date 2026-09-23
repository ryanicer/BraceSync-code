// T348 防回潮：医护角色进「数据概览」不得被一个 403 拖成整页空白。
//
// 复现的现场（docs/tasks/ella/T341-evidence/dashboard-probe.json 的 doctor 段）：
// doctor_li 下 6 个 dashboard 端点全 200 且有内容，唯一失败的是 GET /api/v1/teams → 403
// （网关 rbac.go adminOnlyPatterns 把 /api/v1/teams 收在 admin 专属，这是正确行为）。
// 旧实现用 Promise.all 并发 7 个请求 ⇒ 一个 reject 全盘弃，7 行赋值一行都没执行。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus, { ElMessage } from 'element-plus'
import type { DashboardKPI, DoctorRanking, TeamRanking } from '@bracesync/shared-types'

vi.mock('../src/api', () => ({
  fetchDashboardKPI: vi.fn(async (): Promise<DashboardKPI> => ({
    totalPatients: 8,
    todayActiveWear: 3,
    todayAlerts: 1,
    avgWearHours: 7.4,
    deviceOnlineRate: 33.33,
    monthNewPatients: 2,
  })),
  fetchWearTrend: vi.fn(async () => [
    { date: '09-17', avgHours: 0 },
    { date: '09-18', avgHours: 2.5 },
    { date: '09-19', avgHours: 0 },
    { date: '09-20', avgHours: 0 },
    { date: '09-21', avgHours: 9 },
    { date: '09-22', avgHours: 0 },
    { date: '09-23', avgHours: 0 },
  ]),
  fetchAlertTrend: vi.fn(async () => [
    { date: '09-17', count: 0 },
    { date: '09-18', count: 2 },
  ]),
  fetchTeamRanking: vi.fn(async (): Promise<TeamRanking[]> => [
    { rank: 1, teamName: '脊柱矫形一组', patientCount: 4, avgDailyWear: 9.1, complianceRate: 66.67 },
    { rank: 2, teamName: '脊柱矫形二组', patientCount: 2, avgDailyWear: 8.2, complianceRate: 50 },
    { rank: 3, teamName: '康复理疗三组', patientCount: 1, avgDailyWear: 7.5, complianceRate: 33.33 },
    { rank: 4, teamName: '测试组1', patientCount: 3, avgDailyWear: 6.1, complianceRate: 0 },
  ]),
  fetchDoctorRanking: vi.fn(async (): Promise<DoctorRanking[]> => [
    { rank: 1, doctorName: '李医师', teamName: '测试组1', patientCount: 3, complianceRate: 0 },
    { rank: 2, doctorName: '王医师', teamName: '脊柱矫形二组', patientCount: 2, complianceRate: 0 },
  ]),
  fetchWearDistribution: vi.fn(async () => [
    { range: '< 4小时', count: 1 },
    { range: '4-6小时', count: 2 },
    { range: '6-8小时', count: 3 },
    { range: '8-10小时', count: 1 },
    { range: '≥ 10小时', count: 1 },
  ]),
  // 医生 token 打这个端点必 403：文案按网关返回原文
  fetchTeams: vi.fn(async (): Promise<never> => {
    throw new Error('forbidden: role not allowed for this endpoint')
  }),
}))

import * as api from '../src/api'
import DashboardPage from '../src/pages/dashboard/index.vue'

async function settle() {
  for (let i = 0; i < 5; i++) await flushPromises()
}

describe('Dashboard 数据概览 · 局部请求失败（T348）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
    document.body.innerHTML = ''
  })

  it('页面不再请求 /api/v1/teams（该端点 admin 专属，医护必 403）', async () => {
    mount(DashboardPage, { global: { plugins: [createPinia(), ElementPlus] } })
    await settle()

    expect(vi.mocked(api.fetchTeams)).not.toHaveBeenCalled()
  })

  it('/teams 不可用时 6 张 KPI、4 张图表、2 张排行表全部照常渲染且不弹错误', async () => {
    const errorSpy = vi.spyOn(ElMessage, 'error')
    const wrapper = mount(DashboardPage, { global: { plugins: [createPinia(), ElementPlus] } })
    await settle()

    expect(wrapper.findAll('.kpi-card')).toHaveLength(6)
    expect(wrapper.text()).toContain('8') // 累计患者
    expect(wrapper.text()).toContain('33.33%') // 设备在线率
    expect(wrapper.findAll('canvas')).toHaveLength(4) // 含「各团队管理患者数」
    expect(wrapper.text()).toContain('脊柱矫形一组') // 团队佩戴达标排行
    expect(wrapper.text()).toContain('李医师') // 医生管理患者排行
    // 旧包现场：两张排行表各一个「暂无数据」（el-table 空态是 .el-table__empty-block，不是 .el-empty）。
    // 🔴 必须用 wrapper.findAll 而不是 document.querySelectorAll —— VTU 未传 attachTo 时组件根不在
    // document 里，后者恒为 0（探针实测：空态真实存在时 document 侧仍数到 0 ⇒ 永真断言）。
    expect(wrapper.findAll('.el-table__empty-block')).toHaveLength(0)
    expect(errorSpy).not.toHaveBeenCalled()
    errorSpy.mockRestore()
  })

  it('dashboard 自身端点失败时其余数据仍落盘，并提示一次失败原因', async () => {
    const errorSpy = vi.spyOn(ElMessage, 'error')
    vi.mocked(api.fetchWearDistribution).mockRejectedValueOnce(
      new Error('query wear distribution failed'),
    )

    const wrapper = mount(DashboardPage, { global: { plugins: [createPinia(), ElementPlus] } })
    await settle()

    expect(wrapper.findAll('.kpi-card')).toHaveLength(6)
    expect(wrapper.findAll('canvas')).toHaveLength(3) // 仅佩戴时长分布一张缺数据
    expect(errorSpy).toHaveBeenCalledTimes(1)
    expect(errorSpy).toHaveBeenCalledWith('query wear distribution failed')
    errorSpy.mockRestore()
  })

  // 反证：上一条的「空态数为 0」不能是永真断言 —— happy-dom 里 el-table 无数据时确实会渲染
  // .el-table__empty-block。全部端点都失败 ⇒ 0 张 KPI + 两张表都是空态，选择器必须命中 2。
  it('反证：全部端点失败时空态选择器真能命中（0 张 KPI + 2 个空态块）', async () => {
    const errorSpy = vi.spyOn(ElMessage, 'error')
    for (const fn of [
      api.fetchDashboardKPI, api.fetchWearTrend, api.fetchAlertTrend,
      api.fetchTeamRanking, api.fetchDoctorRanking, api.fetchWearDistribution,
    ]) {
      vi.mocked(fn).mockRejectedValue(new Error('gateway unavailable'))
    }

    const wrapper = mount(DashboardPage, { global: { plugins: [createPinia(), ElementPlus] } })
    await settle()

    expect(wrapper.findAll('.kpi-card')).toHaveLength(0)
    expect(wrapper.findAll('.el-table__empty-block')).toHaveLength(2)
    errorSpy.mockRestore()
  })
})
