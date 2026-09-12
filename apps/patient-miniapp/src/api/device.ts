/**
 * 设备 API（T094b）
 *
 * - GET  /api/v1/devices                    患者设备列表
 * - POST /api/v1/devices/:deviceId/wifi-status  WiFi 配网状态上报
 */

import { request } from '../utils/request'

export interface PatientDevice {
  id: string
  name: string
  ble_name: string
  ble_mac: string
  wifi_status: 'unconfigured' | 'configuring' | 'connected' | 'failed'
  last_online_at?: string
}

export async function listPatientDevices(): Promise<PatientDevice[]> {
  return request<PatientDevice[]>({
    url: '/api/v1/devices',
    method: 'GET',
  })
}

export interface WifiStatusReport {
  deviceId: string
  status: 'success' | 'failed'
  ssid?: string
  error_code?: string
  error_message?: string
}

export async function reportWifiStatus(report: WifiStatusReport): Promise<void> {
  await request<void>({
    url: `/api/v1/devices/${report.deviceId}/wifi-status`,
    method: 'POST',
    data: {
      status: report.status,
      ssid: report.ssid,
      error_code: report.error_code,
      error_message: report.error_message,
    },
  })
}
