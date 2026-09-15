/**
 * 设备 API（患者端）
 *
 * - POST /api/v1/devices/:deviceId/wifi     WiFi 配网成功回写（对齐技师端 setDeviceWifi）
 *
 * 注意：T192 冲突 C —— 后端设备契约中没有 ble_mac / ble_name 字段，
 * 患者端 BLE 标识只能来自运行时蓝牙扫描结果（utils/ble.ts startBleScan）。
 */

import { request } from '../utils/request'

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
