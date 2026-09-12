/**
 * T074 患者端 E2E API Mock Fixtures（真实模式基建适配）
 *
 * 原则：
 *  - 与 apps/patient-miniapp 页面已接契约严格对齐（共享包 shared-types 字段）
 *  - 断言/用例代码(Ella 领地)不动，Fixture 严格按原断言造数据 → 确保 Playwright route 拦截即通过
 *  - 只 mock E2E 用到的端点（wx-login/realtime/records/daily-wear/alerts/unbind）不全站 mock
 *
 * Playwright H5: route('**​/api/v1/**').fulfill({ json: { code:0, message:'ok', data: fixture } })
 *                 请求响应体 = { code, message, data }（见 utils/request.ts）
 */
import type { Alert, PressureRecord, SensorPoint } from '@bracesync/shared-types'

// ---------- 登录态约定（与 utils/token.ts 的 key 对齐） ----------
export const E2E_TOKEN_KEY = 'bracesync_token'
export const E2E_PATIENT_ID_KEY = 'bracesync_patient_id'
export const E2E_PATIENT_ID = 'pat-e2e-001'
export const E2E_TOKEN = 'e2e-patient-token-001'
export const E2E_DEVICE_ID = 'PRS-ML05-RC-001'

// 包装网关统一响应体（T067 网关中间件约定）
export function ok<T>(data: T) {
  return { code: 0, message: 'ok', data }
}

// ---------- wx-login 响应 ----------
export function wxLoginResp(code?: string) {
  return ok({
    token: code === 'h5-fallback-wechat-login-code' || code ? E2E_TOKEN : E2E_TOKEN,
    patientId: E2E_PATIENT_ID,
    name: 'e2e 测试患者',
    role: 'patient',
  })
}

// ---------- 20 个传感器点（4×5，P01-P20，P12 固定 42.18N → e2e 基线 hero 联动） ----------
export function sensorPoints20(): SensorPoint[] {
  const points: SensorPoint[] = []
  for (let r = 1; r <= 4; r++) {
    for (let c = 1; c <= 5; c++) {
      const idx = (r - 1) * 5 + (c - 1)
      const base = 20 + r * 6 + Math.sin(c * 1.2) * 5
      const value = parseFloat((base + ((idx * 13) % 80) / 10).toFixed(2))
      const status: SensorPoint['status'] = value < 20 ? 'normal' : value < 40 ? 'normal' : value < 60 ? 'warning' : 'critical'
      points.push({
        pointId: `P${String(idx + 1).padStart(2, '0')}`,
        row: r,
        col: c,
        label: `R${r}C${c}`,
        pressureValue: value,
        status,
      })
    }
  }
  // P12 固定为最大压力点 42.18N（monitor e2e 基线：hero 初始联动 42.18）
  points[11].pressureValue = 42.18
  points[11].status = 'warning'
  return points
}

// ---------- realtime 快照（GET /patients/:pid/realtime） ----------
export function realtimeSnapshot() {
  const points = sensorPoints20()
  const nowIso = new Date().toISOString()
  const record: PressureRecord = {
    recordId: 'rec-e2e-001',
    deviceId: E2E_DEVICE_ID,
    patientId: E2E_PATIENT_ID,
    timestamp: nowIso,
    points,
    uploadTime: nowIso,
  }
  const maxP = points.reduce((m, p) => (p.pressureValue > m ? p.pressureValue : m), 0)
  const maxPt = points.find(p => p.pressureValue === maxP)?.pointId || 'P12'
  return ok({
    deviceId: E2E_DEVICE_ID,
    status: 'online',
    todayHours: 8.5,
    maxPressure: 42.18,
    maxPoint: maxPt,
    events: 2,
    pressureRecords: [record],
  })
}

