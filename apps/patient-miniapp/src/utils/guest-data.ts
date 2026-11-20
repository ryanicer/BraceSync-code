/**
 * 游客体验模式的本地演示数据（仅 GET 读接口）。
 * 复用 src/mock/ 下的既有假数据函数，不新造真实患者信息，不调用后端。
 */
import { mockRealtime, mockSensorPoints } from '../mock/monitor'
import { mockDevice } from '../mock/device'
import { mockPatientAlerts } from '../mock/alerts'
import { mockWearingData, mockPressureAnomalies } from '../mock/history'
import { GUEST_PATIENT_ID } from './guest'

/** 按请求 URL 返回对应演示数据；未识别返回 undefined（由调用方兜空态） */
export function guestResponse<T>(url: string): T | undefined {
  const u = url.split('?')[0]

  // 实时监测：快照（含压力热力图）
  if (/\/realtime$/.test(u)) {
    const snap = mockRealtime()
    const points = mockSensorPoints()
    return {
      ...snap,
      deviceId: mockDevice().deviceId,
      battery: 86,
      todayHours: 8.5,
      pressureRecords: [{
        recordId: 'DEMO-REC-001',
        deviceId: mockDevice().deviceId,
        patientId: GUEST_PATIENT_ID,
        timestamp: new Date().toISOString(),
        uploadTime: new Date().toISOString(),
        points,
      }],
      pressureHeatmap: points.map(p => ({ pointId: p.pointId, pressureValue: p.pressureValue, pressureKpa: null })),
      heatmapMaxKpa: null,
      pressureLowN: 20,
      pressureHighN: 60,
      events: 2,
    } as unknown as T
  }

  // 历史趋势（records）：返回一页演示帧
  if (/\/records$/.test(u)) {
    const points = mockSensorPoints()
    const now = Date.now()
    const list = Array.from({ length: 24 }, (_, i) => ({
      recordId: `DEMO-REC-${i}`,
      deviceId: mockDevice().deviceId,
      patientId: GUEST_PATIENT_ID,
      timestamp: new Date(now - (23 - i) * 3600_000).toISOString(),
      uploadTime: new Date(now - (23 - i) * 3600_000).toISOString(),
      points,
    }))
    return { list, total: list.length, page: 1, pageSize: 24 } as unknown as T
  }

  // 异常告警
  if (/\/alerts$/.test(u)) {
    return { list: mockPatientAlerts(), total: 2, page: 1, pageSize: 100 } as unknown as T
  }

  // 佩戴数据
  if (/\/daily-wear$/.test(u)) {
    return mockWearingData().map(d => ({
      date: d.date,
      hours: d.hours,
      targetHours: 12,
      status: d.status,
    })) as unknown as T
  }

  // 用户档案
  if (/\/patient\/profile$/.test(u)) {
    return {
      patientId: GUEST_PATIENT_ID,
      name: '体验用户',
      gender: '—',
      age: null,
      phone: '—',
      deviceId: mockDevice().deviceId,
    } as unknown as T
  }

  // 复查记录
  if (/\/review-records$/.test(u)) {
    return [] as unknown as T
  }

  // 康复建议 / 护理团队
  if (/\/advice$/.test(u) || /\/care-team$/.test(u)) {
    return [] as unknown as T
  }

  // 穿戴记录 / 压力异常（feelings / anomaly 若用到）
  if (/\/feelings$/.test(u)) return [] as unknown as T

  // 上述问题点位历史
  if (/\/pressure-anomalies$/.test(u)) {
    return mockPressureAnomalies() as unknown as T
  }

  return undefined
}
