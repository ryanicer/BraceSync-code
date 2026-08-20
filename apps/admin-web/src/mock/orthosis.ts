// 矫形日志域 mock 数据（对齐 api-contracts.ts getOrthosisPlans/saveOrthosisPlan/
// getFeelingLogs/getHealthReports，PRD §7D.8 医生工作台）
import type { OrthosisPlan, FeelingLog, HealthReport } from '@bracesync/shared-types'

const PLANS: OrthosisPlan[] = [
  { planId: 'PLAN-001', patientId: 'PT-001', doctorId: 'DOC-001', content: '每日佩戴目标 22h；重点加压区 T7-T9 左侧；每 3 个月复查 X 光评估 Cobb 角变化。', version: 'v2.1', createdAt: '2026-07-20T10:00:00+08:00' },
  { planId: 'PLAN-002', patientId: 'PT-001', doctorId: 'DOC-001', content: '初版方案：每日佩戴 20h，观察皮肤耐受情况。', version: 'v1.0', createdAt: '2026-03-12T11:00:00+08:00' },
  { planId: 'PLAN-003', patientId: 'PT-002', doctorId: 'DOC-001', content: 'Cobb 35°，佩戴目标 23h，腰段加压垫增厚 2mm，6 周后复查。', version: 'v1.2', createdAt: '2026-06-30T14:30:00+08:00' },
]

const FEELINGS: FeelingLog[] = [
  { logId: 'FL-001', patientId: 'PT-001', logDate: '2026-08-11', comfortScore: 3.5, discomfortAreas: ['thoracic'], notes: '上午有点闷，下午适应一些', replyContent: null, replyTime: null },
  { logId: 'FL-002', patientId: 'PT-001', logDate: '2026-08-10', comfortScore: 4, discomfortAreas: [], notes: '整体不错', replyContent: '继续保持，注意睡姿', replyTime: '2026-08-10T21:00:00+08:00' },
  { logId: 'FL-003', patientId: 'PT-002', logDate: '2026-08-11', comfortScore: 2.5, discomfortAreas: ['lumbar', 'pelvis'], notes: '腰部压得比较疼', replyContent: null, replyTime: null },
]

const REPORTS: HealthReport[] = [
  { reportId: 'RPT-001', patientId: 'PT-001', reportType: 'weekly', periodStart: '2026-08-04', periodEnd: '2026-08-10', wearComplianceRate: 92.5, avgPressure: 38.2, trendJudgment: 'up', suggestion: '佩戴依从性良好，建议维持当前方案，关注胸段压力点。', generateTime: '2026-08-11T02:00:00+08:00' },
  { reportId: 'RPT-002', patientId: 'PT-001', reportType: 'monthly', periodStart: '2026-07-01', periodEnd: '2026-07-31', wearComplianceRate: 88.1, avgPressure: 36.9, trendJudgment: 'flat', suggestion: '月度达标率略有波动，建议加强晚间佩戴。', generateTime: '2026-08-01T02:00:00+08:00' },
  { reportId: 'RPT-003', patientId: 'PT-002', reportType: 'weekly', periodStart: '2026-08-04', periodEnd: '2026-08-10', wearComplianceRate: 76.3, avgPressure: 45.7, trendJudgment: 'down', suggestion: '达标率下降，腰段压力偏高，建议复诊调整加压区。', generateTime: '2026-08-11T02:00:00+08:00' },
]

export function mockOrthosisPlans(patientId: string): OrthosisPlan[] {
  return PLANS.filter((p) => p.patientId === patientId).map((p) => ({ ...p }))
}

export function mockFeelingLogs(patientId: string): FeelingLog[] {
  return FEELINGS.filter((f) => f.patientId === patientId).map((f) => ({ ...f }))
}

export function mockHealthReports(patientId: string): HealthReport[] {
  return REPORTS.filter((r) => r.patientId === patientId).map((r) => ({ ...r }))
}
