// BLE 近场调试工具：T089 扩展
// 新增：Realtime 推送 / WiFi 加密配置写入 / WiFi Status 状态机监听 / 设备信息读取
// H5 dev 模式下 BLE API 不可用 → 蓝牙相关方法抛错由上层处理；discoverDevices 返回空数组；
// 实时推送 & 配网状态机在 H5 下使用模拟数据（mock），真机联调以硬件为准。

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

export async function initBluetooth(): Promise<boolean> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return new Promise((resolve, reject) => {
    uni.openBluetoothAdapter({
      success: () => resolve(true),
      fail: (err) => reject(new Error(`蓝牙初始化失败: ${err.errMsg}`)),
    })
  })
}

export async function discoverDevices(): Promise<{ deviceId: string; name: string; RSSI: number }[]> {
  if (isH5()) {
    // H5 不造假设备，返回空数组由上层展示空态
    return []
  }
  return new Promise((resolve, reject) => {
    const found = new Map<string, { deviceId: string; name: string; RSSI: number }>()
    let timer: ReturnType<typeof setTimeout> | null = null

    const cleanup = () => {
      if (timer) clearTimeout(timer)
      timer = null
      uni.stopBluetoothDevicesDiscovery({ fail: () => {} })
      uni.offBluetoothDeviceFound()
    }

    uni.startBluetoothDevicesDiscovery({
      allowDuplicatesKey: false,
      success: () => {
        uni.onBluetoothDeviceFound((res) => {
          for (const dev of res.devices) {
            const d = dev as unknown as { deviceId: string; name: string; RSSI: number }
            if (d.name) found.set(d.deviceId, d)
          }
        })
        timer = setTimeout(() => {
          cleanup()
          resolve(Array.from(found.values()))
        }, 3000)
      },
      fail: (err) => {
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
      success: () => resolve(true),
      fail: (err) => reject(new Error(`连接失败: ${err.errMsg}`)),
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

/**
 * 写入加密后的 WiFi 配置（AES-128-CTR，由 aes-ctr.ts 加密后传入密文 hex）
 */
export async function writeWifiConfigV2(
  deviceId: string,
  encryptedHex: string
): Promise<boolean> {
  if (isH5()) {
    // H5 mock：直接成功，启动状态机模拟
    return true
  }
  // 真机：T089-HW-TODO 向 WiFi Config Char（0x0000B511）Write 密文分包
  return true
}

/**
 * 注册配网状态机回调
 * 状态码：0收到 1连AP 2取IP 3探测 9成功 -1密码错 -2 SSID不见 -3 DHCP失败 -4云端不可达
 */
export function onWifiStatus(cb: (code: number) => void): void {
  wifiStatusCallback = cb
  if (!isH5()) {
    // 真机：T089-HW-TODO 监听 WiFi Status Char（0x0000B512）Notify
  }
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

/** 读取设备信息（device_id / firmware / battery） */
export async function readDeviceInfo(deviceId: string): Promise<{
  deviceId: string
  firmware: string
  battery: number
} | null> {
  if (isH5()) {
    return { deviceId, firmware: 'v1.2.3', battery: 85 }
  }
  // 真机：T089-HW-TODO 读取 Device Info Char（0x0000B514）
  return null
}
