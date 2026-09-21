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
// 对齐后端 HistoryPage 分页契约：{ list: [...], total, page, pageSize }
// （data-service GetHistory 返回 HistoryPage，前端 request 解包 envelope 后拿到此对象）
export function pressureRecords(period: string, _date: string) {
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
  return { list: out, total: out.length, page: 1, pageSize: out.length }
}

// ---------- wearing 日记录 15 条（2026-06-28 → 2026-07-12，15 天） ----------
//  断言 anomaly（T221 日历版）：切到 '2026年7月' 后 3 红 6 橙圆点、
//                 点 07-08 详情卡 3.1h/严重不足、压力页签 07-12 3 条异常
// 形状对齐后端 DailyWearDayDTO（data-service model.go）：date/wearMinutes/avgPressure/
// maxPressure/maxPoint/frameCount/abnormalCount。hours/status 由页面派生
// （≥16h ok / ≥4h warn / <4h error），夹具不再自带前端字段（真机曾因形状错位全空）。
export interface WearingFixtureRow {
  date: string
  wearMinutes: number
  avgPressure: number
  maxPressure: number
  maxPoint: string
  frameCount: number
  abnormalCount: number
}
/** 按小时数生成 DTO 行（wearMinutes = hours*60，页面派生回同一 hours） */
function wearDay(date: string, hours: number): WearingFixtureRow {
  return {
    date,
    wearMinutes: Math.round(hours * 60),
    avgPressure: 28.5,
    maxPressure: 42.18,
    maxPoint: 'P12',
    frameCount: 1728,
    abnormalCount: 0,
  }
}
export function wearing15(): WearingFixtureRow[] {
  return [
    wearDay('2026-07-12', 10.2),
    wearDay('2026-07-11', 16.8),
    wearDay('2026-07-10', 8.5),
    wearDay('2026-07-09', 15.2),
    wearDay('2026-07-08', 3.1),
    wearDay('2026-07-07', 18.2),
    wearDay('2026-07-06', 17.5),
    wearDay('2026-07-05', 12.1),
    wearDay('2026-07-04', 9.3),
    wearDay('2026-07-03', 16.0),
    wearDay('2026-07-02', 11.7),
    wearDay('2026-07-01', 17.1),
    wearDay('2026-06-30', 14.5),
    wearDay('2026-06-29', 6.2),
    wearDay('2026-06-28', 18.5),
  ]
}

