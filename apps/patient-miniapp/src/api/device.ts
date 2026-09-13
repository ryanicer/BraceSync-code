/**
 * 设备 API（T182 对齐技师端）
 *
 * - GET  /api/v1/devices                    患者设备列表
 * - POST /api/v1/devices/:deviceId/wifi     WiFi 配网成功回写（对齐技师端 setDeviceWifi）
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

/**
 * 配网成功后回写 WiFi 状态（对齐技师端 setDeviceWifi）
 * POST /api/v1/devices/:deviceId/wifi，数据 { ssid }
 */
export async function reportDeviceWifi(
  deviceId: string,
  ssid: string
): Promise<{ deviceId: string; wifiStatus: string }> {
  return request<{ deviceId: string; wifiStatus: string }>({
    url: `/api/v1/devices/${deviceId}/wifi`,
    method: 'POST',
    data: { ssid },
  })
}
