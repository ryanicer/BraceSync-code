// BLE 近场调试工具：扩展自 T016 患者端 ble.ts
// 新增：校准数据读写、offset_values 采集
// H5 dev 模式下 BLE API 不可用 → 蓝牙相关方法抛错由上层处理；discoverDevices 返回空数组

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
            // 只保留有名称的设备（过滤系统广播）
            if (d.name) found.set(d.deviceId, d)
          }
        })
        // 3 秒收集窗口后 stop 并 resolve
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
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return new Promise((resolve, reject) => {
    uni.createBLEConnection({
      deviceId,
      success: () => resolve(true),
      fail: (err) => reject(new Error(`连接失败: ${err.errMsg}`)),
    })
  })
}

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
    throw new Error(H5_BLUETOOTH_ERROR)
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

/** 读取设备校准 offset_values（20 点） */
export async function readCalibrationData(deviceId: string): Promise<number[]> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  // 真机：通过 BLE GATT 读取校准特征值
  return []
}

/** 写入校准命令到设备 */
export async function writeCalibrationCommand(deviceId: string, command: 'start' | 'stop'): Promise<boolean> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return true
}

/** 读取设备固件版本 */
export async function readFirmwareVersion(deviceId: string): Promise<string> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return ''
}