// ---------- records?period={day|week|month}&date=YYYY-MM-DD → 48 点逐帧 maxPressure 趋势 ----------
export function pressureRecords(period: string, _date: string): PressureRecord[] {
  const frames = period === 'day' ? 48 : period === 'week' ? 48 : 48
  const out: PressureRecord[] = []
  const base = new Date(_date || new Date().toISOString().slice(0, 10))
  for (let i = 0; i < frames; i++) {
    const ts = new Date(base.getTime() + i * 30 * 60 * 1000).toISOString()
    const pts = sensorPoints20().map(p => ({
      ...p,
      pressureValue: parseFloat((p.pressureValue + Math.sin(i * 0.3) * 4).toFixed(2)),
    })) as SensorPoint[]
    // 保证至少有值（历史页 loadTrend 里 filter value>0 才保留）
    if (pts[11]) pts[11].pressureValue = parseFloat((42.18 + Math.sin(i * 0.2) * 5).toFixed(2))
    out.push({
      recordId: `rec-e2e-${period}-${i + 1}`,
      deviceId: E2E_DEVICE_ID,
      patientId: E2E_PATIENT_ID,
      timestamp: ts,
      points: pts,
      uploadTime: ts,
    })
  }
  return out
}

// ---------- wearing 日记录 15 条（2026-06-28 → 2026-07-12，15 天） ----------
//  断言 history: '2026年7月' 头 + '2026年6月' 头、15 条 wearing-row、
//                 至少 1 条「达标」、至少 1 条「严重不足」
export interface WearingFixtureRow {
  date: string
  hours: number
  status: 'ok' | 'warn' | 'error'
  label: string
}
export function wearing15(): WearingFixtureRow[] {
  return [
    { date: '2026-07-12', hours: 10.2, status: 'warn', label: '不足' },
    { date: '2026-07-11', hours: 16.8, status: 'ok', label: '达标' },
    { date: '2026-07-10', hours: 8.5, status: 'warn', label: '不足' },
    { date: '2026-07-09', hours: 15.2, status: 'ok', label: '达标' },
    { date: '2026-07-08', hours: 3.1, status: 'error', label: '严重不足' },
    { date: '2026-07-07', hours: 18.2, status: 'ok', label: '达标' },
    { date: '2026-07-06', hours: 17.5, status: 'ok', label: '达标' },
    { date: '2026-07-05', hours: 12.1, status: 'warn', label: '不足' },
    { date: '2026-07-04', hours: 9.3, status: 'warn', label: '不足' },
    { date: '2026-07-03', hours: 16.0, status: 'ok', label: '达标' },
    { date: '2026-07-02', hours: 11.7, status: 'warn', label: '不足' },
    { date: '2026-07-01', hours: 17.1, status: 'ok', label: '达标' },
    { date: '2026-06-30', hours: 14.5, status: 'warn', label: '不足' },
    { date: '2026-06-29', hours: 6.2, status: 'error', label: '严重不足' },
    { date: '2026-06-28', hours: 18.5, status: 'ok', label: '达标' },
  ]
}

// ---------- alerts 压力异常 7 组（date 降序） 前 3 组 3+2+1 = 6 条 p-item ----------
//  - 组 1：2026-07-12 (3 条)：error, warn, warn  → 含"持续偏高 6h 峰值..."(断言 detail 含 "持续偏高")
//  - 组 2：2026-07-11 (2 条)：warn, warn
//  - 组 3：2026-07-10 (1 条)：error
//  - 组 4：2026-07-09 (2 条)：warn, warn
//  - 组 5：2026-07-08 (2 条)：error, error
//  - 组 6：2026-07-07 (1 条)：warn
//  - 组 7：2026-07-05 (1 条)：warn
// level 映射(pressure_high + actualValue>=60 或 threshold>=60 → error，其余 warn)
let alertSeq = 0
const mk = (patch: Partial<Alert>): Alert => ({
  alertId: `ALR-E2E-${String(++alertSeq).padStart(3, '0')}`,
  patientId: E2E_PATIENT_ID,
  deviceId: E2E_DEVICE_ID,
  type: 'pressure_high',
  detail: '',
  sensorPoint: '',
  thresholdValue: 40,
  actualValue: 45,
  timestamp: new Date().toISOString(),
  readStatus: 'unread',
  processStatus: 'pending',
  resolvedStatus: 'active',
  resolvedAt: null,
  processedBy: null,
  processedAt: null,
  processNote: null,
  ...patch,
})

