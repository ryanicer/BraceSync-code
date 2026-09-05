// BLE 近场调试工具：T089 扩展
// 新增：Realtime 推送 / WiFi 加密配置写入 / WiFi Status 状态机监听 / 设备信息读取
// R5：全链路日志（ble-log.ts）、B512 Notify 订阅、权限前置、蓝牙开关监听
// H5 dev 模式下 BLE API 不可用 → 蓝牙相关方法抛错由上层处理；discoverDevices 返回空数组；
// 实时推送 & 配网状态机在 H5 下使用模拟数据（mock），真机联调以硬件为准。

import { bleLog } from './ble-log'

// 检查是否在 H5 环境（BLE 不可用）
function isH5(): boolean {
  // #ifdef H5
  return true
  // #endif
  // #ifndef H5
  return false
  // #endif
}

const H5_BLUETOOTH_ERROR = '蓝牙功能仅支持真机使用，请在手机上操作'

// BLE GATT UUID（协议定稿 §1）
const SERVICE_UUID = '0000b510-0000-1000-8000-00805f9b34fb'
const CHAR_WIFI_CONFIG = '0000b511-0000-1000-8000-00805f9b34fb'
const CHAR_WIFI_STATUS = '0000b512-0000-1000-8000-00805f9b34fb'
const CHAR_REALTIME = '0000b513-0000-1000-8000-00805f9b34fb'
const CHAR_DEVICE_INFO = '0000b514-0000-1000-8000-00805f9b34fb'

// 模块级扫描定时器（供蓝牙开关监听回调清理）
let discoveryTimer: ReturnType<typeof setTimeout> | null = null
let adapterStateRegistered = false

function isAuthDeny(errMsg: string): boolean {
  const m = (errMsg || '').toLowerCase()
  return m.includes('auth') || m.includes('deny') || m.includes('permission')
}

/**
 * R5-2 权限前置：扫描前检查 scope.userLocation（Android BLE 必需）
 * iOS 无需位置权限，但仍走 getSetting 不影响。
 * 未授权则请求；拒绝则引导 openSetting。
 */
export async function ensureLocationPermission(): Promise<boolean> {
  if (isH5()) return true
  // #ifdef MP-WEIXIN
  return new Promise((resolve) => {
    try {
      wx.getSetting({
        success: (res) => {
          if (res.authSetting['scope.userLocation']) {
            resolve(true)
            return
          }
          wx.authorize({
            scope: 'scope.userLocation',
            success: () => resolve(true),
            fail: () => {
              bleLog.warn('位置权限授权被拒，引导 openSetting')
              uni.showModal({
                title: '权限未授权',
                content: '蓝牙/位置权限未授权，请在设置中开启后重试',
                confirmText: '去设置',
                success: (r) => {
                  if (r.confirm) {
                    wx.openSetting({
                      success: (s) => resolve(!!s.authSetting['scope.userLocation']),
                      fail: () => resolve(false),
                    })
                  } else {
                    resolve(false)
                  }
                },
                fail: () => resolve(false),
              })
            },
          })
        },
        fail: () => resolve(true), // getSetting 失败不阻断
      })
    } catch (e) {
      resolve(true)
    }
  })
  // #endif
  // #ifndef MP-WEIXIN
  return true
  // #endif
}

/** R5-3 注册蓝牙适配器状态变化监听（available=false 时提示并终止扫描） */
function registerAdapterStateListener() {
  if (adapterStateRegistered || isH5()) return
  adapterStateRegistered = true
  uni.onBluetoothAdapterStateChange((res) => {
    if (!res.available) {
      bleLog.warn('手机蓝牙已关闭，终止扫描')
      uni.showToast({ title: '手机蓝牙已关闭', icon: 'none' })
      if (discoveryTimer) {
        clearTimeout(discoveryTimer)
        discoveryTimer = null
      }
      uni.stopBluetoothDevicesDiscovery({ fail: () => {} })
    }
  })
}

