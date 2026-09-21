import { request, USE_MOCK } from '../utils/request'

/** 绑定/换绑响应（后端 BindResponseDTO） */
export interface BindResult {
  deviceId: string
  status: string
  /** true=本次关闭过既有绑定：设备从其他患者换来，或 T299 患者级确认换绑 */
  swapped?: boolean
  /** T299 确认换绑时被解除绑定的该患者原设备号；未发生则不返回该字段 */
  patientSwappedFrom?: string
}

/**
 * 设备绑定（真实：POST /api/v1/devices/:deviceId/bind）
 * @param confirmSwap T299 换绑意图：目标患者已绑定其它设备时，false → 后端 409/20409（不静默改绑），
 *                    true → 后端同事务先解旧设备再绑本机。技师在确认框点「换绑」后置 true 重试。
 * @returns { deviceId, status, swapped?, patientSwappedFrom? }
 */
export async function bindDevice(
  deviceId: string,
  patientId: string,
  confirmSwap = false
): Promise<BindResult> {
  if (USE_MOCK) {
    // T089-MOCK: 等后端 T084 对齐后切换真实
    await new Promise((r) => setTimeout(r, 400))
    return {
      deviceId,
      status: 'online',
      swapped: false,
    }
  }
  return request<BindResult>({
    url: `/api/v1/devices/${deviceId}/bind`,
    method: 'POST',
    data: { patientId, confirmSwap },
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
