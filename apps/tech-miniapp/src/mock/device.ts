import type { Device } from '@bracesync/shared-types'

export function mockDevice(): Device {
  return {
    deviceId: 'PRS-ML05-RC-001',
    model: 'PRS-ML05-RC',
    firmwareVersion: 'v1.2.3',
    patientId: null,
    wifiSsid: null,
    bindTime: null,
    status: 'unbound',
    lastReportAt: null,
  }
}

export function mockBoundDevice(): Device {
  return {
    deviceId: 'PRS-ML05-RC-002',
    model: 'PRS-ML05-RC',
    firmwareVersion: 'v1.2.3',
    patientId: 'pat-001',
    wifiSsid: 'Home_WiFi_5G',
    bindTime: '2026-08-10T08:00:00Z',
    status: 'online',
    lastReportAt: new Date().toISOString(),
  }
}

/** 模拟设备扫描列表 */
export function mockScanResults() {
  return [
    { deviceId: 'PRS-ML05-RC-001', name: 'PRS-ML05-RC-001', RSSI: -45 },
    { deviceId: 'PRS-ML05-RC-003', name: 'PRS-ML05-RC-003', RSSI: -62 },
    { deviceId: 'PRS-ML05-RC-007', name: 'PRS-ML05-RC-007', RSSI: -78 },
  ]
}
