// T144 契约漂移门禁（admin-web 侧，契约权威）
//
// 目标：防止「前API层消费的响应字段名」与「契约类型（packages/shared-types）」漂移。
// 参考案例：T143 —— 前端 listInstallRecords 读 res.records，而契约 PaginatedResponse 字段是 list。
//
// 机制：
//  1) 夹具字段名「同源自契约」：对象字面量以 shared-types 契约类型（PaginatedResponse<T> 等）显式声明，
//     字段名即契约字段名（若写错字段名，类型检查/多余属性会直接报错）。
//  2) 走真实消费路径：vi.mock utils/request 为 USE_MOCK=false + 可控 request，让被测 API 走真实分支。
//  3) 断言 list.length >= 1：接入方一旦误读非契约字段（如 .records），契约夹具不含该字段 → undefined → 断言失败 → CI 变红。
import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest'
import type {
  PaginatedResponse,
  Patient,
  Device,
  Alert,
  InstallRecord,
  ReviewRecord,
  Team,
  Doctor,
} from '@bracesync/shared-types'
import {
  fetchPatients,
  fetchDevices,
  fetchAlerts,
  fetchInstallRecords,
  fetchReviewRecords,
  fetchTeams,
  fetchDoctors,
  fetchSystemSettings,
  saveSystemSettingsApi,
  processAlertApi,
  fetchAbnormalReport,
  exportAbnormalReportApi,
  teamNameOf,
  doctorNameOf,
} from '../src/api'

// vi.mock 会被提升到文件顶部；在 factory 内创建 mock 函数并导出，
// 后续 import 拿到的就是同一个 mock 实例（对齐 tech-miniapp test/provision-cache.spec.ts 既有模式）。
vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false, API_BASE_URL: '' }
})

import { request } from '../src/utils/request'

// typecheck 下将 request 视作 vitest Mock，以便可用 mockReset/mockResolvedValue。
const requestMock = request as unknown as Mock

// ===== 契约同源夹具（字段名来自 shared-types 契约类型，非手写常量） =====

