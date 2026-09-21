// 矫形日志域 mock 数据（对齐 api-contracts.ts getOrthosisPlans/saveOrthosisPlan/
// getFeelingLogs/getHealthReports/getFeelingLogsAdmin，PRD §7D.8 医生工作台 + T289 8.1 跨患者流）
import type { FeelingLog, HealthReport, OrthosisPlan } from '@bracesync/shared-types'
import { mockPatientDetail } from './patients'

const PLANS: OrthosisPlan[] = [
  { planId: 'PLAN-001', patientId: 'PT-001', doctorId: 'DOC-001', content: '每日佩戴目标 22h；重点加压区 T7-T9 左侧；每 3 个月复查 X 光评估 Cobb 角变化。', version: 'v2.1', createdAt: '2026-07-20T10:00:00+08:00' },
  { planId: 'PLAN-002', patientId: 'PT-001', doctorId: 'DOC-001', content: '初版方案：每日佩戴 20h，观察皮肤耐受情况。', version: 'v1.0', createdAt: '2026-03-12T11:00:00+08:00' },
  { planId: 'PLAN-003', patientId: 'PT-002', doctorId: 'DOC-001', content: 'Cobb 35°，佩戴目标 23h，腰段加压垫增厚 2mm，6 周后复查。', version: 'v1.2', createdAt: '2026-06-30T14:30:00+08:00' },
]

// T289 8.1：跨患者日志流样例补足到 8 条（设计稿 矫形日志.html:121「共 8 条记录」同量级），
// 覆盖 贴合/不适/未评 三种 comfort_level（FL-008 的 null 对齐真库现状：种子行 comfort_level 全为 NULL）。
const FEELINGS: FeelingLog[] = [
  { logId: 'FL-001', patientId: 'PT-001', logDate: '2026-08-11', comfortScore: 3.5, feeling: 'fitted', discomfortAreas: ['thoracic'], notes: '上午有点闷，下午适应一些', replyContent: null, replyTime: null },
  { logId: 'FL-002', patientId: 'PT-001', logDate: '2026-08-10', comfortScore: 4, feeling: 'fitted', discomfortAreas: [], notes: '整体不错', replyContent: '继续保持，注意睡姿', replyTime: '2026-08-10T21:00:00+08:00' },
  { logId: 'FL-003', patientId: 'PT-002', logDate: '2026-08-11', comfortScore: 2.5, feeling: 'discomfort', discomfortAreas: ['lumbar', 'pelvis'], notes: '腰部压得比较疼', replyContent: null, replyTime: null },
  { logId: 'FL-004', patientId: 'PT-003', logDate: '2026-08-11', comfortScore: 4.5, feeling: 'fitted', discomfortAreas: [], notes: '按方案佩戴满 22 小时，无不适', replyContent: null, replyTime: null },
  { logId: 'FL-005', patientId: 'PT-003', logDate: '2026-08-10', comfortScore: 2, feeling: 'discomfort', discomfortAreas: ['neck'], notes: '颈部边缘磨红，垫了薄棉布后缓解', replyContent: '磨红处每日拍照记录，持续加重请复诊', replyTime: '2026-08-10T20:30:00+08:00' },
  { logId: 'FL-006', patientId: 'PT-004', logDate: '2026-08-09', comfortScore: 3, feeling: 'fitted', discomfortAreas: [], notes: '体育课已请假，佩戴正常', replyContent: null, replyTime: null },
  { logId: 'FL-007', patientId: 'PT-002', logDate: '2026-08-09', comfortScore: 4, feeling: 'fitted', discomfortAreas: [], notes: '调整后压痛消失', replyContent: null, replyTime: null },
  { logId: 'FL-008', patientId: 'PT-006', logDate: '2026-08-08', comfortScore: null, feeling: null, discomfortAreas: [], notes: '只填了备注，未选感受', replyContent: null, replyTime: null },
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

/**
 * T289 8.1：GET /api/v1/admin/feeling-logs 的 mock 版（契约 getFeelingLogsAdmin）。
 * 口径逐条对齐契约：keyword 只匹配患者姓名（后端当前不匹配患者编号）、日期闭区间、
 * feeling 直接比对 comfort_level、按 log_date 倒序（同日按 logId 倒序）、total 为全量筛选条数。
 */
export function mockFeelingLogsAdmin(params: {
  keyword?: string
  startDate?: string
  endDate?: string
  feeling?: 'fitted' | 'discomfort'
  page?: number
  pageSize?: number
}): { list: FeelingLog[]; total: number; page: number; pageSize: number } {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 20
  let list = FEELINGS.map((f) => ({ ...f, patientName: mockPatientDetail(f.patientId)?.name ?? null }))
  if (params.keyword) {
    const kw = params.keyword.toLowerCase()
    list = list.filter((f) => (f.patientName ?? '').toLowerCase().includes(kw))
  }
  if (params.startDate) list = list.filter((f) => f.logDate >= params.startDate!)
  if (params.endDate) list = list.filter((f) => f.logDate <= params.endDate!)
  if (params.feeling) list = list.filter((f) => f.feeling === params.feeling)
  list.sort((a, b) => (a.logDate === b.logDate ? b.logId.localeCompare(a.logId) : b.logDate.localeCompare(a.logDate)))
  const start = (page - 1) * pageSize
  return { list: list.slice(start, start + pageSize), total: list.length, page, pageSize }
}

// T247 8.3: 医生回复感受日志 mock
export function mockReplyFeelingLog(logId: string, replyContent: string): void {
  const f = FEELINGS.find((x) => x.logId === logId)
  if (f) {
    f.replyContent = replyContent
    f.replyTime = new Date().toISOString()
  }
}

export function mockHealthReports(patientId: string): HealthReport[] {
  return REPORTS.filter((r) => r.patientId === patientId).map((r) => ({ ...r }))
}
