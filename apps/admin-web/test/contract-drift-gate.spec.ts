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
} from '@bracesync/shared-types'
import {
  fetchPatients,
  fetchDevices,
  fetchAlerts,
  fetchInstallRecords,
  fetchReviewRecords,
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