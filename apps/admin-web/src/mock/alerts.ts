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
  // T289 2.6/2.7：新类型（wear_duration_short）+ 第三态（processing）各一条，
  // 否则「佩戴时长不足」与「处理中」在 mock 下没有可渲染的行。
  {
    alertId: 'ALR-007', patientId: 'PT-004', deviceId: 'DEV-D2A012', type: 'wear_duration_short',
    detail: '当日佩戴 6.5 小时，低于目标 18 小时', sensorPoint: '', thresholdValue: 18, actualValue: 6.5,
    timestamp: '2026-08-10T08:05:00+08:00', readStatus: 'unread', processStatus: 'processing',
    resolvedStatus: 'active', resolvedAt: null, inProgressAt: '2026-08-10T08:30:00+08:00',
    processedBy: null, processedAt: null, processNote: null,
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

/** T289 2.7：对齐后端 startProcessingAlert 语义（pending→processing 幂等；processed 409；不存在 404） */
export function mockStartProcessing(alertId: string): { alertId: string; processStatus: string; inProgressAt: string } {
  const row = ALERTS.find((a) => a.alertId === alertId)
  if (!row) throw new Error(`告警不存在: ${alertId}`)
  if (row.processStatus === 'processed') throw new Error('告警已处理，不允许重新打开')
  if (row.processStatus === 'pending') {
    row.processStatus = 'processing'
    row.inProgressAt = new Date().toISOString()
  }
  return { alertId, processStatus: row.processStatus, inProgressAt: row.inProgressAt ?? '' }
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

// ========== T300 患者异常报告（汇总 + CSV 导出）mock ==========
// 形状对齐 services/alert-service/internal/handler/report.go（abnormalReportData 四视图 / export 16 列）。
// mock 沿用后端同一设计：明细是唯一来源、汇总由明细派生 ⇒ 两个视图在 mock 里也不会互相矛盾。

export interface AbnormalReportCount {
  key: string
  count: number
}

/** GET /api/v1/admin/abnormal-reports 的 data */
export interface AbnormalReport {
  patientId: string
  start: string
  end: string
  total: number
  byStatus: AbnormalReportCount[]
  byType: AbnormalReportCount[]
  byDay: AbnormalReportCount[]
}

export interface AbnormalReportQuery {
  patientId: string
  /** 北京日历日 YYYY-MM-DD，含端点 */
  start: string
  end: string
}

/** 导出的单条明细（字段集与后端 CSV 的 16 列一一对应） */
interface AbnormalDetailRow {
  alertId: string
  patientId: string
  patientName: string
  deviceId: string
  type: string
  sensorPoint: string
  detail: string
  thresholdN: number
  actualN: number
  /** 北京时间 'YYYY-MM-DD HH:mm:ss'（后端同口径，便于医院侧直接翻阅） */
  ts: string
  day: string
  readStatus: string
  processStatus: string
  inProgressAt: string | null
  processedAt: string | null
  processedBy: string | null
  processNote: string | null
}

// 中文口径与后端 handler/report.go 的 reportAlertTypeLabels 保持一致（措辞跟「告警管理」页现状）
const REPORT_TYPE_LABELS: Record<string, string> = {
  pressure_high: '压力偏高',
  pressure_fluctuation: '压力波动',
  wear_interrupt: '佩戴中断',
  sensor_drift: '传感器漂移',
  wear_duration_short: '佩戴时长不足',
}
const REPORT_TYPE_KEYS = Object.keys(REPORT_TYPE_LABELS)
const REPORT_STATUS_KEYS = ['pending', 'processing', 'processed']
const REPORT_STATUS_LABELS: Record<string, string> = {
  pending: '待处理',
  processing: '处理中',
  processed: '已处理',
}
const REPORT_READ_LABELS: Record<string, string> = { unread: '未读', read: '已读' }
const REPORT_PROCESSORS = ['张建国', '李明华', '王医生']
/** mock 明细上限（与后端 repo.MaxExportRows 同一意图：区间拉满也不至于卡住浏览器） */
const REPORT_MOCK_MAX_ROWS = 400

function labelOfReport(labels: Record<string, string>, key: string): string {
  return labels[key] ?? key
}

/** 'YYYY-MM-DD' → UTC 日序号；非日期串返回 null（区间不合法时 mock 给空结果） */
function dayIndex(date: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  if (!m) return null
  const ms = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  return Number.isNaN(ms) ? null : Math.floor(ms / 86400000)
}

function dayLabel(idx: number): string {
  return new Date(idx * 86400000).toISOString().slice(0, 10)
}

function hashCode(s: string): number {
  let h = 0
  for (const c of s) h = (h * 31 + c.charCodeAt(0)) % 100003
  return h
}

/** 按「患者 + 日期范围」造一份稳定可复现的明细（同一入参永远同一结果） */
function abnormalRowsFor(q: AbnormalReportQuery): AbnormalDetailRow[] {
  const first = dayIndex(q.start)
  const last = dayIndex(q.end)
  if (first === null || last === null || last < first) return []
  const seed = hashCode(q.patientId)
  const patientName = ALERT_PATIENT_NAMES[q.patientId] ?? q.patientId
  const deviceId = `DEV-${(seed % 65536).toString(16).toUpperCase().padStart(4, '0')}`
  const rows: AbnormalDetailRow[] = []
  for (let day = first; day <= last && rows.length < REPORT_MOCK_MAX_ROWS; day++) {
    const perDay = (seed + day * 7) % 3
    for (let j = 0; j < perDay; j++) {
      const v = (seed + day * 11 + j * 13) % 17
      const type = REPORT_TYPE_KEYS[(seed + day + j) % REPORT_TYPE_KEYS.length]
      const status = REPORT_STATUS_KEYS[v % 3]
      const label = labelOfReport(REPORT_TYPE_LABELS, type)
      const threshold = type === 'pressure_high' ? 45 : type === 'sensor_drift' ? 3 : 0
      const actual = threshold ? threshold + (v % 9) * 1.5 : 0
      const dayText = dayLabel(day)
      const time = `${String(8 + (v % 12)).padStart(2, '0')}:${String(v * 3 % 60).padStart(2, '0')}:00`
      rows.push({
        alertId: String(20000 + ((seed * 977 + day + j) % 70000)),
        patientId: q.patientId,
        patientName,
        deviceId,
        type,
        sensorPoint: `P${String(((seed + j) % 20) + 1).padStart(2, '0')}`,
        detail: `${label}${threshold ? `，实测 ${actual.toFixed(1)}N` : ''}`,
        thresholdN: threshold,
        actualN: actual,
        ts: `${dayText} ${time}`,
        day: dayText,
        readStatus: v % 4 === 0 ? 'unread' : 'read',
        processStatus: status,
        inProgressAt: status === 'pending' ? null : `${dayText} ${time}`,
        processedAt: status === 'processed' ? `${dayText} ${time}` : null,
        processedBy: status === 'processed' ? REPORT_PROCESSORS[v % REPORT_PROCESSORS.length] : null,
        processNote: status === 'processed' ? '已电话指导患者调整佩戴方案' : null,
      })
    }
  }
  return rows
}

function countBy(rows: AbnormalDetailRow[], pick: (r: AbnormalDetailRow) => string): AbnormalReportCount[] {
  const acc = new Map<string, number>()
  for (const r of rows) {
    const key = pick(r)
    acc.set(key, (acc.get(key) ?? 0) + 1)
  }
  return [...acc.entries()].map(([key, count]) => ({ key, count }))
}

export function mockAbnormalReport(q: AbnormalReportQuery): AbnormalReport {
  const rows = abnormalRowsFor(q)
  const byStatus = new Map(countBy(rows, (r) => r.processStatus).map((c) => [c.key, c.count]))
  return {
    patientId: q.patientId,
    start: q.start,
    end: q.end,
    total: rows.length,
    // 三视角排序口径同后端：状态固定序（缺项补 0）、类型按计数降序、日期升序
    byStatus: REPORT_STATUS_KEYS.map((k) => ({ key: k, count: byStatus.get(k) ?? 0 })),
    byType: countBy(rows, (r) => r.type).sort((a, b) => b.count - a.count || a.key.localeCompare(b.key)),
    byDay: countBy(rows, (r) => r.day).sort((a, b) => a.key.localeCompare(b.key)),
  }
}

const REPORT_CSV_HEADER = [
  '告警ID', '患者ID', '患者姓名', '设备ID', '异常类型', '采集点', '详情',
  '阈值(N)', '实际值(N)', '采集时间(北京)', '读取状态', '处理状态',
  '开始处理时间', '处理时间', '处理人', '处理备注',
]

function csvField(v: string | number | null): string {
  const s = v === null || v === undefined ? '' : String(v)
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

/** mock 模式的 CSV 正文（含 BOM，与后端导出同口径，便于前端下载链路在 mock 下也可点） */
export function mockAbnormalReportCsv(q: AbnormalReportQuery): string {
  const rows = abnormalRowsFor(q)
  const lines = [REPORT_CSV_HEADER.map(csvField).join(',')]
  for (const r of rows) {
    lines.push([
      r.alertId, r.patientId, r.patientName, r.deviceId,
      labelOfReport(REPORT_TYPE_LABELS, r.type), r.sensorPoint, r.detail,
      r.thresholdN.toFixed(2), r.actualN.toFixed(2), r.ts,
      labelOfReport(REPORT_READ_LABELS, r.readStatus), labelOfReport(REPORT_STATUS_LABELS, r.processStatus),
      r.inProgressAt ?? '', r.processedAt ?? '', r.processedBy ?? '', r.processNote ?? '',
    ].map(csvField).join(','))
  }
  return `\ufeff${lines.join('\r\n')}\r\n`
}