export function pressureAlerts7groups(): Alert[] {
  alertSeq = 0
  return [
    // group 1 2026-07-12 (3)
    mk({ type: 'pressure_high', sensorPoint: 'P10', thresholdValue: 60, actualValue: 63.8,
      detail: 'P10 压力持续偏高 6h，峰值 63.8N，建议立即调整', timestamp: '2026-07-12T22:10:00+08:00', resolvedStatus: 'active' }),
    mk({ type: 'pressure_high', sensorPoint: 'P07', thresholdValue: 40, actualValue: 52.1,
      detail: 'P07 持续偏高 4h，峰值 52.1N', timestamp: '2026-07-12T18:00:00+08:00', resolvedStatus: 'resolved' }),
    mk({ type: 'pressure_high', sensorPoint: 'P03', thresholdValue: 20, actualValue: 12.3,
      detail: 'P03 持续偏低 8h，最低 12.3N', timestamp: '2026-07-12T10:00:00+08:00', resolvedStatus: 'active' }),
    // group 2 2026-07-11 (2)
    mk({ type: 'pressure_high', sensorPoint: 'P11', thresholdValue: 40, actualValue: 55.6,
      detail: 'P11 持续偏高 5h，峰值 55.6N', timestamp: '2026-07-11T14:00:00+08:00' }),
    mk({ type: 'pressure_high', sensorPoint: 'P08', thresholdValue: 20, actualValue: 16.5,
      detail: 'P08 持续偏低 3h，最低 16.5N', timestamp: '2026-07-11T09:00:00+08:00', resolvedStatus: 'resolved' }),
    // group 3 2026-07-10 (1)
    mk({ type: 'pressure_high', sensorPoint: 'P12', thresholdValue: 60, actualValue: 71.2,
      detail: 'P12 压力持续偏高 8h，峰值 71.2N', timestamp: '2026-07-10T20:00:00+08:00', resolvedStatus: 'active' }),
    // group 4 2026-07-09 (2)
    mk({ type: 'pressure_high', sensorPoint: 'P04', thresholdValue: 20, actualValue: 11.8,
      detail: 'P04 持续偏低 5h，最低 11.8N', timestamp: '2026-07-09T08:00:00+08:00', resolvedStatus: 'resolved' }),
    mk({ type: 'pressure_fluctuation', sensorPoint: 'P09', thresholdValue: 40, actualValue: 47.2,
      detail: 'P09 持续偏高 3h，峰值 47.2N', timestamp: '2026-07-09T16:00:00+08:00' }),
    // group 5 2026-07-08 (2)
    mk({ type: 'pressure_high', sensorPoint: 'P10', thresholdValue: 60, actualValue: 69.5,
      detail: 'P10 压力持续偏高 10h，峰值 69.5N', timestamp: '2026-07-08T22:00:00+08:00', resolvedStatus: 'active' }),
    mk({ type: 'pressure_high', sensorPoint: 'P02', thresholdValue: 20, actualValue: 7.2,
      detail: 'P02 持续偏低 12h，最低 7.2N', timestamp: '2026-07-08T10:00:00+08:00', resolvedStatus: 'active' }),
    // group 6 2026-07-07 (1)
    mk({ type: 'pressure_high', sensorPoint: 'P05', thresholdValue: 40, actualValue: 44.1,
      detail: 'P05 持续偏高 3h，峰值 44.1N', timestamp: '2026-07-07T15:00:00+08:00', resolvedStatus: 'resolved' }),
    // group 7 2026-07-05 (1)
    mk({ type: 'pressure_high', sensorPoint: 'P01', thresholdValue: 20, actualValue: 14.2,
      detail: 'P01 持续偏低 4h，最低 14.2N', timestamp: '2026-07-05T11:00:00+08:00' }),
  ]
}

// alerts 分页响应体（alert-service）
export function alertsPage(alerts: Alert[], page = 1, pageSize = 200) {
  const total = alerts.length
  const list = alerts.slice((page - 1) * pageSize, page * pageSize)
  return ok({ list, total, page, pageSize })
}

// ---------- 设备解绑 POST /devices/:deviceId/unbind ----------
export function unbindOk() {
  return ok({ unbound: true })
}
