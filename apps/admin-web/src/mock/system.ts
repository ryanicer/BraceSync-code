// 系统管理域 mock 数据（角色/权限矩阵 PRD §7D.11，系统配置 PRD §7D.12，
// 通知规则与发送记录对齐 api-contracts.ts getNotifyRules/getNotificationLogs）
import type { NotifyRule, NotificationRecord, RolePermissions } from '@bracesync/shared-types'
import { DEFAULT_THRESHOLDS } from '@bracesync/constants'
import { PRESET_ROLES, modulesForRole } from '../router/permissions'

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
  if (roleCrudStore.length === 0) roleCrudStore.push(...mockAdminRoleSeeds())
  return roleCrudStore.map((r) => ({ ...r }))
}

function mockAdminRoleSeeds(): AdminRoleRow[] {
  return [
    { roleId: 'ROLE-ADMIN', name: PRESET_ROLES[0].name, description: PRESET_ROLES[0].description, memberCount: 3, createdAt: '2026-01-01T00:00:00+08:00', status: 'enabled', preset: true },
    { roleId: 'ROLE-DOCTOR', name: PRESET_ROLES[1].name, description: PRESET_ROLES[1].description, memberCount: 5, createdAt: '2026-01-01T00:00:00+08:00', status: 'enabled', preset: true },
    { roleId: 'ROLE-CS', name: PRESET_ROLES[2].name, description: PRESET_ROLES[2].description, memberCount: 2, createdAt: '2026-01-01T00:00:00+08:00', status: 'enabled', preset: true },
  ]
}

// T253-11.2: 角色 CRUD mock（可变存储，对齐 T252 后端 roles_t252.go 语义：
// 重名 409 / 预置角色锁定改名、不可删 / 被账号引用不可删）
const roleCrudStore: AdminRoleRow[] = []

/** 角色模板（对齐后端 GET /admin/role-templates 的 5 条，key 同契约 RoleTemplate.key） */
export interface RoleTemplateItem {
  key: string
  name: string
  description: string
  permissions: { scope: string; modules: string[] }
}

export function mockRoleTemplates(): RoleTemplateItem[] {
  return [
    { key: 'admin', name: '超级管理员', description: '系统全部权限', permissions: { scope: 'all', modules: ['dashboard', 'realtime', 'patients', 'teams', 'devices', 'alerts', 'comm', 'orthosis', 'install', 'tech', 'perm', 'config'] } },
    { key: 'director', name: '主任医师', description: '患者管理+数据分析+团队管理', permissions: { scope: 'all', modules: ['dashboard', 'realtime', 'patients', 'teams', 'alerts', 'orthosis', 'install'] } },
    { key: 'doctor', name: '主治医师', description: '患者数据+告警处理+沟通', permissions: { scope: 'team', modules: ['dashboard', 'realtime', 'patients', 'alerts', 'comm', 'orthosis'] } },
    { key: 'therapist', name: '康复师', description: '患者数据查看+矫形日志+沟通', permissions: { scope: 'team', modules: ['realtime', 'patients', 'alerts', 'comm', 'orthosis'] } },
    { key: 'nurse', name: '护士', description: '患者列表查看+基本沟通', permissions: { scope: 'team', modules: ['patients', 'comm'] } },
  ]
}

export interface CreateRoleInput {
  name: string
  description?: string
  template?: string
  permissions?: { scope: string; modules: string[] }
}

export function mockCreateRole(input: CreateRoleInput): AdminRoleRow {
  const name = input.name.trim()
  if (roleCrudStore.some((r) => r.name === name)) {
    throw new Error(`角色名已存在: ${name}`)
  }
  const hex = Array.from({ length: 10 }, () => '0123456789ABCDEF'[Math.floor(Math.random() * 16)]).join('')
  const row: AdminRoleRow = {
    roleId: `ROLE_C${hex}`,
    name,
    description: input.description?.trim() ?? '',
    memberCount: 0,
    createdAt: new Date().toISOString(),
    status: 'enabled',
    preset: false,
  }
  roleCrudStore.push(row)
  return { ...row }
}