// ---------- alerts 异常事件 7 组（date 降序，T298 起按后端 4 类口径造行） ----------
//  T298：detail 一律用 alert-service engine.go 的自述句格式（前端不再拼数、设计稿无「偏低」类型）；
//  佩戴时长不足的 thresholdValue/actualValue 按引擎实况写**分钟**（18h → 1080）。
//  - 组 1：2026-07-12 (3 条)：压力偏高 error + 压力偏高 warn + 佩戴时长不足 warn
//  - 组 2：2026-07-11 (2 条)：压力偏高 warn + 传感器标定异常 warn
//  - 组 3：2026-07-10 (1 条)：压力偏高 error
//  - 组 4：2026-07-09 (2 条)：压力偏高 warn + 压力波动 warn（历史类型）
//  - 组 5：2026-07-08 (2 条)：压力偏高 error + 佩戴时长不足 warn
//  - 组 6：2026-07-07 (1 条)：压力偏高 warn
//  - 组 7：2026-07-05 (2 条)：压力偏高 warn + 传感器标定异常 warn
// level 映射(pressure_high + actualValue>=60 或 threshold>=60 → error，其余 warn)
// 日期集合与改前一致（05/07/08/09/10/11/12），故日历圆点仍是 3 红 6 橙
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
      detail: '压力偏高：采集点 P10 压力 63.8N 超阈值 60.0N', timestamp: '2026-07-12T22:10:00+08:00', resolvedStatus: 'active' }),
    mk({ type: 'pressure_high', sensorPoint: 'P07', thresholdValue: 40, actualValue: 52.1,
      detail: '压力偏高：采集点 P07 压力 52.1N 超阈值 40.0N', timestamp: '2026-07-12T18:00:00+08:00', resolvedStatus: 'resolved' }),
    mk({ type: 'wear_duration_short', sensorPoint: '', thresholdValue: 1080, actualValue: 612,
      detail: `佩戴时长不足：${E2E_PATIENT_ID} 于 2026-07-12 累计佩戴 10.2 小时，低于目标 18.0 小时`,
      timestamp: '2026-07-12T23:59:59+08:00' }),
    // group 2 2026-07-11 (2)
    mk({ type: 'pressure_high', sensorPoint: 'P11', thresholdValue: 40, actualValue: 55.6,
      detail: '压力偏高：采集点 P11 压力 55.6N 超阈值 40.0N', timestamp: '2026-07-11T14:00:00+08:00' }),
    mk({ type: 'sensor_drift', sensorPoint: 'P08', thresholdValue: 15, actualValue: 21.4,
      detail: '传感器漂移：空载采集点 P08 读数 21.4N 异常（阈值 15.0N），通知技师+运营',
      timestamp: '2026-07-11T09:00:00+08:00', resolvedStatus: 'resolved' }),
    // group 3 2026-07-10 (1)
    mk({ type: 'pressure_high', sensorPoint: 'P12', thresholdValue: 60, actualValue: 71.2,
      detail: '压力偏高：采集点 P12 压力 71.2N 超阈值 60.0N', timestamp: '2026-07-10T20:00:00+08:00', resolvedStatus: 'active' }),
    // group 4 2026-07-09 (2)
    mk({ type: 'pressure_high', sensorPoint: 'P04', thresholdValue: 20, actualValue: 24.6,
      detail: '压力偏高：采集点 P04 压力 24.6N 超阈值 20.0N', timestamp: '2026-07-09T08:00:00+08:00', resolvedStatus: 'resolved' }),
    mk({ type: 'pressure_fluctuation', sensorPoint: 'P09', thresholdValue: 40, actualValue: 47.2,
      detail: '压力波动：采集点 P09 波动率 47.2% 超阈值 40.0%（历史规则，引擎已不再产生）', timestamp: '2026-07-09T16:00:00+08:00' }),
    // group 5 2026-07-08 (2)
    mk({ type: 'pressure_high', sensorPoint: 'P10', thresholdValue: 60, actualValue: 69.5,
      detail: '压力偏高：采集点 P10 压力 69.5N 超阈值 60.0N', timestamp: '2026-07-08T22:00:00+08:00', resolvedStatus: 'active' }),
    mk({ type: 'wear_duration_short', sensorPoint: '', thresholdValue: 1080, actualValue: 186,
      detail: `佩戴时长不足：${E2E_PATIENT_ID} 于 2026-07-08 累计佩戴 3.1 小时，低于目标 18.0 小时`,
      timestamp: '2026-07-08T23:59:59+08:00', resolvedStatus: 'resolved' }),
    // group 6 2026-07-07 (1)
    mk({ type: 'pressure_high', sensorPoint: 'P05', thresholdValue: 40, actualValue: 44.1,
      detail: '压力偏高：采集点 P05 压力 44.1N 超阈值 40.0N', timestamp: '2026-07-07T15:00:00+08:00', resolvedStatus: 'resolved' }),
    // group 7 2026-07-05 (2)
    mk({ type: 'pressure_high', sensorPoint: 'P01', thresholdValue: 20, actualValue: 22.6,
      detail: '压力偏高：采集点 P01 压力 22.6N 超阈值 20.0N', timestamp: '2026-07-05T11:00:00+08:00' }),
    mk({ type: 'sensor_drift', sensorPoint: 'P06', thresholdValue: 15, actualValue: 18.9,
      detail: '传感器漂移：空载采集点 P06 读数 18.9N 异常（阈值 15.0N），通知技师+运营',
      timestamp: '2026-07-05T16:00:00+08:00', resolvedStatus: 'resolved' }),
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
