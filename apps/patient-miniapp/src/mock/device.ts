import type { Device } from '@bracesync/shared-types'

export function mockDevice(): Device {
  return {
    deviceId: 'PRS-ML05-RC-001',
    model: 'PRS-ML05-RC',
    firmwareVersion: 'v1.2.3',
    patientId: 'pat-001',
    wifiSsid: '2.4G-Network',
    bindTime: '2026-07-01T08:00:00Z',
    status: 'online',
    lastReportAt: new Date().toISOString(),
  }
}