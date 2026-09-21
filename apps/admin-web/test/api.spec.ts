// API 层 mock 分支单测（USE_MOCK=true 契约形状校验）
import { describe, it, expect } from 'vitest'
import {
  fetchDashboardKPI, fetchWearTrend, fetchAlertTrend, fetchTeamRanking, fetchDoctorRanking,
  fetchWearDistribution, fetchPatients, fetchAlerts, fetchDevices, fetchTeams,
  fetchFeedbacks, fetchPatientRealtime, fetchNotifyRules, fetchNotificationLogs,
  fetchAbnormalReport,
} from '../src/api'
import { mockAbnormalReport, mockAbnormalReportCsv } from '../src/mock/alerts'
import { USE_MOCK } from '../src/utils/request'

describe('API 层（USE_MOCK 模式）', () => {
  it('USE_MOCK 开关为 true（T021 就绪前）', () => {
    expect(USE_MOCK).toBe(true)
  })

  it('Dashboard 聚合接口返回契约形状（T021 契约先行）', async () => {
    const kpi = await fetchDashboardKPI('today')
    expect(kpi.totalPatients).toBeGreaterThan(0)
    expect(kpi.todayActiveWear).toBeGreaterThan(0)
    expect(kpi.todayAlerts).toBeGreaterThan(0)
    expect(kpi.avgWearHours).toBeGreaterThan(0)
    expect(kpi.deviceOnlineRate).toBeGreaterThan(0)
    expect(kpi.monthNewPatients).toBeGreaterThan(0)

    const wearTrend = await fetchWearTrend(7)
    expect(wearTrend).toHaveLength(7)
    expect(wearTrend[0]).toHaveProperty('date')
    expect(wearTrend[0]).toHaveProperty('avgHours')

    const alertTrend = await fetchAlertTrend(7)
    expect(alertTrend).toHaveLength(7)
    expect(alertTrend[0]).toHaveProperty('count')

    const teamRanking = await fetchTeamRanking()
    expect(teamRanking[0].rank).toBe(1)
    const doctorRanking = await fetchDoctorRanking()
    expect(doctorRanking[0].rank).toBe(1)
    const distribution = await fetchWearDistribution()
    expect(distribution.length).toBeGreaterThanOrEqual(5)
  })

  it('KPI 支持 today/week/month 三个周期', async () => {
    for (const period of ['today', 'week', 'month'] as const) {
      const kpi = await fetchDashboardKPI(period)
      expect(kpi.totalPatients).toBe(1256)
    }
  })

  it('患者列表支持关键字/团队筛选与分页', async () => {
    const all = await fetchPatients({ page: 1, pageSize: 10 })
    expect(all.total).toBeGreaterThan(0)
    const searched = await fetchPatients({ keyword: '林小雨' })
    expect(searched.list.every((p) => p.name.includes('林小雨') || p.patientId.includes('林小雨'))).toBe(true)
    const byTeam = await fetchPatients({ teamId: 'TEAM-001' })
    expect(byTeam.list.every((p) => p.teamId === 'TEAM-001')).toBe(true)
  })

  it('告警列表支持类型/状态筛选（复用 T019B 契约）', async () => {
    const pending = await fetchAlerts({ status: 'pending' })
    expect(pending.list.every((a) => a.processStatus === 'pending')).toBe(true)
    const byType = await fetchAlerts({ type: 'pressure_high' })
    expect(byType.list.every((a) => a.type === 'pressure_high')).toBe(true)
  })

  it('设备/团队/反馈/通知规则/通知记录返回非空列表', async () => {
    expect((await fetchDevices({})).list.length).toBeGreaterThan(0)
    expect((await fetchTeams()).length).toBeGreaterThan(0)
    expect((await fetchFeedbacks({})).length).toBeGreaterThan(0)
    expect((await fetchNotifyRules()).length).toBe(4)
    expect((await fetchNotificationLogs({})).list.length).toBeGreaterThan(0)
  })

  it('患者实时快照契约字段完整（getPatientRealtime）', async () => {
    const snapshot = await fetchPatientRealtime('PT-001')
    expect(['online', 'offline', 'abnormal']).toContain(snapshot.status)
    expect(snapshot).toHaveProperty('todayHours')
    expect(snapshot).toHaveProperty('maxPressure')
    expect(snapshot).toHaveProperty('events')
    expect(Array.isArray(snapshot.pressureRecords)).toBe(true)
    expect(Array.isArray(snapshot.alerts)).toBe(true)
  })
})

// T300 异常报告（患者管理页抽屉）——mock 分支：三视图自洽 + CSV 与汇总同源
describe('T300 异常报告', () => {
  const range = { patientId: 'PT-001', start: '2026-09-01', end: '2026-09-10' }

  it('汇总返回后端三视图，且各组计数都等于 total', async () => {
    const res = await fetchAbnormalReport(range)
    expect(res.patientId).toBe('PT-001')
    expect(res.total).toBeGreaterThan(0)
    expect(res.byStatus.map((s) => s.key)).toEqual(['pending', 'processing', 'processed'])
    const sum = (list: { count: number }[]) => list.reduce((n, x) => n + x.count, 0)
    expect(sum(res.byStatus)).toBe(res.total)
    expect(sum(res.byType)).toBe(res.total)
    expect(sum(res.byDay)).toBe(res.total)
    expect(res.byType[0].count).toBeGreaterThanOrEqual(res.byType[res.byType.length - 1].count)
    expect(res.byDay.every((d, i, arr) => i === 0 || arr[i - 1].key < d.key)).toBe(true)
    expect(res.byDay.every((d) => d.key >= range.start && d.key <= range.end)).toBe(true)
    // 计数按日铺开（不是「整段塞一天」的假数据）
    expect(res.byDay.length).toBeGreaterThan(1)
    expect(new Set(res.byDay.map((d) => d.count)).size).toBeGreaterThan(1)
  })

  it('CSV 表头 16 列、明细行数等于汇总 total', async () => {
    const csv = mockAbnormalReportCsv(range)
    expect(csv.startsWith('\ufeff')).toBe(true)
    const lines = csv.slice(1).trim().split('\r\n')
    expect(lines[0].split(',')).toHaveLength(16)
    expect(lines.length - 1).toBe(mockAbnormalReport(range).total)
  })

  it('区间反向或格式非法时给空汇总，不抛错', async () => {
    const empty = await fetchAbnormalReport({ ...range, start: range.end, end: range.start })
    expect(empty.total).toBe(0)
    expect(empty.byStatus.every((s) => s.count === 0)).toBe(true)
    expect(empty.byType).toEqual([])
    expect(empty.byDay).toEqual([])
  })
})
