// Dashboard mock 数据（对齐 api-contracts.ts getDashboardKPI/getWearTrend/getAlertTrend/
// getTeamRanking/getDoctorRanking/getWearDistribution，T021 完成后切真实 API）
import type { DashboardKPI, TeamRanking, DoctorRanking } from '@bracesync/shared-types'

export function mockDashboardKPI(period: 'today' | 'week' | 'month'): DashboardKPI {
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

export function mockWearTrend(days: number): { date: string; avgHours: number }[] {
  const data = [
    { date: '08-05', avgHours: 7.8 },
    { date: '08-06', avgHours: 8.1 },
    { date: '08-07', avgHours: 7.9 },
    { date: '08-08', avgHours: 8.3 },
    { date: '08-09', avgHours: 8.0 },
    { date: '08-10', avgHours: 8.5 },
    { date: '08-11', avgHours: 8.2 },
  ]
  return data.slice(-Math.min(days, data.length))
}

export function mockAlertTrend(days: number): { date: string; count: number }[] {
  const data = [
    { date: '08-05', count: 52 },
    { date: '08-06', count: 48 },
    { date: '08-07', count: 55 },
    { date: '08-08', count: 43 },
    { date: '08-09', count: 50 },
    { date: '08-10', count: 39 },
    { date: '08-11', count: 47 },
  ]
  return data.slice(-Math.min(days, data.length))
}

export function mockTeamRanking(): TeamRanking[] {
  return [
    { rank: 1, teamName: '脊柱侧弯一组', patientCount: 186, avgDailyWear: 9.2, complianceRate: 94.6 },
    { rank: 2, teamName: '脊柱侧弯二组', patientCount: 204, avgDailyWear: 8.8, complianceRate: 91.2 },
    { rank: 3, teamName: '术后康复组', patientCount: 158, avgDailyWear: 8.5, complianceRate: 88.0 },
    { rank: 4, teamName: '儿童矫形组', patientCount: 312, avgDailyWear: 7.8, complianceRate: 82.7 },
    { rank: 5, teamName: '成人矫形组', patientCount: 275, avgDailyWear: 7.2, complianceRate: 76.4 },
  ]
}

export function mockDoctorRanking(): DoctorRanking[] {
  return [
    { rank: 1, doctorName: '张建国', teamName: '脊柱侧弯一组', patientCount: 68, complianceRate: 96.2 },
    { rank: 2, doctorName: '李明华', teamName: '术后康复组', patientCount: 55, complianceRate: 93.5 },
    { rank: 3, doctorName: '陈小芳', teamName: '脊柱侧弯二组', patientCount: 72, complianceRate: 90.8 },
    { rank: 4, doctorName: '王磊', teamName: '儿童矫形组', patientCount: 84, complianceRate: 85.1 },
    { rank: 5, doctorName: '赵敏', teamName: '成人矫形组', patientCount: 63, complianceRate: 82.4 },
  ]
}

export function mockWearDistribution(): { range: string; count: number }[] {
  return [
    { range: '< 4小时', count: 85 },
    { range: '4-6小时', count: 156 },
    { range: '6-8小时', count: 312 },
    { range: '8-10小时', count: 428 },
    { range: '≥ 10小时', count: 275 },
  ]
}