export async function initBluetooth(): Promise<boolean> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return new Promise((resolve, reject) => {
    uni.openBluetoothAdapter({
      success: () => {
        bleLog.info('adapter 初始化成功')
        registerAdapterStateListener()
        resolve(true)
      },
      fail: (err) => {
        bleLog.error('adapter 初始化失败', err?.errMsg)
        if (isAuthDeny(err?.errMsg || '')) {
          ensureLocationPermission().then((ok) => {
            if (ok) resolve(true)
            else reject(new Error(`蓝牙初始化失败: ${err.errMsg}`))
          })
        } else {
          reject(new Error(`蓝牙初始化失败: ${err.errMsg}`))
        }
      },
    })
  })
}

export async function discoverDevices(): Promise<{ deviceId: string; name: string; RSSI: number }[]> {
  if (isH5()) {
    // H5 不造假设备，返回空数组由上层展示空态
    return []
  }
  // R5-2 权限前置
  const permOk = await ensureLocationPermission()
  if (!permOk) {
    throw new Error('蓝牙/位置权限未授权，请在设置中开启后重试')
  }
  return new Promise((resolve, reject) => {
    const found = new Map<string, { deviceId: string; name: string; RSSI: number }>()

    const cleanup = () => {
      if (discoveryTimer) {
        clearTimeout(discoveryTimer)
        discoveryTimer = null
      }
      uni.stopBluetoothDevicesDiscovery({ fail: () => {} })
      uni.offBluetoothDeviceFound()
    }

    // TODO: services: [SERVICE_UUID] 过滤待真机验证广播服务后再加
    uni.startBluetoothDevicesDiscovery({
      allowDuplicatesKey: false,
      success: () => {
        bleLog.info('扫描启动成功')
        uni.onBluetoothDeviceFound((res) => {
          const devs = res.devices as unknown as { deviceId: string; name: string; RSSI: number }[]
          bleLog.info(`onBluetoothDeviceFound 原始设备数=${devs.length}`, devs.slice(0, 10).map((d) => ({ name: d.name, deviceId: d.deviceId, RSSI: d.RSSI })))
          for (const dev of devs) {
            const d = dev
            // 协议 §1：广播名 BSYNC-{device_id 后 6 位}，仅保留 BraceSync 设备
            if (d.name && d.name.startsWith('BSYNC-')) found.set(d.deviceId, d)
          }
        })
        discoveryTimer = setTimeout(() => {
          cleanup()
          bleLog.info(`BSYNC- 过滤后设备数=${found.size}`)
          resolve(Array.from(found.values()))
        }, 3000)
      },
      fail: (err) => {
        bleLog.error('扫描启动失败', err?.errMsg)
        cleanup()
        reject(new Error(`设备扫描失败: ${err.errMsg}`))
      },
    })
  })
}

export async function createBLEConnection(deviceId: string): Promise<boolean> {
  if (isH5()) {
    // H5 下 BLE 不可用，返回 false 让上层走"连接失败但不阻断"分支
    return false
  }
  return new Promise((resolve, reject) => {
    uni.createBLEConnection({
      deviceId,
      success: () => {
        bleLog.info(`连接成功 deviceId=${deviceId}`)
        resolve(true)
      },
      fail: (err) => {
        bleLog.error(`连接失败 deviceId=${deviceId}`, err?.errMsg)
        reject(new Error(`连接失败: ${err.errMsg}`))
      },
    })
  })
}

// T089-DEPRECATED: 旧明文版 writeWiFiConfig，联调稳定后清理（用 writeWifiConfigV2 替代）
export async function writeWiFiConfig(ssid: string, password: string): Promise<boolean> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  const payload = JSON.stringify({ ssid, password })
  const buffer = new TextEncoder().encode(payload)
  return new Promise((resolve) => {
    resolve(Boolean(buffer.byteLength > 0))
  })
}

export async function closeBLEConnection(deviceId: string): Promise<void> {
  if (isH5()) {
    return
  }
  return new Promise((resolve) => {
    uni.closeBLEConnection({
      deviceId,
      success: () => resolve(),
      fail: () => resolve(),
    })
  })
}

// ===== 技师端扩展：校准 & 基线 =====

// T089-DEPRECATED: 用 startRealtimePressure + onRealtimeFrame 替代
export async function readCalibrationData(deviceId: string): Promise<number[]> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return []
}

