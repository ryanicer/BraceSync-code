// 系统管理域 mock 数据（角色/权限矩阵 PRD §7D.11，系统配置 PRD §7D.12，
// 通知规则与发送记录对齐 api-contracts.ts getNotifyRules/getNotificationLogs）
import type { NotifyRule, NotificationRecord } from '@bracesync/shared-types'
import { DEFAULT_THRESHOLDS } from '@bracesync/constants'
import { PRESET_ROLES, ROLE_PAGE_MATRIX } from '../router/permissions'

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

// T247: 角色权限 mock（基于 ROLE_PAGE_MATRIX，预置角色不可编辑权限但可读）
const rolePermStore: Record<string, string[]> = {}

export function mockRolePermissions(roleId: string): { roleId: string; permissions: string[] } {
  if (!rolePermStore[roleId]) {
    // 按 roleId 匹配预置角色
    const key = roleId === 'ROLE-ADMIN' ? 'admin' : roleId === 'ROLE-DOCTOR' ? 'doctor' : 'cs'
    rolePermStore[roleId] = [...(ROLE_PAGE_MATRIX[key as keyof typeof ROLE_PAGE_MATRIX] ?? [])]
  }
  return { roleId, permissions: [...rolePermStore[roleId]] }
}

export function mockUpdateRolePermissions(roleId: string, permissions: string[]): void {
  rolePermStore[roleId] = [...permissions]
}

/** 系统配置（PRD §7D.12，默认值对齐 @bracesync/constants DEFAULT_THRESHOLDS） */
export interface SystemSettings {
  dailyWearTargetHours: number
  pressureHighThresholdN: number
  pressureFluctuationPct: number
  wearInterruptMinutes: number
  sensorDriftN: number
  wifiPresets: { ssid: string; password: string }[]
  // T247 12.5: 设计稿系统参数三项（系统配置.html:88-90）
  collectionIntervalSec: number
  dataRetentionDays: number
  maxPatients: number
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
    collectionIntervalSec: 60,
    dataRetentionDays: 365,
    maxPatients: 10000,
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

// ========== T253-12.3 操作日志 mock（契约：docs/api/api-contracts.ts AuditLog/getAuditLogs，T252 后端同形） ==========

/** 审计日志行（对齐 DB audit_logs，PRD §8.2） */
export interface AuditLog {
  logId: number
  operatorId: string | null
  operatorName: string | null
  operatorRole: string | null // ROLE_ADMIN / ROLE_DOCTOR / ROLE_CS / patient / technician
  action: string // login / data_modify / config_change / permission_change / data_read
  actionLabel: string
  targetType: string | null
  targetId: string | null
  description: string
  detail: Record<string, unknown> | null
  ip: string | null
  ts: string
}

const AUDIT_LOGS: AuditLog[] = [
  { logId: 6, operatorId: 'ops_admin', operatorName: '运营小张', operatorRole: 'ROLE_ADMIN', action: 'config_change', actionLabel: '配置变更', targetType: 'alert_rule', targetId: 'points', description: '保存告警规则：统一上限 45N / 下限 10N，逐点变更 20 个', detail: null, ip: '192.168.1.10', ts: '2026-09-20T19:58:10+08:00' },
  { logId: 5, operatorId: 'ops_admin', operatorName: '运营小张', operatorRole: 'ROLE_ADMIN', action: 'permission_change', actionLabel: '权限变更', targetType: 'role', targetId: 'ROLE-DOCTOR', description: '写入角色 ROLE-DOCTOR 的权限矩阵', detail: null, ip: '192.168.1.10', ts: '2026-09-20T18:20:08+08:00' },
  { logId: 4, operatorId: 'DOC-001', operatorName: '张建国', operatorRole: 'ROLE_DOCTOR', action: 'data_read', actionLabel: '数据查看', targetType: 'patient', targetId: 'PT-001', description: '查看患者档案 PT-001（等保 §9.2a 读留痕）', detail: null, ip: '192.168.1.55', ts: '2026-09-20T16:35:21+08:00' },
  { logId: 3, operatorId: 'ops_admin', operatorName: '运营小张', operatorRole: 'ROLE_ADMIN', action: 'data_modify', actionLabel: '数据修改', targetType: 'technician', targetId: 'TEC-002', description: '创建技师账号', detail: null, ip: '192.168.1.10', ts: '2026-09-20T14:12:33+08:00' },
  { logId: 2, operatorId: 'DOC-001', operatorName: '张建国', operatorRole: 'ROLE_DOCTOR', action: 'login', actionLabel: '登录', targetType: null, targetId: null, description: '登录成功', detail: null, ip: '192.168.1.55', ts: '2026-09-20T08:55:42+08:00' },
  { logId: 1, operatorId: 'ops_admin', operatorName: '运营小张', operatorRole: 'ROLE_ADMIN', action: 'config_change', actionLabel: '配置变更', targetType: 'sys_config', targetId: null, description: '写入系统参数（§7D.12）', detail: null, ip: '192.168.1.10', ts: '2026-09-19T17:45:02+08:00' },
]

export function mockAuditLogs(params: { date?: string; action?: string; operator?: string; page?: number; pageSize?: number }): { list: AuditLog[]; total: number; page: number; pageSize: number } {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 20
  let list = AUDIT_LOGS.map((r) => ({ ...r }))
  if (params.date) list = list.filter((r) => r.ts.slice(0, 10) === params.date)
  if (params.action) list = list.filter((r) => r.action === params.action)
  if (params.operator) {
    const kw = params.operator.toLowerCase()
    list = list.filter((r) => (r.operatorId ?? '').toLowerCase().includes(kw) || (r.operatorName ?? '').toLowerCase().includes(kw))
  }
  const start = (page - 1) * pageSize
  return { list: list.slice(start, start + pageSize), total: list.length, page, pageSize }
}