const patientRow: Patient = {
  patientId: 'P00001',
  name: '测试患者',
  gender: 'female',
  age: 14,
  diagnosis: null,
  cobbAngle: null,
  deviceId: null,
  teamId: null,
  doctorId: null,
  status: 'active',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const deviceRow: Device = {
  deviceId: 'D00001',
  model: 'PRS-ML05-RC',
  firmwareVersion: '1.0.0',
  patientId: null,
  wifiSsid: null,
  bindTime: null,
  status: 'unbound',
  lastReportAt: null,
}

const alertRow: Alert = {
  alertId: 'A00001',
  patientId: 'P00001',
  deviceId: 'D00001',
  type: 'pressure_high',
  detail: '压力过高',
  sensorPoint: 'R3C2',
  thresholdValue: 25,
  actualValue: 40,
  timestamp: '2026-01-01T00:00:00Z',
  readStatus: 'unread',
  processStatus: 'pending',
  resolvedStatus: 'active',
  resolvedAt: null,
  processedBy: null,
  processedAt: null,
  processNote: null,
}

const installRow: InstallRecord = {
  installId: 'INS001',
  deviceId: 'D00001',
  patientId: 'P00001',
  techId: 'TH0001',
  calibrateTime: '2026-01-01T00:00:00Z',
  baselineId: null,
  notes: '',
  signatureUrl: '',
  wifiStatus: 'connected',
}

const reviewRow: ReviewRecord = {
  reviewId: 'RV0001',
  patientId: 'P00001',
  reviewDate: '2026-01-01',
  reviewType: 'follow-up',
  findings: null,
  nextReviewDate: null,
  doctorId: null,
  reportFileId: null,
  reportFileName: null,
  reportContentType: null,
  reportSize: null,
  reportUploadedAt: null,
  reportDownloadUrl: null,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const patientPage: PaginatedResponse<Patient> = { list: [patientRow], total: 1, page: 1, pageSize: 10 }
const devicePage: PaginatedResponse<Device> = { list: [deviceRow], total: 1, page: 1, pageSize: 10 }
const alertPage: PaginatedResponse<Alert> = { list: [alertRow], total: 1, page: 1, pageSize: 10 }
const installPage: PaginatedResponse<InstallRecord> = { list: [installRow], total: 1, page: 1, pageSize: 10 }

describe('T144 契约漂移门禁（admin-web）', () => {
  beforeEach(() => {
    requestMock.mockReset()
  })

  it('fetchPatients 消费契约 PaginatedResponse.list 并返回非空', async () => {
    requestMock.mockResolvedValue(patientPage)
    const res = await fetchPatients({ page: 1, pageSize: 10 })
    // 若改为误读 res.records → undefined → 断言失败（报红）
    expect(res.list.length).toBeGreaterThanOrEqual(1)
    expect(res.list[0].patientId).toBe(patientRow.patientId)
  })

  it('fetchDevices 消费契约 PaginatedResponse.list 并返回非空', async () => {
    requestMock.mockResolvedValue(devicePage)
    const res = await fetchDevices({})
    expect(res.list.length).toBeGreaterThanOrEqual(1)
    expect(res.list[0].deviceId).toBe(deviceRow.deviceId)
  })

  it('fetchAlerts 消费契约 PaginatedResponse.list 并返回非空', async () => {
    requestMock.mockResolvedValue(alertPage)
    const res = await fetchAlerts({ page: 1, pageSize: 10 })
    expect(res.list.length).toBeGreaterThanOrEqual(1)
    expect(res.list[0].alertId).toBe(alertRow.alertId)
  })

  it('fetchInstallRecords 消费契约 PaginatedResponse.list 并返回非空', async () => {
    requestMock.mockResolvedValue(installPage)
    const res = await fetchInstallRecords({ page: 1, pageSize: 10 })
    expect(res.list.length).toBeGreaterThanOrEqual(1)
    expect(res.list[0].installId).toBe(installRow.installId)
  })

  it('fetchReviewRecords 消费契约 ReviewRecord[] 并返回非空', async () => {
    requestMock.mockResolvedValue([reviewRow])
    const res = await fetchReviewRecords('P00001')
    expect(res.length).toBeGreaterThanOrEqual(1)
    expect(res[0].reviewId).toBe(reviewRow.reviewId)
  })
})

// ===== T269 admin 缺陷真实模式守卫 =====
// 本文件恒以 USE_MOCK=false 运行（vi.mock utils/request），故能拦住「mock e2e 结构性看不见」的一类缺陷。
// 夹具刻意用后端 ID 命名空间（TEAM01 / D0001），而非 mock 表的 TEAM-001 / DOC-001。

/** request 只收单个 options 对象，取最近一次调用的入参 */
function lastRequest(): { url: string; method?: string; data?: Record<string, unknown> } {
  const calls = requestMock.mock.calls as unknown as [{ url: string; method?: string; data?: Record<string, unknown> }][]
  return calls[calls.length - 1][0]
}

const backendTeamRow: Team = { teamId: 'TEAM01', name: '脊柱矫形一组', memberCount: 3, patientCount: 12 }
const backendDoctorRow: Doctor = {
  doctorId: 'D0001', name: '李医师', title: '主治医师', department: '脊柱外科',
  teamId: 'TEAM01', phoneMasked: '138****0001', patientCount: 5, status: 'enabled',
}

/** 后端 SystemSettingsDTO 字段名（services/user-service/internal/model/model.go） */
const BACKEND_SETTINGS_KEYS = [
  'collectIntervalSeconds', 'dailyWearTargetHours', 'maxPatients', 'pressureFluctuationPct',
  'pressureHighThresholdN', 'retentionDays', 'sensorDriftN', 'wearInterruptMinutes', 'wifiPresets',
]

describe('T269 真实模式契约守卫', () => {
  it('D1 团队名/医生名取自后端字典，mock 查表不再串到真实模式', async () => {
    requestMock.mockResolvedValueOnce([backendTeamRow])
    await fetchTeams()
    requestMock.mockResolvedValueOnce([backendDoctorRow])
    await fetchDoctors()

    expect(teamNameOf('TEAM01')).toBe('脊柱矫形一组')
    expect(doctorNameOf('D0001')).toBe('李医师')
    // 字典查不到时回落原始编号（不得伪装成 mock 表里的名字）
    expect(teamNameOf('TEAM99')).toBe('TEAM99')
    expect(doctorNameOf('D9999')).toBe('D9999')
    expect(teamNameOf(null)).toBe('-')
  })

  it('D2 系统配置读写按后端 DTO 字段名透传', async () => {
    requestMock.mockResolvedValueOnce({
      dailyWearTargetHours: 22, pressureHighThresholdN: 45, pressureFluctuationPct: 30,
      wearInterruptMinutes: 60, sensorDriftN: 2.8, wifiPresets: [],
      collectIntervalSeconds: 1800, retentionDays: 365, maxPatients: 10000,
    })
    const settings = await fetchSystemSettings()
    // 页面表单直接消费该返回值 ⇒ 字段名错一个就显示写死默认值 + 保存 400
    expect(settings.collectIntervalSeconds).toBe(1800)
    expect(settings.retentionDays).toBe(365)

    await saveSystemSettingsApi(settings)
    const req = lastRequest()
    expect(req.method).toBe('PUT')
    expect(Object.keys(req.data ?? {}).sort()).toEqual(BACKEND_SETTINGS_KEYS)
    expect(req.data?.collectIntervalSeconds).toBe(1800)
  })

  it('D4 处理告警把备注随体提交', async () => {
    requestMock.mockResolvedValue(null)
    await processAlertApi('205', '已电话指导患者调整佩戴位置')
    const req = lastRequest()
    expect(req.url).toBe('/api/v1/alerts/205/process')
    expect(req.method).toBe('POST')
    expect(req.data).toEqual({ note: '已电话指导患者调整佩戴位置' })

    await processAlertApi('206')
    expect(lastRequest().data).toBeUndefined()
  })

  it('T300 异常报告汇总走 admin 端点，消费后端三视图键名', async () => {
    requestMock.mockResolvedValue({
      patientId: 'P00001', start: '2026-09-01', end: '2026-09-03', total: 2,
      byStatus: [{ key: 'pending', count: 2 }, { key: 'processing', count: 0 }, { key: 'processed', count: 0 }],
      byType: [{ key: 'pressure_high', count: 2 }],
      byDay: [{ key: '2026-09-02', count: 2 }],
    })
    const res = await fetchAbnormalReport({ patientId: 'P00001', start: '2026-09-01', end: '2026-09-03' })
    const req = lastRequest()
    expect(req.url).toBe('/api/v1/admin/abnormal-reports')
    expect(req.data).toEqual({ patientId: 'P00001', start: '2026-09-01', end: '2026-09-03' })
    // 抽屉直接渲染这三组 key/count ⇒ 任一名对不上就整表空白
    expect(res.byStatus[1]).toEqual({ key: 'processing', count: 0 })
    expect(res.byType[0].key).toBe('pressure_high')
    expect(res.byDay[0].count).toBe(2)
  })

  it('T300 CSV 导出带 token，失败时透出后端文案而不是落一个坏文件', async () => {
    localStorage.setItem('admin_token', 'TOK300')
    const createUrl = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:abnormal')
    const revokeUrl = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: { get: () => 'attachment; filename="abnormal-report-P00001-2026-09-01_2026-09-03.csv"' },
      blob: async () => new Blob(['\ufeff告警ID\n'], { type: 'text/csv' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await exportAbnormalReportApi({ patientId: 'P00001', start: '2026-09-01', end: '2026-09-03' })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/abnormal-reports/export?patientId=P00001&start=2026-09-01&end=2026-09-03')
    expect((fetchMock.mock.calls[0][1] as { headers: Record<string, string> }).headers.Authorization).toBe('Bearer TOK300')
    expect(createUrl).toHaveBeenCalledTimes(1)
    expect(revokeUrl).toHaveBeenCalledWith('blob:abnormal')

    // 4xx 时后端回 JSON 信封而非 CSV
    fetchMock.mockResolvedValue({
      ok: false,
      status: 403,
      headers: { get: () => null },
      json: async () => ({ code: 403, message: 'abnormal report is staff-only', data: null }),
    })
    await expect(exportAbnormalReportApi({ patientId: 'P00001', start: '2026-09-01', end: '2026-09-03' }))
      .rejects.toThrow('abnormal report is staff-only')
    expect(createUrl).toHaveBeenCalledTimes(1)

    vi.unstubAllGlobals()
    createUrl.mockRestore()
    revokeUrl.mockRestore()
    localStorage.removeItem('admin_token')
  })
})