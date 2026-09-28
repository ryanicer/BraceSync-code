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

/**
 * 「设备 WiFi 已清除」的留痕上报（T448，Boss 裁定甲案：只加审计，不扩枚举）
 *
 * 清除动作本身是纯 BLE（utils/ble.ts 向 B513 写 0x02、等 B512 notify=0），
 * 按 T446 §二 的定性它不该有专用端点，所以这里复用配网回写的同一条路由：
 * body 只带 cleared=true（后端与 ssid 互斥），后端据此写一行 audit_logs，
 * 不改 devices.wifi_ssid、不改 install_records.wifi_status。
 *
 * 🔴 调用方不要 await 它来卡住交付流程：留痕失败只记日志（见 pages/wifi-config/index.vue）。
 */
export async function reportWifiCleared(deviceId: string): Promise<void> {
  if (USE_MOCK) {
    // T089-MOCK: 与 setDeviceWifi 同形，mock 下不发请求
    await new Promise((r) => setTimeout(r, 200))
    return
  }
  await request<null>({
    url: `/api/v1/devices/${deviceId}/wifi`,
    method: 'POST',
    data: { cleared: true },
  })
}
