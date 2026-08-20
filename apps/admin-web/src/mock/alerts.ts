// 告警域 mock 数据（对齐 api-contracts.ts getAlerts/processAlert，复用 T019B alerts 数据模式）
import type { Alert } from '@bracesync/shared-types'

const ALERTS: Alert[] = [
  {
    alertId: 'ALR-001', patientId: 'PT-001', deviceId: 'DEV-A3F312', type: 'pressure_high',
    detail: 'P10 压力持续偏高，峰值 68.5N，超出阈值 8.5N', sensorPoint: 'P10', thresholdValue: 60, actualValue: 68.5,
    timestamp: '2026-08-11T14:30:00+08:00', readStatus: 'unread', processStatus: 'pending',
    resolvedStatus: 'active', resolvedAt: null, processedBy: null, processedAt: null, processNote: null,
  },
  {
    alertId: 'ALR-002', patientId: 'PT-002', deviceId: 'DEV-B7E456', type: 'wear_interrupt',
    detail: '佩戴中断超过 30 分钟，疑似摘除', sensorPoint: '', thresholdValue: 0, actualValue: 0,
    timestamp: '2026-08-11T13:15:00+08:00', readStatus: 'unread', processStatus: 'pending',
    resolvedStatus: 'resolved', resolvedAt: '2026-08-11T14:00:00+08:00', processedBy: null, processedAt: null, processNote: null,
  },
  {
    alertId: 'ALR-003', patientId: 'PT-001', deviceId: 'DEV-A3F312', type: 'pressure_fluctuation',
    detail: 'P05 压力波动异常，短时间内多次超阈值', sensorPoint: 'P05', thresholdValue: 40, actualValue: 52.3,
    timestamp: '2026-08-11T11:45:00+08:00', readStatus: 'read', processStatus: 'processed',
    resolvedStatus: 'active', resolvedAt: null, processedBy: '张建国', processedAt: '2026-08-11T12:30:00+08:00',
    processNote: '已通知患者调整佩戴位置',
  },
  {
    alertId: 'ALR-004', patientId: 'PT-003', deviceId: 'DEV-C9D789', type: 'sensor_drift',
    detail: 'P12 传感器数据漂移，基线偏移超过 15%', sensorPoint: 'P12', thresholdValue: 15, actualValue: 23.7,
    timestamp: '2026-08-11T10:00:00+08:00', readStatus: 'read', processStatus: 'pending',
    resolvedStatus: 'active', resolvedAt: null, processedBy: null, processedAt: null, processNote: null,
  },
  {
    alertId: 'ALR-005', patientId: 'PT-002', deviceId: 'DEV-B7E456', type: 'pressure_high',
    detail: 'P03 压力偏高，峰值 55.2N', sensorPoint: 'P03', thresholdValue: 40, actualValue: 55.2,
    timestamp: '2026-08-10T16:20:00+08:00', readStatus: 'read', processStatus: 'processed',
    resolvedStatus: 'resolved', resolvedAt: '2026-08-10T18:00:00+08:00', processedBy: '李明华',
    processedAt: '2026-08-10T17:00:00+08:00', processNote: '已远程指导患者调整',
  },
  {
    alertId: 'ALR-006', patientId: 'PT-004', deviceId: 'DEV-D2A012', type: 'wear_interrupt',
    detail: '佩戴中断超过 1 小时', sensorPoint: '', thresholdValue: 0, actualValue: 0,
    timestamp: '2026-08-10T09:30:00+08:00', readStatus: 'read', processStatus: 'processed',
    resolvedStatus: 'resolved', resolvedAt: '2026-08-10T11:00:00+08:00', processedBy: '张建国',
    processedAt: '2026-08-10T10:15:00+08:00', processNote: '患者反馈临时摘除洗澡',
  },
]

// 患者姓名映射（管理端列表展示用，真实模式由后端 join 返回）
export const ALERT_PATIENT_NAMES: Record<string, string> = {
  'PT-001': '林小雨', 'PT-002': '陈子航', 'PT-003': '王梓萌', 'PT-004': '刘俊熙',
}

export function mockAlerts(params: { patientId?: string; type?: string; status?: string; page?: number; pageSize?: number }): { list: Alert[]; total: number; page: number; pageSize: number } {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 10
  let list = ALERTS.map((a) => ({ ...a }))
  if (params.patientId) list = list.filter((a) => a.patientId === params.patientId)
  if (params.type) list = list.filter((a) => a.type === params.type)
  if (params.status) list = list.filter((a) => a.processStatus === params.status)
  const start = (page - 1) * pageSize
  return { list: list.slice(start, start + pageSize), total: list.length, page, pageSize }
}
