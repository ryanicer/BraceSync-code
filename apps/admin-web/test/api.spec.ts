// API 层 mock 分支单测（USE_MOCK=true 契约形状校验）
import { describe, it, expect } from 'vitest'
import {
  fetchDashboardKPI, fetchWearTrend, fetchAlertTrend, fetchTeamRanking, fetchDoctorRanking,
  fetchWearDistribution, fetchPatients, fetchAlerts, fetchDevices, fetchTeams, fetchTechnicians,
  fetchFeedbacks, fetchPatientRealtime, fetchPatientDailyWear, fetchNotifyRules, fetchNotificationLogs,
  fetchAbnormalReport,
} from '../src/api'
import { mockAbnormalReport, mockAbnormalReportCsv } from '../src/mock/alerts'
import { isHiddenAlertType } from '@bracesync/shared-utils'
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

    const teamRanking = await fetchTeamRanking('today')
    expect(teamRanking[0].rank).toBe(1)
    const doctorRanking = await fetchDoctorRanking('today')
    expect(doctorRanking[0].rank).toBe(1)
    const distribution = await fetchWearDistribution('today')
    expect(distribution.length).toBeGreaterThanOrEqual(5)
  })

  it('T489：排行/分布 mock 随 period 变值，患者数列保持存量不变', async () => {
    const week = await fetchTeamRanking('week')
    const month = await fetchTeamRanking('month')
    const today = await fetchTeamRanking('today')
    expect(week[0].avgDailyWear).not.toBe(today[0].avgDailyWear)
    expect(month[0].complianceRate).not.toBe(today[0].complianceRate)
    // patientCount 是存量口径（后端 SQL 患者侧无日期谓词），三周期必须同值
    expect(month.map((t) => t.patientCount)).toEqual(today.map((t) => t.patientCount))

    const docWeek = await fetchDoctorRanking('week')
    expect(docWeek[0].complianceRate).not.toBe((await fetchDoctorRanking('today'))[0].complianceRate)
    expect(docWeek[0].patientCount).toBe(68)

    const distMonth = await fetchWearDistribution('month')
    const distToday = await fetchWearDistribution('today')
    expect(distMonth[3].count).toBeGreaterThan(distToday[3].count)

    // 趋势端点只吃 days：days=30 必须真给 30 个点（旧实现硬编码 7 条，slice 会把多出来的天数吃掉）
    expect(await fetchWearTrend(30)).toHaveLength(30)
    expect(await fetchAlertTrend(30)).toHaveLength(30)
    const wear7 = await fetchWearTrend(7)
    expect(wear7[0].date).toBe('08-05')
    expect(wear7[6].date).toBe('08-11')
    expect(wear7.map((d) => d.avgHours)).toEqual([7.8, 8.1, 7.9, 8.3, 8.0, 8.5, 8.2])
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

  // T344 工作台「数据视图」两个新取数口
  it('日佩戴聚合按闭区间逐日返回，告警支持 patientId 过滤', async () => {
    const rows = await fetchPatientDailyWear('PT-001', '2026-09-17', '2026-09-23')
    expect(rows.map((r) => r.date)).toEqual([
      '2026-09-17', '2026-09-18', '2026-09-19', '2026-09-20', '2026-09-21', '2026-09-22', '2026-09-23',
    ])
    expect(rows[0]).toHaveProperty('wearMinutes')
    expect(rows[0]).toHaveProperty('avgPressure')

    const pt004 = await fetchAlerts({ patientId: 'PT-004', pageSize: 100 })
    expect(pt004.list.length).toBeGreaterThan(0)
    expect(pt004.list.every((a) => a.patientId === 'PT-004')).toBe(true)
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

// T627 方案乙：技师归属的类型过滤（前端这一半 —— 后端那一半在 user-service 的用例里）
describe('T627 技师归属类型过滤', () => {
  it('不带过滤时列全量，每行都带出所属团队的类型', async () => {
    const all = await fetchTechnicians({ pageSize: 100 })
    expect(all.list.length).toBe(all.total)
    expect(all.list.every((t) => t.teamType === 'medical' || t.teamType === 'maintenance')).toBe(true)
  })

  it('maintenance 一支只出维护班组的行，且 total 是过滤后的条数', async () => {
    const maint = await fetchTechnicians({ pageSize: 100, teamType: 'maintenance' })
    expect(maint.list.length).toBeGreaterThan(0)
    expect(maint.list.every((t) => t.teamType === 'maintenance')).toBe(true)
    // 验收判据 3 的那一句「维护类技师不误归医护」：行的 teamId 必须落在维护班组集合里
    const maintTeamIds = new Set((await fetchTeams({ teamType: 'maintenance' })).map((t) => t.teamId))
    expect(maint.list.every((t) => maintTeamIds.has(t.teamId))).toBe(true)
    const all = await fetchTechnicians({ pageSize: 100 })
    expect(maint.total).toBeLessThan(all.total)
    expect(maint.total).toBe(maint.list.length)
  })

  it('medical 一支仍列出存量挂在医护团队的技师（刷数前的形状，过滤不掩盖它）', async () => {
    const med = await fetchTechnicians({ pageSize: 100, teamType: 'medical' })
    expect(med.list.length).toBeGreaterThan(0)
    expect(med.list.every((t) => t.teamType === 'medical')).toBe(true)
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

  // T430（PRD §7D.6 历史数据处置拍 C·Boss 09-27）：原判据「明细行数 == 汇总 total」随裁定失效——
  // 已裁砍除类型的历史行只从导出/展示面收口，汇总（数据层）不动 ⇒ 等式换成「total 减去隐藏行」。
  // 隐藏条数按同一份未过滤的 byType 现算，不写死数字，换区间也不会腐烂。
  it('CSV 表头 16 列、明细行数 = 汇总 total 减去已裁砍除类型的历史行', async () => {
    const csv = mockAbnormalReportCsv(range)
    expect(csv.startsWith('\ufeff')).toBe(true)
    const lines = csv.slice(1).trim().split('\r\n')
    expect(lines[0].split(',')).toHaveLength(16)
    const summary = mockAbnormalReport(range)
    const hidden = summary.byType
      .filter((x) => isHiddenAlertType(x.key))
      .reduce((n, x) => n + x.count, 0)
    expect(hidden, '本区间须真含被隐藏类型的历史行，否则本条没有判别力').toBeGreaterThan(0)
    expect(lines.length - 1).toBe(summary.total - hidden)
    const body = lines.slice(1).join('\n')
    expect(body).not.toContain('压力波动')
    expect(body).not.toContain('pressure_fluctuation')
  })

  it('区间反向或格式非法时给空汇总，不抛错', async () => {
    const empty = await fetchAbnormalReport({ ...range, start: range.end, end: range.start })
    expect(empty.total).toBe(0)
    expect(empty.byStatus.every((s) => s.count === 0)).toBe(true)
    expect(empty.byType).toEqual([])
    expect(empty.byDay).toEqual([])
  })
})
