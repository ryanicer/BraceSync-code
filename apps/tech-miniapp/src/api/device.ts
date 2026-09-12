import { request, USE_MOCK } from '../utils/request'

/**
 * 设备绑定（真实：POST /api/v1/devices/:deviceId/bind）
 * @returns { deviceId, status, swapped? } swapped=true 表示从其他患者换绑
 */
export async function bindDevice(
  deviceId: string,
  patientId: string
): Promise<{ deviceId: string; status: string; swapped?: boolean }> {
  if (USE_MOCK) {
    // T089-MOCK: 等后端 T084 对齐后切换真实
    await new Promise((r) => setTimeout(r, 400))
    return {
      deviceId,
      status: 'online',
      swapped: false,
    }
  }
  return request<{ deviceId: string; status: string; swapped?: boolean }>({
    url: `/api/v1/devices/${deviceId}/bind`,
    method: 'POST',
    data: { patientId },
  })
}

/**
 * 配网成功后回写 WiFi 状态（云端 devices.wifi_ssid + install_records.wifi_status 回填）
 * 真实：POST /api/v1/devices/:deviceId/wifi
 */
export async function setDeviceWifi(
  deviceId: string,
  ssid: string
): Promise<{ deviceId: string; wifiStatus: string }> {
  if (USE_MOCK) {
    // T089-MOCK: 等后端 wifi-writeback 接口就绪后切换
    await new Promise((r) => setTimeout(r, 300))
    return { deviceId, wifiStatus: 'connected' }
  }
  return request<{ deviceId: string; wifiStatus: string }>({
    url: `/api/v1/devices/${deviceId}/wifi`,
    method: 'POST',
    data: { ssid },
  })
}
