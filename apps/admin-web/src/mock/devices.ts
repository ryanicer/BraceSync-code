// 设备域 mock 数据（对齐 api-contracts.ts getDevices）
import type { Device } from '@bracesync/shared-types'

const DEVICES: Device[] = [
  { deviceId: 'DEV-A3F312', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.2', patientId: 'PT-001', wifiSsid: 'Hospital-WiFi', bindTime: '2026-03-12T10:00:00+08:00', status: 'online', lastReportAt: '2026-08-11T14:30:00+08:00' },
  { deviceId: 'DEV-B7E456', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.2', patientId: 'PT-002', wifiSsid: 'Home-5G', bindTime: '2026-04-02T14:00:00+08:00', status: 'online', lastReportAt: '2026-08-11T14:28:00+08:00' },
  { deviceId: 'DEV-C9D789', model: 'PRS-ML05-RC', firmwareVersion: 'v1.3.9', patientId: 'PT-003', wifiSsid: 'Home-WiFi', bindTime: '2026-05-18T15:30:00+08:00', status: 'abnormal', lastReportAt: '2026-08-11T09:10:00+08:00' },
  { deviceId: 'DEV-D2A012', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.2', patientId: 'PT-004', wifiSsid: 'Home-WiFi-2', bindTime: '2026-06-01T10:30:00+08:00', status: 'offline', lastReportAt: '2026-08-10T22:15:00+08:00' },
  { deviceId: 'DEV-E5B347', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.0', patientId: 'PT-006', wifiSsid: 'School-Net', bindTime: '2026-07-15T16:00:00+08:00', status: 'online', lastReportAt: '2026-08-11T14:25:00+08:00' },
  { deviceId: 'DEV-F8C590', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.2', patientId: null, wifiSsid: null, bindTime: null, status: 'unbound', lastReportAt: null },
]

export function mockDevices(params: { keyword?: string }): { list: Device[]; total: number; page: number; pageSize: number } {
  let list = DEVICES.map((d) => ({ ...d }))
  if (params.keyword) {
    const kw = params.keyword.toLowerCase()
    list = list.filter((d) => d.deviceId.toLowerCase().includes(kw) || (d.patientId ?? '').toLowerCase().includes(kw))
  }
  return { list, total: list.length, page: 1, pageSize: list.length }
}

export function mockPatientName(patientId: string | null): string {
  if (!patientId) return '-'
  const names: Record<string, string> = {
    'PT-001': '林小雨', 'PT-002': '陈子航', 'PT-003': '王梓萌',
    'PT-004': '刘俊熙', 'PT-005': '赵欣然', 'PT-006': '孙浩然',
  }
  return names[patientId] ?? patientId
}

// T268: 设备详情 / 绑定历史 / 注册（语义对齐 device-service：register 幂等、
// deviceId 4-48 位 alnum/-/_；bindings 含历史解绑记录）
export interface DeviceBindingRecord {
  bindingId: string
  deviceId: string
  patientId: string
  bindAt: string
  unbindAt: string | null
  reason: string | null
  operatorId: string | null
}

const BINDINGS: DeviceBindingRecord[] = [
  { bindingId: 'B-001', deviceId: 'DEV-A3F312', patientId: 'PT-001', bindAt: '2026-03-12T10:00:00+08:00', unbindAt: null, reason: null, operatorId: 'A0001' },
  { bindingId: 'B-002', deviceId: 'DEV-C9D789', patientId: 'PT-003', bindAt: '2026-05-18T15:30:00+08:00', unbindAt: '2026-06-01T09:00:00+08:00', reason: 'rebind', operatorId: 'A0001' },
  { bindingId: 'B-003', deviceId: 'DEV-C9D789', patientId: 'PT-003', bindAt: '2026-06-01T09:30:00+08:00', unbindAt: null, reason: null, operatorId: 'A0001' },
]

export function mockDeviceDetail(deviceId: string): Device {
  const dev = DEVICES.find((d) => d.deviceId === deviceId)
  if (!dev) throw new Error(`设备不存在: ${deviceId}`)
  return { ...dev }
}

export function mockDeviceBindings(deviceId: string): DeviceBindingRecord[] {
  return BINDINGS.filter((b) => b.deviceId === deviceId).map((b) => ({ ...b }))
}

const DEVICE_ID_RE = /^[A-Za-z0-9_-]{4,48}$/

export function mockRegisterDevice(input: { deviceId: string; model?: string }): Device {
  const deviceId = input.deviceId.trim()
  if (!DEVICE_ID_RE.test(deviceId)) {
    throw new Error(`无效设备ID: ${deviceId}（4-48 位，字母/数字/-/_）`)
  }
  const existing = DEVICES.find((d) => d.deviceId === deviceId)
  if (existing) return { ...existing }
  const dev: Device = {
    deviceId,
    model: 'PRS-ML05-RC',
    firmwareVersion: '',
    patientId: null,
    wifiSsid: null,
    bindTime: null,
    status: 'unbound',
    lastReportAt: null,
  }
  DEVICES.push(dev)
  return { ...dev }
}