export function mockUpdateRole(roleId: string, input: { name?: string; description?: string; status?: 'enabled' | 'disabled' }): AdminRoleRow {
  const row = roleCrudStore.find((r) => r.roleId === roleId)
  if (!row) throw new Error(`角色不存在: ${roleId}`)
  if (input.name !== undefined) {
    if (row.preset) throw new Error(`预置角色名称不可修改: ${roleId}`)
    const name = input.name.trim()
    if (roleCrudStore.some((r) => r.roleId !== roleId && r.name === name)) {
      throw new Error(`角色名已存在: ${name}`)
    }
    row.name = name
  }
  if (input.description !== undefined) row.description = input.description.trim()
  if (input.status !== undefined) row.status = input.status
  return { ...row }
}

export function mockDeleteRole(roleId: string): void {
  const idx = roleCrudStore.findIndex((r) => r.roleId === roleId)
  if (idx === -1) throw new Error(`角色不存在: ${roleId}`)
  const row = roleCrudStore[idx]
  if (row.preset) throw new Error(`预置角色不可删除: ${roleId}`)
  if (row.memberCount > 0) {
    throw new Error(`角色仍被 ${row.memberCount} 个运营账号使用，请先转移成员`)
  }
  roleCrudStore.splice(idx, 1)
}

// T247 / T345: 角色权限 mock —— 形状即契约 RolePermissions（{scope, modules, items}）。
// 原实现返回 {roleId, permissions: 页面路径[]}，是照前端影子接口自洽写的，
// 于是「本地全绿 + staging 保存 400」这个差值在 mock 层永远看不见。
// modules 初值由 ROLE_PAGE_MATRIX 换算成模块短键（mock 里的预置角色按前端矩阵全放开）；
// items 不物化：后端 GET 会按子权限目录回全勾数组，本页是页面级矩阵、不渲染子权限。
// 注意 mockCreateRole 不落 permissions，所以新建角色在 mock 下矩阵为空（后端会落，走真实 API 不受影响）。
const PRESET_ROLES_BY_ID: Record<string, { role: 'admin' | 'doctor' | 'cs'; scope: RolePermissions['scope'] }> = {
  'ROLE-ADMIN': { role: 'admin', scope: 'all' },
  'ROLE-DOCTOR': { role: 'doctor', scope: 'team' },
  'ROLE-CS': { role: 'cs', scope: 'all_patients' },
}

const rolePermStore: Record<string, RolePermissions> = {}

export function mockRolePermissions(roleId: string): RolePermissions {
  if (!rolePermStore[roleId]) {
    const preset = PRESET_ROLES_BY_ID[roleId]
    rolePermStore[roleId] = preset
      ? { scope: preset.scope, modules: modulesForRole(preset.role) }
      : { scope: 'team', modules: [] }
  }
  const stored = rolePermStore[roleId]
  return { ...stored, modules: [...stored.modules] }
}

export function mockUpdateRolePermissions(roleId: string, permissions: RolePermissions): void {
  rolePermStore[roleId] = { ...permissions, modules: [...permissions.modules] }
}

/** 系统配置（PRD §7D.12，默认值对齐 @bracesync/constants DEFAULT_THRESHOLDS） */
export interface SystemSettings {
  dailyWearTargetHours: number
  pressureHighThresholdN: number
  /** T257 12.4 / T289 12.4：≡ 告警页 Tab2 的 threshold_pressure_low；后端 GET 恒回数值 */
  pressureLowThresholdN?: number | null
  pressureFluctuationPct: number
  wearInterruptMinutes: number
  sensorDriftN: number
  wifiPresets: { ssid: string; password: string }[]
  // T247 12.5: 设计稿系统参数三项（系统配置.html:88-90）；字段名对齐后端 SystemSettingsDTO（T269 D2）
  collectIntervalSeconds: number
  retentionDays: number
  maxPatients: number
}

export function mockSystemSettings(): SystemSettings {
  return {
    dailyWearTargetHours: 22,
    pressureHighThresholdN: DEFAULT_THRESHOLDS.PRESSURE_HIGH_N,
    // 后端 defaultUnifiedLowerN（seed threshold_pressure_low，迁移 000021）同值
    pressureLowThresholdN: 1,
    pressureFluctuationPct: DEFAULT_THRESHOLDS.PRESSURE_FLUCTUATION_PCT,
    wearInterruptMinutes: DEFAULT_THRESHOLDS.WEAR_INTERRUPT_MINUTES,
    sensorDriftN: DEFAULT_THRESHOLDS.SENSOR_DRIFT_N,
    wifiPresets: [
      { ssid: 'Hospital-WiFi', password: '********' },
      { ssid: 'Brace-Clinic', password: '********' },
    ],
    collectIntervalSeconds: 60,
    retentionDays: 365,
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
