// Dashboard mock 数据（对齐 api-contracts.ts getDashboardKPI/getWearTrend/getAlertTrend/
// getTeamRanking/getDoctorRanking/getWearDistribution，T021 完成后切真实 API）
import type { DashboardKPI, TeamRanking, DoctorRanking } from '@bracesync/shared-types'

type Period = 'today' | 'week' | 'month'

// T489：mock 也要能演示「切周期后数据区变化」。因子是本地经验值，不是后端口径。
// 两排行的 patientCount 刻意不乘因子 —— 后端 SQL 患者侧无日期谓词，该列是存量（T489 语义点）。
const PERIOD_FACTOR: Record<Period, { wear: number; rate: number; dist: number }> = {
  today: { wear: 1, rate: 1, dist: 1 },
  week: { wear: 0.96, rate: 0.97, dist: 4.6 },
  month: { wear: 0.92, rate: 0.94, dist: 17.3 },
}

function round1(n: number): number {
  return Math.round(n * 10) / 10
}

export function mockDashboardKPI(period: Period): DashboardKPI {
  const base: DashboardKPI = {
    totalPatients: 1256,
    todayActiveWear: 892,
    todayAlerts: 47,
    avgWearHours: 8.2,
    deviceOnlineRate: 96.8,
    monthNewPatients: 38,
  }
  if (period === 'today') return base
  if (period === 'week') {
    return { ...base, todayAlerts: 312, avgWearHours: 8.0, todayActiveWear: 905 }
  }
  return { ...base, todayAlerts: 1287, avgWearHours: 7.9, todayActiveWear: 878 }
}

// 趋势 mock 末点固定 2026-08-11（与 mockDashboardKPI 的样例日同一天），按 days 向前铺。
// 原实现只有 7 条硬编码 ⇒ days=30 也只回 7 条，切「本月」时趋势区看不出变化（T489 验收标准）。
const TREND_END_UTC = Date.UTC(2026, 7, 11)
const WEAR_CYCLE = [7.8, 8.1, 7.9, 8.3, 8.0, 8.5, 8.2]
const ALERT_CYCLE = [52, 48, 55, 43, 50, 39, 47]

function trendDates(days: number): string[] {
  return Array.from({ length: days }, (_, i) => {
    const d = new Date(TREND_END_UTC - (days - 1 - i) * 86400000)
    return `${String(d.getUTCMonth() + 1).padStart(2, '0')}-${String(d.getUTCDate()).padStart(2, '0')}`
  })
}

export function mockWearTrend(days: number): { date: string; avgHours: number }[] {
  return trendDates(days).map((date, i) => ({ date, avgHours: WEAR_CYCLE[i % WEAR_CYCLE.length] }))
}

export function mockAlertTrend(days: number): { date: string; count: number }[] {
  return trendDates(days).map((date, i) => ({ date, count: ALERT_CYCLE[i % ALERT_CYCLE.length] }))
}

export function mockTeamRanking(period: Period): TeamRanking[] {
  const f = PERIOD_FACTOR[period]
  return [
    { rank: 1, teamName: '脊柱侧弯一组', patientCount: 186, avgDailyWear: 9.2, complianceRate: 94.6 },
    { rank: 2, teamName: '脊柱侧弯二组', patientCount: 204, avgDailyWear: 8.8, complianceRate: 91.2 },
    { rank: 3, teamName: '术后康复组', patientCount: 158, avgDailyWear: 8.5, complianceRate: 88.0 },
    { rank: 4, teamName: '儿童矫形组', patientCount: 312, avgDailyWear: 7.8, complianceRate: 82.7 },
    { rank: 5, teamName: '成人矫形组', patientCount: 275, avgDailyWear: 7.2, complianceRate: 76.4 },
  ].map((t) => ({
    ...t,
    avgDailyWear: round1(t.avgDailyWear * f.wear),
    complianceRate: round1(t.complianceRate * f.rate),
  }))
}

export function mockDoctorRanking(period: Period): DoctorRanking[] {
  const f = PERIOD_FACTOR[period]
  return [
    { rank: 1, doctorName: '张建国', teamName: '脊柱侧弯一组', patientCount: 68, complianceRate: 96.2 },
    { rank: 2, doctorName: '李明华', teamName: '术后康复组', patientCount: 55, complianceRate: 93.5 },
    { rank: 3, doctorName: '陈小芳', teamName: '脊柱侧弯二组', patientCount: 72, complianceRate: 90.8 },
    { rank: 4, doctorName: '王磊', teamName: '儿童矫形组', patientCount: 84, complianceRate: 85.1 },
    { rank: 5, doctorName: '赵敏', teamName: '成人矫形组', patientCount: 63, complianceRate: 82.4 },
  ].map((d) => ({ ...d, complianceRate: round1(d.complianceRate * f.rate) }))
}

export function mockWearDistribution(period: Period): { range: string; count: number }[] {
  const f = PERIOD_FACTOR[period]
  return [
    { range: '< 4小时', count: 85 },
    { range: '4-6小时', count: 156 },
    { range: '6-8小时', count: 312 },
    { range: '8-10小时', count: 428 },
    { range: '≥ 10小时', count: 275 },
  ].map((d) => ({ ...d, count: Math.round(d.count * f.dist) }))
}