// T089-DEPRECATED: 用 startRealtimePressure/stopRealtimePressure 封装
export async function writeCalibrationCommand(deviceId: string, command: 'start' | 'stop'): Promise<boolean> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return true
}

export async function readFirmwareVersion(deviceId: string): Promise<string> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return ''
}

// ===== T089 新增：实时压力推送 =====

let realtimeTimer: ReturnType<typeof setInterval> | null = null
let realtimeCallback: ((frame: number[]) => void) | null = null

/** 启动 BLE 实时压力推送（1Hz，20 点 uint16 小端，值 = N×100） */
export async function startRealtimePressure(deviceId: string): Promise<void> {
  if (isH5()) {
    // H5 mock：每秒推送 20 个接近 0 的随机小数（模拟空载）
    realtimeTimer = setInterval(() => {
      const frame = Array.from({ length: 20 }, () => Math.random() * 0.3 - 0.15)
      realtimeCallback?.(frame)
    }, 1000)
    return
  }
  // 真机：T089-HW-TODO 待硬件 UUID 确认后，向 Realtime Char（0x0000B513）Write 0x01 启动 Notify
  await new Promise((r) => setTimeout(r, 100))
}

/** 停止 BLE 实时压力推送 */
export async function stopRealtimePressure(deviceId: string): Promise<void> {
  if (realtimeTimer) {
    clearInterval(realtimeTimer)
    realtimeTimer = null
  }
  if (isH5()) return
  // 真机：向 Realtime Char Write 0x00 停止 Notify
}

/** 注册实时帧回调 */
export function onRealtimeFrame(cb: (frame: number[]) => void): void {
  realtimeCallback = cb
  if (!isH5()) {
    // 真机：监听 Realtime Char Notify，解析 20×uint16 小端 → number[20]（÷100 转 N）
    // T089-HW-TODO: 解析逻辑待真机联调
  }
}

// ===== T089 新增：WiFi 加密配置写入 + 状态机 =====

let wifiStatusTimer: ReturnType<typeof setInterval> | null = null
let wifiStatusCallback: ((code: number) => void) | null = null
let b512NotifyRegistered = false

/**
 * 写入加密后的 WiFi 配置（AES-128-CTR，由 aes-ctr.ts 加密后传入密文 hex）
 * 协议 §4：密文按每片 ≤180 字节切片，对 B511 特征顺序 Write，无分包序号字节。
 * 写完即完成发送（固件侧 400ms 静默判包）。
 */
const WIFI_CHUNK_SIZE = 180

function hexToBytesLocal(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2)
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.substr(i * 2, 2), 16)
  }
  return bytes
}

export async function writeWifiConfigV2(
  deviceId: string,
  encryptedHex: string
): Promise<boolean> {
  if (isH5()) {
    // H5 mock：直接成功，启动状态机模拟
    return true
  }
  const cipher = hexToBytesLocal(encryptedHex)
  const totalChunks = Math.ceil(cipher.length / WIFI_CHUNK_SIZE)
  bleLog.info(`B511 写入开始：密文长=${cipher.length}B，分片数=${totalChunks}`)
  for (let off = 0, idx = 0; off < cipher.length; off += WIFI_CHUNK_SIZE, idx++) {
    const chunk = cipher.slice(off, off + WIFI_CHUNK_SIZE)
    try {
      await new Promise<void>((resolve, reject) => {
        uni.writeBLECharacteristicValue({
          deviceId,
          serviceId: SERVICE_UUID,
          characteristicId: CHAR_WIFI_CONFIG,
          value: chunk.buffer,
          success: () => resolve(),
          fail: (err) => reject(new Error(`B511 写入失败: ${err.errMsg}`)),
        })
      })
      bleLog.info(`B511 分片 ${idx + 1}/${totalChunks} 写入成功 len=${chunk.length}`)
    } catch (e) {
      bleLog.error(`B511 分片 ${idx + 1}/${totalChunks} 写入失败`, e instanceof Error ? e.message : String(e))
      throw e
    }
  }
  bleLog.info('B511 全部分片写入完成')
  return true
}

