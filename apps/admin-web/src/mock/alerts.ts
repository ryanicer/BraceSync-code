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

// ========== T253-2.2 告警规则配置 mock（契约：docs/api/api-contracts.ts AlertRules，T252 后端同形） ==========

/** 单个采集点规则 = 4×5 网格的一格 */
export interface AlertPointRule {
  pointId: string // 'P01'–'P20'
  row: number // 1–4
  col: number // 1–5
  label: string // 'R1C1'
  monitored: boolean
  upperN: number | null // null = 跟随统一上限
  lowerN: number | null
  effectiveUpperN: number
  effectiveLowerN: number
}

/** 保存时的点位增量（未列出的点位保持原值） */
export interface AlertPointRuleUpdate {
  pointId: string
  monitored?: boolean
  upperN?: number | null
  lowerN?: number | null
}

/** 全局告警规则四项（设计稿 Tab2 第二卡） */
export interface AlertGlobalRules {
  deviceOfflineMinutes: number
  dailyWearMinHours: number
  continuousWearMaxHours: number
  reportTimeoutMinutes: number
}

/** 规则配置聚合视图（一次 GET 渲染整个 Tab2） */
export interface AlertRules {
  unifiedUpperN: number
  unifiedLowerN: number
  points: AlertPointRule[]
  globalRules: AlertGlobalRules
}

const GRID_ROWS = 4
const GRID_COLS = 5
// 默认值对齐 T252 后端：统一上限 §7D.12 = 45N，下限设计稿 = 10N；全局四项同 T252 默认
const DEFAULT_UNIFIED_UPPER_N = 45
const DEFAULT_UNIFIED_LOWER_N = 10
const DEFAULT_GLOBAL_RULES: AlertGlobalRules = {
  deviceOfflineMinutes: 30,
  dailyWearMinHours: 18,
  continuousWearMaxHours: 23,
  reportTimeoutMinutes: 5,
}

function buildDefaultPoints(unifiedUpper: number, unifiedLower: number): AlertPointRule[] {
  const points: AlertPointRule[] = []
  for (let r = 1; r <= GRID_ROWS; r++) {
    for (let c = 1; c <= GRID_COLS; c++) {
      const i = (r - 1) * GRID_COLS + c
      points.push({
        pointId: `P${String(i).padStart(2, '0')}`,
        row: r,
        col: c,
        label: `R${r}C${c}`,
        monitored: true,
        upperN: null,
        lowerN: null,
        effectiveUpperN: unifiedUpper,
        effectiveLowerN: unifiedLower,
      })
    }
  }
  return points
}

let alertRulesState: AlertRules = {
  unifiedUpperN: DEFAULT_UNIFIED_UPPER_N,
  unifiedLowerN: DEFAULT_UNIFIED_LOWER_N,
  points: buildDefaultPoints(DEFAULT_UNIFIED_UPPER_N, DEFAULT_UNIFIED_LOWER_N),
  globalRules: { ...DEFAULT_GLOBAL_RULES },
}

function snapshotAlertRules(): AlertRules {
  return {
    unifiedUpperN: alertRulesState.unifiedUpperN,
    unifiedLowerN: alertRulesState.unifiedLowerN,
    points: alertRulesState.points.map((p) => ({ ...p })),
    globalRules: { ...alertRulesState.globalRules },
  }
}

function recomputeEffective(): void {
  for (const p of alertRulesState.points) {
    p.effectiveUpperN = p.upperN ?? alertRulesState.unifiedUpperN
    p.effectiveLowerN = p.lowerN ?? alertRulesState.unifiedLowerN
  }
}

export function mockAlertRules(): AlertRules {
  return snapshotAlertRules()
}

export function mockSaveAlertPointRules(input: {
  unifiedUpperN?: number
  unifiedLowerN?: number
  points?: AlertPointRuleUpdate[]
}): AlertRules {
  if (input.unifiedUpperN !== undefined) alertRulesState.unifiedUpperN = input.unifiedUpperN
  if (input.unifiedLowerN !== undefined) alertRulesState.unifiedLowerN = input.unifiedLowerN
  for (const u of input.points ?? []) {
    const p = alertRulesState.points.find((x) => x.pointId === u.pointId)
    if (!p) continue
    if (u.monitored !== undefined) p.monitored = u.monitored
    if (u.upperN !== undefined) p.upperN = u.upperN
    if (u.lowerN !== undefined) p.lowerN = u.lowerN
  }
  recomputeEffective()
  return snapshotAlertRules()
}

export function mockResetAlertPointRules(): AlertRules {
  alertRulesState.unifiedUpperN = DEFAULT_UNIFIED_UPPER_N
  alertRulesState.unifiedLowerN = DEFAULT_UNIFIED_LOWER_N
  alertRulesState.points = buildDefaultPoints(DEFAULT_UNIFIED_UPPER_N, DEFAULT_UNIFIED_LOWER_N)
  return snapshotAlertRules()
}

export function mockSaveAlertGlobalRules(input: Partial<AlertGlobalRules>): AlertRules {
  alertRulesState.globalRules = { ...alertRulesState.globalRules, ...input }
  return snapshotAlertRules()
}
