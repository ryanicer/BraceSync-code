// 系统管理域 mock 数据（角色/权限矩阵 PRD §7D.11，系统配置 PRD §7D.12，
// 通知规则与发送记录对齐 api-contracts.ts getNotifyRules/getNotificationLogs）
import type { NotifyRule, NotificationRecord } from '@bracesync/shared-types'
import { DEFAULT_THRESHOLDS } from '@bracesync/constants'
import { PRESET_ROLES } from '../router/permissions'

export interface AdminRoleRow {
  roleId: string
  name: string
  description: string
  memberCount: number
  createdAt: string
  status: 'enabled' | 'disabled'
  preset: boolean
}

export function mockAdminRoles(): AdminRoleRow[] {
  return [
    { roleId: 'ROLE-ADMIN', name: PRESET_ROLES[0].name, description: PRESET_ROLES[0].description, memberCount: 3, createdAt: '2026-01-01T00:00:00+08:00', status: 'enabled', preset: true },
    { roleId: 'ROLE-DOCTOR', name: PRESET_ROLES[1].name, description: PRESET_ROLES[1].description, memberCount: 5, createdAt: '2026-01-01T00:00:00+08:00', status: 'enabled', preset: true },
    { roleId: 'ROLE-CS', name: PRESET_ROLES[2].name, description: PRESET_ROLES[2].description, memberCount: 2, createdAt: '2026-01-01T00:00:00+08:00', status: 'enabled', preset: true },
  ]
}

/** 系统配置（PRD §7D.12，默认值对齐 @bracesync/constants DEFAULT_THRESHOLDS） */
export interface SystemSettings {
  dailyWearTargetHours: number
  pressureHighThresholdN: number
  pressureFluctuationPct: number
  wearInterruptMinutes: number
  sensorDriftN: number
  wifiPresets: { ssid: string; password: string }[]
}

export function mockSystemSettings(): SystemSettings {
  return {
    dailyWearTargetHours: 22,
    pressureHighThresholdN: DEFAULT_THRESHOLDS.PRESSURE_HIGH_N,
    pressureFluctuationPct: DEFAULT_THRESHOLDS.PRESSURE_FLUCTUATION_PCT,
    wearInterruptMinutes: DEFAULT_THRESHOLDS.WEAR_INTERRUPT_MINUTES,
    sensorDriftN: DEFAULT_THRESHOLDS.SENSOR_DRIFT_N,
    wifiPresets: [
      { ssid: 'Hospital-WiFi', password: '********' },
      { ssid: 'Brace-Clinic', password: '********' },
    ],
  }
}

export function mockNotifyRules(): NotifyRule[] {
  return [
    { type: 'pressure_high', channels: ['wechat', 'sms'], notifyTargets: ['patient', 'doctor'], updatedBy: '运营管理员', updatedAt: '2026-07-01T10:00:00+08:00' },
    { type: 'wear_interrupt', channels: ['wechat'], notifyTargets: ['patient'], updatedBy: '运营管理员', updatedAt: '2026-07-01T10:00:00+08:00' },
    { type: 'pressure_fluctuation', channels: ['wechat'], notifyTargets: ['patient', 'doctor'], updatedBy: '运营管理员', updatedAt: '2026-07-01T10:00:00+08:00' },
    { type: 'sensor_drift', channels: ['wechat'], notifyTargets: ['tech', 'ops'], updatedBy: '运营管理员', updatedAt: '2026-07-01T10:00:00+08:00' },
  ]
}

export function mockNotificationLogs(params: { patientId?: string; channel?: string; status?: string; page?: number; pageSize?: number }): { list: NotificationRecord[]; total: number; page: number; pageSize: number } {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 10
  const records: NotificationRecord[] = [
    { recordId: 'NTF-001', patientId: 'PT-001', alertId: 'ALR-001', alertType: 'pressure_high', channel: 'wechat', status: 'sent', content: '压力偏高告警：P10 峰值 68.5N', retryCount: 0, sentAt: '2026-08-11T14:30:05+08:00', createdAt: '2026-08-11T14:30:02+08:00' },
    { recordId: 'NTF-002', patientId: 'PT-002', alertId: 'ALR-002', alertType: 'wear_interrupt', channel: 'wechat', status: 'failed', content: '佩戴中断提醒：超过 30 分钟未检测到佩戴', retryCount: 3, sentAt: null, createdAt: '2026-08-11T13:15:03+08:00' },
    { recordId: 'NTF-003', patientId: 'PT-004', alertId: 'ALR-006', alertType: 'wear_interrupt', channel: 'sms', status: 'degraded', content: '佩戴中断提醒（订阅额度耗尽，降级短信）', retryCount: 0, sentAt: '2026-08-10T09:30:10+08:00', createdAt: '2026-08-10T09:30:04+08:00' },
    { recordId: 'NTF-004', patientId: 'PT-003', channel: 'wechat', status: 'sent', content: '佩戴提醒：今日佩戴目标 22h，已佩戴 8h', retryCount: 0, sentAt: '2026-08-11T20:00:01+08:00', createdAt: '2026-08-11T20:00:00+08:00' },
  ]
  let list = records
  if (params.patientId) list = list.filter((r) => r.patientId === params.patientId)
  if (params.channel) list = list.filter((r) => r.channel === params.channel)
  if (params.status) list = list.filter((r) => r.status === params.status)
  const start = (page - 1) * pageSize
  return { list: list.slice(start, start + pageSize), total: list.length, page, pageSize }
}