/**
 * 注册配网状态机回调
 * 状态码：0收到 1连AP 2取IP 3探测 9成功 -1密码错 -2 SSID不见 -3 DHCP失败 -4云端不可达
 * R5：真机订阅 B512 Notify，记录每次状态原始 int8 值。
 */
let wifiStatusDeviceId = ''

export function onWifiStatus(cb: (code: number) => void): void {
  wifiStatusCallback = cb
  if (isH5()) return
  if (b512NotifyRegistered) return
  if (!wifiStatusDeviceId) return
  b512NotifyRegistered = true
  // 开启 B512 通知
  uni.notifyBLECharacteristicValueChange({
    deviceId: wifiStatusDeviceId,
    serviceId: SERVICE_UUID,
    characteristicId: CHAR_WIFI_STATUS,
    success: () => bleLog.info('B512 Notify 订阅成功'),
    fail: (err) => bleLog.error('B512 Notify 订阅失败', err?.errMsg),
  })
  uni.onBLECharacteristicValueChange((res) => {
    if (res.characteristicId?.toLowerCase() !== CHAR_WIFI_STATUS.toLowerCase()) return
    try {
      const code = new Int8Array(res.value)[0]
      bleLog.info(`B512 状态原始值=${code}`)
      wifiStatusCallback?.(code)
    } catch (e) {
      bleLog.error('B512 状态解析失败', e instanceof Error ? e.message : String(e))
    }
  })
}


/** H5 mock：模拟配网状态机推进（0→1→2→3→9） */
export function startMockWifiStatusSequence(): void {
  if (!isH5()) return
  const seq = [0, 1, 2, 3, 9]
  let idx = 0
  if (wifiStatusTimer) clearInterval(wifiStatusTimer)
  wifiStatusTimer = setInterval(() => {
    if (idx < seq.length) {
      wifiStatusCallback?.(seq[idx])
      idx++
    } else {
      if (wifiStatusTimer) clearInterval(wifiStatusTimer)
    }
  }, 500)
}

/** 停止 mock 状态机 */
export function stopMockWifiStatusSequence(): void {
  if (wifiStatusTimer) {
    clearInterval(wifiStatusTimer)
    wifiStatusTimer = null
  }
}

// ===== T089 新增：设备信息 =====

/**
 * 读取设备信息（协议 §5：B514 Read 一次返回 UTF-8 JSON，snake_case）
 * {"device_id":"...","firmware":"...","battery":100}
 * 字段容错：缺 battery 默认 100；解析失败返回 null，不阻塞流程。
 * R5：记录原始文本（截断 200 字符）与解析结果。
 */
export async function readDeviceInfo(deviceId: string): Promise<{
  deviceId: string
  firmware: string
  battery: number
} | null> {
  if (isH5()) {
    return { deviceId, firmware: 'v1.2.3', battery: 85 }
  }
  return new Promise((resolve) => {
    let settled = false
    const finish = (val: any) => {
      if (settled) return
      settled = true
      uni.offBLECharacteristicValueChange(handler)
      resolve(val)
    }
    const handler = (res: any) => {
      try {
        const text = new TextDecoder().decode(new Uint8Array(res.value))
        const truncated = text.length > 200 ? text.slice(0, 200) + '...(truncated)' : text
        bleLog.info(`B514 原始文本=${truncated}`)
        const data = JSON.parse(text)
        const result = {
          deviceId: data.device_id || deviceId,
          firmware: data.firmware || '',
          battery: typeof data.battery === 'number' ? data.battery : 100,
        }
        bleLog.info(`B514 解析成功`, { firmware: result.firmware, battery: result.battery })
        finish(result)
      } catch (e) {
        bleLog.error('B514 解析失败', e instanceof Error ? e.message : String(e))
        finish(null)
      }
    }
    uni.onBLECharacteristicValueChange(handler)
    uni.readBLECharacteristicValue({
      deviceId,
      serviceId: SERVICE_UUID,
      characteristicId: CHAR_DEVICE_INFO,
      fail: (err) => {
        bleLog.error('B514 读取失败', err?.errMsg)
        finish(null)
      },
    })
    // 3s 超时兜底
    setTimeout(() => {
      bleLog.warn('B514 读取超时')
      finish(null)
    }, 3000)
  })
}